package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// webSearchPageCache stores fetched page content for find_in_page.
var webSearchPageCache = struct {
	mu    sync.RWMutex
	pages map[string]string
}{pages: make(map[string]string)}

// WebSearchResult represents a single search result.
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchHandler executes a web search and returns ranked results with titles, URLs, and snippets.
func WebSearchHandler(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("web_search: 'query' parameter is required")
	}

	results, err := performWebSearch(ctx, query)
	if err != nil {
		return "", fmt.Errorf("web_search failed: %w", err)
	}

	data, _ := json.Marshal(results)
	return string(data), nil
}

// OpenPageHandler fetches page content and returns text.
func OpenPageHandler(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	pageURL, _ := args["url"].(string)
	if pageURL == "" {
		return "", fmt.Errorf("open_page: 'url' parameter is required")
	}

	content, err := fetchPageContent(ctx, pageURL)
	if err != nil {
		return "", fmt.Errorf("open_page failed: %w", err)
	}

	// Cache the content for find_in_page
	webSearchPageCache.mu.Lock()
	webSearchPageCache.pages[pageURL] = content
	webSearchPageCache.mu.Unlock()

	// Truncate if too long
	const maxLen = 50000
	if len(content) > maxLen {
		content = content[:maxLen] + "\n... [truncated]"
	}

	return content, nil
}

// FindInPageHandler searches within fetched page content and returns matching sections.
func FindInPageHandler(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	pageURL, _ := args["url"].(string)
	searchQuery, _ := args["query"].(string)
	if pageURL == "" || searchQuery == "" {
		return "", fmt.Errorf("find_in_page: 'url' and 'query' parameters are required")
	}

	// Look up cached page content
	webSearchPageCache.mu.RLock()
	content, ok := webSearchPageCache.pages[pageURL]
	webSearchPageCache.mu.RUnlock()

	if !ok {
		// Fetch if not cached
		var err error
		content, err = fetchPageContent(ctx, pageURL)
		if err != nil {
			return "", fmt.Errorf("find_in_page: failed to fetch page: %w", err)
		}
		webSearchPageCache.mu.Lock()
		webSearchPageCache.pages[pageURL] = content
		webSearchPageCache.mu.Unlock()
	}

	matches := findInContent(content, searchQuery)
	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for '%s' in %s", searchQuery, pageURL), nil
	}

	data, _ := json.Marshal(matches)
	return string(data), nil
}

// performWebSearch performs a real web search via DuckDuckGo HTML search (no API key needed).
func performWebSearch(ctx context.Context, query string) ([]WebSearchResult, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseDuckDuckGoHTML(string(body)), nil
}

// parseDuckDuckGoHTML extracts search results from DuckDuckGo HTML search page.
func parseDuckDuckGoHTML(htmlBody string) []WebSearchResult {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return nil
	}

	var results []WebSearchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= 10 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "result") {
			r := extractDDGResult(n)
			if r.URL != "" && r.Title != "" {
				results = append(results, r)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func extractDDGResult(n *html.Node) WebSearchResult {
	var r WebSearchResult
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "result__a") {
			r.Title = textContent(node)
			for _, attr := range node.Attr {
				if attr.Key == "href" {
					r.URL = extractDDGURL(attr.Val)
				}
			}
		}
		if node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "result__snippet") {
			r.Snippet = textContent(node)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return r
}

func extractDDGURL(raw string) string {
	// DuckDuckGo wraps URLs in a redirect: //duckduckgo.com/l/?uddg=<encoded>&...
	if strings.Contains(raw, "uddg=") {
		if u, err := url.Parse(raw); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	return raw
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, c := range strings.Fields(attr.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

// fetchPageContent fetches the text content of a URL.
func fetchPageContent(ctx context.Context, pageURL string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "aiops-codex/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}

// findInContent searches for a query string within content and returns matching sections.
func findInContent(content, query string) []string {
	query = strings.ToLower(query)
	lines := strings.Split(content, "\n")
	var matches []string

	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			// Include context: 2 lines before and after
			start := i - 2
			if start < 0 {
				start = 0
			}
			end := i + 3
			if end > len(lines) {
				end = len(lines)
			}
			section := strings.Join(lines[start:end], "\n")
			matches = append(matches, section)

			// Limit to 10 matches
			if len(matches) >= 10 {
				break
			}
		}
	}

	return matches
}

// RegisterWebSearchTools registers web_search, open_page, and find_in_page tools.
func RegisterWebSearchTools(registry *ToolRegistry) {
	registry.Register(ToolEntry{
		Name:        "web_search",
		Description: "Execute a web search and return ranked results with titles, URLs, and snippets",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
		Handler:    WebSearchHandler,
		IsReadOnly: true,
	})

	registry.Register(ToolEntry{
		Name:        "open_page",
		Description: "Fetch a web page and return its text content",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch",
				},
			},
			"required": []string{"url"},
		},
		Handler:    OpenPageHandler,
		IsReadOnly: true,
	})

	registry.Register(ToolEntry{
		Name:        "find_in_page",
		Description: "Search within a fetched page's content and return matching sections",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL of the page to search in",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The text to search for within the page",
				},
			},
			"required": []string{"url", "query"},
		},
		Handler:    FindInPageHandler,
		IsReadOnly: true,
	})
}
