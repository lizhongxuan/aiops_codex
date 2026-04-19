package server

import (
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// buildEffectivePrompt assembles the full effective prompt from sections.
// It joins all non-empty sections with double newlines, prefixed by section name in brackets.
func buildEffectivePrompt(sections []PromptSection) string {
	var parts []string
	for _, s := range sections {
		if s.Content == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]\n%s", s.Name, s.Content))
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) dynamicPromptSections(sessionID, hostID string, policy model.TurnPolicy) []PromptSection {
	if a == nil {
		return nil
	}
	profile := a.mainAgentProfile()
	sections := []PromptSection{
		{
			Name:      "HostContext",
			Content:   strings.TrimSpace(a.renderMainAgentDeveloperInstructions(profile, hostID, true)),
			Static:    false,
			CacheHint: "dynamic:host",
		},
		{
			Name: "RuntimePolicy",
			Content: strings.Join([]string{
				fmt.Sprintf("intentClass=%s", firstNonEmptyValue(strings.TrimSpace(policy.IntentClass), "factual")),
				fmt.Sprintf("lane=%s", firstNonEmptyValue(strings.TrimSpace(policy.Lane), "answer")),
				fmt.Sprintf("requiredTools=%s", firstNonEmptyValue(strings.Join(policy.RequiredTools, ", "), "-")),
				fmt.Sprintf("requiredEvidenceKinds=%s", firstNonEmptyValue(strings.Join(policy.RequiredEvidenceKinds, ", "), "-")),
				fmt.Sprintf("requiredCitationKinds=%s", firstNonEmptyValue(strings.Join(policy.RequiredCitationKinds, ", "), "-")),
				fmt.Sprintf("needsPlanArtifact=%t", policy.NeedsPlanArtifact),
				fmt.Sprintf("needsApproval=%t", policy.NeedsApproval),
				fmt.Sprintf("needsAssumptions=%t", policy.NeedsAssumptions),
				fmt.Sprintf("needsDisambiguation=%t", policy.NeedsDisambiguation),
				fmt.Sprintf("knowledgeFreshness=%s", firstNonEmptyValue(strings.TrimSpace(policy.KnowledgeFreshness), "stable")),
				fmt.Sprintf("evidenceContract=%s", firstNonEmptyValue(strings.TrimSpace(policy.EvidenceContract), "none")),
				fmt.Sprintf("answerContract=%s", firstNonEmptyValue(strings.TrimSpace(policy.AnswerContract), "normal")),
				fmt.Sprintf("minimumIndependentSources=%d", policy.MinimumIndependentSources),
				fmt.Sprintf("requireSourceAttribution=%t", policy.RequireSourceAttribution),
				fmt.Sprintf("allowEarlyStop=%t", policy.AllowEarlyStop),
				fmt.Sprintf("finalGateStatus=%s", firstNonEmptyValue(strings.TrimSpace(policy.FinalGateStatus), turnFinalGatePending)),
			}, "\n"),
			Static:    false,
			CacheHint: "dynamic:policy",
		},
	}
	if sessionID != "" {
		sections = append(sections, PromptSection{
			Name:      "LoopRuntime",
			Content:   strings.TrimSpace(a.buildReActLoopInstructions(reActLoopKindWorkspace, sessionID, hostID, true)),
			Static:    false,
			CacheHint: "dynamic:loop",
		})
	}
	return sections
}
