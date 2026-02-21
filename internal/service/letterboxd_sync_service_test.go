package service

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"film-heatmap/internal/domain"
	store "film-heatmap/internal/store/sqlite"
)

type fakeDownloader struct {
	zipPath string
	err     error
}

func (f *fakeDownloader) ValidateCredentials(ctx context.Context, creds LetterboxdCredentials) error {
	return nil
}

func (f *fakeDownloader) DownloadLatestExport(ctx context.Context, creds LetterboxdCredentials, outDir string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	in, err := os.Open(f.zipPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out := filepath.Join(outDir, "letterboxd-export.zip")
	outF, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer outF.Close()
	if _, err := outF.ReadFrom(in); err != nil {
		return "", err
	}
	return out, nil
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
	cfg, err := cfgSvc.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.BrowserAuthEnabled = true
	if err := cfgSvc.Save(cfg); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(dir, "export.zip")
	if err := writeDiaryZip(zipPath); err != nil {
		t.Fatal(err)
	}
	syncSvc := NewLetterboxdSyncService(csvSvc, cfgSvc, &fakeDownloader{zipPath: zipPath})
	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	syncSvc.now = func() time.Time { return now }

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
	cfg, err = cfgSvc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastExportHash == "" || cfg.LastSyncAt != now.Format(time.RFC3339) {
		t.Fatalf("unexpected config after sync: %+v", cfg)
	}
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
