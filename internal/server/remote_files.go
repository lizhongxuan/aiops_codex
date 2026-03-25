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
	note := ""
	if result.Truncated {
		note = "结果已截断，继续缩小目录范围可查看更多。"
	}
	return model.Card{
		ID:      cardID,
		Type:    "ResultSummaryCard",
		Title:   "远程文件列表",
		Summary: fmt.Sprintf("目录 %s 下找到 %d 个条目。", result.Path, len(result.Entries)),
		Status:  "completed",
		KVRows: []model.KeyValueRow{
			{Key: "主机", Value: hostID},
			{Key: "目录", Value: result.Path},
		},
		FileItems: buildFileListItems(hostID, result.Entries),
		Text:      note,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
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
	note := ""
	if result.Truncated {
		note = "结果已截断，继续缩小搜索范围可查看更多。"
	}
	return model.Card{
		ID:      cardID,
		Type:    "ResultSummaryCard",
		Title:   "远程搜索结果",
		Summary: fmt.Sprintf("在 %s 中搜索 %q，命中 %d 个位置。", result.Path, result.Query, len(result.Matches)),
		Status:  "completed",
		KVRows: []model.KeyValueRow{
			{Key: "主机", Value: hostID},
			{Key: "范围", Value: result.Path},
			{Key: "关键词", Value: result.Query},
		},
		FileItems: buildFileSearchItems(hostID, result.Matches),
		Text:      note,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
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
		label := filepathBase(match.Path)
		items = append(items, model.FileItem{
			Label:   label,
			Path:    remoteFileLink(hostID, match.Path, match.Line),
			Kind:    "match",
			Meta:    fmt.Sprintf("第 %d 行", match.Line),
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
