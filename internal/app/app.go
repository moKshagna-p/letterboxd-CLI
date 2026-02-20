package app

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	rows, err := deps.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if err := onboardingImport(deps.csvSvc); err != nil {
			return err
		}
	}

	y := time.Now().Year()
	hm, err := deps.heat.Year(ctx, y)
	if err != nil {
		return err
	}
	printHeatmap(hm)
	fmt.Println("Tip: run `film-heatmap list` or `film-heatmap add --title ...`")
	return nil
}

type dependencies struct {
	store  *store.Store
	logs   *service.LogService
	heat   *service.HeatmapService
	stats  *service.StatsService
	csvSvc *service.CSVService
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
	return &dependencies{
		store:  st,
		logs:   logs,
		heat:   service.NewHeatmapService(st),
		stats:  service.NewStatsService(st),
		csvSvc: service.NewCSVService(logs),
	}, nil
}

func (d *dependencies) run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	ctx := context.Background()
	switch args[0] {
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
	case "heatmap":
		fs := flag.NewFlagSet("heatmap", flag.ContinueOnError)
		year := fs.Int("year", time.Now().Year(), "year")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		hm, err := d.heat.Year(ctx, *year)
		if err != nil {
			return err
		}
		printHeatmap(hm)
		return nil
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
	case "import":
		if len(args) < 2 {
			return errors.New("usage: import <csv|letterboxd> --file <path>")
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
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *file == "" {
				return errors.New("--file is required")
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
			return errors.New("usage: import <csv|letterboxd> --file <path>")
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

func onboardingImport(csvSvc *service.CSVService) error {
	fmt.Println("No logs found. Import your Letterboxd data to get started.")
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

func printUsage() {
	fmt.Println("film-heatmap commands:")
	fmt.Println("  add --title --date [--rating --notes --rewatch]")
	fmt.Println("  list [--from --to --title]")
	fmt.Println("  edit --id [--title --date --rating --notes --rewatch]")
	fmt.Println("  delete --id")
	fmt.Println("  heatmap [--year]")
	fmt.Println("  stats [--year]")
	fmt.Println("  import csv --file")
	fmt.Println("  import letterboxd --file")
	fmt.Println("  export csv --file [--year]")
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
	fmt.Printf("Heatmap %d (%s -> %s)\n", hm.Year, hm.Start, hm.End)
	labels := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	for i, label := range labels {
		fmt.Printf("%s ", label)
		for _, w := range hm.Weeks {
			cell := w[i]
			fmt.Print(intensityChar(cell.Intensity), " ")
		}
		fmt.Println()
	}
	fmt.Println("Legend: . 0, l 1, m 2, h 3, # 4+")
}

func intensityChar(v int) string {
	switch v {
	case 0:
		return "."
	case 1:
		return "l"
	case 2:
		return "m"
	case 3:
		return "h"
	default:
		return "#"
	}
}
