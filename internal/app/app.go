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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"film-heatmap/internal/appmeta"
	"film-heatmap/internal/domain"
	"film-heatmap/internal/service"
	"film-heatmap/internal/ui"
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
	if err := deps.bootstrapInteractiveLaunch(ctx); err != nil {
		return err
	}
	return deps.runUI(ctx)
}

type dependencies struct {
	store   *store.Store
	logs    *service.LogService
	heat    *service.HeatmapService
	stats   *service.StatsService
	lbStats *service.LetterboxdStatsService
	csvSvc  *service.CSVService
	lib     *service.LibraryService
	cfgSvc  *service.AppConfigService
	syncSvc *service.LetterboxdSyncService
}

func (d *dependencies) close() {
	_ = d.store.Close()
}

func newDeps() (*dependencies, error) {
	dbPath := appmeta.LookupEnv("LETTERBOXD_TUI_DB", "FILM_HEATMAP_DB")
	if dbPath == "" {
		dbPath = appmeta.DefaultDBPath()
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	logs := service.NewLogService(st)
	csvSvc := service.NewCSVService(logs)
	libSvc := service.NewLibraryService(st)
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
		lbStats: service.NewLetterboxdStatsService(cfgSvc, downloader),
		csvSvc:  csvSvc,
		lib:     libSvc,
		cfgSvc:  cfgSvc,
		syncSvc: service.NewLetterboxdSyncService(csvSvc, libSvc, cfgSvc, downloader),
	}, nil
}

func (d *dependencies) autoSyncIfConfigured(ctx context.Context) {
	enabled, err := d.syncSvc.Enabled()
	if err != nil || !enabled {
		return
	}
	status, err := d.syncSvc.CredentialStatus()
	if err != nil || status == "not_logged_in" || status == "expired" {
		return
	}
	res, ran, err := d.syncSvc.SyncAndImportIfDue(ctx)
	if err != nil || !ran {
		return // silently skip — non-critical, cached data is available
	}
	if res.ImportResult.Imported > 0 || res.WatchlistAdded > 0 || res.ListsAdded > 0 {
		fmt.Printf("auto-import complete: imported=%d skipped=%d watchlist_added=%d lists_added=%d\n", res.ImportResult.Imported, res.ImportResult.Skipped, res.WatchlistAdded, res.ListsAdded)
	}
}

func (d *dependencies) bootstrapInteractiveLaunch(ctx context.Context) error {
	status, err := d.syncSvc.CredentialStatus()
	if err != nil {
		return err
	}

	if status == "not_logged_in" || status == "expired" {
		fmt.Println("Welcome to letterboxd-tui.")
		fmt.Println("Sign in with your Letterboxd credentials to set up the TUI.")
		if _, err := d.promptAndStoreLetterboxdCredentials(ctx, true); err != nil {
			return err
		}
		d.syncOnLaunch(ctx, true)
		return nil
	}

	d.autoSyncIfConfigured(ctx)

	rows, err := d.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		d.syncOnLaunch(ctx, false)
	}
	return nil
}

func (d *dependencies) syncOnLaunch(ctx context.Context, force bool) {
	var (
		res service.SyncImportResult
		ran bool
		err error
	)
	if force {
		res, err = d.syncSvc.SyncAndImport(ctx)
		ran = err == nil
	} else {
		res, ran, err = d.syncSvc.SyncAndImportIfDue(ctx)
	}
	if err != nil {
		fmt.Printf("warning: unable to sync from Letterboxd yet: %v\n", err)
		return
	}
	if !ran {
		return
	}
	if res.ImportResult.Imported > 0 || res.ImportResult.Skipped > 0 || res.WatchlistAdded > 0 || res.ListsAdded > 0 {
		fmt.Printf("launch sync complete: imported=%d skipped=%d watchlist_added=%d lists_added=%d\n", res.ImportResult.Imported, res.ImportResult.Skipped, res.WatchlistAdded, res.ListsAdded)
	}
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
		d.autoSyncIfConfigured(ctx)
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
	case "lbstats":
		fs := flag.NewFlagSet("lbstats", flag.ContinueOnError)
		view := fs.String("view", "", "one of: watched,reviews,watchlist,lists,tags,all")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return d.runLBStats(ctx, strings.TrimSpace(strings.ToLower(*view)))
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
			fmt.Print("Letterboxd username: ")
			username, _ := r.ReadString('\n')
			username = strings.TrimSpace(username)
			password, err := readPasswordPrompt("Letterboxd password: ")
			if err != nil {
				return err
			}
			creds := service.LetterboxdCredentials{Username: username, Password: password}
			fmt.Println("Validating credentials against Letterboxd...")
			if err := d.syncSvc.ValidateCredentials(context.Background(), creds); err != nil {
				return err
			}
			if err := d.syncSvc.SetCredentials(creds); err != nil {
				return err
			}
			if err := d.syncSvc.SetEnabled(*enableAutoSync); err != nil {
				return err
			}
			fmt.Println("✓ credentials configured")
			fmt.Printf("✓ auto-sync enabled: %t\n", *enableAutoSync)
			fmt.Println("\nNext steps:")
			fmt.Println("  1. Check status: ./letterboxd-tui auth status")
			fmt.Println("  2. Sync data:   ./letterboxd-tui refresh")
			fmt.Println("  3. View data:   make ui")
			return nil
		case "logout":
			if err := d.syncSvc.InvalidateBrowserSession(); err != nil {
				return err
			}
			fmt.Println("credentials cleared from app config")
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
			
			fmt.Println("=== Authentication Status ===")
			fmt.Println()
			
			switch status {
			case "not_logged_in":
				fmt.Println("❌ Status: Not logged in")
				fmt.Println()
				fmt.Println("To access private data (watchlist, lists, reviews):")
				fmt.Println("  ./letterboxd-tui auth login")
			case "expired":
				fmt.Println("⚠️  Status: Session expired")
				fmt.Println()
				fmt.Println("Your session has expired. Re-login:")
				fmt.Println("  ./letterboxd-tui auth login")
			default:
				fmt.Println("✓ Status: Authenticated")
			}
			
			fmt.Println()
			fmt.Println("Auto-sync: " + map[bool]string{true: "enabled ✓", false: "disabled"}[enabled])
			
			username, err := d.syncSvc.Username()
			if err == nil && username != "" {
				fmt.Println("Username: " + username)
			}
			
			if expiresAt != nil {
				fmt.Printf("Auth expires: %s\n", expiresAt.In(time.Local).Format("2006-01-02 15:04"))
			}
			
			if cooldownUntil != nil && cooldownUntil.After(time.Now()) {
				fmt.Printf("⏳ Next sync available: %s\n", cooldownUntil.In(time.Local).Format("2006-01-02 15:04"))
			}
			
			fmt.Println()
			fmt.Println("Next: ./letterboxd-tui refresh")
			return nil
		default:
			return errors.New("usage: auth <login|logout|status>")
		}
	case "refresh":
		fmt.Println("Fetching from Letterboxd...")
		res, err := d.syncSvc.SyncAndImport(ctx)
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Println("✓ Sync complete!")
		fmt.Println()
		fmt.Println("Import results:")
		fmt.Printf("  Films imported:      %d\n", res.ImportResult.Imported)
		fmt.Printf("  Films skipped:       %d\n", res.ImportResult.Skipped)
		fmt.Printf("  Watchlist items:     %d\n", res.WatchlistAdded)
		fmt.Printf("  List items:          %d\n", res.ListsAdded)
		fmt.Println()
		fmt.Println("View your data:")
		fmt.Println("  • Lists:        make ui  →  press 1 (Watched Films)")
		fmt.Println("  • Ratings:      make ui  →  press 2 (Ratings Desk)")
		fmt.Println("  • Watchlist:    make ui  →  press 6 (Watchlist)")
		fmt.Println("  • Collections:  make ui  →  press 7 (Collections)")
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
				fmt.Printf("watchlist_added: %d\n", res.WatchlistAdded)
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

func readPasswordPrompt(prompt string) (string, error) {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	pw, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(pw), nil
}

func (d *dependencies) promptAndStoreLetterboxdCredentials(ctx context.Context, enableAutoSync bool) (service.LetterboxdCredentials, error) {
	r := bufio.NewReader(os.Stdin)
	fmt.Print("Letterboxd username: ")
	username, _ := r.ReadString('\n')
	username = strings.TrimSpace(username)
	password, err := readPasswordPrompt("Letterboxd password: ")
	if err != nil {
		return service.LetterboxdCredentials{}, err
	}
	creds := service.LetterboxdCredentials{Username: username, Password: password}
	fmt.Println("Validating credentials against Letterboxd...")
	if err := d.syncSvc.ValidateCredentials(ctx, creds); err != nil {
		return service.LetterboxdCredentials{}, err
	}
	if err := d.syncSvc.SetCredentials(creds); err != nil {
		return service.LetterboxdCredentials{}, err
	}
	if err := d.syncSvc.SetEnabled(enableAutoSync); err != nil {
		return service.LetterboxdCredentials{}, err
	}
	return creds, nil
}

func (d *dependencies) ensureLetterboxdCredentials(ctx context.Context) (service.LetterboxdCredentials, error) {
	creds, err := d.syncSvc.Credentials()
	if err != nil {
		return service.LetterboxdCredentials{}, err
	}
	if strings.TrimSpace(creds.Username) != "" && strings.TrimSpace(creds.Password) != "" {
		return creds, nil
	}
	fmt.Println("No Letterboxd credentials configured. Set them now.")
	return d.promptAndStoreLetterboxdCredentials(ctx, true)
}

func (d *dependencies) printLBMenu() {
	username, _ := d.syncSvc.Username()
	lines := []string{
		"",
		fmt.Sprintf("          Profile: %s%s%s", uiAccent+uiReset, username, ""),
		"",
		"          1.  Watched      Recently logged films",
		"          2.  Reviews      Latest reviews and notes",
		"          3.  Watchlist    Your planned watches",
		"          4.  Lists        Your Letterboxd collections",
		"          5.  Tags         Frequently used tags",
		"          6.  Heatmap      Visual activity map",
		"          7.  All          Complete overview",
		"",
		"          r.  Refresh      Force sync from Letterboxd",
		"          q.  Back         Return to main menu",
		"",
	}
	printUICard("Letterboxd Stats Menu", "Explore your film profile", lines, uiAccent)
}

func (d *dependencies) runLBStats(ctx context.Context, initialView string) error {
	creds, err := d.ensureLetterboxdCredentials(ctx)
	if err != nil {
		return err
	}

	if initialView != "" {
		return d.showLBView(ctx, creds, initialView)
	}

	r := bufio.NewReader(os.Stdin)
	for {
		printLBStatsBanner()
		d.printLBMenu()
		fmt.Printf("\n%s%s●%s %slbstats%s %s❯%s ", uiCool, uiDim, uiReset, uiAccent, uiReset, uiWarm, uiReset)
		input, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "" {
			continue
		}
		if input == "q" || input == "back" || input == "exit" || input == "quit" {
			return nil
		}

		view := ""
		switch input {
		case "1", "watched":
			view = "watched"
		case "2", "reviews":
			view = "reviews"
		case "3", "watchlist":
			view = "watchlist"
		case "4", "lists":
			view = "lists"
		case "5", "tags":
			view = "tags"
		case "6", "heatmap":
			view = "heatmap"
		case "7", "all":
			view = "all"
		case "r", "refresh":
			fmt.Println("Refreshing from Letterboxd...")
			if _, err := d.syncSvc.SyncAndImport(ctx); err != nil {
				// sync failure during manual refresh is worth showing
				fmt.Printf("%serror:%s sync failed: %v\n", uiError, uiReset, err)
			}
			continue
		default:
			fmt.Printf("%serror:%s invalid selection\n", uiError, uiReset)
			continue
		}

		if err := d.showLBView(ctx, creds, view); err != nil {
			fmt.Printf("%serror:%s %v\n", uiError, uiReset, err)
		}
	}
}

func (d *dependencies) showLBView(ctx context.Context, creds service.LetterboxdCredentials, view string) error {
	// Silently attempt sync — non-critical, uses cache on failure
	_, _, _ = d.syncSvc.SyncAndImportIfDue(ctx)
	res, scrapeErr := d.lbStats.SyncAndLoad(ctx, creds)
	if scrapeErr != nil && len(res.Watched.Items) == 0 && len(res.Reviews.Items) == 0 &&
		len(res.Watchlist.Items) == 0 && len(res.Lists.Items) == 0 && len(res.Tags.Items) == 0 {
		// Major error: scrape failed AND no cached data available
		return fmt.Errorf("unable to load Letterboxd data: %v", scrapeErr)
	}

	printFeature := func(name string, fr service.LBFeatureResult, color string) {
		if view != "all" && view != name {
			return
		}
		limit := min(20, len(fr.Items))
		lines := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			lines = append(lines, fmt.Sprintf("%2d. %s", i+1, fr.Items[i]))
		}
		if len(fr.Items) > limit {
			lines = append(lines, fmt.Sprintf("    ... +%d more", len(fr.Items)-limit))
		}
		status := "unchanged"
		if fr.Changed {
			status = "updated"
		}
		subtitle := fmt.Sprintf("Source: %s | Count: %d | Status: %s", fr.Source, len(fr.Items), status)
		printUICard(strings.ToUpper(name), subtitle, lines, color)
	}

	printFeature("watched", res.Watched, uiAccent)
	printFeature("reviews", res.Reviews, uiRose)
	printFeature("watchlist", res.Watchlist, uiCool)
	printFeature("lists", res.Lists, uiWarm)
	printFeature("tags", res.Tags, uiDim)

	if view == "all" || view == "heatmap" {
		hm, err := d.heat.RecentWeeks(ctx, 53, time.Now())
		if err != nil {
			return err
		}
		printHeatmap(hm)
	}
	return nil
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
	// Create new UI model
	deps := &ui.Dependencies{
		Logs:    d.logs,
		Heat:    d.heat,
		Stats:   d.stats,
		LBStats: d.lbStats,
		CSV:     d.csvSvc,
		Lib:     d.lib,
		Cfg:     d.cfgSvc,
		Sync:    d.syncSvc,
	}

	model := ui.New(ctx, deps)
	p := tea.NewProgram(model)
	_, err := p.Run()
	return err
}

func printUsage() {
	fmt.Printf("%s help:\n", appmeta.CommandName())
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
	fmt.Println("  lbstats [--view watched|reviews|watchlist|lists|tags|heatmap|all]")
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
	fmt.Println("  lbstats [--view watched|reviews|watchlist|lists|tags|heatmap|all]")
	fmt.Println("  ui")
}

func printFeatureHelp() {
	fmt.Println("Feature/Sync Commands:")
	fmt.Println("  auth login [--enable-auto-sync=true|false]")
	fmt.Println("  auth status")
	fmt.Println("  auth logout")
	fmt.Println("  refresh                       fetch latest via automatic scraper")
	fmt.Println("  import letterboxd --auto      same as refresh")
	fmt.Println("  import letterboxd --file <f>  import official Letterboxd export (.zip or diary.csv)")
	fmt.Println("  lbstats [--view ...]          view remote profile stats")
	fmt.Println("")
	fmt.Println("Official Letterboxd CSV Export (Recommended Alternative):")
	fmt.Println("  If automatic scraping fails, you can use the official export:")
	fmt.Println("  1. Go to Letterboxd.com -> Settings -> Data -> Export Your Data")
	fmt.Println("  2. Download the .zip file once it's ready")
	fmt.Println("  3. Run: ./letterboxd-tui import letterboxd --file letterboxd-user-202X-XX-XX.zip")
	fmt.Println("")
	fmt.Println("Timing rules:")
	fmt.Println("  - auth login stores Letterboxd credentials once in local config")
	fmt.Println("  - TUI 'r' key triggers a scraper sync check")
	fmt.Println("  - if no remote changes are found, cached local data is used")
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

const (
	uiReset  = "\x1b[0m"
	uiAccent = "\x1b[38;2;232;180;120m" // warm gold / aged paper
	uiWarm   = "\x1b[38;2;204;102;102m" // dusty red / lobby carpet
	uiRose   = "\x1b[38;2;199;134;157m" // muted mauve-pink
	uiCool   = "\x1b[38;2;130;180;205m" // faded powder blue
	uiError  = "\x1b[38;2;255;82;82m"
	uiDim    = "\x1b[38;2;146;155;165m" // soft gray
	uiPanel  = "\x1b[38;2;90;100;115m"  // slate-blue borders (visible)
	uiStrong = "\x1b[1m"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Minimal CLI output functions for non-TUI mode
func printHeatmap(hm domain.HeatmapMatrix) {
	weeks := orderWeeksChronologically(hm.Weeks)
	if len(weeks) == 0 {
		printCenteredLine(uiDim + "( no heatmap data )" + uiReset)
		return
	}

	totalFilms := totalFilmsInWeeks(weeks)

	monthHeader := buildMonthHeader(weeks)
	const (
		labelWidth = 5
		cellSpan   = 2
	)
	gridWidth := labelWidth + 1 + len(weeks)*cellSpan

	// Title + subtitle
	fmt.Println()
	fmt.Println()
	title := letterSpace("ACTIVITY MAP")
	printCenteredLine(uiStrong + uiAccent + title + uiReset)
	printCenteredLine(uiDim + "── Your viewing pattern over the last year ──" + uiReset)
	fmt.Println()

	// Month header row, centered
	headerLine := fmt.Sprintf(" %*s %s", labelWidth, "", monthHeader)
	headerPadded := padRight(headerLine, gridWidth)
	printCenteredLine(uiDim + headerPadded + uiReset)

	// Day rows, centered
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
		line := fmt.Sprintf(" %-*s %s", labelWidth, label, row.String())
		printCenteredLine(line)
	}

	// Legend bar centered below
	fmt.Println()
	legend := fmt.Sprintf("%sLess %s %s %s %s %s More%s       %sTotal: %d films%s",
		uiDim,
		githubStyleCell(0), githubStyleCell(1), githubStyleCell(2), githubStyleCell(3), githubStyleCell(4),
		uiReset,
		uiAccent, totalFilms, uiReset)
	printCenteredLine(legend)
	fmt.Println()
}

func printUICard(title string, subtitle string, lines []string, color string) {
	innerWidth := 74
	top := "╭" + strings.Repeat("─", innerWidth) + "╮"
	bottom := "╰" + strings.Repeat("─", innerWidth) + "╯"
	side := "│"
	spacedTitle := letterSpace(strings.ToUpper(title))

	fmt.Println()
	printCenteredLine(uiPanel + top + uiReset)
	// breathing room
	printCenteredLine(fmt.Sprintf("%s%s%s%s%s%s%s",
		uiPanel, side, uiReset, centerText(" ", innerWidth), uiPanel, side, uiReset))
	// centered letterspaced title
	printCenteredLine(fmt.Sprintf("%s%s%s %s%s%s %s%s%s",
		uiPanel, side, uiReset,
		uiStrong+color, centerText(spacedTitle, innerWidth-2), uiReset,
		uiPanel, side, uiReset))
	if subtitle != "" {
		// centered subtitle with decorative dashes
		styledSub := "── " + subtitle + " ──"
		printCenteredLine(fmt.Sprintf("%s%s%s %s%s%s %s%s%s",
			uiPanel, side, uiReset,
			uiDim, centerText(styledSub, innerWidth-2), uiReset,
			uiPanel, side, uiReset))
	}
	if len(lines) == 0 {
		printCenteredLine(fmt.Sprintf("%s%s%s %s%s%s %s%s%s",
			uiPanel, side, uiReset,
			uiRose, centerText("( no data )", innerWidth-2), uiReset,
			uiPanel, side, uiReset))
		printCenteredLine(uiPanel + bottom + uiReset)
		return
	}
	for _, line := range lines {
		padded := padRight(clipText(line, innerWidth-4), innerWidth-4)
		printCenteredLine(fmt.Sprintf("%s%s%s  %s%s%s  %s%s%s",
			uiPanel, side, uiReset,
			color, padded, uiReset,
			uiPanel, side, uiReset))
	}
	// breathing room
	printCenteredLine(fmt.Sprintf("%s%s%s%s%s%s%s",
		uiPanel, side, uiReset, centerText(" ", innerWidth), uiPanel, side, uiReset))
	printCenteredLine(uiPanel + bottom + uiReset)
}

func printLBStatsBanner() {
	w := 78
	title := letterSpace("LETTERBOXD ANALYTICS & INSIGHTS")
	sub := "Remote Data  ·  Scraper Feed"
	fmt.Println()
	printCenteredLine(uiPanel + "╔" + strings.Repeat("═", w) + "╗" + uiReset)
	printCenteredLine(uiPanel + "║" + strings.Repeat(" ", w) + "║" + uiReset)
	printCenteredLine(fmt.Sprintf("%s║%s%s%s%s%s║%s",
		uiPanel, uiReset, uiStrong+uiCool, centerText(title, w), uiReset, uiPanel, uiReset))
	printCenteredLine(fmt.Sprintf("%s║%s%s%s%s║%s",
		uiPanel, uiReset, uiDim, centerText("── "+sub+" ──", w), uiPanel, uiReset))
	printCenteredLine(uiPanel + "║" + strings.Repeat(" ", w) + "║" + uiReset)
	printCenteredLine(uiPanel + "╚" + strings.Repeat("═", w) + "╝" + uiReset)
}

func printCenteredLine(s string) {
	width := terminalWidth()
	padding := (width - visibleLen(s)) / 2
	if padding < 0 {
		padding = 0
	}
	fmt.Print(strings.Repeat(" ", padding))
	fmt.Println(s)
}

func terminalWidth() int {
	if c := strings.TrimSpace(os.Getenv("COLUMNS")); c != "" {
		if v, err := strconv.Atoi(c); err == nil && v > 40 {
			return v
		}
	}
	return 110
}

func visibleLen(s string) int {
	clean := ansiEscapePattern.ReplaceAllString(s, "")
	return len([]rune(clean))
}

// letterSpace inserts spaces between each character: "HELLO" -> "H E L L O"
func letterSpace(s string) string {
	runes := []rune(s)
	if len(runes) <= 1 {
		return s
	}
	parts := make([]string, len(runes))
	for i, r := range runes {
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

// centerText centers s within a field of given width, padding both sides.
func centerText(s string, width int) string {
	vLen := visibleLen(s)
	if vLen >= width {
		return s
	}
	left := (width - vLen) / 2
	right := width - vLen - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
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

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
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
			if start+len(month) > len(runes) {
				// Skip labels that would be truncated at the right edge.
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
	// Wes Anderson warm pastel palette.
	switch intensity {
	case 0:
		return "\x1b[38;2;60;68;80m■\x1b[0m" // muted slate (empty)
	case 1:
		return "\x1b[38;2;160;140;100m■\x1b[0m" // aged parchment
	case 2:
		return "\x1b[38;2;200;150;110m■\x1b[0m" // warm camel
	case 3:
		return "\x1b[38;2;204;120;100m■\x1b[0m" // dusty terracotta
	default:
		return "\x1b[38;2;232;180;120m■\x1b[0m" // warm gold (matches uiAccent)
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

func totalFilmsInWeeks(weeks [][]domain.HeatmapCell) int {
	total := 0
	for _, week := range weeks {
		for _, cell := range week {
			total += cell.Count
		}
	}
	return total
}
