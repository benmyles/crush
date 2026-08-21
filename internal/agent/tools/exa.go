package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// WebBackend identifies the backend used for web search and web fetching.
type WebBackend string

const (
	// WebBackendDefault resolves the backend automatically: Exa when
	// EXA_API_KEY is set, DuckDuckGo (search) or direct HTTP (fetch)
	// otherwise.
	WebBackendDefault WebBackend = "default"

	// WebBackendExa forces the Exa backend for searches and fetches.
	WebBackendExa WebBackend = "exa"
)

// WebBackendResolver returns the configured web backend at call time so
// runtime option changes take effect without rebuilding tools. A nil
// resolver always resolves to WebBackendDefault.
type WebBackendResolver func() WebBackend

// resolveWebBackend returns the effective backend and Exa API key for the
// current call.
func resolveWebBackend(resolver WebBackendResolver) (WebBackend, string) {
	setting := WebBackendDefault
	if resolver != nil {
		setting = resolver()
	}

	apiKey := os.Getenv("EXA_API_KEY")
	if setting == WebBackendExa {
		return WebBackendExa, apiKey
	}

	// Default: prefer Exa when a key is available.
	if apiKey != "" {
		return WebBackendExa, apiKey
	}
	return WebBackendDefault, ""
}

// exaAPIURL is a package var so tests can point the client at a local
// httptest server.
var exaAPIURL = "https://api.exa.ai"

// exaRetryDelays is the backoff schedule between retry attempts. Each
// delay is randomized with 25% jitter before use. It is a var so tests can
// shrink it.
var exaRetryDelays = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
}

// exaMaxRetries is how many retries follow the initial request. Requests
// returning 429 or 5xx are retried with backoff.
const exaMaxRetries = 4

// exaMaxCharacters is the largest maxCharacters value the Exa API accepts
// for text content. The API rejects requests that exceed it.
const exaMaxCharacters = 1_000_000

// exaSearchEndpoint returns the Exa search endpoint.
func exaSearchEndpoint() string { return exaAPIURL + "/search" }

// exaContentsEndpoint returns the Exa contents endpoint.
func exaContentsEndpoint() string { return exaAPIURL + "/contents" }

type exaTextConfig struct {
	MaxCharacters int `json:"maxCharacters,omitempty"`
}

type exaHighlightsConfig struct {
	Query         string `json:"query,omitempty"`
	MaxCharacters int    `json:"maxCharacters,omitempty"`
}

type exaSearchContents struct {
	Text       *exaTextConfig       `json:"text,omitempty"`
	Highlights *exaHighlightsConfig `json:"highlights,omitempty"`
}

type exaSearchRequest struct {
	Query      string             `json:"query"`
	Type       string             `json:"type,omitempty"`
	NumResults int                `json:"numResults,omitempty"`
	Contents   *exaSearchContents `json:"contents,omitempty"`
}

type exaSearchResult struct {
	Title      string   `json:"title,omitempty"`
	URL        string   `json:"url,omitempty"`
	Highlights []string `json:"highlights,omitempty"`
	Text       string   `json:"text,omitempty"`
}

type exaSearchResponse struct {
	RequestID string            `json:"requestId,omitempty"`
	Results   []exaSearchResult `json:"results,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type exaContentsRequest struct {
	URLs []string       `json:"urls"`
	Text *exaTextConfig `json:"text,omitempty"`
}

type exaContentsError struct {
	Tag            string `json:"tag,omitempty"`
	HTTPStatusCode int    `json:"httpStatusCode,omitempty"`
}

type exaContentsStatus struct {
	ID     string            `json:"id,omitempty"`
	Status string            `json:"status,omitempty"`
	Error  *exaContentsError `json:"error,omitempty"`
}

type exaContentsResult struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
	Text  string `json:"text,omitempty"`
}

type exaContentsResponse struct {
	Results  []exaContentsResult `json:"results,omitempty"`
	Statuses []exaContentsStatus `json:"statuses,omitempty"`
	Error    string              `json:"error,omitempty"`
}

type exaAPIError struct {
	Error     string `json:"error,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	Tag       string `json:"tag,omitempty"`
}

// isRetryableExaStatus reports whether an HTTP status should be retried
// with backoff: rate limiting and transient server failures.
func isRetryableExaStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// exaRetryBackoff computes the wait before the next attempt. The
// Retry-After header wins when present; otherwise the fixed schedule
// applies with 25% jitter.
func exaRetryBackoff(resp *http.Response, attempt int) time.Duration {
	if resp != nil && resp.Header.Get("Retry-After") != "" {
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}

	delay := exaRetryDelays[len(exaRetryDelays)-1]
	if attempt >= 0 && attempt < len(exaRetryDelays) {
		delay = exaRetryDelays[attempt]
	}
	return jitterDuration(delay, 0.25)
}

// jitterDuration randomizes d by up to ±fraction around its base value.
func jitterDuration(d time.Duration, fraction float64) time.Duration {
	spread := time.Duration(float64(d) * fraction)
	if spread <= 0 {
		return d
	}
	return d - spread + time.Duration(rand.Int64N(2*int64(spread)+1))
}

// doExaRequest sends a JSON request to the Exa API, retrying transient
// failures (429 and 5xx) with backoff.
func doExaRequest(ctx context.Context, client *http.Client, method, endpoint, apiKey string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to encode Exa request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("failed to create Exa request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to execute Exa request: %w", err)
		}

		if isRetryableExaStatus(resp.StatusCode) && attempt < exaMaxRetries {
			wait := exaRetryBackoff(resp, attempt)
			_ = resp.Body.Close()
			if err := waitAsleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		defer resp.Body.Close()

		data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return fmt.Errorf("failed to read Exa response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			var apiErr exaAPIError
			if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
				return fmt.Errorf("exa API error (HTTP %d): %s", resp.StatusCode, apiErr.Error)
			}
			return fmt.Errorf("exa API error: HTTP %d", resp.StatusCode)
		}

		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}

// waitAsleep blocks until the given duration elapses or the context is
// canceled, whichever happens first.
func waitAsleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// searchExa performs a web search through the Exa API.
func searchExa(ctx context.Context, client *http.Client, apiKey, query string, maxResults int) ([]SearchResult, error) {
	reqBody := exaSearchRequest{
		Query:      query,
		Type:       "auto",
		NumResults: maxResults,
		Contents: &exaSearchContents{
			Highlights: &exaHighlightsConfig{Query: query, MaxCharacters: 400},
			Text:       &exaTextConfig{MaxCharacters: 800},
		},
	}

	var resp exaSearchResponse
	if err := doExaRequest(ctx, client, http.MethodPost, exaSearchEndpoint(), apiKey, reqBody, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("exa search failed: %s", resp.Error)
	}

	results := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r.URL == "" {
			continue
		}

		snippet := ""
		if len(r.Highlights) > 0 {
			snippet = strings.TrimSpace(r.Highlights[0])
		}
		if snippet == "" {
			snippet = strings.TrimSpace(r.Text)
		}

		results = append(results, SearchResult{
			Title:    strings.TrimSpace(r.Title),
			Link:     r.URL,
			Snippet:  snippet,
			Position: len(results) + 1,
		})
	}
	return results, nil
}

// fetchExaContents fetches a single URL's content through the Exa contents
// API, which returns clean markdown text. maxCharacters caps the returned
// text; 0 allows the API default. Values above the API's limit are clamped
// so requests never fail validation.
func fetchExaContents(ctx context.Context, client *http.Client, apiKey, rawURL string, maxCharacters int) (string, error) {
	reqBody := exaContentsRequest{URLs: []string{rawURL}}
	if maxCharacters > exaMaxCharacters {
		maxCharacters = exaMaxCharacters
	}
	if maxCharacters > 0 {
		reqBody.Text = &exaTextConfig{MaxCharacters: maxCharacters}
	}

	var resp exaContentsResponse
	if err := doExaRequest(ctx, client, http.MethodPost, exaContentsEndpoint(), apiKey, reqBody, &resp); err != nil {
		return "", err
	}

	// The contents endpoint returns HTTP 200 even when a URL fails; per-URL
	// errors are reported in statuses.
	for _, status := range resp.Statuses {
		if status.Status == "error" {
			if status.Error != nil {
				return "", fmt.Errorf("exa failed to fetch %s: %s", rawURL, status.Error.Tag)
			}
			return "", fmt.Errorf("exa failed to fetch %s", rawURL)
		}
	}

	if len(resp.Results) == 0 {
		return "", fmt.Errorf("exa returned no content for %s", rawURL)
	}

	content := strings.TrimSpace(resp.Results[0].Text)
	if content == "" {
		return "", fmt.Errorf("exa returned empty content for %s", rawURL)
	}
	return content, nil
}

// FetchURLContent fetches a URL, routing through the Exa contents API when
// the resolved web backend is Exa and falling back to a direct HTTP fetch
// otherwise.
func FetchURLContent(ctx context.Context, client *http.Client, resolver WebBackendResolver, rawURL string) (string, error) {
	backend, apiKey := resolveWebBackend(resolver)
	if backend == WebBackendExa {
		if apiKey == "" {
			return "", fmt.Errorf("web backend is set to Exa but EXA_API_KEY is not set")
		}
		return fetchExaContents(ctx, client, apiKey, rawURL, exaMaxCharacters)
	}
	return FetchURLAndConvert(ctx, client, rawURL)
}
