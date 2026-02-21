package service

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"film-heatmap/internal/domain"
	store "film-heatmap/internal/store/sqlite"
)

type fakeDownloader struct {
	zipPath       string
	err           error
	validateErr   error
	sourceZipPath string
}

func (f *fakeDownloader) ValidateCredentials(ctx context.Context, creds LetterboxdCredentials) error {
	return f.validateErr
}

func (f *fakeDownloader) DownloadLatestExport(ctx context.Context, creds LetterboxdCredentials, outDir string) (DownloadedExport, error) {
	if f.err != nil {
		return DownloadedExport{}, f.err
	}
	in, err := os.Open(f.zipPath)
	if err != nil {
		return DownloadedExport{}, err
	}
	defer in.Close()
	out := filepath.Join(outDir, "letterboxd-export.zip")
	outF, err := os.Create(out)
	if err != nil {
		return DownloadedExport{}, err
	}
	defer outF.Close()
	if _, err := outF.ReadFrom(in); err != nil {
		return DownloadedExport{}, err
	}
	source := f.sourceZipPath
	if source == "" {
		source = f.zipPath
	}
	return DownloadedExport{ImportPath: out, SourcePath: source}, nil
}

func TestLetterboxdSyncServiceSyncAndImport(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("FILM_HEATMAP_CONFIG", cfgPath)
	t.Setenv("FILM_HEATMAP_AUTO_SYNC", "")

	db := filepath.Join(dir, "test.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs := NewLogService(st)
	csvSvc := NewCSVService(logs)
	cfgSvc, err := NewAppConfigService()
	if err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(dir, "export.zip")
	if err := writeDiaryZip(zipPath); err != nil {
		t.Fatal(err)
	}
	sourceZip := filepath.Join(dir, "downloaded-export.zip")
	if err := copyTestFile(zipPath, sourceZip); err != nil {
		t.Fatal(err)
	}

	syncSvc := NewLetterboxdSyncService(csvSvc, nil, cfgSvc, &fakeDownloader{zipPath: zipPath, sourceZipPath: sourceZip})
	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	syncSvc.now = func() time.Time { return now }

	if err := syncSvc.ActivateBrowserSession(time.Hour); err != nil {
		t.Fatal(err)
	}

	res, err := syncSvc.SyncAndImport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.ImportResult.Imported != 2 {
		t.Fatalf("expected imported=2 got %+v", res.ImportResult)
	}
	rows, err := logs.List(context.Background(), domain.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(rows))
	}
	cfg, err := cfgSvc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastExportHash == "" || cfg.LastSyncAt != now.Format(time.RFC3339) {
		t.Fatalf("unexpected config after sync: %+v", cfg)
	}
	if cfg.SyncCooldownUntil == "" {
		t.Fatalf("expected sync cooldown to be set")
	}
	if _, err := os.Stat(sourceZip); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source zip to be removed, err=%v", err)
	}
}

func TestLetterboxdSyncServiceSyncAndImportIfDue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("FILM_HEATMAP_CONFIG", cfgPath)
	t.Setenv("FILM_HEATMAP_AUTO_SYNC", "")

	db := filepath.Join(dir, "test.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs := NewLogService(st)
	csvSvc := NewCSVService(logs)
	cfgSvc, err := NewAppConfigService()
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "export.zip")
	if err := writeDiaryZip(zipPath); err != nil {
		t.Fatal(err)
	}
	syncSvc := NewLetterboxdSyncService(csvSvc, nil, cfgSvc, &fakeDownloader{zipPath: zipPath})
	base := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	syncSvc.now = func() time.Time { return base }

	if err := syncSvc.ActivateBrowserSession(time.Hour); err != nil {
		t.Fatal(err)
	}
	_, ran, err := syncSvc.SyncAndImportIfDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("expected first sync to run")
	}

	_, ran, err = syncSvc.SyncAndImportIfDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("expected second sync to be skipped by cooldown")
	}
}

func TestLetterboxdSyncServiceExpiredAuth(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("FILM_HEATMAP_CONFIG", cfgPath)

	db := filepath.Join(dir, "test.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs := NewLogService(st)
	csvSvc := NewCSVService(logs)
	cfgSvc, err := NewAppConfigService()
	if err != nil {
		t.Fatal(err)
	}
	syncSvc := NewLetterboxdSyncService(csvSvc, nil, cfgSvc, &fakeDownloader{})
	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	syncSvc.now = func() time.Time { return now }

	if err := syncSvc.ActivateBrowserSession(10 * time.Minute); err != nil {
		t.Fatal(err)
	}
	syncSvc.now = func() time.Time { return now.Add(11 * time.Minute) }

	if _, err := syncSvc.SyncAndImport(context.Background()); err == nil {
		t.Fatal("expected error when auth expired")
	}
	status, err := syncSvc.CredentialStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != "expired" {
		t.Fatalf("expected expired status, got %q", status)
	}
}

func copyTestFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Close()
}

func writeDiaryZip(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("diary.csv")
	if err != nil {
		_ = zw.Close()
		return err
	}
	csv := "Date,Name,Year,Letterboxd URI,Rating,Rewatch,Tags,Watched Date\n2026-02-10,Inception,2010,url,4.5,No,,2026-02-10\n2026-02-11,Dune,2021,url,★★★★,Yes,,2026-02-11\n"
	if _, err := w.Write([]byte(csv)); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}
