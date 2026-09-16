package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OriginCard marks a raw recipe parsed from the delivered recipe's printed card
// (PDF). It is the exact variant delivered, like an account capture, but has
// less detail (no images, only calories), so the normalizer prefers an account
// capture, then a card, then the canonical public page.
const OriginCard = "card"

const (
	cardBaseURL  = "https://www.hellofresh.com/recipecards/card/"
	maxCardBytes = 64 << 20
)

// CardTarget is a delivered recipe whose printed card is worth fetching.
type CardTarget struct {
	DeliveredID string `json:"deliveredId"`
	Name        string `json:"name"`
	// Reason is "variant" (the public page resolved to a different variant) or
	// "steps" (the page or capture has no steps).
	Reason string `json:"reason"`
	// CardLink is the card URL a capture names, or a public page names when
	// that page is the delivered recipe itself (no redirect).
	CardLink string `json:"cardLink,omitempty"`
}

// cardMiss records a delivered ID with no card at any known URL, so re-runs
// don't ask again.
type cardMiss struct {
	DeliveredID string    `json:"deliveredId"`
	CheckedAt   time.Time `json:"checkedAt"`
	Tried       []string  `json:"tried"`
}

// CardTargets lists delivered recipes whose exact details are missing: public
// pages (without an account capture) that are a different variant or have no
// steps, and account captures with no steps. Ordered by delivered ID.
func CardTargets(raws []RawRecipe, history History) ([]CardTarget, error) {
	parsed, captured, err := parseRaws(raws)
	if err != nil {
		return nil, err
	}
	ordered := map[string]OrderedRecipe{}
	for _, r := range history.UniqueRecipes() {
		ordered[r.DeliveredID] = r
	}
	var out []CardTarget
	for _, p := range parsed {
		o, ok := ordered[p.raw.DeliveredID]
		if !ok {
			continue
		}
		t := CardTarget{DeliveredID: o.DeliveredID, Name: o.Name}
		switch {
		case p.raw.Origin == OriginAccount:
			// A capture is the exact variant; only missing steps need its card.
			if len(p.recipe.Steps) > 0 {
				continue
			}
			t.Reason, t.CardLink = "steps", p.recipe.CardLink
		case p.raw.Origin != "" || captured[p.raw.DeliveredID]:
			continue
		case o.Name != "" && !sameVariant(o.Name, p.recipe):
			t.Reason = "variant"
		case len(p.recipe.Steps) == 0:
			t.Reason = "steps"
			if p.recipe.CardLink != "" && strings.TrimSuffix(p.recipe.ID, "-en-US") == o.DeliveredID {
				t.CardLink = p.recipe.CardLink
			}
		default:
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeliveredID < out[j].DeliveredID })
	return out, nil
}

// cardURLs returns the URLs to try for a target, most specific first.
func cardURLs(base string, t CardTarget) []string {
	var urls []string
	if strings.HasPrefix(t.CardLink, base) {
		urls = append(urls, t.CardLink)
	}
	for _, u := range []string{base + t.DeliveredID + ".pdf", base + t.DeliveredID + "-en-US.pdf"} {
		if u != t.CardLink {
			urls = append(urls, u)
		}
	}
	return urls
}

// errCardMissing means the card storage has no object at a URL. HelloFresh's
// card bucket answers a missing key with an S3 AccessDenied document rather
// than 404, which is not a refusal of the client.
var errCardMissing = errors.New("no card at this URL")

// CardStats summarizes a card fetch run.
type CardStats struct {
	Fetched, Missing, Cached int
}

// FetchCards downloads the printed card PDF for each target into
// dir/cards/<deliveredId>.pdf, politely: sequential, with f.Delay between
// requests, never re-requesting a cached card or a recorded miss unless
// retryMissing is set. A refusal that isn't a missing object stops the run.
func (f *Fetcher) FetchCards(ctx context.Context, targets []CardTarget, dir string, retryMissing bool) (CardStats, error) {
	var stats CardStats
	cardDir := filepath.Join(dir, "cards")
	if err := os.MkdirAll(cardDir, 0o755); err != nil {
		return stats, err
	}
	requested := false
	for _, t := range targets {
		if !recipeIDRe.MatchString(t.DeliveredID) {
			continue
		}
		pdfPath := filepath.Join(cardDir, t.DeliveredID+".pdf")
		missPath := filepath.Join(cardDir, t.DeliveredID+".missing.json")
		if _, err := os.Stat(pdfPath); err == nil {
			stats.Cached++
			continue
		}
		if _, err := os.Stat(missPath); err == nil && !retryMissing {
			stats.Cached++
			continue
		}

		var tried []string
		var body []byte
		for _, u := range cardURLs(f.cardBase(), t) {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
			if requested {
				if err := f.Sleep(ctx, f.Delay); err != nil {
					return stats, err
				}
			}
			requested = true
			tried = append(tried, u)
			b, err := f.fetchCard(ctx, u)
			if errors.Is(err, errCardMissing) {
				continue
			}
			if err != nil {
				return stats, fmt.Errorf("card %s: %w", t.DeliveredID, err)
			}
			body = b
			break
		}
		if body == nil {
			stats.Missing++
			f.Log.Info("no card", "id", t.DeliveredID, "name", t.Name)
			if err := writeJSONAtomic(missPath, cardMiss{DeliveredID: t.DeliveredID, CheckedAt: f.Now().UTC(), Tried: tried}); err != nil {
				return stats, err
			}
			continue
		}
		if err := os.WriteFile(pdfPath+".tmp", body, 0o644); err != nil {
			return stats, err
		}
		if err := os.Rename(pdfPath+".tmp", pdfPath); err != nil {
			return stats, err
		}
		_ = os.Remove(missPath)
		stats.Fetched++
		f.Log.Info("card saved", "id", t.DeliveredID, "name", t.Name, "bytes", len(body))
	}
	return stats, nil
}

func (f *Fetcher) cardBase() string {
	if f.CardBaseURL != "" {
		return f.CardBaseURL
	}
	return cardBaseURL
}

func (f *Fetcher) fetchCard(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, f.cardBase()) {
		return nil, fmt.Errorf("url outside recipe cards: %s", url)
	}
	var lastErr error
	for attempt := 1; attempt <= f.MaxAttempts; attempt++ {
		if attempt > 1 {
			if err := f.Sleep(ctx, f.Delay*time.Duration(1<<(attempt-1))); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", f.UserAgent)
		req.Header.Set("Accept", "application/pdf")
		resp, err := f.Client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCardBytes+1))
		resp.Body.Close()
		switch {
		case len(body) > maxCardBytes:
			return nil, fmt.Errorf("card larger than %d bytes", maxCardBytes)
		case resp.StatusCode == http.StatusNotFound:
			return nil, errCardMissing
		case resp.StatusCode == http.StatusForbidden && bytes.Contains(body, []byte("<Code>AccessDenied</Code>")):
			return nil, errCardMissing
		case resp.StatusCode == http.StatusForbidden:
			return nil, ErrBlocked
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		case readErr != nil:
			lastErr = readErr
			continue
		}
		if !bytes.HasPrefix(bytes.TrimLeft(body, "\x00\r\n\t "), []byte("%PDF-")) {
			return nil, fmt.Errorf("response is not a PDF (%s)", resp.Header.Get("Content-Type"))
		}
		return body, nil
	}
	return nil, fmt.Errorf("gave up after %d attempts: %w", f.MaxAttempts, lastErr)
}

// ParseCachedCards parses every cached card PDF in dir/cards and writes the
// result as a card-origin raw recipe to dir/cards/<deliveredId>.json. A card
// that doesn't parse leaves no JSON behind, so the normalizer never uses a
// partial card. It returns the IDs that failed with their reasons.
func ParseCachedCards(dir string, now time.Time) (parsed int, failed map[string]string, err error) {
	failed = map[string]string{}
	paths, err := filepath.Glob(filepath.Join(dir, "cards", "*.pdf"))
	if err != nil {
		return 0, nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".pdf")
		jsonPath := strings.TrimSuffix(p, ".pdf") + ".json"
		data, err := os.ReadFile(p)
		if err != nil {
			return parsed, failed, err
		}
		card, perr := ParseCard(data)
		if perr != nil {
			failed[id] = perr.Error()
			if err := os.Remove(jsonPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return parsed, failed, err
			}
			continue
		}
		recipe, err := json.Marshal(card.hfRecipe(id))
		if err != nil {
			return parsed, failed, err
		}
		raw := RawRecipe{
			DeliveredID:  id,
			Origin:       OriginCard,
			RequestedURL: "",
			FinalURL:     "",
			FetchedAt:    now.UTC(),
			Recipe:       recipe,
		}
		if err := writeJSONAtomic(jsonPath, raw); err != nil {
			return parsed, failed, err
		}
		parsed++
	}
	return parsed, failed, nil
}

func runCards(ctx context.Context, logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("cards", flag.ContinueOnError)
	historyPath := fs.String("history", "data/order-history.json", "order history exported from the browser")
	rawDir := fs.String("raw", "data/raw", "directory containing raw recipe files")
	delay := fs.Duration("delay", 3*time.Second, "minimum delay between requests")
	retryMissing := fs.Bool("retry-missing", false, "ask again for cards recorded as missing")
	offline := fs.Bool("offline", false, "only re-parse cached cards; make no requests")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*offline {
		history, err := LoadHistory(*historyPath)
		if err != nil {
			return err
		}
		raws, err := LoadRawRecipes(*rawDir)
		if err != nil {
			return err
		}
		targets, err := CardTargets(raws, history)
		if err != nil {
			return err
		}
		logger.Info("card targets", "count", len(targets))
		fetcher := NewFetcher(logger)
		fetcher.Delay = *delay
		stats, err := fetcher.FetchCards(ctx, targets, *rawDir, *retryMissing)
		logger.Info("card fetch finished", "fetched", stats.Fetched, "missing", stats.Missing, "cached", stats.Cached)
		if err != nil {
			return err
		}
	}

	parsed, failed, err := ParseCachedCards(*rawDir, time.Now())
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(failed))
	for id := range failed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		logger.Warn("card not parsed; its review item stays", "id", id, "reason", failed[id])
	}
	logger.Info("cards parsed", "parsed", parsed, "failed", len(failed))
	return nil
}
