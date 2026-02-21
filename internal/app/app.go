package app

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/service"
	store "film-heatmap/internal/store/sqlite"
)

func Run(args []string) error {
	deps, err := newDeps()
	if err != nil {
		return err
	}
	defer deps.close()
	return deps.run(args)
}

func RunAuto(args []string) error {
	deps, err := newDeps()
	if err != nil {
		return err
	}
	defer deps.close()

	if len(args) > 0 {
		return deps.run(args)
	}

	ctx := context.Background()
	if err := deps.autoSyncIfConfigured(ctx); err != nil {
		fmt.Println("auto-sync skipped:", err)
	}
	rows, err := deps.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if err := onboardingImport(deps.csvSvc, deps.syncSvc); err != nil {
			return err
		}
	}

	return deps.runUI(ctx)
}

type dependencies struct {
	store   *store.Store
	logs    *service.LogService
	heat    *service.HeatmapService
	stats   *service.StatsService
	csvSvc  *service.CSVService
	lib     *service.LibraryService
	cfgSvc  *service.AppConfigService
	syncSvc *service.LetterboxdSyncService
}

func (d *dependencies) close() {
	_ = d.store.Close()
}

func newDeps() (*dependencies, error) {
	dbPath := os.Getenv("FILM_HEATMAP_DB")
	if dbPath == "" {
		dbPath = "film-heatmap.db"
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	logs := service.NewLogService(st)
	csvSvc := service.NewCSVService(logs)
	cfgSvc, err := service.NewAppConfigService()
	if err != nil {
		return nil, err
	}
	downloader, err := service.NewLetterboxdDownloader()
	if err != nil {
		return nil, err
	}
	return &dependencies{
		store:   st,
		logs:    logs,
		heat:    service.NewHeatmapService(st),
		stats:   service.NewStatsService(st),
		csvSvc:  csvSvc,
		lib:     service.NewLibraryService(st),
		cfgSvc:  cfgSvc,
		syncSvc: service.NewLetterboxdSyncService(csvSvc, cfgSvc, downloader),
	}, nil
}

func (d *dependencies) autoSyncIfConfigured(ctx context.Context) error {
	enabled, err := d.syncSvc.Enabled()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	status, err := d.syncSvc.CredentialStatus()
	if err != nil {
		return err
	}
	if status == "not_logged_in" {
		return errors.New("browser auth unavailable")
	}
	if status == "expired" {
		return errors.New("browser auth expired")
	}
	res, ran, err := d.syncSvc.SyncAndImportIfDue(ctx)
	if err != nil {
		return err
	}
	if !ran {
		return nil
	}
	fmt.Printf("auto-import complete: imported=%d skipped=%d\n", res.ImportResult.Imported, res.ImportResult.Skipped)
	return nil
}

func (d *dependencies) run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	ctx := context.Background()
	switch args[0] {
	case "help":
		if len(args) == 1 {
			printUsage()
			return nil
		}
		switch strings.ToLower(args[1]) {
		case "backend":
			printBackendHelp()
			return nil
		case "features":
			printFeatureHelp()
			return nil
		default:
			return errors.New("usage: help [backend|features]")
		}
	case "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		title := fs.String("title", "", "film title")
		date := fs.String("date", "", "date YYYY-MM-DD or RFC3339")
		ratingStr := fs.String("rating", "", "0.5..5.0 step 0.5")
		notes := fs.String("notes", "", "optional notes")
		rewatch := fs.Bool("rewatch", false, "rewatch flag")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		loggedAt, err := parseDateFlag(*date)
		if err != nil {
			return err
		}
		rating, err := parseOptionalRating(*ratingStr)
		if err != nil {
			return err
		}
		var n *string
		if strings.TrimSpace(*notes) != "" {
			n = notes
		}
		log, err := d.logs.Add(ctx, domain.AddLogInput{Title: *title, LoggedAt: loggedAt, Rating: rating, Rewatch: *rewatch, Notes: n})
		if err != nil {
			return err
		}
		fmt.Printf("added log %s | %s | %s\n", log.ID, log.LocalDate, log.Title)
		return nil
	case "list":
		fs := flag.NewFlagSet("list", flag.ContinueOnError)
		from := fs.String("from", "", "from date")
		to := fs.String("to", "", "to date")
		title := fs.String("title", "", "title contains")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		f, err := parseOptionalDay(*from)
		if err != nil {
			return err
		}
		t, err := parseOptionalDayEnd(*to)
		if err != nil {
			return err
		}
		rows, err := d.logs.List(ctx, domain.ListFilter{From: f, To: t, Title: *title})
		if err != nil {
			return err
		}
		for _, r := range rows {
			rating := "-"
			if r.Rating != nil {
				rating = fmt.Sprintf("%.1f", *r.Rating)
			}
			fmt.Printf("%s | %s | %s | rating:%s | rewatch:%t\n", r.ID, r.LocalDate, r.Title, rating, r.Rewatch)
		}
		fmt.Printf("total: %d\n", len(rows))
		return nil
	case "edit":
		fs := flag.NewFlagSet("edit", flag.ContinueOnError)
		id := fs.String("id", "", "log id")
		title := fs.String("title", "", "title")
		date := fs.String("date", "", "date")
		ratingStr := fs.String("rating", "", "rating")
		notes := fs.String("notes", "", "notes")
		rewatchStr := fs.String("rewatch", "", "true|false")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return errors.New("id is required")
		}
		input := domain.UpdateLogInput{ID: *id}
		if strings.TrimSpace(*title) != "" {
			input.Title = title
		}
		if strings.TrimSpace(*date) != "" {
			t, err := parseDateFlag(*date)
			if err != nil {
				return err
			}
			input.LoggedAt = &t
		}
		if strings.TrimSpace(*ratingStr) != "" {
			r, err := parseOptionalRating(*ratingStr)
			if err != nil {
				return err
			}
			input.Rating = r
		}
		if strings.TrimSpace(*notes) != "" {
			input.Notes = notes
		}
		if strings.TrimSpace(*rewatchStr) != "" {
			v, err := strconv.ParseBool(*rewatchStr)
			if err != nil {
				return err
			}
			input.Rewatch = &v
		}
		log, err := d.logs.Edit(ctx, input)
		if err != nil {
			return err
		}
		fmt.Printf("updated %s | %s | %s\n", log.ID, log.LocalDate, log.Title)
		return nil
	case "delete":
		fs := flag.NewFlagSet("delete", flag.ContinueOnError)
		id := fs.String("id", "", "log id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return errors.New("id is required")
		}
		if err := d.logs.Delete(ctx, *id); err != nil {
			return err
		}
		fmt.Println("deleted", *id)
		return nil
	case "dedupe":
		removed, err := d.logs.Dedupe(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("dedupe complete: removed=%d\n", removed)
		return nil
	case "heatmap":
		if err := d.autoSyncIfConfigured(ctx); err != nil {
			fmt.Println("auto-sync skipped:", err)
		}
		fs := flag.NewFlagSet("heatmap", flag.ContinueOnError)
		year := fs.Int("year", 0, "year")
		recentWeeks := fs.Int("recent-weeks", 53, "show rolling recent weeks ending now")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var hm domain.HeatmapMatrix
		var err error
		if *year > 0 {
			hm, err = d.heat.Year(ctx, *year)
		} else {
			hm, err = d.heat.RecentWeeks(ctx, *recentWeeks, time.Now())
		}
		if err != nil {
			return err
		}
		printHeatmap(hm)
		return nil
	case "ui":
		return d.runUI(ctx)
	case "stats":
		fs := flag.NewFlagSet("stats", flag.ContinueOnError)
		year := fs.Int("year", time.Now().Year(), "year")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		stt, err := d.stats.Year(ctx, *year)
		if err != nil {
			return err
		}
		fmt.Printf("year: %d\n", stt.Year)
		fmt.Printf("total_logs: %d\n", stt.TotalLogs)
		fmt.Printf("active_days: %d\n", stt.ActiveDays)
		fmt.Printf("longest_streak: %d\n", stt.LongestStreak)
		fmt.Printf("current_streak: %d\n", stt.CurrentStreak)
		return nil
	case "auth":
		if len(args) < 2 {
			return errors.New("usage: auth <login|logout|status> [--enable-auto-sync]")
		}
		switch args[1] {
		case "login":
			fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
			enableAutoSync := fs.Bool("enable-auto-sync", true, "enable automatic Letterboxd sync on launch")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			r := bufio.NewReader(os.Stdin)
			for {
				fmt.Println("Opening browser to Letterboxd sign-in...")
				if err := d.syncSvc.ValidateCredentials(context.Background(), service.LetterboxdCredentials{}); err != nil {
					return err
				}
				fmt.Println("After signing in successfully in your browser, press Enter to continue.")
				_, _ = r.ReadString('\n')
				fmt.Print("Did login succeed in browser? [y/N]: ")
				ans, _ := r.ReadString('\n')
				ans = strings.ToLower(strings.TrimSpace(ans))
				if ans == "y" || ans == "yes" {
					break
				}
			}
			if err := d.syncSvc.ActivateBrowserSession(time.Hour); err != nil {
				return err
			}
			if err := d.syncSvc.SetEnabled(*enableAutoSync); err != nil {
				return err
			}
			fmt.Println("browser auth configured")
			fmt.Printf("auto-sync enabled: %t\n", *enableAutoSync)
			return nil
		case "logout":
			if err := d.syncSvc.InvalidateBrowserSession(); err != nil {
				return err
			}
			fmt.Println("logged out from app session (browser auth disabled)")
			return nil
		case "status":
			status, err := d.syncSvc.CredentialStatus()
			if err != nil {
				return err
			}
			enabled, err := d.syncSvc.Enabled()
			if err != nil {
				return err
			}
			expiresAt, err := d.syncSvc.AuthExpiresAt()
			if err != nil {
				return err
			}
			cooldownUntil, err := d.syncSvc.SyncCooldownUntil()
			if err != nil {
				return err
			}
			fmt.Println("auto-sync enabled:", enabled)
			switch status {
			case "not_logged_in":
				fmt.Println("browser auth: not configured")
			case "expired":
				fmt.Println("browser auth: expired")
			default:
				fmt.Println("browser auth:", status)
			}
			if expiresAt != nil {
				fmt.Println("auth expires at:", expiresAt.In(time.Local).Format(time.RFC3339))
			}
			if cooldownUntil != nil && cooldownUntil.After(time.Now()) {
				fmt.Println("next auto-sync after:", cooldownUntil.In(time.Local).Format(time.RFC3339))
			}
			return nil
		default:
			return errors.New("usage: auth <login|logout|status>")
		}
	case "refresh":
		res, err := d.syncSvc.SyncAndImport(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("refresh complete: imported=%d skipped=%d\n", res.ImportResult.Imported, res.ImportResult.Skipped)
		return nil
	case "import":
		if len(args) < 2 {
			return errors.New("usage: import <csv|letterboxd> [--file <path> | --auto]")
		}
		switch args[1] {
		case "csv":
			fs := flag.NewFlagSet("import csv", flag.ContinueOnError)
			file := fs.String("file", "", "path to csv")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *file == "" {
				return errors.New("--file is required")
			}
			res, err := d.csvSvc.Import(ctx, *file)
			if err != nil {
				return err
			}
			fmt.Printf("imported: %d\n", res.Imported)
			fmt.Printf("skipped: %d\n", res.Skipped)
			for _, e := range res.Errors {
				fmt.Println("-", e)
			}
			return nil
		case "letterboxd":
			fs := flag.NewFlagSet("import letterboxd", flag.ContinueOnError)
			file := fs.String("file", "", "path to Letterboxd export zip or diary csv")
			auto := fs.Bool("auto", false, "fetch latest Letterboxd export using browser-auth flow")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *auto {
				res, err := d.syncSvc.SyncAndImport(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("imported: %d\n", res.ImportResult.Imported)
				fmt.Printf("skipped: %d\n", res.ImportResult.Skipped)
				for _, e := range res.ImportResult.Errors {
					fmt.Println("-", e)
				}
				return nil
			}
			if *file == "" {
				return errors.New("--file is required unless --auto is set")
			}
			res, err := d.csvSvc.ImportLetterboxd(ctx, *file)
			if err != nil {
				return err
			}
			fmt.Printf("imported: %d\n", res.Imported)
			fmt.Printf("skipped: %d\n", res.Skipped)
			for _, e := range res.Errors {
				fmt.Println("-", e)
			}
			return nil
		default:
			return errors.New("usage: import <csv|letterboxd> [--file <path> | --auto]")
		}
	case "export":
		if len(args) < 2 || args[1] != "csv" {
			return errors.New("usage: export csv --file <path> [--year YYYY]")
		}
		fs := flag.NewFlagSet("export csv", flag.ContinueOnError)
		file := fs.String("file", "", "path to csv")
		year := fs.Int("year", 0, "year")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *file == "" {
			return errors.New("--file is required")
		}
		var y *int
		if *year > 0 {
			y = year
		}
		if err := d.csvSvc.Export(ctx, *file, y); err != nil {
			return err
		}
		fmt.Println("exported to", *file)
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func onboardingImport(csvSvc *service.CSVService, syncSvc *service.LetterboxdSyncService) error {
	fmt.Println("No logs found. Import your Letterboxd data to get started.")
	fmt.Println("We'll try auto-sync first when browser auth is configured.")
	if syncSvc != nil {
		status, err := syncSvc.CredentialStatus()
		if err == nil && status != "not_logged_in" && status != "expired" {
			res, ran, err := syncSvc.SyncAndImportIfDue(context.Background())
			if err == nil && ran {
				fmt.Printf("Auto-import complete: imported=%d skipped=%d\n", res.ImportResult.Imported, res.ImportResult.Skipped)
				return nil
			}
			if err != nil {
				fmt.Println("Auto-sync failed:", err)
			}
		}
	}
	fmt.Println("Export from Letterboxd and provide either the export ZIP or diary CSV path.")
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Import now? [Y/n]: ")
		ans, _ := r.ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		if ans == "n" || ans == "no" {
			return nil
		}
		if ans == "" || ans == "y" || ans == "yes" {
			break
		}
	}
	fmt.Print("Path to Letterboxd ZIP/CSV: ")
	path, _ := r.ReadString('\n')
	path = normalizePromptPath(path)
	if path == "" {
		return errors.New("import path is required")
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		ok, err := hasDiaryCSV(path)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("diary.csv not found in zip")
		}
	}
	res, err := csvSvc.ImportLetterboxd(context.Background(), path)
	if err != nil {
		return err
	}
	fmt.Printf("Import complete: imported=%d skipped=%d\n", res.Imported, res.Skipped)
	if len(res.Errors) > 0 {
		fmt.Println("Some rows were skipped:")
		for _, e := range res.Errors {
			fmt.Println("-", e)
		}
	}
	return nil
}

// normalizePromptPath lets users paste shell-style escaped paths from terminal prompts.
func normalizePromptPath(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	var b strings.Builder
	b.Grow(len(s))
	escaped := false
	for _, ch := range s {
		if escaped {
			b.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(ch)
	}
	if escaped {
		b.WriteRune('\\')
	}
	return b.String()
}

func hasDiaryCSV(zipPath string) (bool, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "diary.csv") {
			return true, nil
		}
	}
	return false, nil
}

func (d *dependencies) runUI(ctx context.Context) error {
	printUIBanner()
	printUIHelp()
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("\n%s%s●%s %sops-console%s %s❯%s ", uiCool, uiDim, uiReset, uiAccent, uiReset, uiWarm, uiReset)
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if errors.Is(err, io.EOF) {
				return nil
			}
			continue
		}
		if done, cmdErr := d.handleUICommand(ctx, line); cmdErr != nil {
			fmt.Printf("%serror:%s %v\n", uiError, uiReset, cmdErr)
		} else if done {
			return nil
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (d *dependencies) handleUICommand(ctx context.Context, line string) (bool, error) {
	args, err := splitUIArgs(line)
	if err != nil {
		return false, err
	}
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "help":
		printUIHelp()
	case "clear":
		fmt.Print("\x1b[2J\x1b[H")
		printUIBanner()
	case "exit", "quit", "q":
		return true, nil
	case "heatmap":
		hm, err := d.heat.RecentWeeks(ctx, 53, time.Now())
		if err != nil {
			return false, err
		}
		printHeatmap(hm)
	case "stats":
		st, err := d.stats.Year(ctx, time.Now().Year())
		if err != nil {
			return false, err
		}
		printStatsCard(st)
	case "refresh":
		res, err := d.syncSvc.SyncAndImport(ctx)
		if err != nil {
			return false, err
		}
		fmt.Printf("%srefresh complete:%s imported=%d skipped=%d\n", uiAccent, uiReset, res.ImportResult.Imported, res.ImportResult.Skipped)
	case "watched":
		return false, d.showWatched(ctx, 20)
	case "ratings":
		return false, d.showRatings(ctx, 20)
	case "reviews":
		return false, d.showReviews(ctx, 20)
	case "watchlist":
		if len(args) == 1 {
			return false, d.showWatchlist(ctx)
		}
		switch args[1] {
		case "add":
			title, notes, err := parseTitleWithOptionalNotes(args, 2)
			if err != nil {
				return false, err
			}
			item, err := d.lib.AddWatchlist(ctx, domain.AddWatchlistInput{Title: title, Notes: notes})
			if err != nil {
				return false, err
			}
			fmt.Printf("%sadded watchlist:%s %s (%s)\n", uiAccent, uiReset, item.Title, shortID(item.ID))
			return false, d.showWatchlist(ctx)
		case "rm", "remove", "delete":
			if len(args) < 3 {
				return false, errors.New("usage: watchlist rm <item-id>")
			}
			if err := d.lib.RemoveWatchlist(ctx, args[2]); err != nil {
				return false, err
			}
			fmt.Printf("%sremoved watchlist item%s %s\n", uiAccent, uiReset, args[2])
			return false, d.showWatchlist(ctx)
		default:
			return false, errors.New("usage: watchlist [add|rm]")
		}
	case "lists":
		if len(args) == 1 {
			return false, d.showLists(ctx)
		}
		switch args[1] {
		case "create":
			if len(args) < 3 {
				return false, errors.New("usage: lists create <name>")
			}
			name := strings.Join(args[2:], " ")
			lst, err := d.lib.AddList(ctx, domain.AddFilmListInput{Name: name})
			if err != nil {
				return false, err
			}
			fmt.Printf("%screated list:%s %s (%s)\n", uiAccent, uiReset, lst.Name, shortID(lst.ID))
			return false, d.showLists(ctx)
		case "add":
			if len(args) < 4 {
				return false, errors.New("usage: lists add <list-id> <title> [--notes text]")
			}
			listID := args[2]
			title, notes, err := parseTitleWithOptionalNotes(args, 3)
			if err != nil {
				return false, err
			}
			item, err := d.lib.AddListItem(ctx, domain.AddFilmListItemInput{ListID: listID, Title: title, Notes: notes})
			if err != nil {
				return false, err
			}
			fmt.Printf("%sadded to list:%s #%d %s\n", uiAccent, uiReset, item.Position, item.Title)
			return false, d.showListItems(ctx, listID)
		case "view":
			if len(args) < 3 {
				return false, errors.New("usage: lists view <list-id>")
			}
			return false, d.showListItems(ctx, args[2])
		default:
			return false, errors.New("usage: lists [create|add|view]")
		}
	default:
		return false, fmt.Errorf("unknown command: %s (try `help`)", args[0])
	}
	return false, nil
}

func (d *dependencies) showWatched(ctx context.Context, limit int) error {
	rows, err := d.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return err
	}
	lines := make([]string, 0, min(limit, len(rows)))
	lines = append(lines, tableHeader([]string{"Date", "Rate", "Title"}, []int{10, 6, 52}))
	for i, r := range rows {
		if i >= limit {
			break
		}
		rating := "-"
		if r.Rating != nil {
			rating = fmt.Sprintf("%.1f★", *r.Rating)
		}
		lines = append(lines, tableRow([]string{r.LocalDate, rating, r.Title}, []int{10, 6, 52}))
	}
	printUICard("Watched Feed", fmt.Sprintf("Latest %d entries", max(0, len(lines)-1)), lines, uiAccent)
	return nil
}

func (d *dependencies) showRatings(ctx context.Context, limit int) error {
	rows, err := d.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return err
	}
	rated := make([]domain.FilmLog, 0, len(rows))
	for _, r := range rows {
		if r.Rating != nil {
			rated = append(rated, r)
		}
	}
	sort.SliceStable(rated, func(i, j int) bool {
		if *rated[i].Rating == *rated[j].Rating {
			return rated[i].LoggedAt.After(rated[j].LoggedAt)
		}
		return *rated[i].Rating > *rated[j].Rating
	})
	lines := make([]string, 0, min(limit, len(rated)))
	lines = append(lines, tableHeader([]string{"Date", "Score", "Title"}, []int{10, 6, 52}))
	for i, r := range rated {
		if i >= limit {
			break
		}
		lines = append(lines, tableRow([]string{r.LocalDate, fmt.Sprintf("%.1f★", *r.Rating), r.Title}, []int{10, 6, 52}))
	}
	printUICard("Ratings Desk", fmt.Sprintf("Top %d rated logs", max(0, len(lines)-1)), lines, uiWarm)
	return nil
}

func (d *dependencies) showReviews(ctx context.Context, limit int) error {
	rows, err := d.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return err
	}
	lines := make([]string, 0, limit)
	lines = append(lines, tableHeader([]string{"Date", "Film", "Note Preview"}, []int{10, 26, 32}))
	for _, r := range rows {
		if len(lines)-1 >= limit {
			break
		}
		if r.Notes == nil || strings.TrimSpace(*r.Notes) == "" {
			continue
		}
		preview := strings.TrimSpace(*r.Notes)
		lines = append(lines, tableRow([]string{r.LocalDate, r.Title, preview}, []int{10, 26, 32}))
	}
	printUICard("Review Notes", fmt.Sprintf("Recent %d notes", max(0, len(lines)-1)), lines, uiRose)
	return nil
}

func (d *dependencies) showWatchlist(ctx context.Context) error {
	rows, err := d.lib.ListWatchlist(ctx)
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(rows))
	lines = append(lines, tableHeader([]string{"ID", "Added", "Title / Notes"}, []int{8, 10, 50}))
	for _, r := range rows {
		line := r.Title
		if r.Notes != nil && strings.TrimSpace(*r.Notes) != "" {
			line += " | " + *r.Notes
		}
		lines = append(lines, tableRow([]string{shortID(r.ID), r.AddedAt.In(time.Local).Format(domain.DateLayout), line}, []int{8, 10, 50}))
	}
	printUICard("Watchlist Queue", "Planned watches", lines, uiCool)
	return nil
}

func (d *dependencies) showLists(ctx context.Context) error {
	rows, err := d.lib.ListLists(ctx)
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(rows))
	lines = append(lines, tableHeader([]string{"ID", "List", "Films"}, []int{8, 42, 6}))
	for _, lst := range rows {
		items, err := d.lib.ListListItems(ctx, lst.ID)
		if err != nil {
			return err
		}
		lines = append(lines, tableRow([]string{shortID(lst.ID), lst.Name, fmt.Sprintf("%d", len(items))}, []int{8, 42, 6}))
	}
	printUICard("Collections", "Custom lists", lines, uiAccent)
	return nil
}

func (d *dependencies) showListItems(ctx context.Context, listID string) error {
	lists, err := d.lib.ListLists(ctx)
	if err != nil {
		return err
	}
	name := listID
	for _, lst := range lists {
		if lst.ID == listID {
			name = lst.Name
			break
		}
	}
	rows, err := d.lib.ListListItems(ctx, listID)
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(rows))
	lines = append(lines, tableHeader([]string{"#", "Title", "Notes"}, []int{3, 36, 24}))
	for _, r := range rows {
		notes := ""
		if r.Notes != nil && strings.TrimSpace(*r.Notes) != "" {
			notes = *r.Notes
		}
		lines = append(lines, tableRow([]string{fmt.Sprintf("%d", r.Position), r.Title, notes}, []int{3, 36, 24}))
	}
	printUICard("Collection View", fmt.Sprintf("%s (%d films)", name, len(rows)), lines, uiWarm)
	return nil
}

const (
	uiReset  = "\x1b[0m"
	uiAccent = "\x1b[38;2;255;168;76m"
	uiWarm   = "\x1b[38;2;255;116;94m"
	uiRose   = "\x1b[38;2;240;102;156m"
	uiCool   = "\x1b[38;2;104;193;255m"
	uiError  = "\x1b[38;2;255;82;82m"
	uiDim    = "\x1b[38;2;146;162;189m"
	uiPanel  = "\x1b[38;2;34;49;75m"
	uiStrong = "\x1b[1m"
)

func printUIBanner() {
	fmt.Println(uiPanel + "┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓" + uiReset)
	fmt.Printf("%s┃%s %sLETTERBOXD OPERATIONS CONSOLE%s%s  %sData • Analytics • Curation%s %s┃%s\n",
		uiPanel, uiReset, uiStrong, uiAccent, uiReset, uiDim, uiReset, uiPanel, uiReset)
	fmt.Printf("%s┃%s %sSession:%s interactive shell  %sMode:%s production-grade TUI            %s┃%s\n",
		uiPanel, uiReset, uiDim, uiReset, uiDim, uiReset, uiPanel, uiReset)
	fmt.Println(uiPanel + "┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛" + uiReset)
}

func printUIHelp() {
	fmt.Println(uiDim + "Command Palette" + uiReset)
	fmt.Println("  data:      watched | ratings | reviews | heatmap | stats | refresh")
	fmt.Println("  watchlist: watchlist | watchlist add <title> [--notes text] | watchlist rm <id>")
	fmt.Println("  lists:     lists | lists create <name> | lists add <list-id> <title> [--notes text] | lists view <list-id>")
	fmt.Println("  system:    clear | help | exit")
}

func printUICard(title string, subtitle string, lines []string, color string) {
	innerWidth := 74
	top := "┌" + strings.Repeat("─", innerWidth) + "┐"
	bottom := "└" + strings.Repeat("─", innerWidth) + "┘"
	fmt.Printf("\n%s%s%s\n", uiPanel, top, uiReset)
	fmt.Printf("%s│%s %s%s%s\n", uiPanel, uiReset, uiStrong, clipText(title, innerWidth-1), uiReset)
	if subtitle != "" {
		fmt.Printf("%s│%s %s%s\n", uiPanel, uiReset, uiDim, clipText(subtitle, innerWidth-1)+uiReset)
		fmt.Printf("%s│%s %s\n", uiPanel, uiReset, strings.Repeat("·", innerWidth-1))
	}
	if len(lines) == 0 {
		fmt.Printf("%s│%s %s(no data)%s\n", uiPanel, uiReset, uiRose, uiReset)
		fmt.Printf("%s%s%s\n", uiPanel, bottom, uiReset)
		return
	}
	for _, line := range lines {
		padded := padRight(clipText(line, innerWidth-1), innerWidth-1)
		fmt.Printf("%s│%s %s%s\n", uiPanel, uiReset, color, padded+uiReset)
	}
	fmt.Printf("%s%s%s\n", uiPanel, bottom, uiReset)
}

func printStatsCard(stt domain.YearStats) {
	lines := []string{tableHeader([]string{"Metric", "Value"}, []int{32, 8})}
	stats := [][]string{
		{"Year", fmt.Sprintf("%d", stt.Year)},
		{"Total Logs", fmt.Sprintf("%d", stt.TotalLogs)},
		{"Active Days", fmt.Sprintf("%d", stt.ActiveDays)},
		{"Longest Streak", fmt.Sprintf("%d", stt.LongestStreak)},
		{"Current Streak", fmt.Sprintf("%d", stt.CurrentStreak)},
	}
	for _, row := range stats {
		lines = append(lines, tableRow(row, []int{32, 8}))
	}
	printUICard("Analytics Snapshot", "Year summary", lines, uiCool)
}

func tableHeader(cols []string, widths []int) string {
	upper := make([]string, 0, len(cols))
	for _, c := range cols {
		upper = append(upper, strings.ToUpper(c))
	}
	return tableRow(upper, widths)
}

func tableRow(cols []string, widths []int) string {
	parts := make([]string, 0, len(cols))
	for i, c := range cols {
		w := 12
		if i < len(widths) {
			w = widths[i]
		}
		parts = append(parts, padRight(clipText(c, w), w))
	}
	return strings.Join(parts, "  ")
}

func clipText(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func parseTitleWithOptionalNotes(args []string, start int) (string, *string, error) {
	if start >= len(args) {
		return "", nil, errors.New("title is required")
	}
	noteFlag := -1
	for i := start; i < len(args); i++ {
		if args[i] == "--notes" {
			noteFlag = i
			break
		}
	}
	var titleParts []string
	var notes *string
	if noteFlag >= 0 {
		titleParts = args[start:noteFlag]
		if noteFlag+1 >= len(args) {
			return "", nil, errors.New("--notes requires text")
		}
		n := strings.Join(args[noteFlag+1:], " ")
		notes = &n
	} else {
		titleParts = args[start:]
	}
	title := strings.TrimSpace(strings.Join(titleParts, " "))
	if title == "" {
		return "", nil, errors.New("title is required")
	}
	return title, notes, nil
}

func splitUIArgs(line string) ([]string, error) {
	var out []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, ch := range line {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				current.WriteRune(ch)
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				out = append(out, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out, nil
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func printUsage() {
	fmt.Println("film-heatmap help:")
	fmt.Println("  help backend   backend/data commands")
	fmt.Println("  help features  auth/sync commands")
	fmt.Println("")
	fmt.Println("quick commands:")
	fmt.Println("  add --title --date [--rating --notes --rewatch]")
	fmt.Println("  list [--from --to --title]")
	fmt.Println("  edit --id [--title --date --rating --notes --rewatch]")
	fmt.Println("  delete --id")
	fmt.Println("  dedupe")
	fmt.Println("  heatmap [--recent-weeks 53] [--year YYYY]")
	fmt.Println("  stats [--year]")
	fmt.Println("  ui")
}

func printBackendHelp() {
	fmt.Println("Backend/Data Commands:")
	fmt.Println("  add --title --date [--rating --notes --rewatch]")
	fmt.Println("  list [--from --to --title]")
	fmt.Println("  edit --id [--title --date --rating --notes --rewatch]")
	fmt.Println("  delete --id")
	fmt.Println("  dedupe                       remove exact duplicate logs")
	fmt.Println("  import csv --file <path>")
	fmt.Println("  import letterboxd --file <path-to-zip-or-csv>")
	fmt.Println("  export csv --file <path> [--year YYYY]")
	fmt.Println("  heatmap [--recent-weeks 53] [--year YYYY]")
	fmt.Println("  stats [--year]")
	fmt.Println("  ui")
}

func printFeatureHelp() {
	fmt.Println("Feature/Sync Commands:")
	fmt.Println("  auth login [--enable-auto-sync=true|false]")
	fmt.Println("  auth status")
	fmt.Println("  auth logout")
	fmt.Println("  refresh")
	fmt.Println("  import letterboxd --auto")
	fmt.Println("")
	fmt.Println("Timing rules:")
	fmt.Println("  - browser auth session is valid for 1 hour after `auth login`")
	fmt.Println("  - auto-sync checks run at most once per hour")
	fmt.Println("  - successful auto import deletes the detected export zip from Downloads")
	fmt.Println("")
	fmt.Println("Account/session flow:")
	fmt.Println("  1) Logout current app session: auth logout")
	fmt.Println("  2) Login again via browser: auth login")
	fmt.Println("  3) Refresh latest Letterboxd data: refresh (or import letterboxd --auto)")
}

func parseDateFlag(v string) (time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return time.Now(), nil
	}
	if len(v) == len(domain.DateLayout) {
		return time.ParseInLocation(domain.DateLayout, v, time.Local)
	}
	return time.Parse(time.RFC3339, v)
}

func parseOptionalDay(v string) (*time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(domain.DateLayout, v, time.Local)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parseOptionalDayEnd(v string) (*time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(domain.DateLayout, v, time.Local)
	if err != nil {
		return nil, err
	}
	t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	return &t, nil
}

func parseOptionalRating(v string) (*float64, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	r, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func printHeatmap(hm domain.HeatmapMatrix) {
	weeks := orderWeeksChronologically(hm.Weeks)
	if len(weeks) == 0 {
		fmt.Println("No heatmap data.")
		return
	}

	monthHeader := buildMonthHeader(weeks)
	const (
		labelWidth = 3
		cellSpan   = 2 // 1-char cell + 1 space
	)

	fmt.Println()
	fmt.Printf(" %*s %s\n", labelWidth, "", monthHeader)
	for day := 0; day < 7; day++ {
		label := ""
		switch day {
		case 1:
			label = "Mon"
		case 3:
			label = "Wed"
		case 5:
			label = "Fri"
		}
		var row strings.Builder
		for _, week := range weeks {
			row.WriteString(githubStyleCell(week[day].Intensity))
			row.WriteByte(' ')
		}
		fmt.Printf(" %-*s %s\n", labelWidth, label, row.String())
	}
	fmt.Println()
}

func buildMonthHeader(weeks [][]domain.HeatmapCell) string {
	if len(weeks) == 0 {
		return ""
	}
	span := 2
	width := len(weeks) * span
	runes := make([]rune, width)
	for i := range runes {
		runes[i] = ' '
	}
	lastMonth := ""
	lastLabelEnd := -4
	for wi, week := range weeks {
		if len(week) == 0 {
			continue
		}
		// Use Sunday cell as the week anchor for month labels.
		d, err := time.Parse(domain.DateLayout, week[0].Date)
		if err != nil {
			continue
		}
		month := d.Format("Jan")
		if wi == 0 || month != lastMonth {
			start := wi * span
			if start-lastLabelEnd < 3 {
				start = lastLabelEnd + 3
			}
			if start >= len(runes) {
				continue
			}
			for i, ch := range month {
				if start+i >= len(runes) {
					break
				}
				runes[start+i] = ch
			}
			lastMonth = month
			lastLabelEnd = start + len(month)
		}
	}
	return strings.TrimRight(string(runes), " ")
}

func githubStyleCell(intensity int) string {
	// Warm palette to avoid a typical "developer green" graph look.
	switch intensity {
	case 0:
		return "\x1b[38;2;47;62;86m■\x1b[0m"
	case 1:
		return "\x1b[38;2;143;94;35m■\x1b[0m"
	case 2:
		return "\x1b[38;2;186;103;54m■\x1b[0m"
	case 3:
		return "\x1b[38;2;224;118;96m■\x1b[0m"
	default:
		return "\x1b[38;2;255;152;122m■\x1b[0m"
	}
}

func orderWeeksChronologically(weeks [][]domain.HeatmapCell) [][]domain.HeatmapCell {
	sorted := make([][]domain.HeatmapCell, 0, len(weeks))
	for _, week := range weeks {
		if len(week) > 0 {
			sorted = append(sorted, week)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i][0].Date < sorted[j][0].Date
	})
	return sorted
}
