package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"film-heatmap/internal/domain"
)

func (s *Store) AddWatchlistItem(ctx context.Context, input domain.AddWatchlistInput) (domain.WatchlistItem, error) {
	if err := domain.ValidateTitle(input.Title); err != nil {
		return domain.WatchlistItem{}, err
	}
	id := NewID()
	notesVal := "NULL"
	if input.Notes != nil {
		notesVal = q(*input.Notes)
	}
	addedAt := time.Now().UTC().Format(time.RFC3339)
	sql := fmt.Sprintf(`
INSERT INTO watchlist_items(id, profile_id, title, notes, added_at)
VALUES(%s, %s, %s, %s, %s);
`, q(id), q(s.profileID), q(strings.TrimSpace(input.Title)), notesVal, q(addedAt))
	if err := s.exec(ctx, sql); err != nil {
		return domain.WatchlistItem{}, err
	}
	return s.GetWatchlistItem(ctx, id)
}

func (s *Store) GetWatchlistItem(ctx context.Context, id string) (domain.WatchlistItem, error) {
	sql := fmt.Sprintf(`
SELECT id, profile_id, title, COALESCE(notes, ''), added_at
FROM watchlist_items
WHERE id = %s AND profile_id = %s
LIMIT 1;
`, q(id), q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return domain.WatchlistItem{}, err
	}
	if len(rows) == 0 {
		return domain.WatchlistItem{}, errors.New("watchlist item not found")
	}
	return parseWatchlistRow(rows[0])
}

func (s *Store) ListWatchlistItems(ctx context.Context) ([]domain.WatchlistItem, error) {
	sql := fmt.Sprintf(`
SELECT id, profile_id, title, COALESCE(notes, ''), added_at
FROM watchlist_items
WHERE profile_id = %s
ORDER BY added_at DESC;
`, q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]domain.WatchlistItem, 0, len(rows))
	for _, row := range rows {
		v, err := parseWatchlistRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) DeleteWatchlistItem(ctx context.Context, id string) error {
	sql := fmt.Sprintf(`DELETE FROM watchlist_items WHERE id = %s AND profile_id = %s;`, q(id), q(s.profileID))
	return s.exec(ctx, sql)
}

func parseWatchlistRow(row []string) (domain.WatchlistItem, error) {
	if len(row) < 5 {
		return domain.WatchlistItem{}, errors.New("invalid watchlist row")
	}
	addedAt, err := time.Parse(time.RFC3339, row[4])
	if err != nil {
		return domain.WatchlistItem{}, err
	}
	var notes *string
	if strings.TrimSpace(row[3]) != "" {
		n := row[3]
		notes = &n
	}
	return domain.WatchlistItem{
		ID:        row[0],
		ProfileID: row[1],
		Title:     row[2],
		Notes:     notes,
		AddedAt:   addedAt,
	}, nil
}

func (s *Store) AddFilmList(ctx context.Context, input domain.AddFilmListInput) (domain.FilmList, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.FilmList{}, errors.New("list name is required")
	}
	id := NewID()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	sql := fmt.Sprintf(`
INSERT INTO film_lists(id, profile_id, name, created_at)
VALUES(%s, %s, %s, %s);
`, q(id), q(s.profileID), q(name), q(createdAt))
	if err := s.exec(ctx, sql); err != nil {
		return domain.FilmList{}, err
	}
	return s.GetFilmList(ctx, id)
}

func (s *Store) GetFilmList(ctx context.Context, id string) (domain.FilmList, error) {
	sql := fmt.Sprintf(`
SELECT id, profile_id, name, created_at
FROM film_lists
WHERE id = %s AND profile_id = %s
LIMIT 1;
`, q(id), q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return domain.FilmList{}, err
	}
	if len(rows) == 0 {
		return domain.FilmList{}, errors.New("list not found")
	}
	return parseFilmListRow(rows[0])
}

func (s *Store) ListFilmLists(ctx context.Context) ([]domain.FilmList, error) {
	sql := fmt.Sprintf(`
SELECT id, profile_id, name, created_at
FROM film_lists
WHERE profile_id = %s
ORDER BY created_at DESC;
`, q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FilmList, 0, len(rows))
	for _, row := range rows {
		v, err := parseFilmListRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseFilmListRow(row []string) (domain.FilmList, error) {
	if len(row) < 4 {
		return domain.FilmList{}, errors.New("invalid list row")
	}
	createdAt, err := time.Parse(time.RFC3339, row[3])
	if err != nil {
		return domain.FilmList{}, err
	}
	return domain.FilmList{
		ID:        row[0],
		ProfileID: row[1],
		Name:      row[2],
		CreatedAt: createdAt,
	}, nil
}

func (s *Store) AddFilmListItem(ctx context.Context, input domain.AddFilmListItemInput) (domain.FilmListItem, error) {
	if strings.TrimSpace(input.ListID) == "" {
		return domain.FilmListItem{}, errors.New("list id is required")
	}
	if err := domain.ValidateTitle(input.Title); err != nil {
		return domain.FilmListItem{}, err
	}
	if _, err := s.GetFilmList(ctx, input.ListID); err != nil {
		return domain.FilmListItem{}, err
	}
	position, err := s.nextListItemPosition(ctx, input.ListID)
	if err != nil {
		return domain.FilmListItem{}, err
	}
	id := NewID()
	notesVal := "NULL"
	if input.Notes != nil {
		notesVal = q(*input.Notes)
	}
	addedAt := time.Now().UTC().Format(time.RFC3339)
	sql := fmt.Sprintf(`
INSERT INTO film_list_items(id, list_id, title, notes, position, added_at)
VALUES(%s, %s, %s, %s, %d, %s);
`, q(id), q(input.ListID), q(strings.TrimSpace(input.Title)), notesVal, position, q(addedAt))
	if err := s.exec(ctx, sql); err != nil {
		return domain.FilmListItem{}, err
	}
	return s.GetFilmListItem(ctx, id)
}

func (s *Store) nextListItemPosition(ctx context.Context, listID string) (int, error) {
	sql := fmt.Sprintf(`
SELECT COALESCE(CAST(MAX(position) AS TEXT), '0')
FROM film_list_items
WHERE list_id = %s;
`, q(listID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return 1, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(rows[0][0]))
	if err != nil {
		return 0, err
	}
	return v + 1, nil
}

func (s *Store) GetFilmListItem(ctx context.Context, id string) (domain.FilmListItem, error) {
	sql := fmt.Sprintf(`
SELECT i.id, i.list_id, i.title, COALESCE(i.notes, ''), CAST(i.position AS TEXT), i.added_at
FROM film_list_items i
JOIN film_lists l ON l.id = i.list_id
WHERE i.id = %s AND l.profile_id = %s
LIMIT 1;
`, q(id), q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return domain.FilmListItem{}, err
	}
	if len(rows) == 0 {
		return domain.FilmListItem{}, errors.New("list item not found")
	}
	return parseFilmListItemRow(rows[0])
}

func (s *Store) ListFilmListItems(ctx context.Context, listID string) ([]domain.FilmListItem, error) {
	sql := fmt.Sprintf(`
SELECT i.id, i.list_id, i.title, COALESCE(i.notes, ''), CAST(i.position AS TEXT), i.added_at
FROM film_list_items i
JOIN film_lists l ON l.id = i.list_id
WHERE i.list_id = %s AND l.profile_id = %s
ORDER BY i.position ASC;
`, q(listID), q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FilmListItem, 0, len(rows))
	for _, row := range rows {
		v, err := parseFilmListItemRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseFilmListItemRow(row []string) (domain.FilmListItem, error) {
	if len(row) < 6 {
		return domain.FilmListItem{}, errors.New("invalid list item row")
	}
	position, err := strconv.Atoi(row[4])
	if err != nil {
		return domain.FilmListItem{}, err
	}
	addedAt, err := time.Parse(time.RFC3339, row[5])
	if err != nil {
		return domain.FilmListItem{}, err
	}
	var notes *string
	if strings.TrimSpace(row[3]) != "" {
		n := row[3]
		notes = &n
	}
	return domain.FilmListItem{
		ID:       row[0],
		ListID:   row[1],
		Title:    row[2],
		Notes:    notes,
		Position: position,
		AddedAt:  addedAt,
	}, nil
}
