package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"film-heatmap/internal/domain"
	store "film-heatmap/internal/store/sqlite"
)

func TestImportLetterboxdDiaryCSV(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs := NewLogService(st)
	csvSvc := NewCSVService(logs)
	ctx := context.Background()

	in := filepath.Join(dir, "diary.csv")
	content := "Date,Name,Year,Letterboxd URI,Rating,Rewatch,Tags,Watched Date\n2026-02-10,Inception,2010,url,4.5,No,,2026-02-10\n2026-02-11,Dune,2021,url,★★★★,Yes,,2026-02-11\n"
	if err := os.WriteFile(in, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := csvSvc.ImportLetterboxd(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || res.Skipped != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	rows, err := logs.List(ctx, domain.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	res2, err := csvSvc.ImportLetterboxd(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Imported != 0 {
		t.Fatalf("expected second import to be idempotent, got %+v", res2)
	}
	rows2, err := logs.List(ctx, domain.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 2 {
		t.Fatalf("expected 2 rows after second import, got %d", len(rows2))
	}
}
