package service

import (
	"context"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/store/sqlite"
)

type LibraryService struct {
	store *sqlite.Store
}

func NewLibraryService(store *sqlite.Store) *LibraryService {
	return &LibraryService{store: store}
}

func (s *LibraryService) AddWatchlist(ctx context.Context, input domain.AddWatchlistInput) (domain.WatchlistItem, error) {
	return s.store.AddWatchlistItem(ctx, input)
}

func (s *LibraryService) ListWatchlist(ctx context.Context) ([]domain.WatchlistItem, error) {
	return s.store.ListWatchlistItems(ctx)
}

func (s *LibraryService) RemoveWatchlist(ctx context.Context, id string) error {
	return s.store.DeleteWatchlistItem(ctx, id)
}

func (s *LibraryService) AddList(ctx context.Context, input domain.AddFilmListInput) (domain.FilmList, error) {
	return s.store.AddFilmList(ctx, input)
}

func (s *LibraryService) ListLists(ctx context.Context) ([]domain.FilmList, error) {
	return s.store.ListFilmLists(ctx)
}

func (s *LibraryService) AddListItem(ctx context.Context, input domain.AddFilmListItemInput) (domain.FilmListItem, error) {
	return s.store.AddFilmListItem(ctx, input)
}

func (s *LibraryService) ListListItems(ctx context.Context, listID string) ([]domain.FilmListItem, error) {
	return s.store.ListFilmListItems(ctx, listID)
}
