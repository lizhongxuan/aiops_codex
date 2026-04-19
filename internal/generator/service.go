package generator

import (
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// LintIssue describes a single validation problem found during linting.
type LintIssue struct {
	Field   string `json:"field"`
	Level   string `json:"level"` // error | warning | info
	Message string `json:"message"`
}

// GeneratorService provides the core logic for generating draft Skill,
// UICard, and Bundle definitions from external metadata sources.
type GeneratorService struct{}

// NewGeneratorService creates a ready-to-use GeneratorService.
func NewGeneratorService() *GeneratorService {
	return &GeneratorService{}
}

// GenerateSkillFromMCP produces a draft AgentSkill from MCP tool metadata.
func (g *GeneratorService) GenerateSkillFromMCP(toolName, toolDesc string, inputSchema map[string]any) (*model.AgentSkill, error) {
	if strings.TrimSpace(toolName) == "" {
		return nil, fmt.Errorf("toolName is required")
	}
	skill := buildSkillFromMCP(toolName, toolDesc, inputSchema)
	return &skill, nil
}

// GenerateCardFromScript produces a draft UICardDefinition from a ScriptConfigProfile.
func (g *GeneratorService) GenerateCardFromScript(config model.ScriptConfigProfile) (*model.UICardDefinition, error) {
	if strings.TrimSpace(config.ScriptName) == "" {
		return nil, fmt.Errorf("scriptName is required")
	}
	card := buildCardFromScript(config)
	return &card, nil
}

// GenerateBundleFromCoroot produces a draft UICardDefinition (bundle kind)
// from Coroot service type and query schema.
func (g *GeneratorService) GenerateBundleFromCoroot(serviceType string, querySchema map[string]any) (*model.UICardDefinition, error) {
	if strings.TrimSpace(serviceType) == "" {
		return nil, fmt.Errorf("serviceType is required")
	}
	card := buildBundleFromCoroot(serviceType, querySchema)
	return &card, nil
}

// GenerateSkillsFromCoroot batch-generates draft AgentSkills from Coroot tool
// metadata. Each generated skill is lint-validated; skills with error-level
// issues are filtered out of the result.
func (g *GeneratorService) GenerateSkillsFromCoroot(tools []CorootToolMeta) ([]model.AgentSkill, error) {
	if len(tools) == 0 {
		return nil, fmt.Errorf("tools list is empty")
	}
	drafts := buildSkillsFromCorootTools(tools)
	var valid []model.AgentSkill
	for i := range drafts {
		if !hasErrorIssues(g.Lint(&drafts[i])) {
			valid = append(valid, drafts[i])
		}
	}
	return valid, nil
}

// GenerateCardFromCoroot produces a draft UICardDefinition from a Coroot
// service type and query schema. The result is lint-validated; if the card
// has error-level issues it is rejected.
func (g *GeneratorService) GenerateCardFromCoroot(serviceType string, querySchema map[string]any) (*model.UICardDefinition, error) {
	if strings.TrimSpace(serviceType) == "" {
		return nil, fmt.Errorf("serviceType is required")
	}
	card := buildBundleFromCoroot(serviceType, querySchema)
	issues := g.Lint(&card)
	if hasErrorIssues(issues) {
		return nil, fmt.Errorf("generated card has lint errors: %s", formatLintErrors(issues))
	}
	return &card, nil
}

// hasErrorIssues returns true if any issue has error level.
func hasErrorIssues(issues []LintIssue) bool {
	for _, issue := range issues {
		if issue.Level == "error" {
			return true
		}
	}
	return false
}

// formatLintErrors returns a summary string of error-level lint issues.
func formatLintErrors(issues []LintIssue) string {
	var msgs []string
	for _, issue := range issues {
		if issue.Level == "error" {
			msgs = append(msgs, issue.Field+": "+issue.Message)
		}
	}
	return strings.Join(msgs, "; ")
}

// Lint validates a draft object and returns any issues found.
// It accepts *model.AgentSkill or *model.UICardDefinition.
func (g *GeneratorService) Lint(draft any) []LintIssue {
	switch v := draft.(type) {
	case *model.AgentSkill:
		return lintSkill(v)
	case *model.UICardDefinition:
		return lintCard(v)
	default:
		return []LintIssue{{Field: "_type", Level: "error", Message: "unsupported draft type"}}
	}
}

func lintSkill(s *model.AgentSkill) []LintIssue {
	var issues []LintIssue
	if strings.TrimSpace(s.ID) == "" {
		issues = append(issues, LintIssue{Field: "id", Level: "error", Message: "id is required"})
	}
	if strings.TrimSpace(s.Name) == "" {
		issues = append(issues, LintIssue{Field: "name", Level: "error", Message: "name is required"})
	}
	if strings.TrimSpace(s.Description) == "" {
		issues = append(issues, LintIssue{Field: "description", Level: "warning", Message: "description is recommended"})
	}
	if s.Status != "draft" && s.Status != "active" && s.Status != "disabled" && s.Status != "" {
		issues = append(issues, LintIssue{Field: "status", Level: "error", Message: "invalid status value"})
	}
	return issues
}

func lintCard(c *model.UICardDefinition) []LintIssue {
	var issues []LintIssue
	if strings.TrimSpace(c.ID) == "" {
		issues = append(issues, LintIssue{Field: "id", Level: "error", Message: "id is required"})
	}
	if strings.TrimSpace(c.Name) == "" {
		issues = append(issues, LintIssue{Field: "name", Level: "error", Message: "name is required"})
	}
	if strings.TrimSpace(c.Kind) == "" {
		issues = append(issues, LintIssue{Field: "kind", Level: "error", Message: "kind is required"})
	}
	if strings.TrimSpace(c.Renderer) == "" {
		issues = append(issues, LintIssue{Field: "renderer", Level: "warning", Message: "renderer is recommended"})
	}
	validKinds := map[string]bool{
		"readonly_summary": true, "readonly_chart": true,
		"action_panel": true, "form_panel": true,
		"monitor_bundle": true, "remediation_bundle": true,
	}
	if c.Kind != "" && !validKinds[c.Kind] {
		issues = append(issues, LintIssue{Field: "kind", Level: "warning", Message: "non-standard kind value"})
	}
	if c.Status != "draft" && c.Status != "active" && c.Status != "disabled" && c.Status != "" {
		issues = append(issues, LintIssue{Field: "status", Level: "error", Message: "invalid status value"})
	}
	return issues
}
