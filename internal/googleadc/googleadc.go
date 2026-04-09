package googleadc

import (
	"context"
	"net/http"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
)

const (
	// CloudPlatformScope is the scope required for Vertex AI API access.
	CloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
)

// NewTokenProvider resolves Google Application Default Credentials and returns
// a token provider that refreshes access tokens automatically.
func NewTokenProvider(ctx context.Context) (auth.TokenProvider, error) {
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: []string{CloudPlatformScope},
	})
	if err != nil {
		return nil, err
	}
	return creds.TokenProvider, nil
}

// NewHTTPClient creates an HTTP client that injects Google ADC bearer tokens on
// every request while preserving the base client's other settings.
func NewHTTPClient(ctx context.Context, base *http.Client) (*http.Client, error) {
	tokenProvider, err := NewTokenProvider(ctx)
	if err != nil {
		return nil, err
	}
	return NewHTTPClientWithTokenProvider(base, tokenProvider), nil
}

// NewHTTPClientWithTokenProvider creates an HTTP client that injects bearer
// tokens from the provided token provider. This is primarily intended for
// testing.
func NewHTTPClientWithTokenProvider(base *http.Client, tokenProvider auth.TokenProvider) *http.Client {
	if base == nil {
		base = &http.Client{}
	}

	client := *base
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &transportWithADC{
		base:          transport,
		tokenProvider: tokenProvider,
	}
	return &client
}

type transportWithADC struct {
	base          http.RoundTripper
	tokenProvider auth.TokenProvider
}

func (t *transportWithADC) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokenProvider.Token(req.Context())
	if err != nil {
		return nil, err
	}

	clonedReq := req.Clone(req.Context())
	clonedReq.Header = req.Header.Clone()
	tokenType := token.Type
	if tokenType == "" {
		tokenType = "Bearer"
	}
	clonedReq.Header.Set("Authorization", tokenType+" "+token.Value)
	return t.base.RoundTrip(clonedReq)
}
