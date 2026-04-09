package googleadc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloud.google.com/go/auth"
	"github.com/stretchr/testify/require"
)

type staticTokenProvider struct {
	token *auth.Token
}

func (s staticTokenProvider) Token(context.Context) (*auth.Token, error) {
	return s.token, nil
}

func TestNewHTTPClientWithTokenProvider(t *testing.T) {
	t.Parallel()

	t.Run("injects bearer token", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := NewHTTPClientWithTokenProvider(nil, staticTokenProvider{
			token: &auth.Token{Value: "test-token"},
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	})

	t.Run("overrides existing authorization header", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "Bearer refreshed-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := NewHTTPClientWithTokenProvider(nil, staticTokenProvider{
			token: &auth.Token{Value: "refreshed-token"},
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer stale-token")

		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	})
}
