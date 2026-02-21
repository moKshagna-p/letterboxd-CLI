package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppConfigLoadSaveAndMarkSync(t *testing.T) {
	t.Setenv("FILM_HEATMAP_AUTO_SYNC", "")
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("FILM_HEATMAP_CONFIG", path)

	svc, err := NewAppConfigService()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoSyncEnabled {
		t.Fatal("expected auto sync default true")
	}

	cfg.AutoSyncEnabled = false
	cfg.CredentialsRef = "local:credentials.json"
	if err := svc.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AutoSyncEnabled {
		t.Fatal("expected saved auto sync false")
	}
	if loaded.CredentialsRef == "" {
		t.Fatal("expected credentials ref to persist")
	}

	stamp := time.Date(2026, 2, 21, 12, 30, 0, 0, time.UTC)
	if err := svc.MarkSync("abc123", stamp); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.LastExportHash != "abc123" {
		t.Fatalf("unexpected hash %q", after.LastExportHash)
	}
	if after.LastSyncAt != stamp.Format(time.RFC3339) {
		t.Fatalf("unexpected sync time %q", after.LastSyncAt)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty config file")
	}
}

func TestAppConfigEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("FILM_HEATMAP_CONFIG", path)
	t.Setenv("FILM_HEATMAP_AUTO_SYNC", "false")

	svc, err := NewAppConfigService()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(AppConfig{AutoSyncEnabled: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoSyncEnabled {
		t.Fatal("expected env var override to disable auto sync")
	}
}
