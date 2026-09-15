package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrBlocked means HelloFresh refused requests. The run stops rather than retrying
// so the importer never tries to work around access controls.
var ErrBlocked = errors.New("request refused (HTTP 403); stopping — do not retry aggressively")

const (
	defaultAllowedPrefix = "https://www.hellofresh.com/recipes/"
	maxPageBytes         = 8 << 20
)

var recipeIDRe = regexp.MustCompile(`^[a-f0-9]{24}$`)

// Fetcher downloads public recipe pages politely: one request at a time with a
// minimum delay, bounded retries with backoff on 429/5xx, and a hard stop on 403.
type Fetcher struct {
	Client        *http.Client
	UserAgent     string
	AllowedPrefix string
	Delay         time.Duration
	MaxAttempts   int
	Limit         int
	Now           func() time.Time
	Sleep         func(context.Context, time.Duration) error
	Log           *slog.Logger
}

// RawRecipe is the stored source record for one delivered recipe.
type RawRecipe struct {
	DeliveredID  string          `json:"deliveredId"`
	RequestedURL string          `json:"requestedUrl"`
	FinalURL     string          `json:"finalUrl"`
	FetchedAt    time.Time       `json:"fetchedAt"`
	Recipe       json.RawMessage `json:"recipe"`
}

// Failure records a recipe that could not be fetched.
type Failure struct {
	DeliveredID string `json:"deliveredId"`
	URL         string `json:"url"`
	Reason      string `json:"reason"`
}

// FetchStats summarizes a fetch run.
type FetchStats struct {
	Fetched  int
	Skipped  int
	Failures []Failure
}

// NewFetcher returns a Fetcher with production defaults.
func NewFetcher(logger *slog.Logger) *Fetcher {
	return &Fetcher{
		Client:        &http.Client{Timeout: 30 * time.Second},
		UserAgent:     "DinnerOS-HelloFresh-Importer/1.0 (personal order history import)",
		AllowedPrefix: defaultAllowedPrefix,
		Delay:         2500 * time.Millisecond,
		MaxAttempts:   4,
		Now:           time.Now,
		Sleep:         sleepContext,
		Log:           logger,
	}
}

// FetchAll saves raw data for every recipe not already on disk. Existing files
// are never re-fetched, so re-running resumes where it stopped.
func (f *Fetcher) FetchAll(ctx context.Context, recipes []OrderedRecipe, outDir string) (FetchStats, error) {
	var stats FetchStats
	dir := filepath.Join(outDir, "recipes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stats, fmt.Errorf("create output dir: %w", err)
	}

	requested := false
	for _, r := range recipes {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if !recipeIDRe.MatchString(r.DeliveredID) {
			stats.Failures = append(stats.Failures, Failure{r.DeliveredID, r.URL, "invalid recipe id"})
			continue
		}
		path := filepath.Join(dir, r.DeliveredID+".json")
		if _, err := os.Stat(path); err == nil {
			stats.Skipped++
			continue
		}
		if !strings.HasPrefix(r.URL, f.AllowedPrefix) {
			stats.Failures = append(stats.Failures, Failure{r.DeliveredID, r.URL, "url outside allowed recipe pages"})
			continue
		}
		if f.Limit > 0 && stats.Fetched >= f.Limit {
			break
		}

		if requested {
			if err := f.Sleep(ctx, f.Delay); err != nil {
				return stats, err
			}
		}
		requested = true

		raw, err := f.fetchOne(ctx, r)
		if errors.Is(err, ErrBlocked) || errors.Is(err, context.Canceled) {
			_ = writeFailures(outDir, stats.Failures)
			return stats, err
		}
		if err != nil {
			f.Log.Warn("recipe fetch failed", "id", r.DeliveredID, "name", r.Name, "error", err)
			stats.Failures = append(stats.Failures, Failure{r.DeliveredID, r.URL, err.Error()})
			continue
		}
		if err := writeJSONAtomic(path, raw); err != nil {
			return stats, err
		}
		stats.Fetched++
		f.Log.Info("recipe saved", "id", r.DeliveredID, "name", r.Name, "done", stats.Fetched+stats.Skipped, "total", len(recipes))
	}

	return stats, writeFailures(outDir, stats.Failures)
}

func (f *Fetcher) fetchOne(ctx context.Context, r OrderedRecipe) (RawRecipe, error) {
	var lastErr error
	for attempt := 1; attempt <= f.MaxAttempts; attempt++ {
		if attempt > 1 {
			backoff := f.Delay * time.Duration(1<<(attempt-1))
			if err := f.Sleep(ctx, backoff); err != nil {
				return RawRecipe{}, err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
		if err != nil {
			return RawRecipe{}, err
		}
		req.Header.Set("User-Agent", f.UserAgent)
		req.Header.Set("Accept", "text/html")

		resp, err := f.Client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusForbidden:
			return RawRecipe{}, ErrBlocked
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		case resp.StatusCode != http.StatusOK:
			return RawRecipe{}, fmt.Errorf("HTTP %d", resp.StatusCode)
		case readErr != nil:
			lastErr = readErr
			continue
		}

		finalURL := resp.Request.URL.String()
		if !strings.HasPrefix(finalURL, f.AllowedPrefix) {
			return RawRecipe{}, fmt.Errorf("redirected away from recipe pages to %s", finalURL)
		}
		recipe, err := ExtractRecipe(body)
		if err != nil {
			return RawRecipe{}, err
		}
		return RawRecipe{
			DeliveredID:  r.DeliveredID,
			RequestedURL: r.URL,
			FinalURL:     finalURL,
			FetchedAt:    f.Now().UTC(),
			Recipe:       recipe,
		}, nil
	}
	return RawRecipe{}, fmt.Errorf("gave up after %d attempts: %w", f.MaxAttempts, lastErr)
}

func writeFailures(outDir string, failures []Failure) error {
	path := filepath.Join(outDir, "fetch-failures.json")
	if len(failures) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeJSONAtomic(path, failures)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
