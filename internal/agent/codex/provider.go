// Package codex provides the OpenAI Codex subscription provider catalog.
package codex

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"log/slog"
	"os"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
)

//go:embed provider.json
var embedded []byte

// Embedded returns the embedded Codex provider catalog.
var Embedded = sync.OnceValue(func() catwalk.Provider {
	var provider catwalk.Provider
	if err := json.Unmarshal(embedded, &provider); err != nil {
		slog.Error("Could not use embedded provider data", "err", err)
	}
	return provider
})

const (
	// Name is the default name of this provider.
	Name = "codex"
	// DisplayName is the display name of OpenAI Codex.
	DisplayName = "OpenAI Codex"
	// defaultBaseURL points at the ChatGPT backend Codex Responses
	// endpoint.
	defaultBaseURL = "https://chatgpt.com/backend-api/codex"
	// originator identifies Crush to the Codex backend.
	originator = "crush"
	// UserAgent identifies Crush to the Codex backend.
	UserAgent = "crush"
)

// BaseURL returns the base URL, which is either $CODEX_URL or the
// default ChatGPT backend endpoint.
var BaseURL = sync.OnceValue(func() string {
	return cmp.Or(os.Getenv("CODEX_URL"), defaultBaseURL)
})

// Headers returns the static headers for Codex inference requests. The
// Authorization header is added by the provider transport from the
// stored access token.
func Headers(accountID string) map[string]string {
	return map[string]string{
		"User-Agent":         UserAgent,
		"Originator":         originator,
		"OpenAI-Beta":        "responses=experimental",
		"Chatgpt-Account-Id": accountID,
	}
}
