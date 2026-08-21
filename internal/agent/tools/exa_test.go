package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// serveExaStub points the Exa client at a local server whose handler is
// provided by the caller, and restores the endpoint and backoff on cleanup.
func serveExaStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	origEndpoint := exaAPIURL
	origDelays := exaRetryDelays
	exaAPIURL = srv.URL
	exaRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() {
		exaAPIURL = origEndpoint
		exaRetryDelays = origDelays
	})
}

func TestResolveWebBackend(t *testing.T) {
	t.Run("default without keys falls back", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "")
		t.Setenv("EXA_API_KEY", "")
		backend, key := resolveWebBackend(nil)
		require.Equal(t, WebBackendDefault, backend)
		require.Empty(t, key)
	})

	t.Run("default with only exa key prefers exa", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "")
		t.Setenv("EXA_API_KEY", "test-key")
		backend, key := resolveWebBackend(nil)
		require.Equal(t, WebBackendExa, backend)
		require.Equal(t, "test-key", key)
	})

	t.Run("default with only firecrawl key prefers firecrawl", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "fc-test-key")
		t.Setenv("EXA_API_KEY", "")
		backend, key := resolveWebBackend(nil)
		require.Equal(t, WebBackendFirecrawl, backend)
		require.Equal(t, "fc-test-key", key)
	})

	t.Run("default with both keys prefers firecrawl", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "fc-test-key")
		t.Setenv("EXA_API_KEY", "test-key")
		backend, key := resolveWebBackend(nil)
		require.Equal(t, WebBackendFirecrawl, backend)
		require.Equal(t, "fc-test-key", key)
	})

	t.Run("explicit exa wins without key", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "")
		t.Setenv("EXA_API_KEY", "")
		backend, key := resolveWebBackend(func() WebBackend { return WebBackendExa })
		require.Equal(t, WebBackendExa, backend)
		require.Empty(t, key)
	})

	t.Run("explicit firecrawl wins without key", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "")
		t.Setenv("EXA_API_KEY", "")
		backend, key := resolveWebBackend(func() WebBackend { return WebBackendFirecrawl })
		require.Equal(t, WebBackendFirecrawl, backend)
		require.Empty(t, key)
	})

	t.Run("explicit default prefers firecrawl with key", func(t *testing.T) {
		t.Setenv("FIRECRAWL_API_KEY", "fc-test-key")
		t.Setenv("EXA_API_KEY", "test-key")
		backend, key := resolveWebBackend(func() WebBackend { return WebBackendDefault })
		require.Equal(t, WebBackendFirecrawl, backend)
		require.Equal(t, "fc-test-key", key)
	})
}

func TestSearchExa(t *testing.T) {
	serveExaStub(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/search", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{
					"title": "Example Page",
					"url": "https://example.com/post",
					"highlights": ["A highlighted snippet."],
					"text": "Full text fallback."
				},
				{
					"title": "No Content Page",
					"url": "https://example.com/empty",
					"text": "Text only snippet."
				},
				{"title": "Missing URL"}
			]
		}`))
	})

	results, err := searchExa(context.Background(), http.DefaultClient, "test-key", "example query", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "Example Page", results[0].Title)
	require.Equal(t, "https://example.com/post", results[0].Link)
	require.Equal(t, "A highlighted snippet.", results[0].Snippet)
	require.Equal(t, 1, results[0].Position)
	require.Equal(t, "Text only snippet.", results[1].Snippet)
	require.Equal(t, 2, results[1].Position)
}

func TestSearchExaRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	serveExaStub(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"title": "T", "url": "https://example.com", "text": "snippet"}]}`))
	})

	results, err := searchExa(context.Background(), http.DefaultClient, "test-key", "query", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, int32(3), calls.Load())
}

func TestSearchExaGivesUpAfterRetries(t *testing.T) {
	var calls atomic.Int32
	serveExaStub(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": "rate limited"}`))
	})

	_, err := searchExa(context.Background(), http.DefaultClient, "test-key", "query", 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limited")
	require.Equal(t, int32(exaMaxRetries+1), calls.Load())
}

func TestSearchExaSurfacesAPIError(t *testing.T) {
	serveExaStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "Invalid API key"}`))
	})

	_, err := searchExa(context.Background(), http.DefaultClient, "bad-key", "query", 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Invalid API key")
}

func TestExaRetryBackoffSchedule(t *testing.T) {
	for attempt, base := range []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second} {
		got := exaRetryBackoff(nil, attempt)
		require.InDelta(t, float64(base), float64(got), float64(base)*0.25,
			"attempt %d: expected %v ± 25%% jitter, got %v", attempt, base, got)
	}

	// Retry-After wins over the schedule.
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"7"}}}
	require.Equal(t, 7*time.Second, exaRetryBackoff(resp, 0))
}

func TestFetchExaContents(t *testing.T) {
	serveExaStub(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/contents", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [{"title": "Docs", "url": "https://example.com/doc", "text": "# Heading\n\nBody content."}],
			"statuses": [{"id": "https://example.com/doc", "status": "success"}]
		}`))
	})

	content, err := fetchExaContents(context.Background(), http.DefaultClient, "test-key", "https://example.com/doc", 10_000)
	require.NoError(t, err)
	require.Equal(t, "# Heading\n\nBody content.", content)
}

func TestFetchExaContentsClampsMaxCharacters(t *testing.T) {
	serveExaStub(t, func(w http.ResponseWriter, r *http.Request) {
		var req exaContentsRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotNil(t, req.Text)
		require.Equal(t, exaMaxCharacters, req.Text.MaxCharacters)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"title": "Docs", "text": "content."}]}`))
	})

	content, err := fetchExaContents(context.Background(), http.DefaultClient, "test-key", "https://example.com/doc", 2_000_000)
	require.NoError(t, err)
	require.Equal(t, "content.", content)
}

func TestFetchExaContentsURLError(t *testing.T) {
	serveExaStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [],
			"statuses": [{"id": "https://example.com/missing", "status": "error", "error": {"tag": "CRAWL_NOT_FOUND"}}]
		}`))
	})

	_, err := fetchExaContents(context.Background(), http.DefaultClient, "test-key", "https://example.com/missing", 10_000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CRAWL_NOT_FOUND")
}

func TestFetchURLContentRouting(t *testing.T) {
	var exaHits atomic.Int32
	serveExaStub(t, func(w http.ResponseWriter, _ *http.Request) {
		exaHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"text": "via exa"}]}`))
	})

	// With a key in the environment, the default backend resolves to Exa.
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("EXA_API_KEY", "test-key")
	content, err := FetchURLContent(context.Background(), http.DefaultClient, nil, "https://example.com/doc")
	require.NoError(t, err)
	require.Equal(t, "via exa", content)
	require.Equal(t, int32(1), exaHits.Load())

	// Without a key, the explicit Exa backend reports a clear error.
	t.Setenv("EXA_API_KEY", "")
	_, err = FetchURLContent(context.Background(), http.DefaultClient, func() WebBackend { return WebBackendExa }, "https://example.com/doc")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "EXA_API_KEY"))
}
