package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const skillContextToolName = "load_skill_context"

type skillContextUnifiedTool struct {
	app *App
}

type skillContextDisplayAdapter struct{}

type skillContextSelection struct {
	ID             string
	Name           string
	Path           string
	ActivationMode string
	MatchMode      string
}

type skillContextResolution struct {
	Summary       string
	Skills        []skillContextSelection
	Items         []map[string]any
	MissingSkills []map[string]any
}

func (a *App) skillContextUnifiedTool() UnifiedTool {
	return skillContextUnifiedTool{app: a}
}

func (t skillContextUnifiedTool) Name() string { return skillContextToolName }

func (t skillContextUnifiedTool) Aliases() []string { return nil }

func (t skillContextUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription(skillContextToolName)
}

func (t skillContextUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "Current user request used to decide which skills should be injected.",
			},
		},
		"additionalProperties": false,
	}
}

func (t skillContextUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	message := strings.TrimSpace(getStringAny(req.Input, "message"))
	resolution, err := t.resolve(ctx, message)
	if err != nil {
		return ToolCallResult{}, err
	}
	return ToolCallResult{
		Output:            resolution.Summary,
		DisplayOutput:     resolution.displayPayload(),
		StructuredContent: resolution.structuredContent(),
		Metadata: map[string]any{
			"skipCardProjection":      true,
			"trackActivityCompletion": false,
		},
	}, nil
}

func (skillContextUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (skillContextUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (skillContextUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (skillContextUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (skillContextUnifiedTool) Display() ToolDisplayAdapter { return skillContextDisplayAdapter{} }

func (skillContextDisplayAdapter) RenderUse(ToolCallRequest) *ToolDisplayPayload { return nil }

func (skillContextDisplayAdapter) RenderProgress(ToolProgressEvent) *ToolDisplayPayload { return nil }

func (skillContextDisplayAdapter) RenderResult(result ToolCallResult) *ToolDisplayPayload {
	return result.DisplayOutput
}

func (t skillContextUnifiedTool) resolve(ctx context.Context, message string) (skillContextResolution, error) {
	if t.app == nil {
		return skillContextResolution{}, fmt.Errorf("%s is not configured", skillContextToolName)
	}
	return t.app.resolveSkillContext(ctx, t.app.mainAgentProfile(), message)
}

func (a *App) resolveSkillContext(ctx context.Context, profile model.AgentProfile, message string) (skillContextResolution, error) {
	resolution := skillContextResolution{
		Skills:        make([]skillContextSelection, 0),
		Items:         make([]map[string]any, 0),
		MissingSkills: make([]map[string]any, 0),
	}

	discovered, err := a.listInstalledSkills(ctx)
	if err != nil {
		return resolution, err
	}
	pathMap := buildManagedSkillPathMap(profile, discovered)
	selectedPaths := make(map[string]struct{})

	for _, item := range profile.Skills {
		if !skillEnabledByProfile(item) {
			continue
		}

		activationMode := model.NormalizeAgentSkillActivationMode(item.ActivationMode)
		matchMode := "implicit_default"
		if activationMode == model.AgentSkillActivationExplicit {
			if !explicitSkillRequested(message, item) {
				continue
			}
			matchMode = "explicit_request"
		}

		path := ""
		for _, candidate := range []string{item.ID, item.Name} {
			path = strings.TrimSpace(pathMap[normalizeSkillLookupKey(candidate)])
			if path != "" {
				break
			}
		}
		if path == "" {
			if matchMode == "explicit_request" {
				resolution.MissingSkills = append(resolution.MissingSkills, map[string]any{
					"id":             strings.TrimSpace(item.ID),
					"name":           firstNonEmptyValue(strings.TrimSpace(item.Name), strings.TrimSpace(item.ID)),
					"activationMode": activationMode,
					"matchMode":      matchMode,
					"reason":         "skill_path_not_discovered",
				})
			}
			continue
		}
		if _, exists := selectedPaths[path]; exists {
			continue
		}
		selectedPaths[path] = struct{}{}

		name := firstNonEmptyValue(strings.TrimSpace(item.Name), strings.TrimSpace(item.ID))
		resolution.Skills = append(resolution.Skills, skillContextSelection{
			ID:             strings.TrimSpace(item.ID),
			Name:           name,
			Path:           path,
			ActivationMode: activationMode,
			MatchMode:      matchMode,
		})
		resolution.Items = append(resolution.Items, map[string]any{
			"type": "skill",
			"name": name,
			"path": path,
		})
	}

	resolution.Summary = resolution.summary()
	return resolution, nil
}

func (r skillContextResolution) summary() string {
	if len(r.Skills) == 0 {
		if len(r.MissingSkills) > 0 {
			names := make([]string, 0, len(r.MissingSkills))
			for _, item := range r.MissingSkills {
				if name := strings.TrimSpace(getStringAny(item, "name")); name != "" {
					names = append(names, name)
				}
			}
			if len(names) > 0 {
				return "未注入技能上下文：" + strings.Join(names, ", ")
			}
		}
		return "未注入额外技能上下文"
	}

	names := make([]string, 0, len(r.Skills))
	for _, item := range r.Skills {
		if name := strings.TrimSpace(item.Name); name != "" {
			names = append(names, name)
		}
	}
	return fmt.Sprintf("已注入 %d 个技能上下文：%s", len(r.Skills), strings.Join(names, ", "))
}

func (r skillContextResolution) displayPayload() *ToolDisplayPayload {
	implicitCount := 0
	explicitCount := 0
	for _, item := range r.Skills {
		switch item.MatchMode {
		case "explicit_request":
			explicitCount++
		default:
			implicitCount++
		}
	}

	blocks := make([]ToolDisplayBlock, 0, 4)
	if len(r.Skills) == 0 && len(r.MissingSkills) > 0 {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockWarning,
			Title: "技能未注入",
			Text:  r.Summary,
			Items: cloneNestedAnyValue(r.MissingSkills).([]map[string]any),
		})
	}
	if len(r.Skills) > 0 {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockResultStats,
			Title: "技能注入统计",
			Items: []map[string]any{
				{"label": "注入技能", "value": fmt.Sprintf("%d", len(r.Skills))},
				{"label": "默认注入", "value": fmt.Sprintf("%d", implicitCount)},
				{"label": "显式匹配", "value": fmt.Sprintf("%d", explicitCount)},
			},
		})

		items := make([]map[string]any, 0, len(r.Skills))
		for _, item := range r.Skills {
			items = append(items, map[string]any{
				"label": item.Name,
				"value": fmt.Sprintf("%s · %s", item.MatchMode, item.ActivationMode),
				"path":  item.Path,
			})
		}
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockKVList,
			Title: "匹配技能",
			Items: items,
		})
	}
	blocks = append(blocks, ToolDisplayBlock{
		Kind:  ToolDisplayBlockText,
		Title: "上下文摘要",
		Text:  r.Summary,
	})

	return &ToolDisplayPayload{
		Summary:   r.Summary,
		Activity:  strings.Join(r.skillNames(), ", "),
		Blocks:    blocks,
		SkipCards: true,
		Metadata: map[string]any{
			"compatibilityWrapper": true,
		},
	}
}

func (r skillContextResolution) structuredContent() map[string]any {
	skills := make([]map[string]any, 0, len(r.Skills))
	implicitCount := 0
	explicitCount := 0
	for _, item := range r.Skills {
		if item.MatchMode == "explicit_request" {
			explicitCount++
		} else {
			implicitCount++
		}
		skills = append(skills, map[string]any{
			"id":             item.ID,
			"name":           item.Name,
			"path":           item.Path,
			"activationMode": item.ActivationMode,
			"matchMode":      item.MatchMode,
		})
	}

	return map[string]any{
		"summary":       r.Summary,
		"skillCount":    len(r.Skills),
		"implicitCount": implicitCount,
		"explicitCount": explicitCount,
		"skills":        skills,
		"items":         cloneNestedAnyValue(r.Items),
		"missingSkills": cloneNestedAnyValue(r.MissingSkills),
		"wrapperMode":   "compatibility",
	}
}

func (r skillContextResolution) skillNames() []string {
	names := make([]string, 0, len(r.Skills))
	for _, item := range r.Skills {
		if name := strings.TrimSpace(item.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}
