package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultAuthSessionTTL = time.Hour
	defaultSyncCooldown   = time.Hour
)

type LetterboxdSyncService struct {
	csvSvc *CSVService
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
	ImportResult CSVImportResult
	ExportHash   string
}

func NewLetterboxdSyncService(csvSvc *CSVService, config *AppConfigService, dl LetterboxdExportDownloader) *LetterboxdSyncService {
	return &LetterboxdSyncService{csvSvc: csvSvc, config: config, dl: dl, now: time.Now}
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
	return s.config.Save(cfg)
}

func (s *LetterboxdSyncService) CredentialStatus() (string, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return "", err
	}
	if !cfg.BrowserAuthEnabled {
		return "not_logged_in", nil
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
	if !isAuthValidAt(cfg, s.now()) {
		return SyncImportResult{}, errors.New("browser auth is missing or expired; run `auth login`")
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
		dlRes, err = s.dl.DownloadLatestExport(ctx, LetterboxdCredentials{}, workDir)
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
		return SyncImportResult{ImportResult: CSVImportResult{}, ExportHash: hash}, nil
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
	_ = removeSourceZip(dlRes.SourcePath)
	return SyncImportResult{ImportResult: res, ExportHash: hash}, nil
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
