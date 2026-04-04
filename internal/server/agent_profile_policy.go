package server

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type commandPolicyDecision struct {
	Category string
	Mode     string
	Readonly bool
}

func capabilityStateFromProfile(profile model.AgentProfile, capability string) string {
	switch capability {
	case "commandExecution":
		return profile.CapabilityPermissions.CommandExecution
	case "fileRead":
		return profile.CapabilityPermissions.FileRead
	case "fileSearch":
		return profile.CapabilityPermissions.FileSearch
	case "fileChange":
		return profile.CapabilityPermissions.FileChange
	case "terminal":
		return profile.CapabilityPermissions.Terminal
	case "webSearch":
		return profile.CapabilityPermissions.WebSearch
	case "webOpen":
		return profile.CapabilityPermissions.WebOpen
	case "approval":
		return profile.CapabilityPermissions.Approval
	case "multiAgent":
		return profile.CapabilityPermissions.MultiAgent
	case "plan":
		return profile.CapabilityPermissions.Plan
	case "summary":
		return profile.CapabilityPermissions.Summary
	default:
		return model.AgentCapabilityEnabled
	}
}

func (a *App) mainAgentCapabilityState(capability string) string {
	return capabilityStateFromProfile(a.mainAgentProfile(), capability)
}

func (a *App) hostAgentDefaultProfile() model.AgentProfile {
	profile, ok := a.store.AgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	if !ok {
		return model.DefaultAgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	}
	return model.CompleteAgentProfile(profile)
}

func normalizeCapabilityState(state string) string {
	switch strings.TrimSpace(state) {
	case model.AgentCapabilityDisabled:
		return model.AgentCapabilityDisabled
	case model.AgentCapabilityApprovalRequired:
		return model.AgentCapabilityApprovalRequired
	default:
		return model.AgentCapabilityEnabled
	}
}

func mergeCapabilityStates(states ...string) string {
	merged := model.AgentCapabilityEnabled
	for _, state := range states {
		switch normalizeCapabilityState(state) {
		case model.AgentCapabilityDisabled:
			return model.AgentCapabilityDisabled
		case model.AgentCapabilityApprovalRequired:
			merged = model.AgentCapabilityApprovalRequired
		}
	}
	return merged
}

func capabilityDisabled(state string) bool {
	return strings.TrimSpace(state) == model.AgentCapabilityDisabled
}

func capabilityNeedsApproval(state string) bool {
	return strings.TrimSpace(state) == model.AgentCapabilityApprovalRequired
}

func (a *App) effectiveCapabilityState(hostID, capability string) string {
	mainState := a.mainAgentCapabilityState(capability)
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if !isRemoteHostID(hostID) {
		return mainState
	}
	hostProfile := a.hostAgentDefaultProfile()
	hostState := capabilityStateFromProfile(hostProfile, capability)
	return mergeCapabilityStates(mainState, hostState)
}

func (a *App) ensureCapabilityAllowed(capability string) error {
	return a.ensureCapabilityAllowedForHost(model.ServerLocalHostID, capability)
}

func (a *App) ensureCapabilityAllowedForHost(hostID, capability string) error {
	state := a.effectiveCapabilityState(hostID, capability)
	if capabilityDisabled(state) {
		if isRemoteHostID(defaultHostID(strings.TrimSpace(hostID))) {
			return fmt.Errorf("%s capability is disabled by the current effective agent profile (main-agent + host-agent-default)", capability)
		}
		return fmt.Errorf("%s capability is disabled by the current main-agent profile", capability)
	}
	return nil
}

func (a *App) evaluateCommandPolicy(command string) (commandPolicyDecision, error) {
	return a.evaluateCommandPolicyForHost(model.ServerLocalHostID, command)
}

func normalizePermissionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case model.AgentPermissionModeDeny:
		return model.AgentPermissionModeDeny
	case model.AgentPermissionModeReadonlyOnly:
		return model.AgentPermissionModeReadonlyOnly
	case model.AgentPermissionModeApprovalRequired:
		return model.AgentPermissionModeApprovalRequired
	default:
		return model.AgentPermissionModeAllow
	}
}

func mergePermissionModes(modes ...string) string {
	priority := map[string]int{
		model.AgentPermissionModeAllow:            0,
		model.AgentPermissionModeApprovalRequired: 1,
		model.AgentPermissionModeReadonlyOnly:     2,
		model.AgentPermissionModeDeny:             3,
	}
	merged := model.AgentPermissionModeAllow
	for _, mode := range modes {
		normalized := normalizePermissionMode(mode)
		if priority[normalized] > priority[merged] {
			merged = normalized
		}
	}
	return merged
}

func profileCommandMode(profile model.AgentProfile, category string) string {
	mode := normalizePermissionMode(profile.CommandPermissions.DefaultMode)
	if profile.CommandPermissions.CategoryPolicies != nil {
		if override, ok := profile.CommandPermissions.CategoryPolicies[category]; ok && strings.TrimSpace(override) != "" {
			mode = normalizePermissionMode(override)
		}
	}
	return mode
}

func (a *App) evaluateCommandPolicyForHost(hostID, command string) (commandPolicyDecision, error) {
	profile := a.mainAgentProfile()
	decision := commandPolicyDecision{
		Category: classifyCommandCategory(command),
		Mode:     profileCommandMode(profile, classifyCommandCategory(command)),
		Readonly: isReadonlyCommand(command),
	}
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if isRemoteHostID(hostID) {
		hostProfile := a.hostAgentDefaultProfile()
		decision.Mode = mergePermissionModes(decision.Mode, profileCommandMode(hostProfile, decision.Category))
	}
	enabled := boolValue(profile.CommandPermissions.Enabled, true)
	if isRemoteHostID(hostID) {
		enabled = enabled && boolValue(a.hostAgentDefaultProfile().CommandPermissions.Enabled, true)
	}
	if !enabled {
		if isRemoteHostID(hostID) {
			return decision, fmt.Errorf("command execution is disabled by the current effective agent profile (main-agent + host-agent-default)")
		}
		return decision, fmt.Errorf("command execution is disabled by the current main-agent profile")
	}
	allowShellWrapper := boolValue(profile.CommandPermissions.AllowShellWrapper, true)
	if isRemoteHostID(hostID) {
		allowShellWrapper = allowShellWrapper && boolValue(a.hostAgentDefaultProfile().CommandPermissions.AllowShellWrapper, true)
	}
	if !allowShellWrapper && commandUsesShellWrapper(command) {
		if isRemoteHostID(hostID) {
			return decision, fmt.Errorf("shell wrapper commands are disabled by the current effective agent profile (main-agent + host-agent-default)")
		}
		return decision, fmt.Errorf("shell wrapper commands are disabled by the current main-agent profile")
	}
	allowSudo := boolValue(profile.CommandPermissions.AllowSudo, false)
	if isRemoteHostID(hostID) {
		allowSudo = allowSudo && boolValue(a.hostAgentDefaultProfile().CommandPermissions.AllowSudo, false)
	}
	if !allowSudo && commandUsesSudo(command) {
		if isRemoteHostID(hostID) {
			return decision, fmt.Errorf("sudo commands are disabled by the current effective agent profile (main-agent + host-agent-default)")
		}
		return decision, fmt.Errorf("sudo commands are disabled by the current main-agent profile")
	}
	switch decision.Mode {
	case model.AgentPermissionModeDeny:
		if isRemoteHostID(hostID) {
			return decision, fmt.Errorf("%s commands are denied by the current effective agent profile (main-agent + host-agent-default)", decision.Category)
		}
		return decision, fmt.Errorf("%s commands are denied by the current main-agent profile", decision.Category)
	case model.AgentPermissionModeReadonlyOnly:
		if !decision.Readonly {
			if isRemoteHostID(hostID) {
				return decision, fmt.Errorf("%s commands are restricted to readonly_only by the current effective agent profile (main-agent + host-agent-default)", decision.Category)
			}
			return decision, fmt.Errorf("%s commands are restricted to readonly_only by the current main-agent profile", decision.Category)
		}
	}
	return decision, nil
}

func (a *App) ensureWritableRoots(paths []string) error {
	return a.ensureWritableRootsForHost(model.ServerLocalHostID, paths)
}

func (a *App) ensureWritableRootsForHost(hostID string, paths []string) error {
	profile := a.mainAgentProfile()
	roots := profile.CommandPermissions.AllowedWritableRoots
	if err := ensurePathsWithinRoots(paths, roots, "the current main-agent profile"); err != nil {
		return err
	}
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if isRemoteHostID(hostID) {
		hostRoots := a.hostAgentDefaultProfile().CommandPermissions.AllowedWritableRoots
		if err := ensurePathsWithinRoots(paths, hostRoots, "the current host-agent-default profile"); err != nil {
			return err
		}
	}
	return nil
}

func ensurePathsWithinRoots(paths, roots []string, profileLabel string) error {
	if len(roots) == 0 {
		return nil
	}
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if !pathWithinRoots(trimmed, roots) {
			return fmt.Errorf("%s is outside the allowed writable roots for %s", trimmed, profileLabel)
		}
	}
	return nil
}

func minPositive(items ...int) int {
	result := 0
	for _, item := range items {
		if item <= 0 {
			continue
		}
		if result == 0 || item < result {
			result = item
		}
	}
	return result
}

func (a *App) effectiveCommandTimeoutSeconds(hostID string) int {
	mainTimeout := a.mainAgentProfile().CommandPermissions.DefaultTimeoutSeconds
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if !isRemoteHostID(hostID) {
		return mainTimeout
	}
	hostTimeout := a.hostAgentDefaultProfile().CommandPermissions.DefaultTimeoutSeconds
	return minPositive(mainTimeout, hostTimeout)
}

func classifyCommandCategory(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "system_inspection"
	}
	program := strings.ToLower(filepath.Base(fields[0]))
	switch program {
	case "apt", "apt-get", "yum", "dnf", "apk", "brew", "pip", "pip3", "npm", "pnpm", "yarn":
		return "package_mutation"
	case "rm", "mv", "cp", "chmod", "chown", "mkdir", "touch", "install", "ln", "tee", "truncate", "dd":
		return "filesystem_mutation"
	case "cat", "grep", "rg", "sed", "head", "tail", "wc", "cut", "sort", "uniq", "find", "ls":
		if program == "sed" && !isReadonlyCommand(command) {
			return "filesystem_mutation"
		}
		if program == "find" && !isReadonlyCommand(command) {
			return "filesystem_mutation"
		}
		return "file_read"
	case "systemctl", "service":
		if isReadonlyCommand(command) {
			return "service_read"
		}
		return "service_mutation"
	case "ss", "netstat", "curl", "wget", "dig", "nslookup", "ping", "ip":
		return "network_read"
	case "docker", "kubectl", "git":
		if isReadonlyCommand(command) {
			switch program {
			case "git":
				return "file_read"
			default:
				return "system_inspection"
			}
		}
		return "service_mutation"
	default:
		if isReadonlyCommand(command) {
			return "system_inspection"
		}
		return "filesystem_mutation"
	}
}

func isReadonlyCommand(command string) bool {
	return validateReadonlyCommand(command) == nil
}

func commandUsesShellWrapper(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	prefixes := []string{
		"/bin/sh -c", "sh -c", "/bin/bash -c", "/bin/bash -lc", "bash -c", "bash -lc",
		"/bin/zsh -c", "/bin/zsh -lc", "zsh -c", "zsh -lc", "fish -c",
	}
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		return strings.HasPrefix(lower, prefix)
	})
}

func commandUsesSudo(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	if strings.EqualFold(filepath.Base(fields[0]), "sudo") {
		return true
	}
	lower := " " + strings.ToLower(command) + " "
	return strings.Contains(lower, " sudo ")
}

func pathWithinRoots(path string, roots []string) bool {
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		cleanRoot := filepath.Clean(strings.TrimSpace(root))
		if cleanRoot == "." || cleanRoot == "" {
			continue
		}
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func changePaths(changes []model.FileChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if trimmed := strings.TrimSpace(change.Path); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func (a *App) rejectApprovalByProfile(sessionID, rawID string, approval model.ApprovalRequest, title, message string) {
	now := model.NowString()
	approval.Status = "blocked_by_profile"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.auditApprovalLifecycleEvent("approval.decision", sessionID, approval, "decline", approval.Status, approval.RequestedAt, now, map[string]any{
		"blockedByProfile": true,
		"reason":           message,
	})
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("error"),
		Type:      "ErrorCard",
		Title:     title,
		Message:   message,
		Text:      message,
		Status:    "failed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)
	_ = a.respondCodex(context.Background(), rawID, map[string]any{
		"decision": "decline",
	})
}

func (a *App) autoApproveLocalApprovalByProfile(sessionID string, approval model.ApprovalRequest) bool {
	now := model.NowString()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.respondCodex(ctx, approval.RequestIDRaw, map[string]any{
		"decision": "accept",
	}); err != nil {
		return false
	}
	approval.Status = "accepted_by_profile_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved by profile",
		Text:      "当前 main-agent profile 允许该操作直接执行，因此已自动放行。",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.auditApprovalLifecycleEvent("approval.decision", sessionID, approval, "accept", approval.Status, approval.RequestedAt, now, map[string]any{
		"autoApprovedByProfile": true,
	})
	a.broadcastSnapshot(sessionID)
	return true
}

// capabilityGatewayResult holds the result of the three-layer capability gateway evaluation.
type capabilityGatewayResult struct {
	// Layer is one of: structured_read, controlled_mutation, raw_shell.
	Layer string
	// Allowed indicates whether the tool is permitted under the current profile.
	Allowed bool
	// Reason provides a human-readable explanation when Allowed is false.
	Reason string
}

// evaluateCapabilityGateway implements the three-tier capability gateway:
//
//  1. structured_read  – host.* tools that map to predefined safe commands.
//  2. controlled_mutation – execute_system_mutation (always requires approval).
//  3. raw_shell – execute_readonly_query and other raw shell tools.
//
// It returns which layer the tool belongs to and whether the current effective
// profile allows the tool to proceed.
func (a *App) evaluateCapabilityGateway(hostID, toolName string) capabilityGatewayResult {
	hostID = defaultHostID(strings.TrimSpace(hostID))

	// Layer 1: structured_read – host.* tools.
	if isStructuredReadTool(toolName) {
		state := a.effectiveCapabilityState(hostID, "commandExecution")
		if capabilityDisabled(state) {
			return capabilityGatewayResult{
				Layer:   CapabilityLayerStructuredRead,
				Allowed: false,
				Reason:  "commandExecution capability is disabled by the current effective agent profile",
			}
		}
		return capabilityGatewayResult{
			Layer:   CapabilityLayerStructuredRead,
			Allowed: true,
		}
	}

	// Layer 2: controlled_mutation – execute_system_mutation and controlled mutation tools.
	if toolName == "execute_system_mutation" || isControlledMutationTool(toolName) {
		commandState := a.effectiveCapabilityState(hostID, "commandExecution")
		fileChangeState := a.effectiveCapabilityState(hostID, "fileChange")
		if capabilityDisabled(commandState) && capabilityDisabled(fileChangeState) {
			return capabilityGatewayResult{
				Layer:   CapabilityLayerControlledMutation,
				Allowed: false,
				Reason:  "both commandExecution and fileChange capabilities are disabled by the current effective agent profile",
			}
		}
		return capabilityGatewayResult{
			Layer:   CapabilityLayerControlledMutation,
			Allowed: true,
		}
	}

	// Layer 3: raw_shell – execute_readonly_query and other raw tools.
	switch toolName {
	case "execute_readonly_query":
		state := a.effectiveCapabilityState(hostID, "commandExecution")
		if capabilityDisabled(state) {
			return capabilityGatewayResult{
				Layer:   CapabilityLayerRawShell,
				Allowed: false,
				Reason:  "commandExecution capability is disabled by the current effective agent profile",
			}
		}
		return capabilityGatewayResult{
			Layer:   CapabilityLayerRawShell,
			Allowed: true,
		}
	case "list_remote_files":
		fileReadState := a.effectiveCapabilityState(hostID, "fileRead")
		fileSearchState := a.effectiveCapabilityState(hostID, "fileSearch")
		if capabilityDisabled(fileReadState) && capabilityDisabled(fileSearchState) {
			return capabilityGatewayResult{
				Layer:   CapabilityLayerRawShell,
				Allowed: false,
				Reason:  "both fileRead and fileSearch capabilities are disabled",
			}
		}
		return capabilityGatewayResult{
			Layer:   CapabilityLayerRawShell,
			Allowed: true,
		}
	case "read_remote_file":
		state := a.effectiveCapabilityState(hostID, "fileRead")
		if capabilityDisabled(state) {
			return capabilityGatewayResult{
				Layer:   CapabilityLayerRawShell,
				Allowed: false,
				Reason:  "fileRead capability is disabled by the current effective agent profile",
			}
		}
		return capabilityGatewayResult{
			Layer:   CapabilityLayerRawShell,
			Allowed: true,
		}
	case "search_remote_files":
		state := a.effectiveCapabilityState(hostID, "fileSearch")
		if capabilityDisabled(state) {
			return capabilityGatewayResult{
				Layer:   CapabilityLayerRawShell,
				Allowed: false,
				Reason:  "fileSearch capability is disabled by the current effective agent profile",
			}
		}
		return capabilityGatewayResult{
			Layer:   CapabilityLayerRawShell,
			Allowed: true,
		}
	default:
		return capabilityGatewayResult{
			Layer:   CapabilityLayerRawShell,
			Allowed: false,
			Reason:  fmt.Sprintf("unknown tool %q", toolName),
		}
	}
}
