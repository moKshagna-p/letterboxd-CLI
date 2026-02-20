package domain

import "time"

type WatchlistItem struct {
	ID        string
	ProfileID string
	Title     string
	Notes     *string
	AddedAt   time.Time
}

type AddWatchlistInput struct {
	Title string
	Notes *string
}

type FilmList struct {
	ID        string
	ProfileID string
	Name      string
	CreatedAt time.Time
}

type AddFilmListInput struct {
	Name string
}

type FilmListItem struct {
	ID       string
	ListID   string
	Title    string
	Notes    *string
	Position int
	AddedAt  time.Time
}

type AddFilmListItemInput struct {
	ListID string
	Title  string
	Notes  *string
}
