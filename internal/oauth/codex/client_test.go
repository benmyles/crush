package codex

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestTranslateError(t *testing.T) {
	t.Parallel()

	t.Run("usage limit", func(t *testing.T) {
		t.Parallel()

		body := `{"error":{"code":"usage_limit_reached","message":"original"},"plan_type":"pro","resets_at":"tomorrow"}`
		rewritten, ok := translateError([]byte(body))
		require.True(t, ok)

		var parsed struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal([]byte(rewritten), &parsed))
		require.Equal(t, "usage_limit_reached", parsed.Error.Code)
		require.Contains(t, parsed.Error.Message, "ChatGPT usage limit")
		require.Contains(t, parsed.Error.Message, "pro")
		require.Contains(t, parsed.Error.Message, "tomorrow")
	})

	t.Run("expired login", func(t *testing.T) {
		t.Parallel()

		body := `{"error":{"code":"invalid_api_key","message":"bad key"}}`
		rewritten, ok := translateError([]byte(body))
		require.True(t, ok)
		require.Contains(t, rewritten, "crush login codex")
	})

	t.Run("unknown code untouched", func(t *testing.T) {
		t.Parallel()

		body := `{"error":{"code":"something_else","message":"nope"}}`
		_, ok := translateError([]byte(body))
		require.False(t, ok)
	})

	t.Run("invalid json untouched", func(t *testing.T) {
		t.Parallel()

		_, ok := translateError([]byte("not json"))
		require.False(t, ok)
	})
}

func TestErrorTransport(t *testing.T) {
	t.Parallel()

	t.Run("success passthrough", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("stream-data"))
		}))
		defer server.Close()

		transport := &errorTransport{base: http.DefaultTransport}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("{}"))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "stream-data", string(got))
	})

	t.Run("auth error passthrough", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"no token"}}`))
		}))
		defer server.Close()

		transport := &errorTransport{base: http.DefaultTransport}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("{}"))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"error":{"message":"no token"}}`, string(got))
	})

	t.Run("quota error rewritten", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"rate_limit_exceeded","message":"slow down"},"plan_type":"plus"}`))
		}))
		defer server.Close()

		transport := &errorTransport{base: http.DefaultTransport}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("{}"))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(got), "ChatGPT usage limit")
		require.Contains(t, string(got), "plus")
	})

	t.Run("untranslated error preserved", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"stop"}}`))
		}))
		defer server.Close()

		transport := &errorTransport{base: http.DefaultTransport}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("{}"))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"error":{"code":"bad_request","message":"stop"}}`, string(got))
	})
}

func TestZstdTransportCompressesResponsesRequests(t *testing.T) {
	t.Parallel()

	var (
		gotEncoding string
		gotBody     []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := &http.Client{Transport: &zstdTransport{base: http.DefaultTransport}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, "zstd", gotEncoding)
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	plain, err := decoder.DecodeAll(gotBody, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5.6-sol"}`, string(plain))
}

func TestZstdTransportSkipsNonResponsesRequests(t *testing.T) {
	t.Parallel()

	var (
		gotEncoding string
		gotBody     string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := &http.Client{Transport: &zstdTransport{base: http.DefaultTransport}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/other", strings.NewReader(`{"plain":true}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Empty(t, gotEncoding)
	require.JSONEq(t, `{"plain":true}`, gotBody)
}
