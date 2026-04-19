package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type runtimeToolProjection struct {
	app *App
}

func NewRuntimeToolProjection(app *App) ToolLifecycleSubscriber {
	return runtimeToolProjection{app: app}
}

func (p runtimeToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	p.app.projectToolLifecycleRuntime(event.SessionID, event)
	return nil
}

func (a *App) projectToolLifecycleRuntime(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	switch event.Type {
	case ToolLifecycleEventStarted:
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "executing")
		a.setRuntimeTurnPhase(sessionID, phase)
		a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
			if shouldTrackToolActivityStart(event) {
				applyToolActivityStart(rt, event)
			}
		})
	case ToolLifecycleEventApprovalRequested:
		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		a.store.UpdateRuntime(sessionID, clearToolActivity)
	case ToolLifecycleEventApprovalResolved:
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "thinking")
		a.setRuntimeTurnPhase(sessionID, phase)
		a.store.UpdateRuntime(sessionID, clearToolActivity)
	case ToolLifecycleEventCompleted:
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "thinking")
		a.setRuntimeTurnPhase(sessionID, phase)
		a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
			applyToolActivityCompletion(rt, event)
		})
	case ToolLifecycleEventFailed:
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "thinking")
		a.setRuntimeTurnPhase(sessionID, phase)
		a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
			applyToolActivityCompletion(rt, event)
		})
	}
}

func applyToolActivityStart(rt *model.RuntimeState, event ToolLifecycleEvent) {
	if rt == nil {
		return
	}

	displayActivity := strings.TrimSpace(toolProjectionDisplayActivityFromEvent(event))
	target := strings.TrimSpace(firstNonEmptyValue(displayActivity, event.ActivityTarget, event.Label))
	query := strings.TrimSpace(firstNonEmptyValue(displayActivity, event.ActivityQuery))
	toolName := strings.ToLower(strings.TrimSpace(event.ToolName))
	switch toolName {
	case "read_file", "read_remote_file", "host_file_read", "open_page", "find_in_page":
		rt.Activity.CurrentReadingFile = target
	case "list_files", "list_dir", "list_remote_files":
		rt.Activity.CurrentListingPath = target
	case "search_files", "search_remote_files", "host_file_search":
		rt.Activity.CurrentSearchKind = "content"
		rt.Activity.CurrentSearchQuery = query
	case "web_search":
		rt.Activity.CurrentSearchKind = "web"
		rt.Activity.CurrentSearchQuery = query
		rt.Activity.CurrentWebSearchQuery = query
	case "write_file", "apply_patch":
		rt.Activity.CurrentChangingFile = target
	default:
		if target != "" {
			rt.Activity.CurrentChangingFile = target
		}
	}
}

func clearToolActivity(rt *model.RuntimeState) {
	if rt == nil {
		return
	}
	rt.Activity.CurrentReadingFile = ""
	rt.Activity.CurrentChangingFile = ""
	rt.Activity.CurrentListingPath = ""
	rt.Activity.CurrentSearchKind = ""
	rt.Activity.CurrentSearchQuery = ""
	rt.Activity.CurrentWebSearchQuery = ""
}

func applyToolActivityCompletion(rt *model.RuntimeState, event ToolLifecycleEvent) {
	if rt == nil {
		return
	}

	args := toolLifecycleArguments(event)
	outputData := toolLifecycleOutputData(event)
	displayActivity := strings.TrimSpace(toolProjectionDisplayActivityFromEvent(event))
	toolName := strings.ToLower(strings.TrimSpace(event.ToolName))
	if !shouldTrackToolActivityCompletion(event) {
		clearToolActivity(rt)
		return
	}

	switch toolName {
	case "web_search":
		query := strings.TrimSpace(firstNonEmptyValue(displayActivity, event.ActivityQuery, getStringAny(args, "query")))
		clearToolActivity(rt)
		if query != "" {
			rt.Activity.SearchedWebQueries = append(rt.Activity.SearchedWebQueries, model.ActivityEntry{Query: query})
		}
		rt.Activity.SearchCount = len(rt.Activity.SearchedWebQueries) + len(rt.Activity.SearchedContentQueries)
	case "open_page", "find_in_page":
		clearToolActivity(rt)
		rt.Activity.FilesViewed++
	case "list_files", "list_dir", "list_remote_files":
		clearToolActivity(rt)
		rt.Activity.ListCount++
	case "read_file":
		clearToolActivity(rt)
		rt.Activity.FilesViewed++
	case "read_remote_file":
		clearToolActivity(rt)
		path := strings.TrimSpace(firstNonEmptyValue(getStringAny(outputData, "path"), getStringAny(args, "path")))
		if path == "" {
			rt.Activity.FilesViewed++
			return
		}
		entry := model.ActivityEntry{Label: filepathBase(path), Path: path}
		appendUniqueActivityEntry(&rt.Activity.ViewedFiles, entry, func(existing, next model.ActivityEntry) bool {
			return existing.Path != "" && existing.Path == next.Path
		})
		rt.Activity.FilesViewed = len(rt.Activity.ViewedFiles)
	case "host_file_read":
		clearToolActivity(rt)
		path := strings.TrimSpace(firstNonEmptyValue(getStringAny(outputData, "path"), getStringAny(args, "path")))
		if path == "" {
			rt.Activity.FilesViewed++
			return
		}
		entry := model.ActivityEntry{Label: filepathBase(path), Path: path}
		appendUniqueActivityEntry(&rt.Activity.ViewedFiles, entry, func(existing, next model.ActivityEntry) bool {
			return existing.Path != "" && existing.Path == next.Path
		})
		rt.Activity.FilesViewed = len(rt.Activity.ViewedFiles)
	case "search_files":
		query := strings.TrimSpace(firstNonEmptyValue(displayActivity, event.ActivityQuery, getStringAny(args, "query")))
		clearToolActivity(rt)
		if query != "" {
			rt.Activity.SearchedContentQueries = append(rt.Activity.SearchedContentQueries, model.ActivityEntry{Query: query})
		}
		rt.Activity.SearchCount = len(rt.Activity.SearchedWebQueries) + len(rt.Activity.SearchedContentQueries)
	case "search_remote_files":
		query := strings.TrimSpace(firstNonEmptyValue(displayActivity, event.ActivityQuery, getStringAny(outputData, "query"), getStringAny(args, "query")))
		path := strings.TrimSpace(firstNonEmptyValue(getStringAny(outputData, "path"), getStringAny(args, "path")))
		clearToolActivity(rt)
		rt.Activity.SearchCount++
		if query != "" {
			matchCount, _ := getIntAny(outputData, "matchCount", "matches")
			entry := model.ActivityEntry{
				Label: "搜索内容：" + query,
				Query: query,
				Path:  path,
			}
			if path != "" {
				entry.Label = "在 " + path + " 中搜索 " + query
			}
			if matchCount > 0 {
				entry.Label += fmt.Sprintf("（命中 %d 个位置）", matchCount)
				rt.Activity.SearchLocationCount += matchCount
			}
			appendUniqueActivityEntry(&rt.Activity.SearchedContentQueries, entry, func(existing, next model.ActivityEntry) bool {
				return existing.Path == next.Path && existing.Query == next.Query
			})
		}
	case "host_file_search":
		query := strings.TrimSpace(firstNonEmptyValue(displayActivity, event.ActivityQuery, getStringAny(outputData, "query"), getStringAny(args, "pattern"), getStringAny(args, "query")))
		path := strings.TrimSpace(firstNonEmptyValue(getStringAny(outputData, "path"), getStringAny(args, "path")))
		clearToolActivity(rt)
		rt.Activity.SearchCount++
		if query != "" {
			matchCount, _ := getIntAny(outputData, "matchCount", "matches")
			entry := model.ActivityEntry{
				Label: "搜索内容：" + query,
				Query: query,
				Path:  path,
			}
			if path != "" {
				entry.Label = "在 " + path + " 中搜索 " + query
			}
			if matchCount > 0 {
				entry.Label += fmt.Sprintf("（命中 %d 个位置）", matchCount)
				rt.Activity.SearchLocationCount += matchCount
			}
			appendUniqueActivityEntry(&rt.Activity.SearchedContentQueries, entry, func(existing, next model.ActivityEntry) bool {
				return existing.Path == next.Path && existing.Query == next.Query
			})
		}
	case "execute_command", "readonly_host_inspect", "shell_command":
		clearToolActivity(rt)
		rt.Activity.CommandsRun++
	case "write_file", "apply_patch":
		clearToolActivity(rt)
		rt.Activity.FilesChanged++
	default:
		clearToolActivity(rt)
	}
}

func toolProjectionDisplayActivityFromEvent(event ToolLifecycleEvent) string {
	for _, source := range []map[string]any{projectionMapFromSource(event.Payload, "display"), projectionMapFromSource(event.Metadata, "display")} {
		if source == nil {
			continue
		}
		if activity, ok := toolProjectionLookup(source, "activity"); ok {
			if s := strings.TrimSpace(fmt.Sprint(activity)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func shouldTrackToolActivityStart(event ToolLifecycleEvent) bool {
	if event.Payload == nil {
		return true
	}
	value, ok := event.Payload["trackActivityStart"]
	if !ok {
		return true
	}
	tracked, ok := value.(bool)
	if !ok {
		return true
	}
	return tracked
}

func shouldTrackToolActivityCompletion(event ToolLifecycleEvent) bool {
	if event.Payload == nil {
		return false
	}
	value, ok := event.Payload["trackActivityCompletion"]
	if !ok {
		return false
	}
	tracked, ok := value.(bool)
	return ok && tracked
}

func toolLifecycleArguments(event ToolLifecycleEvent) map[string]any {
	if event.Payload == nil {
		return nil
	}
	if value, ok := event.Payload["arguments"]; ok {
		if args, ok := asStringAnyMap(value); ok {
			return args
		}
	}
	return nil
}

func toolLifecycleOutputData(event ToolLifecycleEvent) map[string]any {
	if event.Payload == nil {
		return nil
	}
	if value, ok := event.Payload["outputData"]; ok {
		if data, ok := asStringAnyMap(value); ok {
			return data
		}
	}
	return nil
}
