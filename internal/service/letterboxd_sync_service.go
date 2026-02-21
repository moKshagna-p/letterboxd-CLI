package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"
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

type LetterboxdExportDownloader interface {
	DownloadLatestExport(ctx context.Context, creds LetterboxdCredentials, outDir string) (string, error)
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

func (s *LetterboxdSyncService) SetBrowserAuthConfigured(enabled bool) error {
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	cfg.BrowserAuthEnabled = enabled
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
	return "browser_authenticated", nil
}

func (s *LetterboxdSyncService) ValidateCredentials(ctx context.Context, creds LetterboxdCredentials) error {
	if s.dl == nil {
		return errors.New("downloader not configured")
	}
	return s.dl.ValidateCredentials(ctx, creds)
}

func (s *LetterboxdSyncService) SyncAndImport(ctx context.Context) (SyncImportResult, error) {
	cfg, err := s.config.Load()
	if err != nil {
		return SyncImportResult{}, err
	}
	if !cfg.BrowserAuthEnabled {
		return SyncImportResult{}, errors.New("browser auth is not configured; run `auth login` first")
	}
	if s.dl == nil {
		return SyncImportResult{}, errors.New("downloader not configured")
	}
	tmpDir, err := os.MkdirTemp("", "film-heatmap-sync-")
	if err != nil {
		return SyncImportResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	var exportPath string
	for attempt := 1; attempt <= 2; attempt++ {
		exportPath, err = s.dl.DownloadLatestExport(ctx, LetterboxdCredentials{}, tmpDir)
		if err == nil {
			break
		}
		if attempt == 2 {
			return SyncImportResult{}, fmt.Errorf("download export: %w", err)
		}
	}

	hash, err := fileSHA256(exportPath)
	if err != nil {
		return SyncImportResult{}, err
	}
	res, err := s.csvSvc.ImportLetterboxd(ctx, exportPath)
	if err != nil {
		return SyncImportResult{}, err
	}
	if err := s.config.MarkSync(hash, s.now()); err != nil {
		return SyncImportResult{}, err
	}
	cfg, err = s.config.Load()
	if err != nil {
		return SyncImportResult{}, err
	}
	cfg.LastImportedZipPath = exportPath
	if err := s.config.Save(cfg); err != nil {
		return SyncImportResult{}, err
	}
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
