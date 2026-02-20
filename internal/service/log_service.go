package service

import (
	"context"
	"time"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/store/sqlite"
)

type LogService struct {
	store *sqlite.Store
}

func NewLogService(store *sqlite.Store) *LogService {
	return &LogService{store: store}
}

func (s *LogService) Add(ctx context.Context, input domain.AddLogInput) (domain.FilmLog, error) {
	if input.LoggedAt.IsZero() {
		input.LoggedAt = time.Now()
	}
	return s.store.AddLog(ctx, input)
}

func (s *LogService) List(ctx context.Context, filter domain.ListFilter) ([]domain.FilmLog, error) {
	return s.store.ListLogs(ctx, filter)
}

func (s *LogService) Edit(ctx context.Context, input domain.UpdateLogInput) (domain.FilmLog, error) {
	return s.store.UpdateLog(ctx, input)
}

func (s *LogService) Delete(ctx context.Context, id string) error {
	return s.store.DeleteLog(ctx, id)
}
