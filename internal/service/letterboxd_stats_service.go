package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LetterboxdSnapshotProvider interface {
	ScrapeSnapshot(ctx context.Context, creds LetterboxdCredentials) (LetterboxdSnapshot, error)
}

type cachedFeature struct {
	Hash      string   `json:"hash"`
	Items     []string `json:"items"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type statsCache struct {
	Username string                   `json:"username"`
	Features map[string]cachedFeature `json:"features"`
}

type LBFeatureResult struct {
	Name    string
	Items   []string
	Source  string
	Changed bool
}

type LetterboxdStatsResult struct {
	Watched   LBFeatureResult
	Reviews   LBFeatureResult
	Watchlist LBFeatureResult
	Lists     LBFeatureResult
	Tags      LBFeatureResult
}

type LetterboxdStatsService struct {
	cfg      *AppConfigService
	provider LetterboxdSnapshotProvider
	now      func() time.Time
}

func NewLetterboxdStatsService(cfg *AppConfigService, provider LetterboxdSnapshotProvider) *LetterboxdStatsService {
	return &LetterboxdStatsService{cfg: cfg, provider: provider, now: time.Now}
}

func (s *LetterboxdStatsService) SyncAndLoad(ctx context.Context, creds LetterboxdCredentials) (LetterboxdStatsResult, error) {
	cache, _ := s.loadCache()
	fresh, err := s.provider.ScrapeSnapshot(ctx, creds)
	if err != nil {
		return fromCache(cache), err
	}
	cache.Username = strings.TrimSpace(creds.Username)
	if cache.Features == nil {
		cache.Features = map[string]cachedFeature{}
	}

	out := LetterboxdStatsResult{}
	out.Watched = s.mergeFeature(cache, "watched", fresh.Watched)
	out.Reviews = s.mergeFeature(cache, "reviews", fresh.Reviews)
	out.Watchlist = s.mergeFeature(cache, "watchlist", fresh.Watchlist)
	out.Lists = s.mergeFeature(cache, "lists", fresh.Lists)
	out.Tags = s.mergeFeature(cache, "tags", fresh.Tags)

	_ = s.saveCache(cache)
	return out, nil
}

func (s *LetterboxdStatsService) mergeFeature(cache *statsCache, key string, fresh []string) LBFeatureResult {
	fresh = uniqueTitles(fresh)
	h := hashItems(fresh)
	existing, ok := cache.Features[key]
	changed := !ok || existing.Hash != h
	res := LBFeatureResult{Name: key}
	if !changed {
		res.Items = existing.Items
		res.Source = "scrape"
		res.Changed = false
		return res
	}
	cache.Features[key] = cachedFeature{
		Hash:      h,
		Items:     fresh,
		UpdatedAt: s.now().UTC().Format(time.RFC3339),
	}
	res.Items = fresh
	res.Source = "scrape"
	res.Changed = true
	return res
}

func fromCache(cache *statsCache) LetterboxdStatsResult {
	if cache == nil {
		return LetterboxdStatsResult{}
	}
	get := func(k string) LBFeatureResult {
		c := cache.Features[k]
		return LBFeatureResult{Name: k, Items: c.Items, Source: "cache", Changed: false}
	}
	return LetterboxdStatsResult{
		Watched:   get("watched"),
		Reviews:   get("reviews"),
		Watchlist: get("watchlist"),
		Lists:     get("lists"),
		Tags:      get("tags"),
	}
}

func (s *LetterboxdStatsService) loadCache() (*statsCache, error) {
	path, err := s.cachePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return &statsCache{Features: map[string]cachedFeature{}}, nil
	}
	var c statsCache
	if err := json.Unmarshal(b, &c); err != nil {
		return &statsCache{Features: map[string]cachedFeature{}}, nil
	}
	if c.Features == nil {
		c.Features = map[string]cachedFeature{}
	}
	return &c, nil
}

func (s *LetterboxdStatsService) saveCache(c *statsCache) error {
	path, err := s.cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (s *LetterboxdStatsService) cachePath() (string, error) {
	cfgPath := s.cfg.ConfigPath()
	return filepath.Join(filepath.Dir(cfgPath), "letterboxd-stats-cache.json"), nil
}

func hashItems(items []string) string {
	h := sha256.New()
	for _, it := range items {
		_, _ = h.Write([]byte(strings.TrimSpace(it)))
		_, _ = h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
