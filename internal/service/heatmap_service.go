package service

import (
	"context"
	"time"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/store/sqlite"
)

type HeatmapService struct {
	store *sqlite.Store
}

func NewHeatmapService(store *sqlite.Store) *HeatmapService {
	return &HeatmapService{store: store}
}

func intensity(count int) int {
	switch {
	case count <= 0:
		return 0
	case count == 1:
		return 1
	case count == 2:
		return 2
	case count == 3:
		return 3
	default:
		return 4
	}
}

func (s *HeatmapService) Year(ctx context.Context, year int) (domain.HeatmapMatrix, error) {
	counts, err := s.store.DailyCounts(ctx, year)
	if err != nil {
		return domain.HeatmapMatrix{}, err
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
	for start.Weekday() != time.Sunday {
		start = start.AddDate(0, 0, -1)
	}
	end := time.Date(year, 12, 31, 0, 0, 0, 0, time.Local)
	for end.Weekday() != time.Saturday {
		end = end.AddDate(0, 0, 1)
	}

	weeks := [][]domain.HeatmapCell{}
	week := make([]domain.HeatmapCell, 0, 7)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format(domain.DateLayout)
		count := counts[date]
		week = append(week, domain.HeatmapCell{Date: date, Count: count, Intensity: intensity(count)})
		if len(week) == 7 {
			weeks = append(weeks, week)
			week = make([]domain.HeatmapCell, 0, 7)
		}
	}
	return domain.HeatmapMatrix{
		Year:  year,
		Weeks: weeks,
		Start: start.Format(domain.DateLayout),
		End:   end.Format(domain.DateLayout),
	}, nil
}

func (s *HeatmapService) RecentWeeks(ctx context.Context, weeksCount int, now time.Time) (domain.HeatmapMatrix, error) {
	if weeksCount <= 0 {
		weeksCount = 53
	}
	end := now.In(time.Local)
	for end.Weekday() != time.Saturday {
		end = end.AddDate(0, 0, 1)
	}
	start := end.AddDate(0, 0, -(weeksCount*7)+1)
	for start.Weekday() != time.Sunday {
		start = start.AddDate(0, 0, -1)
	}
	counts, err := s.store.DailyCountsBetween(ctx, start.Format(domain.DateLayout), end.Format(domain.DateLayout))
	if err != nil {
		return domain.HeatmapMatrix{}, err
	}
	weeks := [][]domain.HeatmapCell{}
	week := make([]domain.HeatmapCell, 0, 7)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format(domain.DateLayout)
		count := counts[date]
		week = append(week, domain.HeatmapCell{Date: date, Count: count, Intensity: intensity(count)})
		if len(week) == 7 {
			weeks = append(weeks, week)
			week = make([]domain.HeatmapCell, 0, 7)
		}
	}
	return domain.HeatmapMatrix{
		Year:  now.Year(),
		Weeks: weeks,
		Start: start.Format(domain.DateLayout),
		End:   end.Format(domain.DateLayout),
	}, nil
}
