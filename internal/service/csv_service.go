package service

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"film-heatmap/internal/domain"
)

type CSVImportResult struct {
	Imported int
	Skipped  int
	Errors   []string
}

type CSVService struct {
	logs *LogService
}

func NewCSVService(logs *LogService) *CSVService {
	return &CSVService{logs: logs}
}

func parseCSVDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("empty logged_at")
	}
	if len(raw) == len(domain.DateLayout) {
		t, err := time.ParseInLocation(domain.DateLayout, raw, time.Local)
		if err != nil {
			return time.Time{}, err
		}
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func parseLetterboxdDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("missing date")
	}
	return time.ParseInLocation(domain.DateLayout, raw, time.Local)
}

func parseLetterboxdRating(raw string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return &v, nil
	}
	var stars float64
	for _, r := range raw {
		switch r {
		case '★':
			stars += 1.0
		case '½':
			stars += 0.5
		}
	}
	if stars > 0 {
		return &stars, nil
	}
	return nil, fmt.Errorf("invalid rating: %s", raw)
}

func (s *CSVService) Import(ctx context.Context, file string) (CSVImportResult, error) {
	f, err := os.Open(file)
	if err != nil {
		return CSVImportResult{}, err
	}
	defer f.Close()
	return s.importGenericCSV(ctx, f)
}

func (s *CSVService) ImportLetterboxd(ctx context.Context, file string) (CSVImportResult, error) {
	ext := strings.ToLower(filepath.Ext(file))
	if ext == ".zip" {
		return s.importLetterboxdZip(ctx, file)
	}
	f, err := os.Open(file)
	if err != nil {
		return CSVImportResult{}, err
	}
	defer f.Close()
	return s.importLetterboxdCSV(ctx, f)
}

func (s *CSVService) importLetterboxdZip(ctx context.Context, path string) (CSVImportResult, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return CSVImportResult{}, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "diary.csv") {
			rc, err := f.Open()
			if err != nil {
				return CSVImportResult{}, err
			}
			defer rc.Close()
			return s.importLetterboxdCSV(ctx, rc)
		}
	}
	return CSVImportResult{}, errors.New("diary.csv not found in zip")
}

func (s *CSVService) importLetterboxdCSV(ctx context.Context, reader io.Reader) (CSVImportResult, error) {
	seen, err := s.loadExistingLogKeys(ctx)
	if err != nil {
		return CSVImportResult{}, err
	}
	r := csv.NewReader(reader)
	head, err := r.Read()
	if err != nil {
		return CSVImportResult{}, err
	}
	idx := map[string]int{}
	for i, h := range head {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	titleIdx := firstIndex(idx, "name", "title")
	dateIdx := firstIndex(idx, "watched date", "date")
	if titleIdx < 0 || dateIdx < 0 {
		return CSVImportResult{}, errors.New("expected Letterboxd diary headers (Name/Watched Date)")
	}
	ratingIdx := firstIndex(idx, "rating")
	rewatchIdx := firstIndex(idx, "rewatch")
	notesIdx := firstIndex(idx, "review", "tags")

	out := CSVImportResult{}
	line := 1
	for {
		line++
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		if dateIdx >= len(row) || titleIdx >= len(row) {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: malformed row", line))
			continue
		}
		loggedAt, err := parseLetterboxdDate(row[dateIdx])
		if err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: invalid watched date", line))
			continue
		}
		title := strings.TrimSpace(row[titleIdx])
		if title == "" {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: empty title", line))
			continue
		}
		var rating *float64
		if ratingIdx >= 0 && ratingIdx < len(row) {
			rating, err = parseLetterboxdRating(row[ratingIdx])
			if err != nil {
				out.Skipped++
				out.Errors = append(out.Errors, fmt.Sprintf("line %d: %v", line, err))
				continue
			}
		}
		rewatch := false
		if rewatchIdx >= 0 && rewatchIdx < len(row) {
			rv := strings.ToLower(strings.TrimSpace(row[rewatchIdx]))
			rewatch = rv == "yes" || rv == "true" || rv == "1"
		}
		var notes *string
		if notesIdx >= 0 && notesIdx < len(row) && strings.TrimSpace(row[notesIdx]) != "" {
			n := row[notesIdx]
			notes = &n
		}
		in := domain.AddLogInput{Title: title, LoggedAt: loggedAt, Rating: rating, Rewatch: rewatch, Notes: notes}
		k := logKeyFromInput(in)
		if _, ok := seen[k]; ok {
			out.Skipped++
			continue
		}
		if _, err := s.logs.Add(ctx, in); err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		seen[k] = struct{}{}
		out.Imported++
	}
	return out, nil
}

func (s *CSVService) importGenericCSV(ctx context.Context, reader io.Reader) (CSVImportResult, error) {
	seen, err := s.loadExistingLogKeys(ctx)
	if err != nil {
		return CSVImportResult{}, err
	}
	r := csv.NewReader(reader)
	head, err := r.Read()
	if err != nil {
		return CSVImportResult{}, err
	}
	idx := map[string]int{}
	for i, h := range head {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	required := []string{"logged_at", "title"}
	for _, k := range required {
		if _, ok := idx[k]; !ok {
			return CSVImportResult{}, fmt.Errorf("missing required header: %s", k)
		}
	}
	out := CSVImportResult{}
	line := 1
	for {
		line++
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		loggedAt, err := parseCSVDate(row[idx["logged_at"]])
		if err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: invalid logged_at", line))
			continue
		}
		title := row[idx["title"]]
		var rating *float64
		if i, ok := idx["rating"]; ok && i < len(row) && strings.TrimSpace(row[i]) != "" {
			v, err := strconv.ParseFloat(strings.TrimSpace(row[i]), 64)
			if err != nil {
				out.Skipped++
				out.Errors = append(out.Errors, fmt.Sprintf("line %d: invalid rating", line))
				continue
			}
			rating = &v
		}
		rewatch := false
		if i, ok := idx["rewatch"]; ok && i < len(row) {
			rv := strings.ToLower(strings.TrimSpace(row[i]))
			rewatch = rv == "1" || rv == "true" || rv == "yes"
		}
		var notes *string
		if i, ok := idx["notes"]; ok && i < len(row) && strings.TrimSpace(row[i]) != "" {
			n := row[i]
			notes = &n
		}
		in := domain.AddLogInput{Title: title, LoggedAt: loggedAt, Rating: rating, Rewatch: rewatch, Notes: notes}
		k := logKeyFromInput(in)
		if _, ok := seen[k]; ok {
			out.Skipped++
			continue
		}
		if _, err := s.logs.Add(ctx, in); err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		seen[k] = struct{}{}
		out.Imported++
	}
	return out, nil
}

func (s *CSVService) loadExistingLogKeys(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.logs.List(ctx, domain.ListFilter{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[logKeyFromLog(r)] = struct{}{}
	}
	return out, nil
}

func logKeyFromInput(in domain.AddLogInput) string {
	rating := ""
	if in.Rating != nil {
		rating = fmt.Sprintf("%.1f", *in.Rating)
	}
	notes := ""
	if in.Notes != nil {
		notes = strings.TrimSpace(*in.Notes)
	}
	title := strings.ToLower(strings.TrimSpace(in.Title))
	date := domain.NormalizeLocalDate(in.LoggedAt)
	return fmt.Sprintf("%s|%s|%s|%t|%s", title, date, rating, in.Rewatch, notes)
}

func logKeyFromLog(l domain.FilmLog) string {
	rating := ""
	if l.Rating != nil {
		rating = fmt.Sprintf("%.1f", *l.Rating)
	}
	notes := ""
	if l.Notes != nil {
		notes = strings.TrimSpace(*l.Notes)
	}
	title := strings.ToLower(strings.TrimSpace(l.Title))
	return fmt.Sprintf("%s|%s|%s|%t|%s", title, l.LocalDate, rating, l.Rewatch, notes)
}

func firstIndex(idx map[string]int, keys ...string) int {
	for _, k := range keys {
		if v, ok := idx[k]; ok {
			return v
		}
	}
	return -1
}

func (s *CSVService) Export(ctx context.Context, file string, year *int) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"logged_at", "title", "rating", "rewatch", "notes"}); err != nil {
		return err
	}
	filter := domain.ListFilter{}
	if year != nil {
		from := time.Date(*year, 1, 1, 0, 0, 0, 0, time.Local)
		to := time.Date(*year, 12, 31, 23, 59, 59, 0, time.Local)
		filter.From = &from
		filter.To = &to
	}
	logs, err := s.logs.List(ctx, filter)
	if err != nil {
		return err
	}
	for _, l := range logs {
		rating := ""
		if l.Rating != nil {
			rating = fmt.Sprintf("%.1f", *l.Rating)
		}
		notes := ""
		if l.Notes != nil {
			notes = *l.Notes
		}
		if err := w.Write([]string{l.LoggedAt.Format(time.RFC3339), l.Title, rating, strconv.FormatBool(l.Rewatch), notes}); err != nil {
			return err
		}
	}
	return nil
}
