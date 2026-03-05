package app

import (
	"strings"
	"testing"

	"film-heatmap/internal/domain"
)

func TestTotalFilmsInWeeksUsesCount(t *testing.T) {
	weeks := [][]domain.HeatmapCell{
		{
			{Date: "2026-01-01", Count: 3, Intensity: 2},
			{Date: "2026-01-02", Count: 1, Intensity: 1},
		},
		{
			{Date: "2026-01-03", Count: 4, Intensity: 4},
		},
	}
	got := totalFilmsInWeeks(weeks)
	if got != 8 {
		t.Fatalf("expected total film count 8, got %d", got)
	}
}

func TestBuildMonthHeaderSkipsTruncatedLabels(t *testing.T) {
	weeks := [][]domain.HeatmapCell{
		{{Date: "2026-01-04"}}, // Jan
		{{Date: "2026-01-11"}},
		{{Date: "2026-01-18"}},
		{{Date: "2026-01-25"}},
		{{Date: "2026-02-01"}}, // Feb
		{{Date: "2026-02-08"}},
		{{Date: "2026-02-15"}},
		{{Date: "2026-03-01"}}, // Mar would truncate in 8-week output
	}
	got := buildMonthHeader(weeks)
	if !strings.Contains(got, "Jan") || !strings.Contains(got, "Feb") {
		t.Fatalf("expected month header to include Jan and Feb, got %q", got)
	}
	if strings.Contains(got, "Mar") || strings.HasSuffix(got, "Ma") {
		t.Fatalf("expected month header to avoid truncated Mar label, got %q", got)
	}
}
