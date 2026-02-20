package domain

import (
	"errors"
	"strings"
	"time"
)

const DateLayout = "2006-01-02"

type FilmLog struct {
	ID        string
	ProfileID string
	Title     string
	LoggedAt  time.Time
	LocalDate string
	Rating    *float64
	Rewatch   bool
	Notes     *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AddLogInput struct {
	Title    string
	LoggedAt time.Time
	Rating   *float64
	Rewatch  bool
	Notes    *string
}

type UpdateLogInput struct {
	ID       string
	Title    *string
	LoggedAt *time.Time
	Rating   *float64
	Rewatch  *bool
	Notes    *string
}

type ListFilter struct {
	From  *time.Time
	To    *time.Time
	Title string
}

func NormalizeLocalDate(t time.Time) string {
	return t.In(time.Local).Format(DateLayout)
}

func ValidateTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("title is required")
	}
	return nil
}

func ValidateRating(rating *float64) error {
	if rating == nil {
		return nil
	}
	v := *rating
	if v < 0.5 || v > 5.0 {
		return errors.New("rating must be between 0.5 and 5.0")
	}
	step := v * 2
	if step != float64(int(step)) {
		return errors.New("rating must be in 0.5 increments")
	}
	return nil
}
