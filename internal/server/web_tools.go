package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	toolruntime "github.com/lizhongxuan/aiops-codex/internal/tools"
)

type webToolExecutor func(context.Context, map[string]any) (string, error)

type webSearchUnifiedTool struct {
	app     *App
	execute webToolExecutor
}

type webFetchUnifiedTool struct {
	execute webToolExecutor
}

type findInPageUnifiedTool struct {
	execute webToolExecutor
}

func (a *App) webSearchUnifiedTool() UnifiedTool {
	return webSearchUnifiedTool{app: a}
}

func (a *App) webFetchUnifiedTool() UnifiedTool {
	return webFetchUnifiedTool{}
}

func (a *App) findInPageUnifiedTool() UnifiedTool {
	return findInPageUnifiedTool{}
}

func (t webSearchUnifiedTool) Name() string { return "web_search" }

func (t webSearchUnifiedTool) Aliases() []string { return nil }

func (t webSearchUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription("web_search")
}

func (t webSearchUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query",
			},
		},
		"required": []string{"query"},
	}
}

func (t webSearchUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	query := strings.TrimSpace(getStringAny(req.Input, "query"))
	if query == "" {
		return ToolCallResult{}, fmt.Errorf("web_search: 'query' parameter is required")
	}

	raw, err := t.run(ctx, map[string]any{"query": query})
	if err != nil {
		return ToolCallResult{}, err
	}
	results, err := parseWebSearchResults(raw)
	if err != nil {
		return ToolCallResult{}, err
	}
	structured := structuredWebSearchContent(query, results)

	if err := ReportToolProgress(ctx, ToolProgressUpdate{
		Phase:         "searching",
		Message:       webSearchProgressSummary(query, len(results)),
		ActivityKind:  "search",
		ActivityQuery: query,
		Payload: map[string]any{
			"query":       query,
			"resultCount": len(results),
			"results":     structured["results"],
		},
	}); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Output:            raw,
		StructuredContent: structured,
		Metadata: map[string]any{
			"trackActivityCompletion": true,
		},
	}, nil
}

func (t webSearchUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t webSearchUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t webSearchUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t webSearchUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t webSearchUnifiedTool) SupportsStreamingProgress() bool { return true }

func (t webSearchUnifiedTool) Display() ToolDisplayAdapter { return webSearchDisplayAdapter{} }

func (t webSearchUnifiedTool) run(ctx context.Context, args map[string]any) (string, error) {
	if t.execute != nil {
		return t.execute(ctx, args)
	}
	mode := ""
	braveAPIKey := ""
	if t.app != nil {
		mode = strings.ToLower(strings.TrimSpace(t.app.cfg.WebSearchMode))
		braveAPIKey = strings.TrimSpace(t.app.cfg.BraveAPIKey)
	}
	switch mode {
	case "disabled":
		return "", fmt.Errorf("web_search is disabled by configuration")
	case "brave":
		if braveAPIKey != "" {
			return toolruntime.BraveSearchHandler(braveAPIKey)(ctx, nil, bifrost.ToolCall{}, args)
		}
	}
	return toolruntime.WebSearchHandler(ctx, nil, bifrost.ToolCall{}, args)
}

func (t webFetchUnifiedTool) Name() string { return "open_page" }

func (t webFetchUnifiedTool) Aliases() []string { return nil }

func (t webFetchUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription("open_page")
}

func (t webFetchUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch",
			},
		},
		"required": []string{"url"},
	}
}

func (t webFetchUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	pageURL := strings.TrimSpace(getStringAny(req.Input, "url"))
	if pageURL == "" {
		return ToolCallResult{}, fmt.Errorf("open_page: 'url' parameter is required")
	}

	content, err := t.run(ctx, map[string]any{"url": pageURL})
	if err != nil {
		return ToolCallResult{}, err
	}
	structured := structuredFetchedPageContent(pageURL, content)

	if err := ReportToolProgress(ctx, ToolProgressUpdate{
		Phase:          "browsing",
		Message:        fetchedPageProgressSummary(getStringAny(structured, "title"), pageURL),
		ActivityKind:   "browse",
		ActivityTarget: pageURL,
		Payload:        cloneNestedAnyMap(structured),
	}); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Output:            content,
		StructuredContent: structured,
		Metadata: map[string]any{
			"trackActivityCompletion": true,
		},
	}, nil
}

func (t webFetchUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t webFetchUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t webFetchUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t webFetchUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t webFetchUnifiedTool) SupportsStreamingProgress() bool { return true }

func (t webFetchUnifiedTool) Display() ToolDisplayAdapter { return webFetchDisplayAdapter{} }

func (t webFetchUnifiedTool) run(ctx context.Context, args map[string]any) (string, error) {
	if t.execute != nil {
		return t.execute(ctx, args)
	}
	return toolruntime.OpenPageHandler(ctx, nil, bifrost.ToolCall{}, args)
}

func (t findInPageUnifiedTool) Name() string { return "find_in_page" }

func (t findInPageUnifiedTool) Aliases() []string { return nil }

func (t findInPageUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription("find_in_page")
}

func (t findInPageUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL of the page to search in",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "The text to search for within the page",
			},
		},
		"required": []string{"url", "query"},
	}
}

func (t findInPageUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	pageURL := strings.TrimSpace(getStringAny(req.Input, "url"))
	query := strings.TrimSpace(getStringAny(req.Input, "query"))
	if pageURL == "" || query == "" {
		return ToolCallResult{}, fmt.Errorf("find_in_page: 'url' and 'query' parameters are required")
	}

	raw, err := t.run(ctx, map[string]any{"url": pageURL, "query": query})
	if err != nil {
		return ToolCallResult{}, err
	}
	structured := structuredFindInPageContent(pageURL, query, raw)

	if err := ReportToolProgress(ctx, ToolProgressUpdate{
		Phase:          "searching",
		Message:        findInPageProgressSummary(query),
		ActivityKind:   "search",
		ActivityTarget: pageURL,
		ActivityQuery:  query,
		Payload:        cloneNestedAnyMap(structured),
	}); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Output:            raw,
		StructuredContent: structured,
		Metadata: map[string]any{
			"trackActivityCompletion": true,
		},
	}, nil
}

func (t findInPageUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t findInPageUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t findInPageUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t findInPageUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t findInPageUnifiedTool) SupportsStreamingProgress() bool { return true }

func (t findInPageUnifiedTool) Display() ToolDisplayAdapter { return findInPageDisplayAdapter{} }

func (t findInPageUnifiedTool) run(ctx context.Context, args map[string]any) (string, error) {
	if t.execute != nil {
		return t.execute(ctx, args)
	}
	return toolruntime.FindInPageHandler(ctx, nil, bifrost.ToolCall{}, args)
}

type webSearchDisplayAdapter struct{}

func (webSearchDisplayAdapter) RenderUse(req ToolCallRequest) *ToolDisplayPayload {
	query := strings.TrimSpace(firstNonEmptyValue(getStringAny(req.Input, "query"), getStringAny(req.Invocation.Arguments, "query")))
	if query == "" {
		return nil
	}
	return &ToolDisplayPayload{
		Summary:  "搜索网页：" + query,
		Activity: query,
		Blocks:   []ToolDisplayBlock{webSearchQueryBlock(query)},
	}
}

func (webSearchDisplayAdapter) RenderProgress(progress ToolProgressEvent) *ToolDisplayPayload {
	query := strings.TrimSpace(firstNonEmptyValue(progress.Update.ActivityQuery, getStringAny(progress.Update.Payload, "query"), getStringAny(progress.Invocation.Arguments, "query")))
	if query == "" {
		return nil
	}
	count, _ := getIntAny(progress.Update.Payload, "resultCount")
	blocks := []ToolDisplayBlock{webSearchQueryBlock(query)}
	if count > 0 {
		blocks = append(blocks, webResultStatsBlock(count))
	}
	return &ToolDisplayPayload{
		Summary:  firstNonEmptyValue(strings.TrimSpace(progress.Update.Message), webSearchProgressSummary(query, count)),
		Activity: query,
		Blocks:   blocks,
	}
}

func (webSearchDisplayAdapter) RenderResult(result ToolCallResult) *ToolDisplayPayload {
	query := strings.TrimSpace(getStringAny(result.StructuredContent, "query"))
	if query == "" {
		return nil
	}
	results := structuredResultItems(result.StructuredContent, "results")
	count := len(results)
	if parsed, ok := getIntAny(result.StructuredContent, "resultCount"); ok && parsed > count {
		count = parsed
	}
	blocks := []ToolDisplayBlock{webSearchQueryBlock(query)}
	if len(results) > 0 {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockLinkList,
			Title: "Sources",
			Items: results,
		})
	}
	blocks = append(blocks, webResultStatsBlock(count))
	return &ToolDisplayPayload{
		Summary:  webSearchResultSummary(query, count),
		Activity: query,
		Blocks:   blocks,
		Metadata: map[string]any{"resultCount": count},
	}
}

type webFetchDisplayAdapter struct{}

func (webFetchDisplayAdapter) RenderUse(req ToolCallRequest) *ToolDisplayPayload {
	pageURL := strings.TrimSpace(firstNonEmptyValue(getStringAny(req.Input, "url"), getStringAny(req.Invocation.Arguments, "url")))
	if pageURL == "" {
		return nil
	}
	return &ToolDisplayPayload{
		Summary:  "浏览网页：" + pageURL,
		Activity: pageURL,
		Blocks: []ToolDisplayBlock{
			fetchedPageLinkBlock(pageURL, ""),
		},
	}
}

func (webFetchDisplayAdapter) RenderProgress(progress ToolProgressEvent) *ToolDisplayPayload {
	pageURL := strings.TrimSpace(firstNonEmptyValue(progress.Update.ActivityTarget, getStringAny(progress.Update.Payload, "url"), getStringAny(progress.Invocation.Arguments, "url")))
	if pageURL == "" {
		return nil
	}
	title := strings.TrimSpace(getStringAny(progress.Update.Payload, "title"))
	return &ToolDisplayPayload{
		Summary:  firstNonEmptyValue(strings.TrimSpace(progress.Update.Message), fetchedPageProgressSummary(title, pageURL)),
		Activity: pageURL,
		Blocks: []ToolDisplayBlock{
			fetchedPageLinkBlock(pageURL, title),
		},
	}
}

func (webFetchDisplayAdapter) RenderResult(result ToolCallResult) *ToolDisplayPayload {
	pageURL := strings.TrimSpace(getStringAny(result.StructuredContent, "url"))
	if pageURL == "" {
		return nil
	}
	title := strings.TrimSpace(getStringAny(result.StructuredContent, "title"))
	summary := strings.TrimSpace(getStringAny(result.StructuredContent, "contentSummary"))
	blocks := []ToolDisplayBlock{
		{
			Kind:  ToolDisplayBlockKVList,
			Title: "Fetched page",
			Items: fetchedPageKVItems(pageURL, title),
		},
	}
	if summary != "" {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockText,
			Title: "Content summary",
			Text:  summary,
		})
	}
	return &ToolDisplayPayload{
		Summary:  fetchedPageResultSummary(title, pageURL),
		Activity: pageURL,
		Blocks:   blocks,
	}
}

type findInPageDisplayAdapter struct{}

func (findInPageDisplayAdapter) RenderUse(req ToolCallRequest) *ToolDisplayPayload {
	pageURL := strings.TrimSpace(firstNonEmptyValue(getStringAny(req.Input, "url"), getStringAny(req.Invocation.Arguments, "url")))
	query := strings.TrimSpace(firstNonEmptyValue(getStringAny(req.Input, "query"), getStringAny(req.Invocation.Arguments, "query")))
	if pageURL == "" || query == "" {
		return nil
	}
	return &ToolDisplayPayload{
		Summary:  "在页面中搜索：" + query,
		Activity: query,
		Blocks: []ToolDisplayBlock{
			webSearchQueryBlock(query),
			fetchedPageLinkBlock(pageURL, ""),
		},
	}
}

func (findInPageDisplayAdapter) RenderProgress(progress ToolProgressEvent) *ToolDisplayPayload {
	pageURL := strings.TrimSpace(firstNonEmptyValue(progress.Update.ActivityTarget, getStringAny(progress.Update.Payload, "url"), getStringAny(progress.Invocation.Arguments, "url")))
	query := strings.TrimSpace(firstNonEmptyValue(progress.Update.ActivityQuery, getStringAny(progress.Update.Payload, "query"), getStringAny(progress.Invocation.Arguments, "query")))
	if pageURL == "" || query == "" {
		return nil
	}
	return &ToolDisplayPayload{
		Summary:  firstNonEmptyValue(strings.TrimSpace(progress.Update.Message), findInPageProgressSummary(query)),
		Activity: query,
		Blocks: []ToolDisplayBlock{
			webSearchQueryBlock(query),
			fetchedPageLinkBlock(pageURL, ""),
		},
	}
}

func (findInPageDisplayAdapter) RenderResult(result ToolCallResult) *ToolDisplayPayload {
	pageURL := strings.TrimSpace(getStringAny(result.StructuredContent, "url"))
	query := strings.TrimSpace(getStringAny(result.StructuredContent, "query"))
	if pageURL == "" || query == "" {
		return nil
	}
	matches := structuredStringItems(result.StructuredContent, "matches")
	count := len(matches)
	if parsed, ok := getIntAny(result.StructuredContent, "matchCount"); ok && parsed > count {
		count = parsed
	}
	blocks := []ToolDisplayBlock{
		webSearchQueryBlock(query),
		fetchedPageLinkBlock(pageURL, ""),
		webResultStatsBlock(count),
	}
	if len(matches) > 0 {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockText,
			Title: "Matches",
			Text:  strings.Join(matches, "\n\n"),
		})
	}
	return &ToolDisplayPayload{
		Summary:  findInPageResultSummary(query, count),
		Activity: query,
		Blocks:   blocks,
	}
}

func parseWebSearchResults(raw string) ([]toolruntime.WebSearchResult, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var results []toolruntime.WebSearchResult
	if err := json.Unmarshal([]byte(trimmed), &results); err != nil {
		return nil, fmt.Errorf("parse web_search results: %w", err)
	}
	return results, nil
}

func structuredWebSearchContent(query string, results []toolruntime.WebSearchResult) map[string]any {
	items := make([]map[string]any, 0, len(results))
	for _, result := range results {
		items = append(items, map[string]any{
			"title":   strings.TrimSpace(result.Title),
			"url":     strings.TrimSpace(result.URL),
			"summary": strings.TrimSpace(result.Snippet),
		})
	}
	return map[string]any{
		"query":       query,
		"resultCount": len(items),
		"results":     items,
	}
}

func structuredFetchedPageContent(pageURL, content string) map[string]any {
	title, summary := describeFetchedPage(pageURL, content)
	return map[string]any{
		"url":            strings.TrimSpace(pageURL),
		"title":          title,
		"contentSummary": summary,
		"contentLength":  len(content),
	}
}

func structuredFindInPageContent(pageURL, query, raw string) map[string]any {
	matches := parseFindInPageMatches(raw)
	return map[string]any{
		"url":        strings.TrimSpace(pageURL),
		"query":      strings.TrimSpace(query),
		"matches":    matches,
		"matchCount": len(matches),
	}
}

func parseFindInPageMatches(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var matches []string
		if err := json.Unmarshal([]byte(trimmed), &matches); err == nil {
			return matches
		}
	}
	return nil
}

func structuredResultItems(content map[string]any, key string) []map[string]any {
	value, ok := content[key]
	if !ok {
		return nil
	}
	if typed, ok := value.([]map[string]any); ok {
		return cloneNestedAnyValue(typed).([]map[string]any)
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, item := range rawItems {
		if typed, ok := item.(map[string]any); ok {
			items = append(items, cloneNestedAnyMap(typed))
		}
	}
	return items
}

func structuredStringItems(content map[string]any, key string) []string {
	value, ok := content[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func webSearchQueryBlock(query string) ToolDisplayBlock {
	return ToolDisplayBlock{
		Kind:  ToolDisplayBlockSearchQueries,
		Title: "Search query",
		Items: []map[string]any{
			{
				"query": query,
				"label": "Query",
			},
		},
	}
}

func webResultStatsBlock(count int) ToolDisplayBlock {
	return ToolDisplayBlock{
		Kind:  ToolDisplayBlockResultStats,
		Title: "Results",
		Items: []map[string]any{
			{
				"label": "Count",
				"value": count,
			},
		},
	}
}

func fetchedPageLinkBlock(pageURL, title string) ToolDisplayBlock {
	item := map[string]any{
		"url": pageURL,
	}
	if strings.TrimSpace(title) != "" {
		item["title"] = strings.TrimSpace(title)
	} else {
		item["title"] = strings.TrimSpace(pageURL)
	}
	return ToolDisplayBlock{
		Kind:  ToolDisplayBlockLinkList,
		Title: "Page",
		Items: []map[string]any{item},
	}
}

func fetchedPageKVItems(pageURL, title string) []map[string]any {
	items := []map[string]any{
		{
			"label": "URL",
			"value": pageURL,
		},
	}
	if strings.TrimSpace(title) != "" {
		items = append([]map[string]any{
			{
				"label": "Title",
				"value": title,
			},
		}, items...)
	}
	return items
}

func webSearchProgressSummary(query string, count int) string {
	if count > 0 {
		return fmt.Sprintf("已获取 %d 条候选来源（%s）", count, query)
	}
	return "正在搜索网页：" + query
}

func webSearchResultSummary(query string, count int) string {
	if count <= 0 {
		return "未找到网页结果：" + query
	}
	return fmt.Sprintf("已搜索网页（%s），找到 %d 条结果", query, count)
}

func fetchedPageProgressSummary(title, pageURL string) string {
	if strings.TrimSpace(title) != "" {
		return "已抓取页面：" + strings.TrimSpace(title)
	}
	return "正在抓取页面：" + strings.TrimSpace(pageURL)
}

func fetchedPageResultSummary(title, pageURL string) string {
	if strings.TrimSpace(title) != "" {
		return "已抓取页面：" + strings.TrimSpace(title)
	}
	return "已抓取页面：" + strings.TrimSpace(pageURL)
}

func findInPageProgressSummary(query string) string {
	return "正在定位页面内容：" + query
}

func findInPageResultSummary(query string, count int) string {
	if count <= 0 {
		return "未在页面中找到：" + query
	}
	return fmt.Sprintf("已在页面中找到 %d 处匹配（%s）", count, query)
}

func describeFetchedPage(pageURL, content string) (string, string) {
	title, text := extractHTMLTitleAndText(content)
	if text == "" {
		text = compactDisplayText(content)
	}
	if title == "" {
		title = fallbackPageTitle(pageURL)
	}
	return title, truncateDisplayText(text, 280)
}

func extractHTMLTitleAndText(content string) (string, string) {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", ""
	}

	var title string
	var textParts []string
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, skip bool) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "noscript":
				skip = true
			case "title":
				if title == "" {
					title = compactDisplayText(nodeText(node))
				}
			}
		}
		if !skip && node.Type == html.TextNode {
			if text := compactDisplayText(node.Data); text != "" {
				textParts = append(textParts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}
	}
	walk(doc, false)
	return title, compactDisplayText(strings.Join(textParts, " "))
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func fallbackPageTitle(pageURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil {
		return strings.TrimSpace(pageURL)
	}
	host := strings.TrimSpace(parsed.Host)
	path := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	switch {
	case host != "" && path != "":
		return host + "/" + path
	case host != "":
		return host
	default:
		return strings.TrimSpace(pageURL)
	}
}

func compactDisplayText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateDisplayText(value string, limit int) string {
	text := compactDisplayText(value)
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
