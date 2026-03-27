package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type agentResponseWaiter struct {
	HostID string
	ch     chan *agentrpc.Envelope
}

type filePreviewResponse struct {
	HostID    string `json:"hostId,omitempty"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

func buildFileReadCard(cardID, hostID string, result *agentrpc.FileReadResult, createdAt string) model.Card {
	content := strings.TrimSpace(result.Content)
	lines := meaningfulFileLines(content, 3)
	summary := fmt.Sprintf("读取 %s，%s。", result.Path, readFileSizeLabel(content, result.Truncated))
	if len(lines) > 0 {
		summary = fmt.Sprintf("读取 %s，%s，首行内容：%s。", result.Path, readFileSizeLabel(content, result.Truncated), lines[0])
	}
	highlights := lines
	if result.Truncated {
		highlights = append(highlights, "文件内容已截断，仅展示前一部分。")
	}
	return model.Card{
		ID:      cardID,
		Type:    "ResultSummaryCard",
		Title:   "远程文件读取",
		Status:  "completed",
		Summary: summary,
		KVRows: []model.KeyValueRow{
			{Key: "主机", Value: hostID},
			{Key: "文件", Value: result.Path},
			{Key: "大小", Value: readFileSizeLabel(content, result.Truncated)},
			{Key: "状态", Value: readFileReadStatus(result.Truncated)},
		},
		FileItems: []model.FileItem{{
			Label:   filepathBase(result.Path),
			Path:    remoteFileLink(hostID, result.Path, 0),
			Kind:    "file",
			Meta:    readFileMeta(content, result.Truncated),
			Preview: previewFileContent(content, 6),
		}},
		Highlights: highlights,
		Text:       readFileTailNote(result.Truncated),
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
}

func (a *App) setAgentResponseWaiter(requestID, hostID string, waiter *agentResponseWaiter) {
	a.fileReqMu.Lock()
	defer a.fileReqMu.Unlock()
	a.fileReqs[requestID] = waiter
}

func (a *App) popAgentResponseWaiter(requestID string) (*agentResponseWaiter, bool) {
	a.fileReqMu.Lock()
	defer a.fileReqMu.Unlock()
	waiter, ok := a.fileReqs[requestID]
	if ok {
		delete(a.fileReqs, requestID)
	}
	return waiter, ok
}

func (a *App) resolveAgentResponseWaiter(requestID string, env *agentrpc.Envelope) {
	waiter, ok := a.popAgentResponseWaiter(requestID)
	if !ok {
		return
	}
	select {
	case waiter.ch <- env:
	default:
	}
}

func (a *App) failAgentResponseWaitersForHost(hostID, message string) {
	a.fileReqMu.Lock()
	waiters := make([]*agentResponseWaiter, 0, len(a.fileReqs))
	for requestID, waiter := range a.fileReqs {
		if waiter.HostID != hostID {
			continue
		}
		waiters = append(waiters, waiter)
		delete(a.fileReqs, requestID)
	}
	a.fileReqMu.Unlock()

	for _, waiter := range waiters {
		select {
		case waiter.ch <- &agentrpc.Envelope{Kind: "error", Error: message}:
		default:
		}
	}
}

func (a *App) handleAgentFileListResult(_ string, payload *agentrpc.FileListResult) {
	if payload == nil {
		return
	}
	a.resolveAgentResponseWaiter(payload.RequestID, &agentrpc.Envelope{
		Kind:           "file/list/result",
		FileListResult: payload,
	})
}

func (a *App) handleAgentFileReadResult(_ string, payload *agentrpc.FileReadResult) {
	if payload == nil {
		return
	}
	a.resolveAgentResponseWaiter(payload.RequestID, &agentrpc.Envelope{
		Kind:           "file/read/result",
		FileReadResult: payload,
	})
}

func (a *App) handleAgentFileSearchResult(_ string, payload *agentrpc.FileSearchResult) {
	if payload == nil {
		return
	}
	a.resolveAgentResponseWaiter(payload.RequestID, &agentrpc.Envelope{
		Kind:             "file/search/result",
		FileSearchResult: payload,
	})
}

func (a *App) handleAgentFileWriteResult(_ string, payload *agentrpc.FileWriteResult) {
	if payload == nil {
		return
	}
	a.resolveAgentResponseWaiter(payload.RequestID, &agentrpc.Envelope{
		Kind:            "file/write/result",
		FileWriteResult: payload,
	})
}

func (a *App) waitForAgentResponse(ctx context.Context, hostID string, env *agentrpc.Envelope, requestID string) (*agentrpc.Envelope, error) {
	waiter := &agentResponseWaiter{
		HostID: hostID,
		ch:     make(chan *agentrpc.Envelope, 1),
	}
	a.setAgentResponseWaiter(requestID, hostID, waiter)
	if err := a.sendAgentEnvelope(hostID, env); err != nil {
		a.popAgentResponseWaiter(requestID)
		return nil, err
	}

	select {
	case <-ctx.Done():
		a.popAgentResponseWaiter(requestID)
		return nil, ctx.Err()
	case result := <-waiter.ch:
		if result == nil {
			return nil, errors.New("empty remote response")
		}
		if strings.TrimSpace(result.Error) != "" {
			return nil, errors.New(strings.TrimSpace(result.Error))
		}
		return result, nil
	}
}

func (a *App) remoteListFiles(ctx context.Context, hostID, path string, recursive bool, maxEntries int) (*agentrpc.FileListResult, error) {
	requestID := model.NewID("flist")
	env, err := a.waitForAgentResponse(ctx, hostID, &agentrpc.Envelope{
		Kind: "file/list",
		FileListRequest: &agentrpc.FileListRequest{
			RequestID:  requestID,
			Path:       strings.TrimSpace(path),
			Recursive:  recursive,
			MaxEntries: maxEntries,
		},
	}, requestID)
	if err != nil {
		return nil, err
	}
	if env.FileListResult == nil {
		return nil, errors.New("invalid remote file list response")
	}
	if strings.TrimSpace(env.FileListResult.Message) != "" {
		return env.FileListResult, errors.New(strings.TrimSpace(env.FileListResult.Message))
	}
	return env.FileListResult, nil
}

func (a *App) remoteReadFile(ctx context.Context, hostID, path string, maxBytes int) (*agentrpc.FileReadResult, error) {
	requestID := model.NewID("fread")
	env, err := a.waitForAgentResponse(ctx, hostID, &agentrpc.Envelope{
		Kind: "file/read",
		FileReadRequest: &agentrpc.FileReadRequest{
			RequestID: requestID,
			Path:      strings.TrimSpace(path),
			MaxBytes:  maxBytes,
		},
	}, requestID)
	if err != nil {
		return nil, err
	}
	if env.FileReadResult == nil {
		return nil, errors.New("invalid remote file read response")
	}
	if strings.TrimSpace(env.FileReadResult.Message) != "" {
		return env.FileReadResult, errors.New(strings.TrimSpace(env.FileReadResult.Message))
	}
	return env.FileReadResult, nil
}

func (a *App) remoteSearchFiles(ctx context.Context, hostID, path, query string, maxMatches int) (*agentrpc.FileSearchResult, error) {
	requestID := model.NewID("fsearch")
	env, err := a.waitForAgentResponse(ctx, hostID, &agentrpc.Envelope{
		Kind: "file/search",
		FileSearchRequest: &agentrpc.FileSearchRequest{
			RequestID:  requestID,
			Path:       strings.TrimSpace(path),
			Query:      strings.TrimSpace(query),
			MaxMatches: maxMatches,
		},
	}, requestID)
	if err != nil {
		return nil, err
	}
	if env.FileSearchResult == nil {
		return nil, errors.New("invalid remote file search response")
	}
	if strings.TrimSpace(env.FileSearchResult.Message) != "" {
		return env.FileSearchResult, errors.New(strings.TrimSpace(env.FileSearchResult.Message))
	}
	return env.FileSearchResult, nil
}

func (a *App) remoteWriteFile(ctx context.Context, hostID, path, content, writeMode string) (*agentrpc.FileWriteResult, error) {
	requestID := model.NewID("fwrite")
	env, err := a.waitForAgentResponse(ctx, hostID, &agentrpc.Envelope{
		Kind: "file/write",
		FileWriteRequest: &agentrpc.FileWriteRequest{
			RequestID: requestID,
			Path:      strings.TrimSpace(path),
			Content:   content,
			WriteMode: strings.TrimSpace(writeMode),
		},
	}, requestID)
	if err != nil {
		return nil, err
	}
	if env.FileWriteResult == nil {
		return nil, errors.New("invalid remote file write response")
	}
	if strings.TrimSpace(env.FileWriteResult.Message) != "" {
		return env.FileWriteResult, errors.New(strings.TrimSpace(env.FileWriteResult.Message))
	}
	return env.FileWriteResult, nil
}

func (a *App) handleFilePreview(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	hostID := defaultHostID(strings.TrimSpace(r.URL.Query().Get("hostId")))
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	if hostID == model.ServerLocalHostID {
		content, truncated, err := readLocalPreviewFile(path, 128*1024)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, filePreviewResponse{
			HostID:    hostID,
			Path:      path,
			Content:   content,
			Truncated: truncated,
		})
		return
	}

	host := a.findHost(hostID)
	if host.Status != "online" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "selected remote host is offline"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := a.remoteReadFile(ctx, hostID, path, 128*1024)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, filePreviewResponse{
		HostID:    hostID,
		Path:      result.Path,
		Content:   result.Content,
		Truncated: result.Truncated,
	})
}

func readLocalPreviewFile(path string, maxBytes int) (string, bool, error) {
	resolved := resolvePreviewPath(path)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, errors.New("path is a directory")
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", false, err
	}
	if looksBinaryBytes(data) {
		return "", false, errors.New("binary file preview is not supported")
	}
	limit := clampMaxBytes(maxBytes, 128*1024)
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}
	return string(data), truncated, nil
}

func resolvePreviewPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = filepath.Join(home, path)
		}
	}
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return path
	}
	return resolved
}

func looksBinaryBytes(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func clampMaxBytes(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	if value > 512*1024 {
		return 512 * 1024
	}
	return value
}

func remoteFileLink(hostID, path string, line int) string {
	base := fmt.Sprintf("remote://%s%s", hostID, path)
	if line > 0 {
		return fmt.Sprintf("%s#L%d", base, line)
	}
	return base
}

func renderFileListMessage(hostID, root string, entries []agentrpc.FileEntry, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "目录 `%s` 下找到这些条目：\n\n", root)
	for _, entry := range entries {
		label := entry.Name
		if entry.Kind == "dir" {
			label += "/"
		}
		fmt.Fprintf(&b, "- [%s](%s)\n", label, remoteFileLink(hostID, entry.Path, 0))
	}
	if truncated {
		b.WriteString("\n仅展示前一部分结果，继续缩小目录范围可查看更多。\n")
	}
	return strings.TrimSpace(b.String())
}

func buildFileListCard(cardID, hostID string, result *agentrpc.FileListResult, createdAt string) model.Card {
	return buildFileListCardWithRecursive(cardID, hostID, result, false, createdAt)
}

func buildFileListCardWithRecursive(cardID, hostID string, result *agentrpc.FileListResult, recursive bool, createdAt string) model.Card {
	files, dirs := countRemoteEntries(result.Entries)
	note := listFileTailNote(result.Truncated)
	recursiveLabel := "否"
	if recursive {
		recursiveLabel = "是"
	}
	return model.Card{
		ID:      cardID,
		Type:    "ResultSummaryCard",
		Title:   "远程文件列表",
		Summary: fmt.Sprintf("目录 %s 下找到 %d 个条目（%d 个文件，%d 个目录）。", result.Path, len(result.Entries), files, dirs),
		Status:  "completed",
		KVRows: []model.KeyValueRow{
			{Key: "主机", Value: hostID},
			{Key: "目录", Value: result.Path},
			{Key: "条目数", Value: fmt.Sprintf("%d", len(result.Entries))},
			{Key: "文件数", Value: fmt.Sprintf("%d", files)},
			{Key: "目录数", Value: fmt.Sprintf("%d", dirs)},
			{Key: "递归", Value: recursiveLabel},
			{Key: "状态", Value: listResultStatus(result.Truncated)},
		},
		FileItems:  buildFileListItems(hostID, result.Entries),
		Text:       note,
		Highlights: listFileHighlights(result.Path, result.Entries, result.Truncated),
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
}

func renderFileSearchMessage(hostID, root, query string, matches []agentrpc.FileMatch, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "在 `%s` 中搜索 `%s`，命中这些位置：\n\n", root, query)
	for _, match := range matches {
		label := fmt.Sprintf("%s:%d", filepath.Base(match.Path), match.Line)
		fmt.Fprintf(&b, "- [%s](%s)\n", label, remoteFileLink(hostID, match.Path, match.Line))
		if strings.TrimSpace(match.Preview) != "" {
			fmt.Fprintf(&b, "  `%s`\n", strings.TrimSpace(match.Preview))
		}
	}
	if truncated {
		b.WriteString("\n结果已截断，继续缩小搜索范围可查看更多。\n")
	}
	return strings.TrimSpace(b.String())
}

func buildFileSearchCard(cardID, hostID string, result *agentrpc.FileSearchResult, createdAt string) model.Card {
	files, lines := countSearchMatches(result.Matches)
	note := searchFileTailNote(result.Truncated)
	return model.Card{
		ID:      cardID,
		Type:    "ResultSummaryCard",
		Title:   "远程搜索结果",
		Summary: fmt.Sprintf("在 %s 中搜索 %q，命中 %d 个位置，涉及 %d 个文件。", result.Path, result.Query, len(result.Matches), files),
		Status:  "completed",
		KVRows: []model.KeyValueRow{
			{Key: "主机", Value: hostID},
			{Key: "范围", Value: result.Path},
			{Key: "关键词", Value: result.Query},
			{Key: "命中", Value: fmt.Sprintf("%d", len(result.Matches))},
			{Key: "文件数", Value: fmt.Sprintf("%d", files)},
			{Key: "行数", Value: fmt.Sprintf("%d", lines)},
			{Key: "状态", Value: searchResultStatus(result.Truncated)},
		},
		FileItems:  buildFileSearchItems(hostID, result.Matches),
		Text:       note,
		Highlights: searchFileHighlights(result.Matches, result.Truncated),
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
}

func buildFileListItems(hostID string, entries []agentrpc.FileEntry) []model.FileItem {
	items := make([]model.FileItem, 0, len(entries))
	for _, entry := range entries {
		label := entry.Name
		if label == "" {
			label = filepathBase(entry.Path)
		}
		if entry.Kind == "dir" {
			label += "/"
		}

		meta := ""
		switch entry.Kind {
		case "dir":
			meta = "目录"
		case "file":
			meta = humanizeFileSize(entry.Size)
		default:
			if entry.Size > 0 {
				meta = humanizeFileSize(entry.Size)
			}
		}

		items = append(items, model.FileItem{
			Label: label,
			Path:  remoteFileLink(hostID, entry.Path, 0),
			Kind:  entry.Kind,
			Meta:  meta,
		})
	}
	return items
}

func buildFileSearchItems(hostID string, matches []agentrpc.FileMatch) []model.FileItem {
	items := make([]model.FileItem, 0, len(matches))
	for _, match := range matches {
		label := match.Path
		if label == "" {
			label = filepathBase(match.Path)
		}
		items = append(items, model.FileItem{
			Label:   label,
			Path:    remoteFileLink(hostID, match.Path, match.Line),
			Kind:    "match",
			Meta:    searchMatchLine(match.Line),
			Preview: strings.TrimSpace(match.Preview),
		})
	}
	return items
}

func renderFileDiff(path, oldContent, newContent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", path)
	fmt.Fprintf(&b, "+++ %s\n", path)
	fmt.Fprintf(&b, "@@\n")
	if strings.TrimSpace(oldContent) != "" {
		for _, line := range strings.Split(strings.ReplaceAll(oldContent, "\r\n", "\n"), "\n") {
			fmt.Fprintf(&b, "-%s\n", line)
		}
	}
	if strings.TrimSpace(newContent) != "" {
		for _, line := range strings.Split(strings.ReplaceAll(newContent, "\r\n", "\n"), "\n") {
			fmt.Fprintf(&b, "+%s\n", line)
		}
	}
	return strings.TrimSpace(b.String())
}

func remoteFileChangeKind(created bool, writeMode string) string {
	if strings.EqualFold(strings.TrimSpace(writeMode), "append") {
		return "append"
	}
	if created {
		return "create"
	}
	return "update"
}

func humanizeFileSize(size int64) string {
	if size <= 0 {
		return ""
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

func readFileSizeLabel(content string, truncated bool) string {
	size := len(content)
	if size == 0 {
		if truncated {
			return "内容已截断"
		}
		return "0 B"
	}
	label := humanizeFileSize(int64(size))
	if truncated {
		return label + "（已截断）"
	}
	return label
}

func readFileMeta(content string, truncated bool) string {
	lines := countMeaningfulLines(content)
	if truncated {
		return fmt.Sprintf("%d 行 · 已截断", lines)
	}
	return fmt.Sprintf("%d 行", lines)
}

func readFileReadStatus(truncated bool) string {
	if truncated {
		return "已截断"
	}
	return "完整"
}

func readFileTailNote(truncated bool) string {
	if truncated {
		return "文件内容已截断，继续缩小读取范围可查看更多。"
	}
	return ""
}

func previewFileContent(content string, maxLines int) string {
	lines := meaningfulFileLines(content, maxLines)
	return strings.Join(lines, "\n")
}

func meaningfulFileLines(content string, limit int) []string {
	lines := make([]string, 0, 4)
	for _, raw := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if limit > 0 && len(lines) >= limit {
			break
		}
	}
	return lines
}

func countMeaningfulLines(content string) int {
	return len(meaningfulFileLines(content, 0))
}

func countRemoteEntries(entries []agentrpc.FileEntry) (files, dirs int) {
	for _, entry := range entries {
		switch entry.Kind {
		case "dir":
			dirs++
		case "file":
			files++
		}
	}
	return files, dirs
}

func listResultStatus(truncated bool) string {
	if truncated {
		return "已截断"
	}
	return "完整"
}

func listFileTailNote(truncated bool) string {
	if truncated {
		return "结果已截断，继续缩小目录范围可查看更多。"
	}
	return ""
}

func listFileHighlights(root string, entries []agentrpc.FileEntry, truncated bool) []string {
	lines := make([]string, 0, 5)
	if root != "" {
		lines = append(lines, "目录: "+root)
	}
	for _, entry := range entries {
		label := entry.Name
		if label == "" {
			label = filepathBase(entry.Path)
		}
		if entry.Kind == "dir" {
			label += "/"
		}
		lines = append(lines, label)
		if len(lines) >= 4 {
			break
		}
	}
	if truncated {
		lines = append(lines, "结果已截断")
	}
	return lines
}

func countSearchMatches(matches []agentrpc.FileMatch) (files int, lines int) {
	seen := make(map[string]struct{})
	for _, match := range matches {
		if strings.TrimSpace(match.Path) != "" {
			seen[match.Path] = struct{}{}
		}
		if match.Line > 0 {
			lines++
		}
	}
	return len(seen), lines
}

func searchResultStatus(truncated bool) string {
	if truncated {
		return "已截断"
	}
	return "完整"
}

func searchFileTailNote(truncated bool) string {
	if truncated {
		return "搜索结果已截断，继续缩小搜索范围可查看更多。"
	}
	return ""
}

func searchMatchLine(line int) string {
	if line > 0 {
		return fmt.Sprintf("第 %d 行", line)
	}
	return ""
}

func searchFileHighlights(matches []agentrpc.FileMatch, truncated bool) []string {
	lines := make([]string, 0, len(matches)+1)
	for _, match := range matches {
		parts := []string{}
		if p := filepathBase(match.Path); p != "" {
			parts = append(parts, p)
		}
		if match.Line > 0 {
			parts = append(parts, fmt.Sprintf("第 %d 行", match.Line))
		}
		if snippet := strings.TrimSpace(match.Preview); snippet != "" {
			parts = append(parts, snippet)
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, " · "))
		}
		if len(lines) >= 4 {
			break
		}
	}
	if truncated {
		lines = append(lines, "搜索结果已截断")
	}
	return lines
}
