package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNoNewExportFound = errors.New("no diary entries found while scraping")
)

type LetterboxdDownloader struct {
	userAgent string
}

type httpStatusError struct {
	target     string
	status     string
	statusCode int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("request failed %s: %s", e.target, e.status)
}

type scrapedDiaryEntry struct {
	Date     string
	Title    string
	Year     string
	FilmPath string
	Rating   string
	Rewatch  bool
	Notes    string
}

type LetterboxdSnapshot struct {
	Watched   []string
	Reviews   []string
	Watchlist []string
	Lists     []string
	Tags      []string
}

func NewLetterboxdDownloader() (*LetterboxdDownloader, error) {
	return &LetterboxdDownloader{
		// Browser-like UA avoids bot filtering that rejects custom tool identifiers.
		userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 13_7_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36",
	}, nil
}

func (d *LetterboxdDownloader) DownloadLatestExport(ctx context.Context, creds LetterboxdCredentials, outDir string) (DownloadedExport, error) {
	if err := validateScrapeCreds(creds); err != nil {
		return DownloadedExport{}, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return DownloadedExport{}, err
	}

	client, err := d.newClient()
	if err != nil {
		return DownloadedExport{}, err
	}
	if _, err := d.loginOrFallback(ctx, client, creds); err != nil {
		return DownloadedExport{}, err
	}

	fmt.Printf("[Downloader] Scraping diary (watched films)...\n")
	entries, err := d.scrapeDiary(ctx, client, creds.Username)
	if err != nil {
		fmt.Printf("[Downloader] Diary scrape error: %v\n", err)
		return DownloadedExport{}, err
	}
	if len(entries) == 0 {
		fmt.Printf("[Downloader] No diary entries found\n")
		return DownloadedExport{}, ErrNoNewExportFound
	}
	fmt.Printf("[Downloader] Scraped %d diary entries\n", len(entries))

	out := filepath.Join(outDir, "letterboxd-diary-scrape.csv")
	if err := writeScrapedDiaryCSV(out, entries); err != nil {
		return DownloadedExport{}, err
	}
	
	// Also scrape and save watchlist
	username := strings.TrimSpace(creds.Username)
	fmt.Printf("[Downloader] Scraping watchlist...\n")
	watchlist, err := d.scrapeSimpleTitleList(ctx, client, fmt.Sprintf("https://letterboxd.com/%s/watchlist/", username), []string{
		`data-film-name="([^"]+)"`,
		`<img[^>]*alt="([^"]+)"[^>]*class="[^"]*image[^"]*"`,
		`<img[^>]*alt="([^"]+)"[^>]*>`,
	})
	if err != nil {
		fmt.Printf("[Downloader] Watchlist scrape error: %v\n", err)
	} else if len(watchlist) > 0 {
		watchlistOut := filepath.Join(outDir, "watchlist-scrape.csv")
		if err := writeScrapedWatchlistCSV(watchlistOut, watchlist); err != nil {
			fmt.Printf("[Downloader] Failed to write watchlist CSV: %v\n", err)
		}
	}
	
	// Also scrape and save lists with content
	fmt.Printf("[Downloader] Scraping lists...\n")
	lists, err := d.scrapeListsDetailed(ctx, client, username)
	if err != nil {
		fmt.Printf("[Downloader] Lists scrape error: %v\n", err)
	} else if len(lists) > 0 {
		listsOut := filepath.Join(outDir, "lists-scrape.csv")
		if err := writeScrapedListsDetailedCSV(listsOut, lists); err != nil {
			fmt.Printf("[Downloader] Failed to write lists CSV: %v\n", err)
		}
	}
	
	return DownloadedExport{ImportPath: out, SourcePath: ""}, nil
}

type scrapedList struct {
	Name string
	Slug string
	Films []string
}

func (d *LetterboxdDownloader) scrapeListsDetailed(ctx context.Context, client *http.Client, username string) ([]scrapedList, error) {
	target := fmt.Sprintf("https://letterboxd.com/%s/lists/", username)
	fmt.Printf("[Scraper] Fetching lists from: %s\n", target)
	page, err := d.fetchHTML(ctx, client, target)
	if err != nil {
		fmt.Printf("[Scraper] Lists fetch error: %v\n", err)
		return nil, err
	}
	
	// Pattern to find list links and names: <h2 class="name prettify"><a href="/username/list/slug/">Name</a></h2>
	// Flexible pattern for h1-h6 and various classes (title, name, etc.)
	re := regexp.MustCompile(`(?is)<h[1-6][^>]*class="[^"]*(?:title|name)[^"]*"[^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(page, -1)
	
	if len(matches) == 0 {
		// Look for ANY link that looks like a list link
		re = regexp.MustCompile(`(?is)<a[^>]*href="([^"]*/list/[^"]+/)"[^>]*>(.*?)</a>`)
		matches = re.FindAllStringSubmatch(page, -1)
	}

	var lists []scrapedList
	seenSlugs := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		slug := strings.TrimSpace(m[1])
		name := strings.TrimSpace(stripHTML(m[2]))
		
		if !strings.Contains(slug, "/list/") || seenSlugs[slug] {
			continue
		}
		
		// Filter out common non-list links that might contain /list/
		if strings.Contains(slug, "/lists/by/") || strings.Contains(slug, "/lists/with/") {
			continue
		}
		seenSlugs[slug] = true
		
		fmt.Printf("[Scraper] Scraping list: %s (%s)\n", name, slug)
		// Small delay to avoid rate limit
		time.Sleep(500 * time.Millisecond)
		
		films, err := d.scrapeListItems(ctx, client, "https://letterboxd.com"+slug)
		if err != nil {
			fmt.Printf("[Scraper] Error scraping items for list %s: %v\n", name, err)
			continue
		}
		lists = append(lists, scrapedList{Name: name, Slug: slug, Films: films})
	}
	return lists, nil
}

func (d *LetterboxdDownloader) scrapeListItems(ctx context.Context, client *http.Client, listURL string) ([]string, error) {
	var films []string
	next := listURL
	for pageNum := 0; pageNum < 50 && next != ""; pageNum++ {
		page, err := d.fetchHTML(ctx, client, next)
		if err != nil {
			return films, err
		}
		
		// Try multiple patterns for film names
		patterns := []string{
			`data-film-name="([^"]+)"`,
			`class="frame-title">([^<]+)</span>`,
			`alt="([^"]+)" class="image"`,
		}
		
		for _, p := range patterns {
			re := regexp.MustCompile(p)
			matches := re.FindAllStringSubmatch(page, -1)
			for _, m := range matches {
				if len(m) >= 2 {
					films = append(films, html.UnescapeString(m[1]))
				}
			}
		}
		
		next = findNextPageURL(next, page)
		if next != "" {
			time.Sleep(300 * time.Millisecond)
		}
	}
	return uniqueTitles(films), nil
}

func writeScrapedListsDetailedCSV(path string, lists []scrapedList) error {
	fmt.Printf("[Scraper] Writing %d detailed lists to CSV: %s\n", len(lists), path)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	// We'll use a format: ListName, FilmTitle
	if err := w.Write([]string{"ListName", "FilmTitle"}); err != nil {
		return err
	}
	for _, l := range lists {
		for _, film := range l.Films {
			if err := w.Write([]string{l.Name, film}); err != nil {
				return err
			}
		}
		// Also write an empty entry if the list is empty just to record the name
		if len(l.Films) == 0 {
			if err := w.Write([]string{l.Name, ""}); err != nil {
				return err
			}
		}
	}
	w.Flush()
	return w.Error()
}


func (d *LetterboxdDownloader) ValidateCredentials(ctx context.Context, creds LetterboxdCredentials) error {
	if err := validateScrapeCreds(creds); err != nil {
		return err
	}
	client, err := d.newClient()
	if err != nil {
		return err
	}
	if _, err := d.loginOrFallback(ctx, client, creds); err != nil {
		return err
	}
	return nil
}

func (d *LetterboxdDownloader) ScrapeSnapshot(ctx context.Context, creds LetterboxdCredentials) (LetterboxdSnapshot, error) {
	if err := validateScrapeCreds(creds); err != nil {
		return LetterboxdSnapshot{}, err
	}
	client, err := d.newClient()
	if err != nil {
		return LetterboxdSnapshot{}, err
	}
	if _, err := d.loginOrFallback(ctx, client, creds); err != nil {
		return LetterboxdSnapshot{}, err
	}
	username := strings.TrimSpace(creds.Username)

	entries, err := d.scrapeDiary(ctx, client, username)
	if err != nil {
		return LetterboxdSnapshot{}, err
	}
	watched := make([]string, 0, len(entries))
	tagSet := map[string]struct{}{}
	for _, e := range entries {
		if e.Title == "" || e.Date == "" {
			continue
		}
		rating := "-"
		if strings.TrimSpace(e.Rating) != "" {
			rating = strings.TrimSpace(e.Rating)
		}
		rewatch := ""
		if e.Rewatch {
			rewatch = " | rewatch"
		}
		watched = append(watched, fmt.Sprintf("%s | %s | rating:%s%s", e.Date, e.Title, rating, rewatch))
	}

	reviews, _ := d.scrapeSimpleTitleList(ctx, client, fmt.Sprintf("https://letterboxd.com/%s/films/reviews/", username), []string{
		`data-film-name="([^"]+)"`,
		`<h2[^>]*class="[^"]*headline[^"]*"[^>]*>\s*<a[^>]*>(.*?)</a>`,
		`<img[^>]*alt="([^"]+)"[^>]*>`,
	})
	watchlist, _ := d.scrapeSimpleTitleList(ctx, client, fmt.Sprintf("https://letterboxd.com/%s/watchlist/", username), []string{
		`data-film-name="([^"]+)"`,
		`<img[^>]*alt="([^"]+)"[^>]*class="[^"]*image[^"]*"`,
		`<img[^>]*alt="([^"]+)"[^>]*>`,
	})
	lists, _ := d.scrapeSimpleTitleList(ctx, client, fmt.Sprintf("https://letterboxd.com/%s/lists/", username), []string{
		`<h2[^>]*class="[^"]*title[^"]*"[^>]*>\s*<a[^>]*>(.*?)</a>`,
		`<a[^>]*href="/%s/list/[^"]+"[^>]*>(.*?)</a>`,
	})
	tagRaw, _ := d.scrapeSimpleTitleList(ctx, client, fmt.Sprintf("https://letterboxd.com/%s/tags/", username), []string{
		`/%s/tag/([^"/]+)"`,
		`/tag/([^"/]+)"`,
	})
	for _, t := range tagRaw {
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if t == "" {
			continue
		}
		tagSet[strings.ToLower(t)] = struct{}{}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sortStrings(tags)
	return LetterboxdSnapshot{
		Watched:   uniqueTitles(watched),
		Reviews:   uniqueTitles(reviews),
		Watchlist: uniqueTitles(watchlist),
		Lists:     uniqueTitles(lists),
		Tags:      uniqueTitles(tags),
	}, nil
}

func (d *LetterboxdDownloader) newClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}, nil
}

func (d *LetterboxdDownloader) scrapeSimpleTitleList(ctx context.Context, client *http.Client, target string, patterns []string) ([]string, error) {
	items := make([]string, 0, 64)
	next := target
	fmt.Printf("[Scraper] Scraping: %s\n", target)
	for pageNum := 0; pageNum < 20 && next != ""; pageNum++ {
		page, err := d.fetchHTML(ctx, client, next)
		if err != nil {
			fmt.Printf("[Scraper] Scraping %s page %d: fetch error\n", target, pageNum)
			return uniqueTitles(items), err
		}
		pageItems := 0
		for _, p := range patterns {
			pat := p
			if strings.Contains(pat, "%s") {
				u, _ := url.Parse(target)
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				user := ""
				if len(parts) > 0 {
					user = regexp.QuoteMeta(parts[0])
				}
				pat = fmt.Sprintf(pat, user)
			}
			re := regexp.MustCompile(`(?is)` + pat)
			matches := re.FindAllStringSubmatch(page, -1)
			for _, m := range matches {
				if len(m) < 2 {
					continue
				}
				v := strings.TrimSpace(stripHTML(m[1]))
				v = strings.Trim(v, "/")
				if v == "" {
					continue
				}
				if strings.EqualFold(v, "letterboxd") || strings.EqualFold(v, "show all") {
					continue
				}
				items = append(items, v)
				pageItems++
			}
		}
		fmt.Printf("[Scraper] %s page %d: found %d items (total: %d)\n", target, pageNum, pageItems, len(items))
		next = findNextPageURL(next, page)
	}
	fmt.Printf("[Scraper] %s: final total %d items\n", target, len(uniqueTitles(items)))
	return uniqueTitles(items), nil
}

func sortStrings(items []string) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (d *LetterboxdDownloader) loginOrFallback(ctx context.Context, client *http.Client, creds LetterboxdCredentials) (bool, error) {
	if err := d.login(ctx, client, creds); err == nil {
		return true, nil
	}
	ok, pubErr := d.canReadPublicProfile(ctx, client, creds.Username)
	if pubErr != nil {
		return false, pubErr
	}
	if !ok {
		return false, errors.New("unable to authenticate and public profile is not accessible")
	}
	return false, nil
}

func (d *LetterboxdDownloader) login(ctx context.Context, client *http.Client, creds LetterboxdCredentials) error {
	signInURL := "https://letterboxd.com/sign-in/"
	signInHTML, err := d.fetchHTML(ctx, client, signInURL)
	if err != nil {
		return err
	}
	formAction, userField, passField, formValues := parseLoginForm(signInHTML)
	formValues.Set(userField, creds.Username)
	formValues.Set(passField, creds.Password)

	loginURL, err := resolveURL(signInURL, formAction)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(formValues.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://letterboxd.com")
	req.Header.Set("Referer", signInURL)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("login failed: %s", resp.Status)
	}

	ok, err := d.hasSessionCookie(client)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("invalid letterboxd credentials")
	}
	return nil
}

func (d *LetterboxdDownloader) scrapeDiary(ctx context.Context, client *http.Client, username string) ([]scrapedDiaryEntry, error) {
	base := fmt.Sprintf("https://letterboxd.com/%s/films/diary/", strings.TrimSpace(username))
	next := base
	seen := map[string]struct{}{}
	all := make([]scrapedDiaryEntry, 0, 256)

	for page := 0; page < 200 && next != ""; page++ {
		htmlPage, err := d.fetchHTML(ctx, client, next)
		if err != nil {
			if isRecoverableDiaryFetchError(err) {
				fmt.Printf("[Scraper] Diary page %d: recoverable error, stopping pagination\n", page)
				break
			}
			return nil, err
		}
		entries := parseDiaryEntriesFromHTML(htmlPage)
		fmt.Printf("[Scraper] Diary page %d: found %d entries\n", page, len(entries))
		for _, e := range entries {
			if e.Date == "" || strings.TrimSpace(e.Title) == "" {
				continue
			}
			k := strings.ToLower(e.Date + "|" + e.Title + "|" + e.FilmPath)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			all = append(all, e)
		}
		next = findNextPageURL(next, htmlPage)
		if next != "" {
			fmt.Printf("[Scraper] Diary page %d: found next page link\n", page)
		}
	}
	
	if len(all) > 0 {
		fmt.Printf("[Scraper] Diary: scraped %d total entries, now fetching reviews for notes...\n", len(all))
		// Fetch review snippets to enrich diary entries
		reviewsMap := d.scrapeReviews(ctx, client, username)
		if len(reviewsMap) > 0 {
			for i := range all {
				key := fmt.Sprintf("%s|%s", all[i].Title, all[i].Date)
				if note, ok := reviewsMap[key]; ok {
					all[i].Notes = note
				}
			}
		}
		return all, nil
	}

	fmt.Printf("[Scraper] Diary scraping found no pages, falling back to RSS\n")
	return d.scrapeDiaryRSS(ctx, client, username)
}

func (d *LetterboxdDownloader) scrapeReviews(ctx context.Context, client *http.Client, username string) map[string]string {
	out := make(map[string]string)
	next := fmt.Sprintf("https://letterboxd.com/%s/films/reviews/", username)
	
	// We only check the first 5 pages of reviews to avoid excessive requests, 
	// assuming recent diary entries are most likely to have reviews.
	for pageNum := 0; pageNum < 5 && next != ""; pageNum++ {
		page, err := d.fetchHTML(ctx, client, next)
		if err != nil {
			break
		}
		
		// Each review block is usually in a <div class="film-detail-content">
		re := regexp.MustCompile(`(?is)<div[^>]*film-detail-content[^>]*>(.*?)</div>\s*</div>`)
		matches := re.FindAllStringSubmatch(page, -1)
		
		foundOnPage := 0
		for _, m := range matches {
			block := m[1]
			title := ""
			// Title is usually in a h2 or h3 link
			if tm := regexp.MustCompile(`(?is)<h[1-6][^>]*>\s*<a[^>]*>(.*?)</a>`).FindStringSubmatch(block); len(tm) >= 2 {
				title = strings.TrimSpace(stripHTML(tm[1]))
			}
			
			date := ""
			// Logged date is in data-viewing-date-str
			if dm := regexp.MustCompile(`\bdata-viewing-date-str="([0-9]{4}-[0-9]{2}-[0-9]{2})"`).FindStringSubmatch(block); len(dm) >= 2 {
				date = dm[1]
			}
			
			review := ""
			// Review body is in class body-text
			if rm := regexp.MustCompile(`(?is)<div[^>]*body-text[^>]*>(.*?)</div>`).FindStringSubmatch(block); len(rm) >= 2 {
				review = strings.TrimSpace(stripHTML(rm[1]))
			}
			
			if title != "" && date != "" && review != "" {
				key := fmt.Sprintf("%s|%s", title, date)
				out[key] = review
				foundOnPage++
			}
		}
		
		fmt.Printf("[Scraper] Reviews page %d: found %d review snippets\n", pageNum, foundOnPage)
		next = findNextPageURL(next, page)
		if next != "" {
			time.Sleep(300 * time.Millisecond)
		}
	}
	return out
}

func (d *LetterboxdDownloader) scrapeDiaryRSS(ctx context.Context, client *http.Client, username string) ([]scrapedDiaryEntry, error) {
	u := fmt.Sprintf("https://letterboxd.com/%s/rss/", strings.TrimSpace(username))
	fmt.Printf("[Scraper] Falling back to RSS: %s\n", u)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", d.userAgent)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[Scraper] RSS fetch error: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Printf("[Scraper] RSS HTTP %s\n", resp.Status)
		return nil, fmt.Errorf("rss fetch failed: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil, err
	}
	entries := parseDiaryEntriesFromRSS(string(b))
	fmt.Printf("[Scraper] RSS: scraped %d entries\n", len(entries))
	return entries, nil
}

func (d *LetterboxdDownloader) fetchHTML(ctx context.Context, client *http.Client, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://letterboxd.com/")
	req.Header.Set("Connection", "keep-alive")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpStatusError{target: target, status: resp.Status, statusCode: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isRecoverableDiaryFetchError(err error) bool {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	switch statusErr.statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	default:
		return false
	}
}

func writeScrapedDiaryCSV(path string, rows []scrapedDiaryEntry) error {
	fmt.Printf("[Scraper] Writing %d scraped entries to CSV: %s\n", len(rows), path)
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("[Scraper] Failed to create CSV file: %v\n", err)
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	// Letterboxd format: Date, Name, Year, Letterboxd URI, Rating, Rewatch, Tags, Watched Date, Review
	if err := w.Write([]string{"Date", "Name", "Year", "Letterboxd URI", "Rating", "Rewatch", "Tags", "Watched Date", "Review"}); err != nil {
		fmt.Printf("[Scraper] Failed to write CSV header: %v\n", err)
		return err
	}
	validRows := 0
	for _, r := range rows {
		if r.Title == "" || r.Date == "" {
			fmt.Printf("[Scraper] Skipping invalid entry: title='%s' date='%s'\n", r.Title, r.Date)
			continue
		}
		rewatch := "No"
		if r.Rewatch {
			rewatch = "Yes"
		}
		uri := ""
		if r.FilmPath != "" {
			uri = "https://letterboxd.com" + r.FilmPath
		}
		if err := w.Write([]string{r.Date, r.Title, r.Year, uri, r.Rating, rewatch, "", r.Date, r.Notes}); err != nil {
			fmt.Printf("[Scraper] Failed to write row: %v\n", err)
			return err
		}
		validRows++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Printf("[Scraper] CSV writer error: %v\n", err)
		return err
	}
	fmt.Printf("[Scraper] Successfully wrote %d/%d entries to CSV\n", validRows, len(rows))
	return nil
}

func writeScrapedWatchlistCSV(path string, titles []string) error {
	fmt.Printf("[Scraper] Writing %d watchlist items to CSV: %s\n", len(titles), path)
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("[Scraper] Failed to create watchlist CSV file: %v\n", err)
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"Name"}); err != nil {
		fmt.Printf("[Scraper] Failed to write watchlist CSV header: %v\n", err)
		return err
	}
	for _, title := range titles {
		if title == "" {
			continue
		}
		if err := w.Write([]string{title}); err != nil {
			fmt.Printf("[Scraper] Failed to write watchlist row: %v\n", err)
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Printf("[Scraper] Watchlist CSV writer error: %v\n", err)
		return err
	}
	fmt.Printf("[Scraper] Successfully wrote %d watchlist items to CSV\n", len(titles))
	return nil
}


func parseLoginForm(page string) (string, string, string, url.Values) {
	formRe := regexp.MustCompile(`(?is)<form[^>]*action="([^"]*login[^"]*)"[^>]*>(.*?)</form>`)
	match := formRe.FindStringSubmatch(page)
	action := "/user/login.do"
	formBody := page
	if len(match) >= 3 {
		action = html.UnescapeString(strings.TrimSpace(match[1]))
		formBody = match[2]
	}
	inputRe := regexp.MustCompile(`(?is)<input[^>]*>`)
	nameRe := regexp.MustCompile(`\bname="([^"]+)"`)
	valueRe := regexp.MustCompile(`\bvalue="([^"]*)"`)
	typeRe := regexp.MustCompile(`\btype="([^"]+)"`)
	values := url.Values{}

	var userField string
	var passField string
	for _, tag := range inputRe.FindAllString(formBody, -1) {
		nameMatch := nameRe.FindStringSubmatch(tag)
		if len(nameMatch) < 2 {
			continue
		}
		name := html.UnescapeString(strings.TrimSpace(nameMatch[1]))
		if name == "" {
			continue
		}
		typ := ""
		typeMatch := typeRe.FindStringSubmatch(tag)
		if len(typeMatch) >= 2 {
			typ = strings.ToLower(strings.TrimSpace(typeMatch[1]))
		}
		value := ""
		valueMatch := valueRe.FindStringSubmatch(tag)
		if len(valueMatch) >= 2 {
			value = html.UnescapeString(valueMatch[1])
		}
		if typ == "hidden" {
			values.Set(name, value)
		}
		l := strings.ToLower(name)
		if userField == "" && (strings.Contains(l, "user") || strings.Contains(l, "email") || strings.Contains(l, "login")) {
			userField = name
		}
		if passField == "" && strings.Contains(l, "pass") {
			passField = name
		}
	}
	if userField == "" {
		userField = "username"
	}
	if passField == "" {
		passField = "password"
	}
	if _, ok := values[userField]; !ok {
		values.Set(userField, "")
	}
	if _, ok := values[passField]; !ok {
		values.Set(passField, "")
	}
	return action, userField, passField, values
}

func parseDiaryEntriesFromHTML(page string) []scrapedDiaryEntry {
	rowRe := regexp.MustCompile(`(?is)<tr[^>]*diary-entry-row[^>]*>(.*?)</tr>`)
	titleRe := regexp.MustCompile(`(?is)<h3[^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	// Extract date from links like /username/diary/films/for/2026/04/19/
	dateLinkRe := regexp.MustCompile(`(?is)/diary/films/for/([0-9]{4})/([0-9]{2})/([0-9]{2})/`)
	
	out := make([]scrapedDiaryEntry, 0, 128)

	rows := rowRe.FindAllStringSubmatch(page, -1)
	fmt.Printf("[Scraper] HTML: found %d diary-entry-row matches\n", len(rows))
	for _, row := range rows {
		fullMatch := row[0]
		block := row[1]
		
		// 1. Try to find title and film path
		title := ""
		filmPath := ""
		if tm := titleRe.FindStringSubmatch(block); len(tm) >= 3 {
			filmPath = strings.TrimSpace(tm[1])
			title = strings.TrimSpace(stripHTML(tm[2]))
		}
		
		if title == "" {
			if m := regexp.MustCompile(`data-film-name="([^"]+)"`).FindStringSubmatch(fullMatch); len(m) >= 2 {
				title = html.UnescapeString(m[1])
			}
		}
		
		if title == "" {
			if m := regexp.MustCompile(`alt="([^"]+)"`).FindStringSubmatch(block); len(m) >= 2 {
				t := html.UnescapeString(m[1])
				if !strings.EqualFold(t, "poster") && t != "" {
					title = t
				}
			}
		}

		if title == "" {
			continue
		}

		// 2. Try to find date
		date := ""
		if m := dateLinkRe.FindStringSubmatch(fullMatch); len(m) >= 4 {
			date = fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
		}
		
		if date == "" {
			if m := regexp.MustCompile(`data-viewing-date-str="([0-9]{4}-[0-9]{2}-[0-9]{2})"`).FindStringSubmatch(fullMatch); len(m) >= 2 {
				date = m[1]
			}
		}
		
		if date == "" {
			// Fallback: search for ANY YYYY-MM-DD pattern
			re := regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}`)
			if m := re.FindString(fullMatch); m != "" {
				date = m
			}
		}

		if date == "" {
			continue // Still no date, skip
		}

		// 3. Extract rating
		rating := ""
		// Look for "rated-X" class where X is 1-10 (0.5 to 5.0 stars)
		if m := regexp.MustCompile(`rated-([0-9]{1,2})`).FindStringSubmatch(block); len(m) >= 2 {
			val, _ := strconv.Atoi(m[1])
			if val > 0 {
				rating = fmt.Sprintf("%.1f", float64(val)/2.0)
			}
		}
		
		if rating == "" {
			// Try the input range value
			if m := regexp.MustCompile(`value="([0-9]+)"`).FindStringSubmatch(block); len(m) >= 2 {
				val, _ := strconv.Atoi(m[1])
				if val > 0 {
					rating = fmt.Sprintf("%.1f", float64(val)/2.0)
				}
			}
		}

		// 4. Extract rewatch
		rewatch := strings.Contains(strings.ToLower(block), "icon-rewatch") || 
			strings.Contains(strings.ToLower(block), "td-rewatch") ||
			strings.Contains(strings.ToLower(block), "icon-status-on") && strings.Contains(strings.ToLower(block), "rewatch")

		// 5. Extract film release year if possible
		year := ""
		if m := regexp.MustCompile(`data-film-release-year="([0-9]{4})"`).FindStringSubmatch(fullMatch); len(m) >= 2 {
			year = m[1]
		}
		if year == "" {
			if m := regexp.MustCompile(`class="releasedate".*?>([0-9]{4})</a>`).FindStringSubmatch(block); len(m) >= 2 {
				year = m[1]
			}
		}

		out = append(out, scrapedDiaryEntry{
			Date:     date,
			Title:    title,
			Year:     year,
			FilmPath: filmPath,
			Rating:   rating,
			Rewatch:  rewatch,
		})
	}

	if len(out) > 0 {
		fmt.Printf("[Scraper] HTML: parsed %d entries from diary-entry-row\n", len(out))
		return out
	}

	fmt.Printf("[Scraper] HTML: diary-entry-row failed to yield entries, trying data-film-name global fallback\n")
	dataCardRe := regexp.MustCompile(`(?is)<[^>]*data-film-name="([^"]+)"[^>]*data-viewing-date-str="([0-9]{4}-[0-9]{2}-[0-9]{2})"[^>]*>`)
	dataMatches := dataCardRe.FindAllStringSubmatch(page, -1)
	fmt.Printf("[Scraper] HTML: found %d global data-film-name matches\n", len(dataMatches))
	for _, m := range dataMatches {
		if len(m) < 3 {
			continue
		}
		out = append(out, scrapedDiaryEntry{Title: html.UnescapeString(m[1]), Date: m[2]})
	}
	return out
}

func parseDiaryEntriesFromRSS(feed string) []scrapedDiaryEntry {
	itemRe := regexp.MustCompile(`(?is)<item>(.*?)</item>`)
	out := make([]scrapedDiaryEntry, 0, 128)
	for _, item := range itemRe.FindAllStringSubmatch(feed, -1) {
		block := item[1]
		title := textFromTag(block, "letterboxd:filmTitle")
		if title == "" {
			title = textFromTag(block, "title")
		}
		title = strings.TrimSpace(stripHTML(title))
		if title == "" {
			continue
		}
		date := textFromTag(block, "letterboxd:watchedDate")
		if date == "" {
			if pub := textFromTag(block, "pubDate"); pub != "" {
				if t, err := parsePubDate(pub); err == nil {
					date = t.Format("2006-01-02")
				}
			}
		}
		if date == "" {
			continue
		}
		rating := strings.TrimSpace(textFromTag(block, "letterboxd:memberRating"))
		rewatch := strings.EqualFold(strings.TrimSpace(textFromTag(block, "letterboxd:rewatch")), "yes")
		
		// Extract review from description
		notes := ""
		if desc := textFromTag(block, "description"); desc != "" {
			// Letterboxd RSS description starts with a poster image <img>, then review text.
			// Example: <p><img src="..."/></p> <p>Review text...</p>
			// We'll strip HTML to get the text.
			notes = strings.TrimSpace(stripHTML(desc))
			// If it's just "Watched on ...", it's not a review.
			if strings.HasPrefix(notes, "Watched on ") {
				notes = ""
			}
		}

		filmPath := ""
		if link := strings.TrimSpace(textFromTag(block, "link")); link != "" {
			if u, err := url.Parse(link); err == nil {
				filmPath = u.Path
			}
		}
		out = append(out, scrapedDiaryEntry{
			Date:     date,
			Title:    title,
			FilmPath: filmPath,
			Rating:   rating,
			Rewatch:  rewatch,
			Notes:    notes,
		})
	}
	return out
}

func textFromTag(block, tag string) string {
	pat := fmt.Sprintf(`(?is)<%s>(.*?)</%s>`, regexp.QuoteMeta(tag), regexp.QuoteMeta(tag))
	re := regexp.MustCompile(pat)
	m := re.FindStringSubmatch(block)
	if len(m) < 2 {
		return ""
	}
	s := strings.TrimSpace(m[1])
	s = strings.TrimPrefix(s, "<![CDATA[")
	s = strings.TrimSuffix(s, "]]>")
	return html.UnescapeString(strings.TrimSpace(s))
}

func findNextPageURL(currentURL, page string) string {
	re := regexp.MustCompile(`(?is)<a[^>]*class="[^"]*\bnext\b[^"]*"[^>]*href="([^"]+)"`)
	m := re.FindStringSubmatch(page)
	if len(m) < 2 {
		return ""
	}
	u, err := resolveURL(currentURL, html.UnescapeString(strings.TrimSpace(m[1])))
	if err != nil {
		return ""
	}
	return u
}

func resolveURL(baseURL, next string) (string, error) {
	b, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(next))
	if err != nil {
		return "", err
	}
	return b.ResolveReference(u).String(), nil
}

func stripHTML(raw string) string {
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	s := tagRe.ReplaceAllString(raw, "")
	return html.UnescapeString(strings.TrimSpace(s))
}

func parsePubDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	layouts := []string{time.RFC1123Z, time.RFC1123, time.RFC3339}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("unsupported pubDate layout")
}

func monthFromShortName(v string) (time.Month, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "jan":
		return time.January, true
	case "feb":
		return time.February, true
	case "mar":
		return time.March, true
	case "apr":
		return time.April, true
	case "may":
		return time.May, true
	case "jun":
		return time.June, true
	case "jul":
		return time.July, true
	case "aug":
		return time.August, true
	case "sep":
		return time.September, true
	case "oct":
		return time.October, true
	case "nov":
		return time.November, true
	case "dec":
		return time.December, true
	default:
		return 0, false
	}
}

func (d *LetterboxdDownloader) hasSessionCookie(client *http.Client) (bool, error) {
	u, err := url.Parse("https://letterboxd.com/")
	if err != nil {
		return false, err
	}
	for _, c := range client.Jar.Cookies(u) {
		n := strings.ToLower(strings.TrimSpace(c.Name))
		if strings.Contains(n, "session") || strings.Contains(n, "signedin") || strings.Contains(n, "auth") {
			return true, nil
		}
	}
	return false, nil
}

func (d *LetterboxdDownloader) canReadPublicProfile(ctx context.Context, client *http.Client, username string) (bool, error) {
	u := fmt.Sprintf("https://letterboxd.com/%s/", strings.TrimSpace(username))
	page, err := d.fetchHTML(ctx, client, u)
	if err != nil {
		return false, err
	}
	l := strings.ToLower(page)
	if strings.Contains(l, "page not found") || strings.Contains(l, "sorry, we can") && strings.Contains(l, "find the page") {
		return false, nil
	}
	return true, nil
}

func validateScrapeCreds(creds LetterboxdCredentials) error {
	if strings.TrimSpace(creds.Username) == "" {
		return errors.New("letterboxd username is required")
	}
	if strings.TrimSpace(creds.Password) == "" {
		return errors.New("letterboxd password is required")
	}
	return nil
}
