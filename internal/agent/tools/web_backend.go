package tools

import "os"

// WebBackend identifies the backend used for web search and web fetching.
type WebBackend string

const (
	// WebBackendDefault resolves the backend automatically: Firecrawl when
	// FIRECRAWL_API_KEY is set, then Exa when EXA_API_KEY is set, and
	// DuckDuckGo (search) or direct HTTP (fetch) otherwise.
	WebBackendDefault WebBackend = "default"

	// WebBackendFirecrawl forces the Firecrawl backend for searches and
	// fetches.
	WebBackendFirecrawl WebBackend = "firecrawl"

	// WebBackendExa forces the Exa backend for searches and fetches.
	WebBackendExa WebBackend = "exa"
)

// WebBackendResolver returns the configured web backend at call time so
// runtime option changes take effect without rebuilding tools. A nil
// resolver always resolves to WebBackendDefault.
type WebBackendResolver func() WebBackend

// resolveWebBackend returns the effective backend and API key for the
// current call. The explicit setting wins when set. The default resolves
// in priority order: Firecrawl, then Exa, then the built-in direct paths.
func resolveWebBackend(resolver WebBackendResolver) (WebBackend, string) {
	setting := WebBackendDefault
	if resolver != nil {
		setting = resolver()
	}

	firecrawlKey := os.Getenv("FIRECRAWL_API_KEY")
	exaKey := os.Getenv("EXA_API_KEY")

	switch setting {
	case WebBackendFirecrawl:
		return WebBackendFirecrawl, firecrawlKey
	case WebBackendExa:
		return WebBackendExa, exaKey
	}

	// Default: prefer Firecrawl, then Exa, when a key is available.
	if firecrawlKey != "" {
		return WebBackendFirecrawl, firecrawlKey
	}
	if exaKey != "" {
		return WebBackendExa, exaKey
	}
	return WebBackendDefault, ""
}
