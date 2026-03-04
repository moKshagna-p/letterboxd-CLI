package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type staticRoundTripper func(req *http.Request) (*http.Response, error)

func (f staticRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestScrapeDiaryFallsBackToRSSOnForbiddenDiaryPage(t *testing.T) {
	d, err := NewLetterboxdDownloader()
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: staticRoundTripper(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Path, "/films/diary/"):
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Status:     "403 Forbidden",
					Body:       io.NopCloser(strings.NewReader("forbidden")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case strings.HasSuffix(req.URL.Path, "/rss/"):
				feed := `<rss><channel><item><letterboxd:filmTitle>Inception</letterboxd:filmTitle><letterboxd:watchedDate>2026-02-10</letterboxd:watchedDate><letterboxd:memberRating>4.5</letterboxd:memberRating><letterboxd:rewatch>no</letterboxd:rewatch><link>https://letterboxd.com/film/inception/</link></item></channel></rss>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body:       io.NopCloser(strings.NewReader(feed)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
		}),
	}

	rows, err := d.scrapeDiary(context.Background(), client, "MahaVeerudu")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one RSS row, got %d", len(rows))
	}
	if rows[0].Title != "Inception" || rows[0].Date != "2026-02-10" {
		t.Fatalf("unexpected RSS row: %+v", rows[0])
	}
}

func TestFetchHTMLReturnsStatusErrorWithCode(t *testing.T) {
	d, err := NewLetterboxdDownloader()
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: staticRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Status:     "403 Forbidden",
				Body:       io.NopCloser(strings.NewReader("forbidden")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	_, err = d.fetchHTML(context.Background(), client, "https://letterboxd.com/test/films/diary/")
	if err == nil {
		t.Fatal("expected fetchHTML to return an error")
	}
	var statusErr *httpStatusError
	if ok := strings.Contains(err.Error(), "403 Forbidden"); !ok {
		t.Fatalf("expected error to include response status, got: %v", err)
	}
	if !isRecoverableDiaryFetchError(err) {
		t.Fatalf("expected 403 to be recoverable for diary scraping, err=%v", err)
	}
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected typed status error, got: %T", err)
	}
	if statusErr.statusCode != http.StatusForbidden {
		t.Fatalf("expected status code 403, got %d", statusErr.statusCode)
	}
}
