package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"film-heatmap/internal/domain"
	store "film-heatmap/internal/store/sqlite"
)

func TestHeatmapIntensityBuckets(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs := NewLogService(st)
	heat := NewHeatmapService(st)
	ctx := context.Background()
	target := time.Date(2026, 2, 20, 12, 0, 0, 0, time.Local)
	for range 4 {
		if _, err := logs.Add(ctx, domain.AddLogInput{Title: "A", LoggedAt: target}); err != nil {
			t.Fatal(err)
		}
	}
	hm, err := heat.Year(ctx, 2026)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range hm.Weeks {
		for _, c := range w {
			if c.Date == "2026-02-20" {
				found = true
				if c.Intensity != 4 {
					t.Fatalf("expected intensity 4, got %d", c.Intensity)
				}
			}
		}
	}
	if !found {
		t.Fatal("date not found in heatmap")
	}
}
