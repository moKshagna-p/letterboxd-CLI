package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultAuthSessionTTL = time.Hour
	defaultSyncCooldown   = 0 * time.Second
)

type LetterboxdSyncService struct {
	csvSvc *CSVService
	lib    *LibraryService
	config *AppConfigService
	dl     LetterboxdExportDownloader
	now    func() time.Time
}

type LetterboxdCredentials struct {
	Username string
	Password string
}

type DownloadedExport struct {
	ImportPath string
	SourcePath string
}

type LetterboxdExportDownloader interface {
	DownloadLatestExport(ctx context.Context, creds LetterboxdCredentials, outDir string) (DownloadedExport, error)
	ValidateCredentials(ctx context.Context, creds LetterboxdCredentials) error
}

type SyncImportResult struct {
	ImportResult   CSVImportResult
	ExportHash     string
	WatchlistAdded int
}

func NewLetterboxdSyncService(csvSvc *CSVService, lib *LibraryService, config *AppConfigService, dl LetterboxdExportDownloader) *LetterboxdSyncService {
	return &LetterboxdSyncService{csvSvc: csvSvc, lib: lib, config: config, dl: dl, now: time.Now}
}

func (s *LetterboxdSyncService) Enabled() (bool, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return false, err
	}
	return cfg.AutoSyncEnabled, nil
}

func (s *LetterboxdSyncService) SetEnabled(enabled bool) error {
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	cfg.AutoSyncEnabled = enabled
	return s.config.Save(cfg)
}

func (s *LetterboxdSyncService) ActivateBrowserSession(ttl time.Duration) error {
	if ttl <= 0 {
		ttl = defaultAuthSessionTTL
	}
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	cfg.BrowserAuthEnabled = true
	cfg.AuthExpiresAt = s.now().Add(ttl).UTC().Format(time.RFC3339)
	return s.config.Save(cfg)
}

func (s *LetterboxdSyncService) InvalidateBrowserSession() error {
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	cfg.BrowserAuthEnabled = false
	cfg.AuthExpiresAt = ""
	cfg.LetterboxdPassword = ""
	return s.config.Save(cfg)
}

func (s *LetterboxdSyncService) SetCredentials(creds LetterboxdCredentials) error {
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	cfg.LetterboxdUsername = strings.TrimSpace(creds.Username)
	cfg.LetterboxdPassword = creds.Password
	if cfg.LetterboxdUsername != "" && cfg.LetterboxdPassword != "" {
		cfg.BrowserAuthEnabled = true
		cfg.AuthExpiresAt = ""
	}
	return s.config.Save(cfg)
}

func (s *LetterboxdSyncService) SetUsername(username string) error {
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	cfg.LetterboxdUsername = strings.TrimSpace(username)
	return s.config.Save(cfg)
}

func (s *LetterboxdSyncService) Credentials() (LetterboxdCredentials, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return LetterboxdCredentials{}, err
	}
	return LetterboxdCredentials{
		Username: strings.TrimSpace(cfg.LetterboxdUsername),
		Password: cfg.LetterboxdPassword,
	}, nil
}

func (s *LetterboxdSyncService) Username() (string, error) {
	creds, err := s.Credentials()
	if err != nil {
		return "", err
	}
	return creds.Username, nil
}

func (s *LetterboxdSyncService) CredentialStatus() (string, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return "", err
	}
	credsSet := strings.TrimSpace(cfg.LetterboxdUsername) != "" && strings.TrimSpace(cfg.LetterboxdPassword) != ""
	if !credsSet && !cfg.BrowserAuthEnabled {
		return "not_logged_in", nil
	}
	if credsSet {
		return "credentials_configured", nil
	}
	exp, ok := parseRFC3339(cfg.AuthExpiresAt)
	if !ok || !exp.After(s.now()) {
		return "expired", nil
	}
	return "browser_authenticated", nil
}

func (s *LetterboxdSyncService) AuthExpiresAt() (*time.Time, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return nil, err
	}
	exp, ok := parseRFC3339(cfg.AuthExpiresAt)
	if !ok {
		return nil, nil
	}
	return &exp, nil
}

func (s *LetterboxdSyncService) SyncCooldownUntil() (*time.Time, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return nil, err
	}
	until, ok := parseRFC3339(cfg.SyncCooldownUntil)
	if !ok {
		return nil, nil
	}
	return &until, nil
}

func (s *LetterboxdSyncService) ValidateCredentials(ctx context.Context, creds LetterboxdCredentials) error {
	if s.dl == nil {
		return errors.New("downloader not configured")
	}
	return s.dl.ValidateCredentials(ctx, creds)
}

func (s *LetterboxdSyncService) SyncAndImport(ctx context.Context) (SyncImportResult, error) {
	return s.syncAndImport(ctx, true)
}

func (s *LetterboxdSyncService) SyncAndImportIfDue(ctx context.Context) (SyncImportResult, bool, error) {
	res, err := s.syncAndImport(ctx, false)
	if err != nil {
		if errors.Is(err, errSyncNotDue) {
			return SyncImportResult{}, false, nil
		}
		return SyncImportResult{}, false, err
	}
	return res, true, nil
}

var errSyncNotDue = errors.New("sync cooldown active")

func (s *LetterboxdSyncService) syncAndImport(ctx context.Context, ignoreCooldown bool) (SyncImportResult, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return SyncImportResult{}, err
	}
	creds := LetterboxdCredentials{
		Username: strings.TrimSpace(cfg.LetterboxdUsername),
		Password: cfg.LetterboxdPassword,
	}
	if strings.TrimSpace(creds.Username) == "" || strings.TrimSpace(creds.Password) == "" {
		return SyncImportResult{}, errors.New("letterboxd credentials missing; run `auth login`")
	}
	if !ignoreCooldown {
		if until, ok := parseRFC3339(cfg.SyncCooldownUntil); ok && until.After(s.now()) {
			return SyncImportResult{}, errSyncNotDue
		}
	}
	if s.dl == nil {
		return SyncImportResult{}, errors.New("downloader not configured")
	}
	workDir, err := os.MkdirTemp("", "film-heatmap-sync-")
	if err != nil {
		return SyncImportResult{}, err
	}
	defer os.RemoveAll(workDir)

	var dlRes DownloadedExport
	for attempt := 1; attempt <= 2; attempt++ {
		dlRes, err = s.dl.DownloadLatestExport(ctx, creds, workDir)
		if err == nil {
			break
		}
		if attempt == 2 {
			return SyncImportResult{}, fmt.Errorf("download export: %w", err)
		}
	}

	hash, err := fileSHA256(dlRes.ImportPath)
	if err != nil {
		return SyncImportResult{}, err
	}
	cfg, err = s.config.Load()
	if err != nil {
		return SyncImportResult{}, err
	}
	if cfg.LastExportHash != "" && cfg.LastExportHash == hash {
		cfg.SyncCooldownUntil = s.now().Add(defaultSyncCooldown).UTC().Format(time.RFC3339)
		if err := s.config.Save(cfg); err != nil {
			return SyncImportResult{}, err
		}
		_ = removeSourceZip(dlRes.SourcePath)
		watchAdded, _ := s.syncWatchlist(ctx, dlRes.ImportPath, strings.TrimSpace(cfg.LetterboxdUsername))
		return SyncImportResult{ImportResult: CSVImportResult{}, ExportHash: hash, WatchlistAdded: watchAdded}, nil
	}

	res, err := s.csvSvc.ImportLetterboxd(ctx, dlRes.ImportPath)
	if err != nil {
		return SyncImportResult{}, err
	}
	cfg.LastSyncAt = s.now().UTC().Format(time.RFC3339)
	cfg.LastExportHash = hash
	cfg.SyncCooldownUntil = s.now().Add(defaultSyncCooldown).UTC().Format(time.RFC3339)
	cfg.LastImportedZipPath = ""
	if err := s.config.Save(cfg); err != nil {
		return SyncImportResult{}, err
	}
	watchAdded, _ := s.syncWatchlist(ctx, dlRes.ImportPath, strings.TrimSpace(cfg.LetterboxdUsername))
	_ = removeSourceZip(dlRes.SourcePath)
	return SyncImportResult{ImportResult: res, ExportHash: hash, WatchlistAdded: watchAdded}, nil
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func parseRFC3339(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func isAuthValidAt(cfg AppConfig, now time.Time) bool {
	if !cfg.BrowserAuthEnabled {
		return false
	}
	exp, ok := parseRFC3339(cfg.AuthExpiresAt)
	if !ok {
		return false
	}
	return exp.After(now)
}

func removeSourceZip(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LetterboxdSyncService) syncWatchlist(ctx context.Context, zipPath string, username string) (int, error) {
	if s.lib == nil {
		return 0, nil
	}
	var titles []string
	var err error
	if strings.EqualFold(filepath.Ext(zipPath), ".zip") {
		titles, err = parseWatchlistFromExportZip(zipPath)
		if err != nil {
			return 0, err
		}
	}
	if len(titles) == 0 && username != "" {
		titles, _ = scrapeLetterboxdWatchlist(username)
	}
	if len(titles) == 0 {
		return 0, nil
	}
	return s.lib.SyncWatchlistTitles(ctx, titles)
}

func parseWatchlistFromExportZip(zipPath string) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if name != "watchlist.csv" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return parseWatchlistCSV(rc)
	}
	return nil, nil
}

func parseWatchlistCSV(r io.Reader) ([]string, error) {
	cr := csv.NewReader(r)
	head, err := cr.Read()
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, h := range head {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "name" || h == "title" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}
	var titles []string
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if idx >= len(row) {
			continue
		}
		title := strings.TrimSpace(row[idx])
		if title != "" {
			titles = append(titles, title)
		}
	}
	return uniqueTitles(titles), nil
}

func scrapeLetterboxdWatchlist(username string) ([]string, error) {
	u := fmt.Sprintf("https://letterboxd.com/%s/watchlist/", strings.TrimSpace(username))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "film-heatmap/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("watchlist page: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	html := string(b)
	re := regexp.MustCompile(`data-film-name=\"([^\"]+)\"`)
	matches := re.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		t := strings.TrimSpace(m[1])
		if t != "" {
			out = append(out, t)
		}
	}
	return uniqueTitles(out), nil
}

func uniqueTitles(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		k := strings.ToLower(strings.TrimSpace(t))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, strings.TrimSpace(t))
	}
	return out
}
