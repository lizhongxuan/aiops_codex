package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type remoteFileListExecutor func(context.Context, string, string, bool, int) (*agentrpc.FileListResult, error)
type remoteFileReadExecutor func(context.Context, string, string, int) (*agentrpc.FileReadResult, error)
type remoteFileSearchExecutor func(context.Context, string, string, string, int) (*agentrpc.FileSearchResult, error)
type remoteFileWriteExecutor func(context.Context, string, string, string, string) (*agentrpc.FileWriteResult, error)
type hostStructuredReadExecutor func(context.Context, ToolInvocation, execSpec) (remoteExecResult, error)

type remoteFileListUnifiedTool struct {
	app       *App
	listFiles remoteFileListExecutor
}

type remoteFileReadUnifiedTool struct {
	app      *App
	readFile remoteFileReadExecutor
}

type remoteFileSearchUnifiedTool struct {
	app         *App
	searchFiles remoteFileSearchExecutor
}

type remoteFileWriteUnifiedTool struct {
	app       *App
	writeFile remoteFileWriteExecutor
}

type hostFileReadUnifiedTool struct {
	app     *App
	execute hostStructuredReadExecutor
}

type hostFileSearchUnifiedTool struct {
	app     *App
	execute hostStructuredReadExecutor
}

type fileToolUseDisplayAdapter struct {
	renderUse func(ToolCallRequest) *ToolDisplayPayload
}

func (a *App) remoteFileListUnifiedTool() UnifiedTool {
	return remoteFileListUnifiedTool{app: a}
}

func (a *App) remoteFileReadUnifiedTool() UnifiedTool {
	return remoteFileReadUnifiedTool{app: a}
}

func (a *App) remoteFileSearchUnifiedTool() UnifiedTool {
	return remoteFileSearchUnifiedTool{app: a}
}

func (a *App) remoteFileWriteUnifiedTool() UnifiedTool {
	return remoteFileWriteUnifiedTool{app: a}
}

func (a *App) hostFileReadUnifiedTool() UnifiedTool {
	return hostFileReadUnifiedTool{app: a}
}

func (a *App) hostFileSearchUnifiedTool() UnifiedTool {
	return hostFileSearchUnifiedTool{app: a}
}

func (a fileToolUseDisplayAdapter) RenderUse(req ToolCallRequest) *ToolDisplayPayload {
	if a.renderUse == nil {
		return nil
	}
	return a.renderUse(req)
}

func (fileToolUseDisplayAdapter) RenderProgress(ToolProgressEvent) *ToolDisplayPayload {
	return nil
}

func (fileToolUseDisplayAdapter) RenderResult(ToolCallResult) *ToolDisplayPayload {
	return nil
}

func (t remoteFileListUnifiedTool) Name() string { return "list_remote_files" }

func (t remoteFileListUnifiedTool) Aliases() []string { return []string{"list_files"} }

func (t remoteFileListUnifiedTool) Description(ToolDescriptionContext) string {
	return remoteToolPromptDescription("list_remote_files")
}

func (t remoteFileListUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path to inspect.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Whether to recurse into descendants.",
			},
			"max_entries": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     500,
				"description": "Optional maximum number of entries.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this listing is needed.",
			},
		},
		"required":             []string{"host", "path", "reason"},
		"additionalProperties": false,
	}
}

func (t remoteFileListUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseRemoteListFilesArgs(req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	result, err := t.run(ctx, hostID, args.Path, args.Recursive, args.MaxEntries)
	if err != nil {
		return ToolCallResult{}, err
	}
	return ToolCallResult{
		Output:            renderFileListMessage(hostID, result.Path, result.Entries, result.Truncated),
		DisplayOutput:     fileListDisplayPayload(hostID, result, args.Recursive),
		StructuredContent: structuredRemoteFileListContent(hostID, result, args.Recursive),
		Metadata: map[string]any{
			"trackActivityCompletion": true,
			"lifecycleMessage":        "已列出 " + result.Path,
		},
	}, nil
}

func (t remoteFileListUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t remoteFileListUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t remoteFileListUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t remoteFileListUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t remoteFileListUnifiedTool) Display() ToolDisplayAdapter {
	return fileToolUseDisplayAdapter{
		renderUse: func(req ToolCallRequest) *ToolDisplayPayload {
			req.Normalize()
			path := strings.TrimSpace(getStringAny(req.Input, "path"))
			if path == "" {
				return nil
			}
			return &ToolDisplayPayload{
				Summary:  "准备列出目录：" + path,
				Activity: path,
			}
		},
	}
}

func (t remoteFileListUnifiedTool) run(ctx context.Context, hostID, path string, recursive bool, maxEntries int) (*agentrpc.FileListResult, error) {
	if t.listFiles != nil {
		return t.listFiles(ctx, hostID, path, recursive, maxEntries)
	}
	if t.app == nil {
		return nil, errors.New("list_remote_files is not configured")
	}
	return t.app.remoteListFiles(ctx, hostID, path, recursive, maxEntries)
}

func (t remoteFileReadUnifiedTool) Name() string { return "read_remote_file" }

func (t remoteFileReadUnifiedTool) Aliases() []string { return []string{"read_file"} }

func (t remoteFileReadUnifiedTool) Description(ToolDescriptionContext) string {
	return remoteToolPromptDescription("read_remote_file")
}

func (t remoteFileReadUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File path to inspect.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"minimum":     256,
				"maximum":     262144,
				"description": "Optional maximum bytes to read.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this read is needed.",
			},
		},
		"required":             []string{"host", "path", "reason"},
		"additionalProperties": false,
	}
}

func (t remoteFileReadUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseRemoteReadFileArgs(req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	result, err := t.run(ctx, hostID, args.Path, args.MaxBytes)
	if err != nil {
		return ToolCallResult{}, err
	}

	toolText := fmt.Sprintf("Read file %s:\n\n%s", result.Path, result.Content)
	if result.Truncated {
		toolText += "\n\n[truncated]"
	}
	return ToolCallResult{
		Output:            toolText,
		DisplayOutput:     fileReadDisplayPayload(hostID, result),
		StructuredContent: structuredFileReadContent(hostID, result, "remote_file_api"),
		Metadata: map[string]any{
			"trackActivityCompletion": true,
			"lifecycleMessage":        "已浏览 " + result.Path,
		},
	}, nil
}

func (t remoteFileReadUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t remoteFileReadUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t remoteFileReadUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t remoteFileReadUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t remoteFileReadUnifiedTool) Display() ToolDisplayAdapter {
	return fileToolUseDisplayAdapter{
		renderUse: func(req ToolCallRequest) *ToolDisplayPayload {
			req.Normalize()
			path := strings.TrimSpace(getStringAny(req.Input, "path"))
			if path == "" {
				return nil
			}
			return &ToolDisplayPayload{
				Summary:  "准备读取文件：" + path,
				Activity: path,
			}
		},
	}
}

func (t remoteFileReadUnifiedTool) run(ctx context.Context, hostID, path string, maxBytes int) (*agentrpc.FileReadResult, error) {
	if t.readFile != nil {
		return t.readFile(ctx, hostID, path, maxBytes)
	}
	if t.app == nil {
		return nil, errors.New("read_remote_file is not configured")
	}
	return t.app.remoteReadFile(ctx, hostID, path, maxBytes)
}

func (t remoteFileSearchUnifiedTool) Name() string { return "search_remote_files" }

func (t remoteFileSearchUnifiedTool) Aliases() []string { return []string{"search_files"} }

func (t remoteFileSearchUnifiedTool) Description(ToolDescriptionContext) string {
	return remoteToolPromptDescription("search_remote_files")
}

func (t remoteFileSearchUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory path to search.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Pattern to search for.",
			},
			"max_matches": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     200,
				"description": "Optional maximum number of matches.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this search is needed.",
			},
		},
		"required":             []string{"host", "path", "query", "reason"},
		"additionalProperties": false,
	}
}

func (t remoteFileSearchUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseRemoteSearchFilesArgs(req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	result, err := t.run(ctx, hostID, args.Path, args.Query, args.MaxMatches)
	if err != nil {
		return ToolCallResult{}, err
	}
	matchCount := len(result.Matches)
	return ToolCallResult{
		Output:            renderFileSearchMessage(hostID, result.Path, result.Query, result.Matches, result.Truncated),
		DisplayOutput:     fileSearchDisplayPayload(hostID, result),
		StructuredContent: structuredFileSearchContent(hostID, result, "remote_file_api"),
		Metadata: map[string]any{
			"trackActivityCompletion": true,
			"lifecycleMessage":        fmt.Sprintf("已搜索内容（命中 %d 个位置）", matchCount),
		},
	}, nil
}

func (t remoteFileSearchUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t remoteFileSearchUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t remoteFileSearchUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t remoteFileSearchUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t remoteFileSearchUnifiedTool) Display() ToolDisplayAdapter {
	return fileToolUseDisplayAdapter{
		renderUse: func(req ToolCallRequest) *ToolDisplayPayload {
			req.Normalize()
			query := strings.TrimSpace(getStringAny(req.Input, "query"))
			path := strings.TrimSpace(getStringAny(req.Input, "path"))
			if query == "" {
				return nil
			}
			summary := "准备搜索内容：" + query
			if path != "" {
				summary = fmt.Sprintf("准备在 %s 中搜索：%s", path, query)
			}
			return &ToolDisplayPayload{
				Summary:  summary,
				Activity: query,
			}
		},
	}
}

func (t remoteFileSearchUnifiedTool) run(ctx context.Context, hostID, path, query string, maxMatches int) (*agentrpc.FileSearchResult, error) {
	if t.searchFiles != nil {
		return t.searchFiles(ctx, hostID, path, query, maxMatches)
	}
	if t.app == nil {
		return nil, errors.New("search_remote_files is not configured")
	}
	return t.app.remoteSearchFiles(ctx, hostID, path, query, maxMatches)
}

func (t remoteFileWriteUnifiedTool) Name() string { return "write_file" }

func (t remoteFileWriteUnifiedTool) Aliases() []string { return nil }

func (t remoteFileWriteUnifiedTool) Description(ToolDescriptionContext) string {
	return remoteToolPromptDescription("write_file")
}

func (t remoteFileWriteUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Target file path to write.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Final file content to write.",
			},
			"write_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"overwrite", "append"},
				"description": "Optional file write mode. Defaults to overwrite; use append to add content to the existing file.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this file write is needed.",
			},
		},
		"required":             []string{"host", "path", "content", "reason"},
		"additionalProperties": false,
	}
}

func (t remoteFileWriteUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseWriteFileToolArgs(req)
	if err != nil {
		return ToolCallResult{}, err
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	if !isRemoteHostID(hostID) {
		return ToolCallResult{}, errors.New("write_file currently requires a selected remote host")
	}
	if t.app != nil {
		if err := t.app.ensureWritableRootsForHost(hostID, []string{args.Path}); err != nil {
			return ToolCallResult{}, err
		}
	}
	result, err := t.run(ctx, hostID, args.Path, args.Content, args.WriteMode)
	if err != nil {
		return ToolCallResult{}, annotateRemoteFileChangeError(args, err)
	}

	cardID := fileWriteResultCardID(req.Invocation)
	now := model.NowString()
	hostName := ""
	if t.app != nil {
		hostName = hostNameOrID(t.app.findHost(hostID))
	}
	card := fileWriteResultCard(cardID, hostID, hostName, result, nil, now, now)
	display := fileWriteDisplayPayload(result)
	return ToolCallResult{
		Output:            fileWriteToolResultMessage(result),
		DisplayOutput:     display,
		StructuredContent: structuredFileWriteContent(hostID, result),
		Metadata: map[string]any{
			"finalCard":               lifecycleCardPayload(card),
			"syncActionArtifacts":     true,
			"trackActivityCompletion": true,
			"lifecycleMessage":        card.Summary,
		},
	}, nil
}

func (t remoteFileWriteUnifiedTool) CheckPermissions(_ context.Context, req ToolCallRequest) (PermissionResult, error) {
	req.Normalize()
	args, err := parseWriteFileToolArgs(req)
	if err != nil {
		return PermissionResult{}, err
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	if !isRemoteHostID(hostID) {
		return PermissionResult{}, errors.New("write_file currently requires a selected remote host")
	}
	if t.app != nil {
		if err := t.app.ensureCapabilityAllowedForHost(hostID, "fileChange"); err != nil {
			return PermissionResult{}, err
		}
		if err := t.app.ensureWritableRootsForHost(hostID, []string{args.Path}); err != nil {
			return PermissionResult{}, err
		}
	}
	return PermissionResult{
		Allowed:           false,
		RequiresApproval:  true,
		ApprovalType:      "write_file",
		ApprovalDecisions: []string{"accept", "accept_session", "decline"},
		Reason:            "write_file requires approval before remote file changes execute",
	}, nil
}

func (t remoteFileWriteUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return false }

func (t remoteFileWriteUnifiedTool) IsReadOnly(ToolCallRequest) bool { return false }

func (t remoteFileWriteUnifiedTool) IsDestructive(ToolCallRequest) bool { return true }

func (t remoteFileWriteUnifiedTool) Display() ToolDisplayAdapter {
	return fileToolUseDisplayAdapter{
		renderUse: func(req ToolCallRequest) *ToolDisplayPayload {
			req.Normalize()
			path := strings.TrimSpace(getStringAny(req.Input, "path"))
			if path == "" {
				return nil
			}
			mode := firstNonEmptyValue(strings.TrimSpace(getStringAny(req.Input, "write_mode", "writeMode")), "overwrite")
			summary := "准备写入文件：" + path
			if strings.EqualFold(mode, "append") {
				summary = "准备追加写入文件：" + path
			}
			return &ToolDisplayPayload{
				Summary:  summary,
				Activity: path,
			}
		},
	}
}

func (t remoteFileWriteUnifiedTool) run(ctx context.Context, hostID, path, content, writeMode string) (*agentrpc.FileWriteResult, error) {
	if t.writeFile != nil {
		return t.writeFile(ctx, hostID, path, content, writeMode)
	}
	if t.app == nil {
		return nil, errors.New("write_file is not configured")
	}
	return t.app.remoteWriteFile(ctx, hostID, path, content, writeMode)
}

func (t hostFileReadUnifiedTool) Name() string { return hostFileReadToolName }

func (t hostFileReadUnifiedTool) Aliases() []string { return nil }

func (t hostFileReadUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription(hostFileReadToolName)
}

func (t hostFileReadUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute file path to read.",
			},
			"max_lines": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     500,
				"description": "Maximum lines to read.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this read is needed.",
			},
		},
		"required":             []string{"host", "path", "reason"},
		"additionalProperties": false,
	}
}

func (t hostFileReadUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseHostFileReadArgs(req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	command, err := buildStructuredReadCommand(hostFileReadToolName, req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	execResult, err := t.run(ctx, req.Invocation, execSpec{
		Command:    command,
		TimeoutSec: 30,
		Readonly:   true,
		ToolName:   hostFileReadToolName,
	})
	if execErr := hostStructuredReadError(execResult, err); execErr != nil {
		return ToolCallResult{}, execErr
	}
	content := hostStructuredReadOutput(execResult)
	result := &agentrpc.FileReadResult{
		Path:    args.Path,
		Content: content,
	}
	return ToolCallResult{
		Output:            fmt.Sprintf("Read file %s:\n\n%s", result.Path, result.Content),
		DisplayOutput:     fileReadDisplayPayload(defaultHostID(args.HostID), result),
		StructuredContent: structuredFileReadContent(defaultHostID(args.HostID), result, "structured_read"),
		Metadata: map[string]any{
			"trackActivityCompletion": true,
			"lifecycleMessage":        "已浏览 " + result.Path,
		},
	}, nil
}

func (t hostFileReadUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t hostFileReadUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t hostFileReadUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t hostFileReadUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t hostFileReadUnifiedTool) Display() ToolDisplayAdapter {
	return fileToolUseDisplayAdapter{
		renderUse: func(req ToolCallRequest) *ToolDisplayPayload {
			req.Normalize()
			path := strings.TrimSpace(getStringAny(req.Input, "path"))
			if path == "" {
				return nil
			}
			return &ToolDisplayPayload{
				Summary:  "准备读取文件：" + path,
				Activity: path,
			}
		},
	}
}

func (t hostFileReadUnifiedTool) run(ctx context.Context, inv ToolInvocation, spec execSpec) (remoteExecResult, error) {
	if t.execute != nil {
		return t.execute(ctx, inv, spec)
	}
	if t.app == nil {
		return remoteExecResult{}, errors.New("host_file_read is not configured")
	}
	return t.app.runRemoteExecWithoutCard(ctx, inv.SessionID, defaultHostID(inv.HostID), spec)
}

func (t hostFileSearchUnifiedTool) Name() string { return hostFileSearchToolName }

func (t hostFileSearchUnifiedTool) Aliases() []string { return nil }

func (t hostFileSearchUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription(hostFileSearchToolName)
}

func (t hostFileSearchUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path to search in.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Pattern to search for.",
			},
			"max_matches": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     200,
				"description": "Maximum matches to return.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this search is needed.",
			},
		},
		"required":             []string{"host", "path", "pattern", "reason"},
		"additionalProperties": false,
	}
}

func (t hostFileSearchUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseHostFileSearchArgs(req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	command, err := buildStructuredReadCommand(hostFileSearchToolName, req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	execResult, err := t.run(ctx, req.Invocation, execSpec{
		Command:    command,
		TimeoutSec: 30,
		Readonly:   true,
		ToolName:   hostFileSearchToolName,
	})
	if execErr := hostStructuredReadError(execResult, err); execErr != nil {
		return ToolCallResult{}, execErr
	}
	result := &agentrpc.FileSearchResult{
		Path:    args.Path,
		Query:   args.Pattern,
		Matches: parseHostFileSearchMatches(hostStructuredReadOutput(execResult)),
	}
	matchCount := len(result.Matches)
	return ToolCallResult{
		Output:            renderFileSearchMessage(defaultHostID(args.HostID), result.Path, result.Query, result.Matches, false),
		DisplayOutput:     fileSearchDisplayPayload(defaultHostID(args.HostID), result),
		StructuredContent: structuredFileSearchContent(defaultHostID(args.HostID), result, "structured_read"),
		Metadata: map[string]any{
			"trackActivityCompletion": true,
			"lifecycleMessage":        fmt.Sprintf("已搜索内容（命中 %d 个位置）", matchCount),
		},
	}, nil
}

func (t hostFileSearchUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t hostFileSearchUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t hostFileSearchUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t hostFileSearchUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t hostFileSearchUnifiedTool) Display() ToolDisplayAdapter {
	return fileToolUseDisplayAdapter{
		renderUse: func(req ToolCallRequest) *ToolDisplayPayload {
			req.Normalize()
			pattern := strings.TrimSpace(getStringAny(req.Input, "pattern"))
			path := strings.TrimSpace(getStringAny(req.Input, "path"))
			if pattern == "" {
				return nil
			}
			summary := "准备搜索内容：" + pattern
			if path != "" {
				summary = fmt.Sprintf("准备在 %s 中搜索：%s", path, pattern)
			}
			return &ToolDisplayPayload{
				Summary:  summary,
				Activity: pattern,
			}
		},
	}
}

func (t hostFileSearchUnifiedTool) run(ctx context.Context, inv ToolInvocation, spec execSpec) (remoteExecResult, error) {
	if t.execute != nil {
		return t.execute(ctx, inv, spec)
	}
	if t.app == nil {
		return remoteExecResult{}, errors.New("host_file_search is not configured")
	}
	return t.app.runRemoteExecWithoutCard(ctx, inv.SessionID, defaultHostID(inv.HostID), spec)
}

type hostFileReadArgs struct {
	HostID   string
	Path     string
	MaxLines int
	Reason   string
}

type hostFileSearchArgs struct {
	HostID     string
	Path       string
	Pattern    string
	MaxMatches int
	Reason     string
}

func parseHostFileReadArgs(arguments map[string]any) (hostFileReadArgs, error) {
	args := hostFileReadArgs{
		HostID:   remoteToolTargetHost(arguments),
		Path:     strings.TrimSpace(getString(arguments, "path")),
		MaxLines: getInt(arguments, "max_lines", "maxLines"),
		Reason:   strings.TrimSpace(getString(arguments, "reason")),
	}
	if args.Path == "" {
		return hostFileReadArgs{}, errors.New("tool requires a path")
	}
	if args.Reason == "" {
		return hostFileReadArgs{}, errors.New("tool requires a reason")
	}
	if args.MaxLines <= 0 {
		args.MaxLines = 100
	}
	return args, nil
}

func parseHostFileSearchArgs(arguments map[string]any) (hostFileSearchArgs, error) {
	args := hostFileSearchArgs{
		HostID:     remoteToolTargetHost(arguments),
		Path:       strings.TrimSpace(getString(arguments, "path")),
		Pattern:    strings.TrimSpace(getStringAny(arguments, "pattern", "query")),
		MaxMatches: getInt(arguments, "max_matches", "maxMatches"),
		Reason:     strings.TrimSpace(getString(arguments, "reason")),
	}
	if args.Path == "" {
		return hostFileSearchArgs{}, errors.New("tool requires a path")
	}
	if args.Pattern == "" {
		return hostFileSearchArgs{}, errors.New("tool requires a pattern")
	}
	if args.Reason == "" {
		return hostFileSearchArgs{}, errors.New("tool requires a reason")
	}
	if args.MaxMatches <= 0 {
		args.MaxMatches = 50
	}
	return args, nil
}

func structuredRemoteFileListContent(hostID string, result *agentrpc.FileListResult, recursive bool) map[string]any {
	files, dirs := countRemoteEntries(result.Entries)
	return map[string]any{
		"hostId":     hostID,
		"path":       result.Path,
		"recursive":  recursive,
		"truncated":  result.Truncated,
		"entryCount": len(result.Entries),
		"fileCount":  files,
		"dirCount":   dirs,
		"entries":    fileListDisplayItems(hostID, result.Entries),
		"scope":      "remote_file_api",
	}
}

func structuredFileReadContent(hostID string, result *agentrpc.FileReadResult, scope string) map[string]any {
	return map[string]any{
		"hostId":      hostID,
		"path":        result.Path,
		"truncated":   result.Truncated,
		"sizeLabel":   readFileSizeLabel(result.Content, result.Truncated),
		"lineCount":   countMeaningfulLines(result.Content),
		"status":      readFileReadStatus(result.Truncated),
		"preview":     previewFileContent(result.Content, 40),
		"warningText": readFileTailNote(result.Truncated),
		"scope":       scope,
	}
}

func structuredFileSearchContent(hostID string, result *agentrpc.FileSearchResult, scope string) map[string]any {
	files, lines := countSearchMatches(result.Matches)
	return map[string]any{
		"hostId":        hostID,
		"path":          result.Path,
		"query":         result.Query,
		"truncated":     result.Truncated,
		"matchCount":    len(result.Matches),
		"fileCount":     files,
		"locationCount": lines,
		"matches":       fileSearchDisplayItems(hostID, result.Matches),
		"warningText":   searchFileTailNote(result.Truncated),
		"scope":         scope,
	}
}

func structuredFileWriteContent(hostID string, result *agentrpc.FileWriteResult) map[string]any {
	diff := renderFileDiff(result.Path, result.OldContent, result.NewContent)
	added, removed := diffLineStats(diff)
	changeKind := remoteFileChangeKind(result.Created, result.WriteMode)
	return map[string]any{
		"hostId":        hostID,
		"path":          result.Path,
		"changeKind":    changeKind,
		"writeMode":     firstNonEmptyValue(strings.TrimSpace(result.WriteMode), "overwrite"),
		"created":       result.Created,
		"cancelable":    result.Cancelable,
		"addedLines":    added,
		"removedLines":  removed,
		"dryRunSummary": truncate(diff, 600),
		"summary":       fileWriteSummaryText(changeKind, result.WriteMode, added, removed),
		"scope":         "remote_file_write",
	}
}

func fileListDisplayPayload(hostID string, result *agentrpc.FileListResult, recursive bool) *ToolDisplayPayload {
	files, dirs := countRemoteEntries(result.Entries)
	blocks := []ToolDisplayBlock{
		{
			Kind:  ToolDisplayBlockResultStats,
			Title: "目录统计",
			Items: []map[string]any{
				{"label": "条目", "value": strconv.Itoa(len(result.Entries))},
				{"label": "文件", "value": strconv.Itoa(files)},
				{"label": "目录", "value": strconv.Itoa(dirs)},
				{"label": "递归", "value": boolLabel(recursive)},
			},
		},
		{
			Kind:  ToolDisplayBlockLinkList,
			Title: "目录条目",
			Items: fileListDisplayItems(hostID, result.Entries),
		},
	}
	if warning := strings.TrimSpace(listFileTailNote(result.Truncated)); warning != "" {
		blocks = append(blocks, ToolDisplayBlock{
			Kind: ToolDisplayBlockWarning,
			Text: warning,
		})
	}
	return &ToolDisplayPayload{
		Summary:  fmt.Sprintf("目录 %s 下找到 %d 个条目", result.Path, len(result.Entries)),
		Activity: result.Path,
		Blocks:   blocks,
	}
}

func fileReadDisplayPayload(hostID string, result *agentrpc.FileReadResult) *ToolDisplayPayload {
	blocks := []ToolDisplayBlock{
		{
			Kind:  ToolDisplayBlockResultStats,
			Title: "读取统计",
			Items: []map[string]any{
				{"label": "大小", "value": readFileSizeLabel(result.Content, result.Truncated)},
				{"label": "行数", "value": strconv.Itoa(countMeaningfulLines(result.Content))},
				{"label": "状态", "value": readFileReadStatus(result.Truncated)},
			},
		},
		{
			Kind:  ToolDisplayBlockFilePreview,
			Title: "文件预览",
			Items: []map[string]any{{
				"path":    result.Path,
				"content": previewFileContent(result.Content, 40),
				"url":     remoteFileLink(hostID, result.Path, 0),
			}},
		},
	}
	if warning := strings.TrimSpace(readFileTailNote(result.Truncated)); warning != "" {
		blocks = append(blocks, ToolDisplayBlock{
			Kind: ToolDisplayBlockWarning,
			Text: warning,
		})
	}
	return &ToolDisplayPayload{
		Summary:  "已读取文件：" + result.Path,
		Activity: result.Path,
		Blocks:   blocks,
	}
}

func fileSearchDisplayPayload(hostID string, result *agentrpc.FileSearchResult) *ToolDisplayPayload {
	files, lines := countSearchMatches(result.Matches)
	blocks := []ToolDisplayBlock{
		{
			Kind:  ToolDisplayBlockSearchQueries,
			Title: "搜索模式",
			Items: []map[string]any{{"query": result.Query}},
		},
		{
			Kind:  ToolDisplayBlockResultStats,
			Title: "命中统计",
			Items: []map[string]any{
				{"label": "命中位置", "value": strconv.Itoa(len(result.Matches))},
				{"label": "文件数", "value": strconv.Itoa(files)},
				{"label": "行号", "value": strconv.Itoa(lines)},
			},
		},
		{
			Kind:  ToolDisplayBlockLinkList,
			Title: "文件命中",
			Items: fileSearchDisplayItems(hostID, result.Matches),
		},
	}
	if warning := strings.TrimSpace(searchFileTailNote(result.Truncated)); warning != "" {
		blocks = append(blocks, ToolDisplayBlock{
			Kind: ToolDisplayBlockWarning,
			Text: warning,
		})
	}
	return &ToolDisplayPayload{
		Summary:  fmt.Sprintf("在 %s 中搜索 %q，命中 %d 个位置", result.Path, result.Query, len(result.Matches)),
		Activity: result.Query,
		Blocks:   blocks,
	}
}

func fileWriteDisplayPayload(result *agentrpc.FileWriteResult) *ToolDisplayPayload {
	diff := renderFileDiff(result.Path, result.OldContent, result.NewContent)
	added, removed := diffLineStats(diff)
	changeKind := remoteFileChangeKind(result.Created, result.WriteMode)
	return &ToolDisplayPayload{
		Summary:  fileWriteCompletionSummary(result.Path, changeKind),
		Activity: result.Path,
		Blocks: []ToolDisplayBlock{
			{
				Kind:  ToolDisplayBlockResultStats,
				Title: "写入结果",
				Items: []map[string]any{
					{"label": "路径", "value": result.Path},
					{"label": "操作", "value": fileWriteOperationLabel(changeKind)},
					{"label": "模式", "value": fileWriteModeLabel(result.WriteMode)},
				},
			},
			{
				Kind:  ToolDisplayBlockFileDiffSummary,
				Title: "写入摘要",
				Items: []map[string]any{{
					"path":    result.Path,
					"summary": fileWriteSummaryText(changeKind, result.WriteMode, added, removed),
					"added":   added,
					"removed": removed,
				}},
			},
		},
	}
}

func fileListDisplayItems(hostID string, entries []agentrpc.FileEntry) []map[string]any {
	items := buildFileListItems(hostID, entries)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"label": item.Label,
			"url":   item.Path,
			"path":  item.Path,
			"value": item.Meta,
		})
	}
	return out
}

func fileSearchDisplayItems(hostID string, matches []agentrpc.FileMatch) []map[string]any {
	items := buildFileSearchItems(hostID, matches)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		label := item.Label
		if strings.TrimSpace(item.Meta) != "" {
			label = strings.TrimSpace(label + " " + item.Meta)
		}
		out = append(out, map[string]any{
			"label": label,
			"url":   item.Path,
			"path":  item.Path,
			"value": item.Preview,
		})
	}
	return out
}

func parseWriteFileToolArgs(req ToolCallRequest) (remoteFileChangeArgs, error) {
	req.Normalize()
	input := cloneNestedAnyMap(req.Input)
	if input == nil {
		input = make(map[string]any)
	}
	if strings.TrimSpace(getStringAny(input, "host", "hostId")) == "" && strings.TrimSpace(req.Invocation.HostID) != "" {
		input["host"] = strings.TrimSpace(req.Invocation.HostID)
	}
	input["mode"] = "file_change"
	return parseRemoteFileChangeArgs(input)
}

func fileWriteResultCardID(inv ToolInvocation) string {
	if callID := strings.TrimSpace(inv.CallID); callID != "" {
		return dynamicToolCardID(callID)
	}
	if invocationID := strings.TrimSpace(inv.InvocationID); invocationID != "" {
		return "result-" + invocationID
	}
	return model.NewID("toolcmd")
}

func fileWriteResultCard(cardID, hostID, hostName string, result *agentrpc.FileWriteResult, baseDetail map[string]any, createdAt, updatedAt string) model.Card {
	detail := cloneAnyMap(baseDetail)
	display := fileWriteDisplayPayload(result)
	diff := renderFileDiff(result.Path, result.OldContent, result.NewContent)
	changeKind := remoteFileChangeKind(result.Created, result.WriteMode)
	detail["filePath"] = result.Path
	detail["changeKind"] = changeKind
	detail["writeMode"] = firstNonEmptyValue(strings.TrimSpace(result.WriteMode), "overwrite")
	detail["cancelable"] = result.Cancelable
	if strings.TrimSpace(getStringAny(detail, "dryRunSummary")) == "" {
		detail["dryRunSummary"] = truncate(diff, 600)
	}
	detail["display"] = toolDisplayPayloadToProjectionMap(display)
	return model.Card{
		ID:        strings.TrimSpace(cardID),
		Type:      "FileChangeCard",
		Title:     "Remote file change",
		Summary:   display.Summary,
		Status:    "completed",
		Changes:   []model.FileChange{{Path: result.Path, Kind: changeKind, Diff: diff}},
		Text:      fileWriteCompletionSummary(result.Path, changeKind),
		HostID:    strings.TrimSpace(hostID),
		HostName:  strings.TrimSpace(hostName),
		Detail:    detail,
		CreatedAt: strings.TrimSpace(createdAt),
		UpdatedAt: strings.TrimSpace(updatedAt),
	}
}

func fileWriteToolResultMessage(result *agentrpc.FileWriteResult) string {
	changeKind := remoteFileChangeKind(result.Created, result.WriteMode)
	switch changeKind {
	case "create":
		return fmt.Sprintf("Created file %s successfully.", result.Path)
	case "append":
		return fmt.Sprintf("Appended to file %s successfully.", result.Path)
	default:
		return fmt.Sprintf("Updated file %s successfully.", result.Path)
	}
}

func fileWriteCompletionSummary(path, changeKind string) string {
	switch strings.TrimSpace(changeKind) {
	case "create":
		return "已创建远程文件 " + path
	case "append":
		return "已追加远程文件 " + path
	default:
		return "已修改远程文件 " + path
	}
}

func fileWriteOperationLabel(changeKind string) string {
	switch strings.TrimSpace(changeKind) {
	case "create":
		return "新建"
	case "append":
		return "追加"
	default:
		return "更新"
	}
}

func fileWriteModeLabel(writeMode string) string {
	switch strings.TrimSpace(writeMode) {
	case "append":
		return "追加"
	default:
		return "覆盖"
	}
}

func fileWriteSummaryText(changeKind, writeMode string, added, removed int) string {
	parts := []string{fileWriteOperationLabel(changeKind), fileWriteModeLabel(writeMode)}
	if added > 0 || removed > 0 {
		parts = append(parts, fmt.Sprintf("+%d -%d", added, removed))
	}
	return strings.Join(parts, " · ")
}

func diffLineStats(diff string) (added, removed int) {
	for _, line := range strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "@@"):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func parseHostFileSearchMatches(output string) []agentrpc.FileMatch {
	matches := make([]agentrpc.FileMatch, 0)
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		match := agentrpc.FileMatch{
			Path: strings.TrimSpace(parts[0]),
		}
		if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			match.Line = n
		}
		if len(parts) == 3 {
			match.Preview = strings.TrimSpace(parts[2])
		}
		matches = append(matches, match)
	}
	return matches
}

func hostStructuredReadOutput(result remoteExecResult) string {
	return strings.TrimSpace(firstNonEmptyValue(result.Stdout, result.Output, result.Stderr, result.Message))
}

func hostStructuredReadError(result remoteExecResult, err error) error {
	if err != nil {
		return err
	}
	if execResultCardStatus(result) == "completed" {
		return nil
	}
	return errors.New(firstNonEmptyValue(
		strings.TrimSpace(result.Error),
		strings.TrimSpace(result.Message),
		strings.TrimSpace(result.Stderr),
		strings.TrimSpace(result.Output),
		"structured read failed",
	))
}

func boolLabel(value bool) string {
	if value {
		return "是"
	}
	return "否"
}
