package service

import (
	"context"
	"strings"

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

func (s *LibraryService) SyncWatchlistTitles(ctx context.Context, titles []string) (int, error) {
	existing, err := s.store.ListWatchlistItems(ctx)
	if err != nil {
		return 0, err
	}
	seen := map[string]struct{}{}
	for _, item := range existing {
		k := strings.ToLower(strings.TrimSpace(item.Title))
		if k != "" {
			seen[k] = struct{}{}
		}
	}
	added := 0
	for _, title := range titles {
		k := strings.ToLower(strings.TrimSpace(title))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		if _, err := s.store.AddWatchlistItem(ctx, domain.AddWatchlistInput{Title: strings.TrimSpace(title)}); err != nil {
			return added, err
		}
		seen[k] = struct{}{}
		added++
	}
	return added, nil
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
