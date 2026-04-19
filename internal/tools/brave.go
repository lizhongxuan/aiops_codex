package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// braveSearchResponse is the top-level JSON returned by the Brave Web Search API.
type braveSearchResponse struct {
	Web struct {
		Results []braveWebResult `json:"results"`
	} `json:"web"`
}

// braveWebResult is a single result entry inside the Brave API response.
type braveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// braveMaxResults is the maximum number of results returned per query.
const braveMaxResults = 10

// BraveSearchHandler executes a web search via the Brave Search API and
// returns results as JSON-encoded []WebSearchResult.
func BraveSearchHandler(apiKey string) ToolHandler {
	return func(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
		query, _ := args["query"].(string)
		if query == "" {
			return "", fmt.Errorf("brave_search: 'query' parameter is required")
		}

		results, err := performBraveSearch(ctx, apiKey, query)
		if err != nil {
			return "", fmt.Errorf("brave_search failed: %w", err)
		}

		data, _ := json.Marshal(results)
		return string(data), nil
	}
}

// performBraveSearch calls the Brave Web Search API and returns up to
// braveMaxResults results.
func performBraveSearch(ctx context.Context, apiKey, query string) ([]WebSearchResult, error) {
	endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(query)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Brave Search API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("Brave Search API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB limit
	if err != nil {
		return nil, fmt.Errorf("read Brave response: %w", err)
	}

	return parseBraveResponse(body)
}

// parseBraveResponse decodes the Brave API JSON and maps it to WebSearchResult
// entries, capping at braveMaxResults.
func parseBraveResponse(data []byte) ([]WebSearchResult, error) {
	var resp braveSearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse Brave response: %w", err)
	}

	results := make([]WebSearchResult, 0, braveMaxResults)
	for _, r := range resp.Web.Results {
		if len(results) >= braveMaxResults {
			break
		}
		results = append(results, WebSearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
		})
	}
	return results, nil
}
