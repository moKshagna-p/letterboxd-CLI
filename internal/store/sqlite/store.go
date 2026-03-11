package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"film-heatmap/internal/domain"
	_ "modernc.org/sqlite"
)

const initSQL = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS profiles (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS film_logs (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL,
  title TEXT NOT NULL,
  logged_at TEXT NOT NULL,
  local_date TEXT NOT NULL,
  rating REAL,
  rewatch INTEGER NOT NULL DEFAULT 0,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(profile_id) REFERENCES profiles(id)
);
CREATE INDEX IF NOT EXISTS idx_film_logs_profile_local_date ON film_logs(profile_id, local_date);
CREATE INDEX IF NOT EXISTS idx_film_logs_profile_logged_at ON film_logs(profile_id, logged_at);
CREATE INDEX IF NOT EXISTS idx_film_logs_profile_title ON film_logs(profile_id, title);
CREATE TABLE IF NOT EXISTS watchlist_items (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL,
  title TEXT NOT NULL,
  notes TEXT,
  added_at TEXT NOT NULL,
  FOREIGN KEY(profile_id) REFERENCES profiles(id)
);
CREATE INDEX IF NOT EXISTS idx_watchlist_items_profile_added_at ON watchlist_items(profile_id, added_at DESC);
CREATE TABLE IF NOT EXISTS film_lists (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(profile_id) REFERENCES profiles(id)
);
CREATE INDEX IF NOT EXISTS idx_film_lists_profile_name ON film_lists(profile_id, name);
CREATE TABLE IF NOT EXISTS film_list_items (
  id TEXT PRIMARY KEY,
  list_id TEXT NOT NULL,
  title TEXT NOT NULL,
  notes TEXT,
  position INTEGER NOT NULL,
  added_at TEXT NOT NULL,
  FOREIGN KEY(list_id) REFERENCES film_lists(id)
);
CREATE INDEX IF NOT EXISTS idx_film_list_items_list_position ON film_list_items(list_id, position);
`

type Store struct {
	db        *sql.DB
	dbPath    string
	profileID string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	st := &Store{db: db, dbPath: path, profileID: "default"}
	if err := st.exec(context.Background(), initSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := st.ensureDefaultProfile(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ensureDefaultProfile(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	sql := fmt.Sprintf(`
INSERT INTO profiles(id, display_name, created_at)
VALUES(%s, %s, %s)
ON CONFLICT(id) DO NOTHING;
`, q(s.profileID), q("Local User"), q(now))
	return s.exec(ctx, sql)
}

func (s *Store) AddLog(ctx context.Context, input domain.AddLogInput) (domain.FilmLog, error) {
	if err := domain.ValidateTitle(input.Title); err != nil {
		return domain.FilmLog{}, err
	}
	if err := domain.ValidateRating(input.Rating); err != nil {
		return domain.FilmLog{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := NewID()
	ratingVal := "NULL"
	if input.Rating != nil {
		ratingVal = fmt.Sprintf("%.1f", *input.Rating)
	}
	notesVal := "NULL"
	if input.Notes != nil {
		notesVal = q(*input.Notes)
	}
	sql := fmt.Sprintf(`
INSERT INTO film_logs(id, profile_id, title, logged_at, local_date, rating, rewatch, notes, created_at, updated_at)
VALUES(%s, %s, %s, %s, %s, %s, %d, %s, %s, %s);
`,
		q(id),
		q(s.profileID),
		q(input.Title),
		q(input.LoggedAt.UTC().Format(time.RFC3339)),
		q(domain.NormalizeLocalDate(input.LoggedAt)),
		ratingVal,
		boolToInt(input.Rewatch),
		notesVal,
		q(now),
		q(now),
	)
	if err := s.exec(ctx, sql); err != nil {
		return domain.FilmLog{}, err
	}
	return s.GetLog(ctx, id)
}

func (s *Store) GetLog(ctx context.Context, id string) (domain.FilmLog, error) {
	sql := fmt.Sprintf(`
SELECT id, profile_id, title, logged_at, local_date,
COALESCE(CAST(rating AS TEXT), ''),
CAST(rewatch AS TEXT),
COALESCE(notes, ''),
created_at, updated_at
FROM film_logs
WHERE id = %s AND profile_id = %s
LIMIT 1;
`, q(id), q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return domain.FilmLog{}, err
	}
	if len(rows) == 0 {
		return domain.FilmLog{}, errors.New("log not found")
	}
	return parseLogRow(rows[0])
}

func (s *Store) ListLogs(ctx context.Context, filter domain.ListFilter) ([]domain.FilmLog, error) {
	where := []string{fmt.Sprintf("profile_id = %s", q(s.profileID))}
	if filter.From != nil {
		where = append(where, fmt.Sprintf("logged_at >= %s", q(filter.From.UTC().Format(time.RFC3339))))
	}
	if filter.To != nil {
		where = append(where, fmt.Sprintf("logged_at <= %s", q(filter.To.UTC().Format(time.RFC3339))))
	}
	if filter.Title != "" {
		where = append(where, fmt.Sprintf("title LIKE %s", q("%"+filter.Title+"%")))
	}
	sql := fmt.Sprintf(`
SELECT id, profile_id, title, logged_at, local_date,
COALESCE(CAST(rating AS TEXT), ''),
CAST(rewatch AS TEXT),
COALESCE(notes, ''),
created_at, updated_at
FROM film_logs
WHERE %s
ORDER BY logged_at DESC;
`, strings.Join(where, " AND "))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return nil, err
	}
	logs := make([]domain.FilmLog, 0, len(rows))
	for _, row := range rows {
		v, err := parseLogRow(row)
		if err != nil {
			return nil, err
		}
		logs = append(logs, v)
	}
	return logs, nil
}

func (s *Store) UpdateLog(ctx context.Context, input domain.UpdateLogInput) (domain.FilmLog, error) {
	current, err := s.GetLog(ctx, input.ID)
	if err != nil {
		return domain.FilmLog{}, err
	}
	title := current.Title
	if input.Title != nil {
		title = *input.Title
	}
	loggedAt := current.LoggedAt
	if input.LoggedAt != nil {
		loggedAt = *input.LoggedAt
	}
	rating := current.Rating
	if input.Rating != nil {
		rating = input.Rating
	}
	rewatch := current.Rewatch
	if input.Rewatch != nil {
		rewatch = *input.Rewatch
	}
	notes := current.Notes
	if input.Notes != nil {
		notes = input.Notes
	}
	if err := domain.ValidateTitle(title); err != nil {
		return domain.FilmLog{}, err
	}
	if err := domain.ValidateRating(rating); err != nil {
		return domain.FilmLog{}, err
	}
	ratingVal := "NULL"
	if rating != nil {
		ratingVal = fmt.Sprintf("%.1f", *rating)
	}
	notesVal := "NULL"
	if notes != nil {
		notesVal = q(*notes)
	}
	sql := fmt.Sprintf(`
UPDATE film_logs
SET title = %s,
logged_at = %s,
local_date = %s,
rating = %s,
rewatch = %d,
notes = %s,
updated_at = %s
WHERE id = %s AND profile_id = %s;
`,
		q(title),
		q(loggedAt.UTC().Format(time.RFC3339)),
		q(domain.NormalizeLocalDate(loggedAt)),
		ratingVal,
		boolToInt(rewatch),
		notesVal,
		q(time.Now().UTC().Format(time.RFC3339)),
		q(input.ID),
		q(s.profileID),
	)
	if err := s.exec(ctx, sql); err != nil {
		return domain.FilmLog{}, err
	}
	return s.GetLog(ctx, input.ID)
}

func (s *Store) DeleteLog(ctx context.Context, id string) error {
	sql := fmt.Sprintf(`DELETE FROM film_logs WHERE id = %s AND profile_id = %s;`, q(id), q(s.profileID))
	return s.exec(ctx, sql)
}

func (s *Store) DedupeLogs(ctx context.Context) (int, error) {
	before, err := s.logCount(ctx)
	if err != nil {
		return 0, err
	}
	sql := fmt.Sprintf(`
DELETE FROM film_logs
WHERE profile_id = %s
  AND EXISTS (
    SELECT 1
    FROM film_logs prev
    WHERE prev.profile_id = film_logs.profile_id
      AND LOWER(TRIM(prev.title)) = LOWER(TRIM(film_logs.title))
      AND prev.local_date = film_logs.local_date
      AND COALESCE(CAST(prev.rating AS TEXT), '') = COALESCE(CAST(film_logs.rating AS TEXT), '')
      AND prev.rewatch = film_logs.rewatch
      AND COALESCE(TRIM(prev.notes), '') = COALESCE(TRIM(film_logs.notes), '')
      AND (prev.created_at < film_logs.created_at OR (prev.created_at = film_logs.created_at AND prev.id < film_logs.id))
  );
`, q(s.profileID))
	if err := s.exec(ctx, sql); err != nil {
		return 0, err
	}
	after, err := s.logCount(ctx)
	if err != nil {
		return 0, err
	}
	if before < after {
		return 0, nil
	}
	return before - after, nil
}

func (s *Store) DailyCounts(ctx context.Context, year int) (map[string]int, error) {
	start := fmt.Sprintf("%d-01-01", year)
	end := fmt.Sprintf("%d-12-31", year)
	return s.DailyCountsBetween(ctx, start, end)
}

func (s *Store) DailyCountsBetween(ctx context.Context, start string, end string) (map[string]int, error) {
	sql := fmt.Sprintf(`
SELECT local_date, CAST(COUNT(*) AS TEXT)
FROM film_logs
WHERE profile_id = %s AND local_date >= %s AND local_date <= %s
GROUP BY local_date;
`, q(s.profileID), q(start), q(end))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		c, err := strconv.Atoi(row[1])
		if err != nil {
			return nil, err
		}
		out[row[0]] = c
	}
	return out, nil
}

func (s *Store) logCount(ctx context.Context) (int, error) {
	sql := fmt.Sprintf(`SELECT CAST(COUNT(*) AS TEXT) FROM film_logs WHERE profile_id = %s;`, q(s.profileID))
	rows, err := s.query(ctx, sql)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return 0, nil
	}
	return strconv.Atoi(rows[0][0])
}

func (s *Store) exec(ctx context.Context, sql string) error {
	if _, err := s.db.ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("sqlite exec failed: %w", err)
	}
	return nil
}

func (s *Store) query(ctx context.Context, sql string) ([][]string, error) {
	rows, err := s.db.QueryContext(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("sqlite query failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([][]string, 0)
	for rows.Next() {
		values := make([]any, len(cols))
		scanTargets := make([]any, len(cols))
		for i := range values {
			scanTargets[i] = &values[i]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range values {
			row[i] = stringifySQLiteValue(v)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func stringifySQLiteValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(t)
	}
}

func parseLogRow(row []string) (domain.FilmLog, error) {
	if len(row) < 10 {
		return domain.FilmLog{}, fmt.Errorf("invalid row shape: %d", len(row))
	}
	logged, err := time.Parse(time.RFC3339, row[3])
	if err != nil {
		return domain.FilmLog{}, err
	}
	created, err := time.Parse(time.RFC3339, row[8])
	if err != nil {
		return domain.FilmLog{}, err
	}
	updated, err := time.Parse(time.RFC3339, row[9])
	if err != nil {
		return domain.FilmLog{}, err
	}
	var rating *float64
	if row[5] != "" {
		v, err := strconv.ParseFloat(row[5], 64)
		if err != nil {
			return domain.FilmLog{}, err
		}
		rating = &v
	}
	var notes *string
	if row[7] != "" {
		n := row[7]
		notes = &n
	}
	return domain.FilmLog{
		ID:        row[0],
		ProfileID: row[1],
		Title:     row[2],
		LoggedAt:  logged,
		LocalDate: row[4],
		Rating:    rating,
		Rewatch:   row[6] == "1",
		Notes:     notes,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func q(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}
