package tools

import (
	"context"
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"charm.land/fantasy"
)

//go:embed web_search.md.tpl
var webSearchDescriptionTmpl []byte

var webSearchDescriptionTpl = template.Must(
	template.New("webSearchDescription").
		Parse(string(webSearchDescriptionTmpl)),
)

// NewWebSearchTool creates a web search tool for sub-agents (no permissions
// needed). The backend resolver selects Firecrawl, Exa, or DuckDuckGo per
// call; pass nil to always use the default resolution.
func NewWebSearchTool(client *http.Client, backendResolver WebBackendResolver) fantasy.AgentTool {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 10
		transport.IdleConnTimeout = 90 * time.Second

		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		}
	}

	return fantasy.NewParallelAgentTool(
		WebSearchToolName,
		renderToolDescription(webSearchDescriptionTpl),
		func(ctx context.Context, params WebSearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Query == "" {
				return fantasy.NewTextErrorResponse("query is required"), nil
			}

			maxResults := params.MaxResults
			if maxResults <= 0 {
				maxResults = 10
			}
			if maxResults > 20 {
				maxResults = 20
			}

			backend, apiKey := resolveWebBackend(backendResolver)
			var results []SearchResult
			var err error
			switch backend {
			case WebBackendFirecrawl:
				if apiKey == "" {
					return fantasy.NewTextErrorResponse("Web backend is set to Firecrawl but FIRECRAWL_API_KEY is not set"), nil
				}
				results, err = searchFirecrawl(ctx, client, apiKey, params.Query, maxResults)
			case WebBackendExa:
				if apiKey == "" {
					return fantasy.NewTextErrorResponse("Web backend is set to Exa but EXA_API_KEY is not set"), nil
				}
				results, err = searchExa(ctx, client, apiKey, params.Query, maxResults)
			default:
				maybeDelaySearch()
				results, err = searchDuckDuckGo(ctx, client, params.Query, maxResults)
			}
			slog.Debug("Web search completed", "query", params.Query, "results", len(results), "err", err)
			if err != nil {
				return fantasy.NewTextErrorResponse("Failed to search: " + err.Error()), nil
			}

			return fantasy.NewTextResponse(formatSearchResults(results)), nil
		},
	)
}
