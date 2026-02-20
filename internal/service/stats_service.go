package service

import (
	"context"
	"sort"
	"time"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/store/sqlite"
)

type StatsService struct {
	store *sqlite.Store
}

func NewStatsService(store *sqlite.Store) *StatsService {
	return &StatsService{store: store}
}

func (s *StatsService) Year(ctx context.Context, year int) (domain.YearStats, error) {
	counts, err := s.store.DailyCounts(ctx, year)
	if err != nil {
		return domain.YearStats{}, err
	}
	total := 0
	days := make([]string, 0, len(counts))
	for d, c := range counts {
		total += c
		days = append(days, d)
	}
	sort.Strings(days)
	longest := 0
	current := 0
	prev := ""
	for _, d := range days {
		if prev == "" {
			current = 1
		} else {
			pd, _ := time.Parse(domain.DateLayout, prev)
			cd, _ := time.Parse(domain.DateLayout, d)
			if pd.AddDate(0, 0, 1).Equal(cd) {
				current++
			} else {
				current = 1
			}
		}
		if current > longest {
			longest = current
		}
		prev = d
	}
	curStreak := 0
	today := time.Now().In(time.Local).Format(domain.DateLayout)
	probe, _ := time.Parse(domain.DateLayout, today)
	for {
		if counts[probe.Format(domain.DateLayout)] <= 0 {
			break
		}
		curStreak++
		probe = probe.AddDate(0, 0, -1)
	}
	return domain.YearStats{Year: year, TotalLogs: total, ActiveDays: len(counts), LongestStreak: longest, CurrentStreak: curStreak}, nil
}
