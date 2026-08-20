package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountID(t *testing.T) {
	t.Parallel()

	claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`
	token := fakeToken(claims)
	require.Equal(t, "acct-123", AccountID(token))
}

func TestAccountIDInvalid(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", AccountID("not-a-jwt"))
	require.Equal(t, "", AccountID("a.b"))
	require.Equal(t, "", AccountID("a.b.c"))
	token := fakeToken(`{"other":true}`)
	require.Equal(t, "", AccountID(token))
}

func fakeToken(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return fmt.Sprintf("%s.%s.sig", header, body)
}

func TestParseInterval(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5, parseInterval(float64(5)))
	require.Equal(t, 7, parseInterval("7"))
	require.Equal(t, 0, parseInterval("slow"))
	require.Equal(t, 0, parseInterval(nil))
}

func TestRequestDeviceCode(t *testing.T) {
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, deviceUserCodePath, r.URL.Path)
		require.Equal(t, UserAgent, r.Header.Get("User-Agent"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_auth_id":"da-1","user_code":"ABCD-EFGH","interval":7}`))
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()

	dc, err := RequestDeviceCode(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"client_id": clientID}, gotBody)
	require.Equal(t, "da-1", dc.DeviceCode)
	require.Equal(t, "ABCD-EFGH", dc.UserCode)
	require.Equal(t, 7, dc.Interval)
	require.Equal(t, int(deviceCodeTimeout.Seconds()), dc.ExpiresIn)
}

func TestRequestDeviceCodeNotAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()

	_, err := RequestDeviceCode(context.Background())
	require.ErrorIs(t, err, ErrNotAvailable)
}

func TestRequestDeviceCodeMissingFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"device_auth_id":"da-1"}`))
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()

	_, err := RequestDeviceCode(context.Background())
	require.ErrorContains(t, err, "missing fields")
}

func TestTryGetToken(t *testing.T) {
	t.Run("pending on 403", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"deviceauth_authorization_pending"}}`))
		}))
		defer server.Close()
		old := baseURL
		baseURL = server.URL
		defer func() { baseURL = old }()

		_, _, err := tryGetToken(context.Background(), "da-1", "ABCD")
		require.ErrorIs(t, err, errPending)
	})

	t.Run("slow_down", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"slow_down"}}`))
		}))
		defer server.Close()
		old := baseURL
		baseURL = server.URL
		defer func() { baseURL = old }()

		_, _, err := tryGetToken(context.Background(), "da-1", "ABCD")
		require.ErrorIs(t, err, errSlowDown)
	})

	t.Run("success", func(t *testing.T) {
		var gotBody map[string]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			_, _ = w.Write([]byte(`{"authorization_code":"code-1","code_verifier":"verifier-1"}`))
		}))
		defer server.Close()
		old := baseURL
		baseURL = server.URL
		defer func() { baseURL = old }()

		code, verifier, err := tryGetToken(context.Background(), "da-1", "ABCD")
		require.NoError(t, err)
		require.Equal(t, "code-1", code)
		require.Equal(t, "verifier-1", verifier)
		require.Equal(t, map[string]string{"device_auth_id": "da-1", "user_code": "ABCD"}, gotBody)
	})
}

func TestExchangeToken(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, oauthTokenPath, r.URL.Path)
		require.NoError(t, r.ParseForm())
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`))
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()

	token, err := ExchangeToken(context.Background(), "code-1", "verifier-1")
	require.NoError(t, err)
	require.Equal(t, "at-1", token.AccessToken)
	require.Equal(t, "rt-1", token.RefreshToken)
	require.Equal(t, 3600, token.ExpiresIn)
	require.NotZero(t, token.ExpiresAt)

	require.Equal(t, "authorization_code", gotForm.Get("grant_type"))
	require.Equal(t, clientID, gotForm.Get("client_id"))
	require.Equal(t, "code-1", gotForm.Get("code"))
	require.Equal(t, "verifier-1", gotForm.Get("code_verifier"))
	require.Equal(t, deviceRedirectURI, gotForm.Get("redirect_uri"))
}

func TestRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
		require.Equal(t, "rt-1", r.PostForm.Get("refresh_token"))
		require.Equal(t, clientID, r.PostForm.Get("client_id"))
		_, _ = w.Write([]byte(`{"access_token":"at-2","refresh_token":"rt-2","expires_in":3600}`))
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()

	token, err := RefreshToken(context.Background(), "rt-1")
	require.NoError(t, err)
	require.Equal(t, "at-2", token.AccessToken)
	require.Equal(t, "rt-2", token.RefreshToken)
}

func TestExchangeTokenError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()
	old := baseURL
	baseURL = server.URL
	defer func() { baseURL = old }()

	_, err := ExchangeToken(context.Background(), "code-1", "verifier-1")
	require.ErrorContains(t, err, "token exchange failed")
	require.True(t, strings.Contains(err.Error(), "invalid_grant"))
}
