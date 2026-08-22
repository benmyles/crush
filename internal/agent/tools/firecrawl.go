package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	firecrawl "github.com/firecrawl/firecrawl/apps/go-sdk"
	"github.com/firecrawl/firecrawl/apps/go-sdk/option"
)

// firecrawlAPIURL is a package var so tests can point the client at a local
// httptest server. When empty, the Firecrawl SDK uses its default API URL.
var firecrawlAPIURL = ""

const (
	// firecrawlMaxRetries caps how many times a rate-limited (HTTP 429)
	// request is retried before the error is surfaced.
	firecrawlMaxRetries = 3
)

// firecrawlRequestTimeout caps each individual Firecrawl API request. It
// is a package var so tests can shrink it.
var firecrawlRequestTimeout = 30 * time.Second

// firecrawlRetryBackoff computes the wait before the given 1-based retry
// attempt (1s, 2s, 4s, ... capped at 15s). It is a package var so tests
// can stub out the sleep.
var firecrawlRetryBackoff = func(attempt int) time.Duration {
	return min(time.Second<<(attempt-1), 15*time.Second)
}

// newFirecrawlClient builds a Firecrawl client for the supplied API key.
// The SDK resolves the key itself from FIRECRAWL_API_KEY when key is empty,
// but callers always pass an explicit key so custom API URLs stay isolated
// from the ambient environment. The SDK's built-in retry is disabled: the
// only transient failure worth retrying here is HTTP 429, and that is
// handled (with UI-visible backoff) by firecrawlWithRetry.
func newFirecrawlClient(apiKey string, client *http.Client) (*firecrawl.Client, error) {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
	}
	if firecrawlAPIURL != "" {
		opts = append(opts, option.WithAPIURL(firecrawlAPIURL))
	}
	if client != nil {
		opts = append(opts, option.WithHTTPClient(client))
	}
	return firecrawl.NewClient(opts...)
}

// isFirecrawlRateLimited reports whether err is a Firecrawl HTTP 429
// response. Every other failure (auth, 5xx, transport) is returned to the
// caller without retrying.
func isFirecrawlRateLimited(err error) bool {
	if _, ok := errors.AsType[*firecrawl.RateLimitError](err); ok {
		return true
	}
	fcErr, ok := errors.AsType[*firecrawl.FirecrawlError](err)
	return ok && fcErr.StatusCode == http.StatusTooManyRequests
}

// firecrawlWithRetry runs call, retrying only HTTP 429 responses with
// exponential backoff. Each attempt gets its own firecrawlRequestTimeout
// context so a single slow request cannot hang the tool. Before each
// backoff, the context's retry notifier (when set) is invoked so the wait
// propagates to the UI.
func firecrawlWithRetry[T any](ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var zero T
	for attempt := range firecrawlMaxRetries + 1 {
		if attempt > 0 {
			delay := firecrawlRetryBackoff(attempt)
			if notify := GetRetryNotifierFromContext(ctx); notify != nil {
				notify(attempt, http.StatusTooManyRequests, delay)
			}
			slog.Warn("Firecrawl request rate limited, retrying", "attempt", attempt, "retry_delay", delay.String())
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return zero, ctx.Err()
			case <-timer.C:
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, firecrawlRequestTimeout)
		result, err := call(attemptCtx)
		cancel()
		if err == nil {
			return result, nil
		}
		if !isFirecrawlRateLimited(err) {
			return zero, err
		}
		if attempt == firecrawlMaxRetries {
			return zero, err
		}
	}
	return zero, nil // Unreachable; the loop always returns.
}

// searchFirecrawl performs a web search through the Firecrawl API. Search
// returns title, URL, and description for each result; descriptions are
// used as snippets.
func searchFirecrawl(ctx context.Context, client *http.Client, apiKey, query string, maxResults int) ([]SearchResult, error) {
	fc, err := newFirecrawlClient(apiKey, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create Firecrawl client: %w", err)
	}

	limit := maxResults
	data, err := firecrawlWithRetry(ctx, func(ctx context.Context) (*firecrawl.SearchData, error) {
		return fc.Search(ctx, query, &firecrawl.SearchOptions{
			Limit: &limit,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("firecrawl search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(data.Web))
	for _, item := range data.Web {
		rawURL, _ := item["url"].(string)
		if rawURL == "" {
			continue
		}

		title, _ := item["title"].(string)
		snippet, _ := item["description"].(string)
		if snippet == "" {
			if markdown, ok := item["markdown"].(string); ok && markdown != "" {
				snippet = markdown
			}
		}

		results = append(results, SearchResult{
			Title:    strings.TrimSpace(title),
			Link:     rawURL,
			Snippet:  strings.TrimSpace(snippet),
			Position: len(results) + 1,
		})
	}
	return results, nil
}

// fetchFirecrawl fetches a single URL's content through the Firecrawl
// scrape API, which returns clean markdown. maxCharacters caps the returned
// text after the fact; 0 keeps the full content.
func fetchFirecrawl(ctx context.Context, client *http.Client, apiKey, rawURL string, maxCharacters int) (string, error) {
	fc, err := newFirecrawlClient(apiKey, client)
	if err != nil {
		return "", fmt.Errorf("failed to create Firecrawl client: %w", err)
	}

	onlyMainContent := true
	removeBase64Images := true
	doc, err := firecrawlWithRetry(ctx, func(ctx context.Context) (*firecrawl.Document, error) {
		return fc.Scrape(ctx, rawURL, &firecrawl.ScrapeOptions{
			Formats:            []string{"markdown"},
			OnlyMainContent:    &onlyMainContent,
			RemoveBase64Images: &removeBase64Images,
		})
	})
	if err != nil {
		return "", fmt.Errorf("firecrawl failed to fetch %s: %w", rawURL, err)
	}

	content := strings.TrimSpace(doc.Markdown)
	if content == "" {
		return "", fmt.Errorf("firecrawl returned empty content for %s", rawURL)
	}
	if maxCharacters > 0 && len(content) > maxCharacters {
		content = content[:maxCharacters]
	}
	return content, nil
}
