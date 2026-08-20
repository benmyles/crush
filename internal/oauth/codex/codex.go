// Package codex provides OpenAI Codex (ChatGPT) integration.
package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/oauth"
)

const (
	// clientID is the official Codex CLI OAuth client ID.
	clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	authBaseURL = "https://auth.openai.com"

	deviceUserCodePath = "/api/accounts/deviceauth/usercode"
	deviceTokenPath    = "/api/accounts/deviceauth/token"
	oauthTokenPath     = "/oauth/token"

	// deviceRedirectURI is the redirect used when exchanging device-flow
	// authorization codes. The server matches it against the value it
	// issued, so it must stay fixed.
	deviceRedirectURI = "https://auth.openai.com/deviceauth/callback"

	// VerificationURI is where users confirm their device code.
	VerificationURI = "https://auth.openai.com/codex/device"

	// deviceCodeTimeout is how long the user has to authorize before the
	// flow gives up.
	deviceCodeTimeout = 15 * time.Minute

	// UserAgent identifies Crush to the auth endpoints.
	UserAgent = "crush"
)

// baseURL is the OAuth auth server base URL. It's a variable so tests
// can point the flow at a local server.
var baseURL = authBaseURL

func endpoint(path string) string {
	return baseURL + path
}

// ErrNotAvailable is returned when the account is not eligible for the
// Codex subscription login flow.
var ErrNotAvailable = errors.New("openai codex device login not available for this account")

var (
	errPending  = errors.New("pending")
	errSlowDown = errors.New("slow_down")
)

// DeviceCode is an OpenAI Codex device authorization grant. DeviceCode
// carries the device_auth_id used when polling for authorization.
type DeviceCode struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	Interval   int    `json:"interval"`
	ExpiresIn  int    `json:"expires_in"`
}

// RequestDeviceCode initiates the device code flow with OpenAI.
func RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	body, err := json.Marshal(map[string]string{"client_id": clientID})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(deviceUserCodePath), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotAvailable
	}
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed: %s - %s", resp.Status, string(responseBody))
	}

	var result struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     any    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.DeviceAuthID == "" || result.UserCode == "" {
		return nil, fmt.Errorf("device code response missing fields: %v", result)
	}

	return &DeviceCode{
		DeviceCode: result.DeviceAuthID,
		UserCode:   result.UserCode,
		Interval:   parseInterval(result.Interval),
		ExpiresIn:  int(deviceCodeTimeout.Seconds()),
	}, nil
}

func parseInterval(v any) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// PollForToken polls OpenAI until the user authorizes the device code,
// then exchanges the authorization code for tokens.
func PollForToken(ctx context.Context, dc *DeviceCode) (*oauth.Token, error) {
	interval := max(dc.Interval, 5)
	deadline := time.Now().Add(deviceCodeTimeout)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		code, verifier, err := tryGetToken(ctx, dc.DeviceCode, dc.UserCode)
		switch {
		case errors.Is(err, errPending):
			continue
		case errors.Is(err, errSlowDown):
			interval += 5
			ticker.Reset(time.Duration(interval) * time.Second)
			continue
		case err != nil:
			return nil, err
		}
		return ExchangeToken(ctx, code, verifier)
	}

	return nil, fmt.Errorf("authorization timed out")
}

func tryGetToken(ctx context.Context, deviceAuthID, userCode string) (authorizationCode, codeVerifier string, err error) {
	body, err := json.Marshal(map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	})
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(deviceTokenPath), strings.NewReader(string(body)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result struct {
			AuthorizationCode string `json:"authorization_code"`
			CodeVerifier      string `json:"code_verifier"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", "", err
		}
		if result.AuthorizationCode == "" || result.CodeVerifier == "" {
			return "", "", fmt.Errorf("device auth token response missing fields: %v", result)
		}
		return result.AuthorizationCode, result.CodeVerifier, nil
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return "", "", errPending
	}

	responseBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &result); err == nil {
		code := parseErrorCode(result.Error)
		switch code {
		case "deviceauth_authorization_pending", "deviceauth_authorization_not_found":
			return "", "", errPending
		case "slow_down", "deviceauth_slow_down":
			return "", "", errSlowDown
		}
	}

	return "", "", fmt.Errorf("device auth failed: %s - %s", resp.Status, string(responseBody))
}

func parseErrorCode(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case map[string]any:
		if code, ok := v["code"].(string); ok {
			return code
		}
	}
	return ""
}

// ExchangeToken exchanges a device-flow authorization code for tokens.
func ExchangeToken(ctx context.Context, authorizationCode, codeVerifier string) (*oauth.Token, error) {
	return exchangeToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {authorizationCode},
		"code_verifier": {codeVerifier},
		"redirect_uri":  {deviceRedirectURI},
	})
}

// RefreshToken refreshes an expired Codex access token.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	return exchangeToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	})
}

func exchangeToken(ctx context.Context, form url.Values) (*oauth.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(oauthTokenPath), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("codex token exchange failed: %s - %s", resp.Status, string(responseBody))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, err
	}
	if result.AccessToken == "" || result.RefreshToken == "" || result.ExpiresIn == 0 {
		return nil, fmt.Errorf("codex token response missing fields: %s", string(responseBody))
	}

	token := &oauth.Token{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}
	token.SetExpiresAt()
	return token, nil
}

// AccountID extracts the chatgpt_account_id claim from an access token.
// It returns an empty string when the token cannot be decoded or lacks
// the claim.
func AccountID(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var claims struct {
		Auth map[string]string `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Auth["chatgpt_account_id"]
}
