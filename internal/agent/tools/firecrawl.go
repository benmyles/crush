package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	firecrawl "github.com/firecrawl/firecrawl/apps/go-sdk"
	"github.com/firecrawl/firecrawl/apps/go-sdk/option"
)

// firecrawlAPIURL is a package var so tests can point the client at a local
// httptest server. When empty, the Firecrawl SDK uses its default API URL.
var firecrawlAPIURL = ""

// newFirecrawlClient builds a Firecrawl client for the supplied API key.
// The SDK resolves the key itself from FIRECRAWL_API_KEY when key is empty,
// but callers always pass an explicit key so custom API URLs stay isolated
// from the ambient environment. Retries for transient failures are handled
// by the SDK's built-in backoff.
func newFirecrawlClient(apiKey string, client *http.Client) (*firecrawl.Client, error) {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if firecrawlAPIURL != "" {
		opts = append(opts, option.WithAPIURL(firecrawlAPIURL))
	}
	if client != nil {
		opts = append(opts, option.WithHTTPClient(client))
	}
	return firecrawl.NewClient(opts...)
}

// searchFirecrawl performs a web search through the Firecrawl API. Search
// returns title, URL, and description for each result; descriptions are
// used as snippets.
func searchFirecrawl(ctx context.Context, client *http.Client, apiKey, query string, maxResults int) ([]SearchResult, error) {
	fc, err := newFirecrawlClient(apiKey, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create Firecrawl client: %w", err)
	}

	limit := maxResults
	data, err := fc.Search(ctx, query, &firecrawl.SearchOptions{
		Limit: &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("firecrawl search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(data.Web))
	for _, item := range data.Web {
		rawURL, _ := item["url"].(string)
		if rawURL == "" {
			continue
		}

		title, _ := item["title"].(string)
		snippet, _ := item["description"].(string)
		if snippet == "" {
			if markdown, ok := item["markdown"].(string); ok && markdown != "" {
				snippet = markdown
			}
		}

		results = append(results, SearchResult{
			Title:    strings.TrimSpace(title),
			Link:     rawURL,
			Snippet:  strings.TrimSpace(snippet),
			Position: len(results) + 1,
		})
	}
	return results, nil
}

// fetchFirecrawl fetches a single URL's content through the Firecrawl
// scrape API, which returns clean markdown. maxCharacters caps the returned
// text after the fact; 0 keeps the full content.
func fetchFirecrawl(ctx context.Context, client *http.Client, apiKey, rawURL string, maxCharacters int) (string, error) {
	fc, err := newFirecrawlClient(apiKey, client)
	if err != nil {
		return "", fmt.Errorf("failed to create Firecrawl client: %w", err)
	}

	onlyMainContent := true
	removeBase64Images := true
	doc, err := fc.Scrape(ctx, rawURL, &firecrawl.ScrapeOptions{
		Formats:            []string{"markdown"},
		OnlyMainContent:    &onlyMainContent,
		RemoveBase64Images: &removeBase64Images,
	})
	if err != nil {
		return "", fmt.Errorf("firecrawl failed to fetch %s: %w", rawURL, err)
	}

	content := strings.TrimSpace(doc.Markdown)
	if content == "" {
		return "", fmt.Errorf("firecrawl returned empty content for %s", rawURL)
	}
	if maxCharacters > 0 && len(content) > maxCharacters {
		content = content[:maxCharacters]
	}
	return content, nil
}
