package server

import (
	"fmt"
	"strings"
)

// attachmentSection formats a single attachment section with a header.
func attachmentSection(header, content string) string {
	if content == "" {
		return ""
	}
	return fmt.Sprintf("[%s]\n%s", header, content)
}

// workspaceStateAttachment builds the workspace_state attachment text.
func (a *App) workspaceStateAttachment(sessionID string) string {
	session := a.store.EnsureSession(sessionID)
	if session == nil {
		return ""
	}

	rt := session.Runtime
	hostID := session.SelectedHostID
	if hostID == "" {
		hostID = "(none)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "session_id: %s\n", sessionID)
	fmt.Fprintf(&b, "selected_host: %s\n", hostID)
	fmt.Fprintf(&b, "runtime_phase: %s\n", rt.Turn.Phase)
	fmt.Fprintf(&b, "turn_active: %v\n", rt.Turn.Active)
	fmt.Fprintf(&b, "mode: %s", session.Meta.Kind)

	return attachmentSection("workspace_state", b.String())
}

// approvalStateAttachment builds the approval_state attachment text.
func (a *App) approvalStateAttachment(sessionID string) string {
	session := a.store.EnsureSession(sessionID)
	if session == nil {
		return ""
	}

	if len(session.Approvals) == 0 {
		return ""
	}

	pending := 0
	blocking := false
	types := map[string]struct{}{}

	for _, ap := range session.Approvals {
		if ap.Status == "pending" {
			pending++
			types[ap.Type] = struct{}{}
			// Command and file-change approvals block execution.
			if ap.Type == "command" || ap.Type == "file_change" {
				blocking = true
			}
		}
	}

	if pending == 0 {
		return ""
	}

	typeList := make([]string, 0, len(types))
	for t := range types {
		typeList = append(typeList, t)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "pending_count: %d\n", pending)
	fmt.Fprintf(&b, "types: %s\n", strings.Join(typeList, ", "))
	fmt.Fprintf(&b, "blocking: %v", blocking)

	return attachmentSection("approval_state", b.String())
}

// eventSummaryAttachment builds the event_summary attachment text.
// Returns the last 5 event summaries from cards, with evidence IDs.
func (a *App) eventSummaryAttachment(sessionID string) string {
	session := a.store.EnsureSession(sessionID)
	if session == nil {
		return ""
	}

	cards := session.Cards
	if len(cards) == 0 {
		return ""
	}

	// Collect the last 5 cards that carry an event-like summary.
	const maxEvents = 5
	var events []string

	start := len(cards) - maxEvents
	if start < 0 {
		start = 0
	}

	for i := start; i < len(cards); i++ {
		c := cards[i]
		summary := c.Summary
		if summary == "" {
			summary = c.Title
		}
		if summary == "" {
			continue
		}

		evidenceID := ""
		if c.Detail != nil {
			if eid, ok := c.Detail["evidenceId"].(string); ok {
				evidenceID = eid
			}
		}

		entry := fmt.Sprintf("- %s", summary)
		if evidenceID != "" {
			entry += fmt.Sprintf(" [evidence:%s]", evidenceID)
		}
		events = append(events, entry)
	}

	if len(events) == 0 {
		return ""
	}

	return attachmentSection("event_summary", strings.Join(events, "\n"))
}

// evidenceSummaryAttachment builds the recent evidence summary attachment text.
func (a *App) evidenceSummaryAttachment(sessionID string) string {
	snapshot := a.snapshot(sessionID)
	if len(snapshot.EvidenceSummaries) == 0 {
		return ""
	}

	const maxEvidence = 5
	start := len(snapshot.EvidenceSummaries) - maxEvidence
	if start < 0 {
		start = 0
	}

	lines := make([]string, 0, maxEvidence)
	for i := start; i < len(snapshot.EvidenceSummaries); i++ {
		record := snapshot.EvidenceSummaries[i]
		summary := firstNonEmptyValue(strings.TrimSpace(record.Summary), strings.TrimSpace(record.Title))
		if summary == "" {
			continue
		}
		citation := firstNonEmptyValue(strings.TrimSpace(record.CitationKey), strings.TrimSpace(record.ID))
		title := strings.TrimSpace(record.Title)
		if title != "" && title != summary {
			lines = append(lines, fmt.Sprintf("- [%s] %s: %s", citation, title, summary))
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", citation, summary))
	}
	if len(lines) == 0 {
		return ""
	}
	return attachmentSection("evidence_summary", strings.Join(lines, "\n"))
}

// planModeAttachment builds the plan_mode attachment text.
func (a *App) planModeAttachment(sessionID string, planMode bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "active: %v\n", planMode)
	if planMode {
		b.WriteString("constraints:\n")
		b.WriteString("  - only readonly tools allowed\n")
		b.WriteString("  - UpdatePlan allowed\n")
		b.WriteString("  - ask_user_question allowed\n")
		b.WriteString("  - ExitPlanMode allowed")
	}

	return attachmentSection("plan_mode", b.String())
}

// permissionModeAttachment builds the permission_mode attachment text.
func (a *App) permissionModeAttachment(sessionID, permissionMode string) string {
	if permissionMode == "" {
		return ""
	}

	mutationAllowed := permissionMode == "allow" || permissionMode == "approval_required"
	workerDispatchAllowed := permissionMode == "allow"

	var b strings.Builder
	fmt.Fprintf(&b, "level: %s\n", permissionMode)
	fmt.Fprintf(&b, "mutation_allowed: %v\n", mutationAllowed)
	fmt.Fprintf(&b, "worker_dispatch_allowed: %v", workerDispatchAllowed)

	return attachmentSection("permission_mode", b.String())
}

// hostContextAttachment builds the host_context attachment text.
func (a *App) hostContextAttachment(hostID string) string {
	if hostID == "" {
		return ""
	}

	host, ok := a.store.Host(hostID)
	if !ok {
		return ""
	}

	online := host.Status == "online"

	var capabilities []string
	if host.Executable {
		capabilities = append(capabilities, "exec")
	}
	if host.TerminalCapable {
		capabilities = append(capabilities, "terminal")
	}
	if len(host.EnabledSkills) > 0 {
		capabilities = append(capabilities, host.EnabledSkills...)
	}

	cwd := "/"
	if host.Kind == "local" {
		cwd = "."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "host_id: %s\n", host.ID)
	fmt.Fprintf(&b, "online: %v\n", online)
	if len(capabilities) > 0 {
		fmt.Fprintf(&b, "capabilities: %s\n", strings.Join(capabilities, ", "))
	}
	fmt.Fprintf(&b, "default_cwd: %s", cwd)

	return attachmentSection("host_context", b.String())
}

// toolSchemaDeltaAttachment builds the tool_schema_delta attachment text.
func (a *App) toolSchemaDeltaAttachment(tools []string) string {
	if len(tools) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("available_tools:\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "  - %s\n", t)
	}

	return attachmentSection("tool_schema_delta", strings.TrimRight(b.String(), "\n"))
}

// memoryPrefetchAttachment builds the memory_prefetch attachment text.
// Returns experience pack hits, long-term memory, or project context summaries.
func (a *App) memoryPrefetchAttachment(sessionID string) string {
	session := a.store.EnsureSession(sessionID)
	if session == nil {
		return ""
	}

	// Check for experience pack context in session items
	var memories []string

	// Look for recent experience pack hits in cards
	for i := len(session.Cards) - 1; i >= 0 && len(memories) < 3; i-- {
		card := session.Cards[i]
		if card.Detail == nil {
			continue
		}
		if kind, _ := card.Detail["kind"].(string); kind == "experience_pack" || kind == "memory" {
			summary := card.Summary
			if summary == "" {
				summary = card.Title
			}
			if summary != "" {
				memories = append(memories, fmt.Sprintf("- %s", summary))
			}
		}
	}

	if len(memories) == 0 {
		return ""
	}

	return attachmentSection("memory_prefetch", strings.Join(memories, "\n"))
}

// mcpInstructionsDeltaAttachment builds the mcp_instructions_delta attachment text.
// Returns MCP tool descriptions and connection status changes.
func (a *App) mcpInstructionsDeltaAttachment(sessionID string) string {
	session := a.store.EnsureSession(sessionID)
	if session == nil {
		return ""
	}

	// Check for MCP-related configuration in the agent profile
	profile := a.mainAgentProfile()
	var mcpItems []string

	for _, mcp := range profile.MCPs {
		if !mcp.Enabled {
			continue
		}
		status := "connected"
		entry := fmt.Sprintf("- %s: %s", mcp.Name, status)
		mcpItems = append(mcpItems, entry)
	}

	if len(mcpItems) == 0 {
		return ""
	}

	return attachmentSection("mcp_instructions_delta", strings.Join(mcpItems, "\n"))
}

// buildAllAttachments builds all dynamic attachments for a ReAct loop iteration.
func (a *App) buildAllAttachments(sessionID, hostID, permissionMode string, planMode bool, tools []string) []string {
	candidates := []string{
		a.workspaceStateAttachment(sessionID),
		a.approvalStateAttachment(sessionID),
		a.eventSummaryAttachment(sessionID),
		a.evidenceSummaryAttachment(sessionID),
		a.planModeAttachment(sessionID, planMode),
		a.permissionModeAttachment(sessionID, permissionMode),
		a.hostContextAttachment(hostID),
		a.toolSchemaDeltaAttachment(tools),
		a.memoryPrefetchAttachment(sessionID),
		a.mcpInstructionsDeltaAttachment(sessionID),
	}

	var result []string
	for _, att := range candidates {
		if att != "" {
			result = append(result, att)
		}
	}
	return result
}
