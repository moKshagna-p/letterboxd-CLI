package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	store "film-heatmap/internal/store/sqlite"
)

func TestCSVImportExportRoundTrip(t *testing.T) {
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

	in := filepath.Join(dir, "in.csv")
	content := "logged_at,title,rating,rewatch,notes\n2026-02-19,Movie A,4.0,true,note\n2026-02-20,Movie B,4.5,false,\n"
	if err := os.WriteFile(in, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := csvSvc.Import(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || res.Skipped != 0 {
		t.Fatalf("unexpected import result: %+v", res)
	}
	out := filepath.Join(dir, "out.csv")
	year := 2026
	if err := csvSvc.Export(ctx, out, &year); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty export")
	}
}
