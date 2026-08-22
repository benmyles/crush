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

// serveFirecrawlStub points the Firecrawl client at a local server whose
// handler is provided by the caller, and restores the API URL on cleanup.
func serveFirecrawlStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	origURL := firecrawlAPIURL
	firecrawlAPIURL = srv.URL
	t.Cleanup(func() {
		firecrawlAPIURL = origURL
	})
}

// stubFirecrawlBackoff replaces the retry backoff with a no-sleep version
// so retry tests run fast, restoring it on cleanup.
func stubFirecrawlBackoff(t *testing.T) {
	t.Helper()
	origBackoff := firecrawlRetryBackoff
	firecrawlRetryBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() {
		firecrawlRetryBackoff = origBackoff
	})
}

func TestSearchFirecrawl(t *testing.T) {
	serveFirecrawlStub(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/search", r.URL.Path)
		require.Equal(t, "Bearer fc-test-key", r.Header.Get("Authorization"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "example query", body["query"])
		require.Equal(t, float64(10), body["limit"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"web": [
					{"title": "Example Page", "url": "https://example.com/post", "description": "A description snippet."},
					{"title": "Markdown Only", "url": "https://example.com/md", "markdown": "# Heading\n\nBody."},
					{"title": "Missing URL"}
				]
			}
		}`))
	})

	results, err := searchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "example query", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "Example Page", results[0].Title)
	require.Equal(t, "https://example.com/post", results[0].Link)
	require.Equal(t, "A description snippet.", results[0].Snippet)
	require.Equal(t, 1, results[0].Position)
	require.Equal(t, "# Heading\n\nBody.", results[1].Snippet)
	require.Equal(t, 2, results[1].Position)
}

func TestSearchFirecrawlSurfacesAPIError(t *testing.T) {
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success": false, "error": "Invalid API key"}`))
	})

	_, err := searchFirecrawl(context.Background(), http.DefaultClient, "bad-key", "query", 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Invalid API key")
}

func TestFetchFirecrawl(t *testing.T) {
	serveFirecrawlStub(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/scrape", r.URL.Path)
		require.Equal(t, "Bearer fc-test-key", r.Header.Get("Authorization"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "https://example.com/doc", body["url"])
		formats, ok := body["formats"].([]any)
		require.True(t, ok)
		require.Contains(t, formats, "markdown")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {"markdown": "# Heading\n\nBody content.", "metadata": {"title": "Docs"}}
		}`))
	})

	content, err := fetchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "https://example.com/doc", 10_000)
	require.NoError(t, err)
	require.Equal(t, "# Heading\n\nBody content.", content)
}

func TestFetchFirecrawlClampsMaxCharacters(t *testing.T) {
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": {"markdown": "1234567890"}}`))
	})

	content, err := fetchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "https://example.com/doc", 5)
	require.NoError(t, err)
	require.Equal(t, "12345", content)
}

func TestFetchFirecrawlEmptyContent(t *testing.T) {
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": {"markdown": ""}}`))
	})

	_, err := fetchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "https://example.com/empty", 10_000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty content")
}

func TestFetchFirecrawlURLError(t *testing.T) {
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "Not found"}`))
	})

	_, err := fetchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "https://example.com/missing", 10_000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Not found")
}

func TestFetchURLContentFirecrawlRouting(t *testing.T) {
	var firecrawlHits atomic.Int32
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		firecrawlHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": {"markdown": "via firecrawl"}}`))
	})

	// With a key in the environment, the default backend resolves to
	// Firecrawl.
	t.Setenv("FIRECRAWL_API_KEY", "fc-test-key")
	t.Setenv("EXA_API_KEY", "")
	content, err := FetchURLContent(context.Background(), http.DefaultClient, nil, "https://example.com/doc")
	require.NoError(t, err)
	require.Equal(t, "via firecrawl", content)
	require.Equal(t, int32(1), firecrawlHits.Load())

	// Without a key, the explicit Firecrawl backend reports a clear error.
	t.Setenv("FIRECRAWL_API_KEY", "")
	_, err = FetchURLContent(context.Background(), http.DefaultClient, func() WebBackend { return WebBackendFirecrawl }, "https://example.com/doc")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "FIRECRAWL_API_KEY"))
}

func TestFetchFirecrawlRetriesOn429(t *testing.T) {
	stubFirecrawlBackoff(t)
	var hits atomic.Int32
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success": false, "error": "Rate limit exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": {"markdown": "recovered"}}`))
	})

	type retryEvent struct {
		attempt    int
		statusCode int
		delay      time.Duration
	}
	var events []retryEvent
	ctx := WithRetryNotifier(context.Background(), func(attempt, statusCode int, delay time.Duration) {
		events = append(events, retryEvent{attempt, statusCode, delay})
	})

	content, err := fetchFirecrawl(ctx, http.DefaultClient, "fc-test-key", "https://example.com/doc", 10_000)
	require.NoError(t, err)
	require.Equal(t, "recovered", content)
	require.Equal(t, int32(3), hits.Load())
	require.Len(t, events, 2)
	require.Equal(t, 1, events[0].attempt)
	require.Equal(t, http.StatusTooManyRequests, events[0].statusCode)
	require.Equal(t, 2, events[1].attempt)
}

func TestFetchFirecrawl429ExhaustsRetries(t *testing.T) {
	stubFirecrawlBackoff(t)
	var hits atomic.Int32
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"success": false, "error": "Rate limit exceeded"}`))
	})

	_, err := fetchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "https://example.com/doc", 10_000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Rate limit exceeded")
	require.Equal(t, int32(firecrawlMaxRetries+1), hits.Load())
}

func TestFetchFirecrawlDoesNotRetryOnServerError(t *testing.T) {
	stubFirecrawlBackoff(t)
	var hits atomic.Int32
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success": false, "error": "Internal error"}`))
	})

	notified := false
	ctx := WithRetryNotifier(context.Background(), func(_, _ int, _ time.Duration) {
		notified = true
	})

	_, err := fetchFirecrawl(ctx, http.DefaultClient, "fc-test-key", "https://example.com/doc", 10_000)
	require.Error(t, err)
	require.Equal(t, int32(1), hits.Load())
	require.False(t, notified)
}

func TestSearchFirecrawlRetriesOn429(t *testing.T) {
	stubFirecrawlBackoff(t)
	var hits atomic.Int32
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success": false, "error": "Rate limit exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {"web": [{"title": "Example", "url": "https://example.com", "description": "snippet"}]}
		}`))
	})

	results, err := searchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "query", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, int32(2), hits.Load())
}

func TestFetchFirecrawlRequestTimeout(t *testing.T) {
	origTimeout := firecrawlRequestTimeout
	firecrawlRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		firecrawlRequestTimeout = origTimeout
	})

	var hits atomic.Int32
	serveFirecrawlStub(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": {"markdown": "too late"}}`))
	})

	_, err := fetchFirecrawl(context.Background(), http.DefaultClient, "fc-test-key", "https://example.com/slow", 10_000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "context deadline exceeded")
	require.Equal(t, int32(1), hits.Load())
}
