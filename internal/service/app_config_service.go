package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AppConfig struct {
	AutoSyncEnabled     bool   `json:"auto_sync_enabled"`
	BrowserAuthEnabled  bool   `json:"browser_auth_enabled"`
	AuthExpiresAt       string `json:"auth_expires_at,omitempty"`
	SyncCooldownUntil   string `json:"sync_cooldown_until,omitempty"`
	CredentialsRef      string `json:"credentials_ref,omitempty"`
	LastSyncAt          string `json:"last_sync_at,omitempty"`
	LastExportHash      string `json:"last_export_hash,omitempty"`
	LastImportedZipPath string `json:"last_imported_zip_path,omitempty"`
}

type AppConfigService struct {
	path string
}

func NewAppConfigService() (*AppConfigService, error) {
	p, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	return &AppConfigService{path: p}, nil
}

func resolveConfigPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("FILM_HEATMAP_CONFIG")); p != "" {
		return p, nil
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "film-heatmap", "config.json"), nil
}

func (s *AppConfigService) ConfigPath() string {
	return s.path
}

func (s *AppConfigService) Load() (AppConfig, error) {
	out := AppConfig{AutoSyncEnabled: true}
	applyAutoSyncEnv := func() {
		if env := strings.TrimSpace(os.Getenv("FILM_HEATMAP_AUTO_SYNC")); env != "" {
			out.AutoSyncEnabled = strings.EqualFold(env, "1") || strings.EqualFold(env, "true") || strings.EqualFold(env, "yes")
		}
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			applyAutoSyncEnv()
			return out, nil
		}
		return AppConfig{}, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		applyAutoSyncEnv()
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return AppConfig{}, err
	}
	applyAutoSyncEnv()
	return out, nil
}

func (s *AppConfigService) Save(cfg AppConfig) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func (s *AppConfigService) MarkSync(lastExportHash string, at time.Time) error {
	cfg, err := s.Load()
	if err != nil {
		return err
	}
	cfg.LastSyncAt = at.Format(time.RFC3339)
	cfg.LastExportHash = lastExportHash
	return s.Save(cfg)
}
