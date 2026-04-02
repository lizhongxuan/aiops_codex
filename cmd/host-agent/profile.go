package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const (
	hostAgentProfileUpdateKind = "profile/update"
	hostAgentProfileAckKind    = "profile/ack"
)

type commandPolicyDecision struct {
	Category string
	Mode     string
	Readonly bool
}

type hostAgentRuntime struct {
	profile *hostAgentProfileStore
}

type hostAgentProfileStore struct {
	mu          sync.RWMutex
	path        string
	profile     model.AgentProfile
	revision    string
	loadedFrom  string
	unsupported []string
}

type hostAgentProfileAckMessage struct {
	ProfileID     string
	Revision      string
	Status        string
	Summary       string
	EnabledSkills []string
	EnabledMCPs   []string
	Unsupported   []string
}

func newHostAgentRuntime() (*hostAgentRuntime, error) {
	store, err := newHostAgentProfileStore()
	if err != nil {
		return nil, err
	}
	return &hostAgentRuntime{profile: store}, nil
}

func newHostAgentProfileStore() (*hostAgentProfileStore, error) {
	path := env("AIOPS_AGENT_PROFILE_PATH", defaultHostAgentProfilePath())
	store := &hostAgentProfileStore{
		path:        path,
		unsupported: []string{"systemPrompt", "runtime"},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func defaultHostAgentProfilePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "aiops-codex-host-agent-profile.json")
	}
	return filepath.Join(home, ".aiops_codex", "host-agent-profile.json")
}

func (s *hostAgentProfileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile := model.DefaultAgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	s.loadedFrom = "default"

	if raw := strings.TrimSpace(os.Getenv("AIOPS_AGENT_PROFILE_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			return fmt.Errorf("decode AIOPS_AGENT_PROFILE_JSON: %w", err)
		}
		s.loadedFrom = "env"
	} else if content, err := os.ReadFile(s.path); err == nil {
		var wrapper hostAgentProfileFile
		if err := json.Unmarshal(content, &wrapper); err == nil && wrapper.Profile.ID != "" {
			profile = wrapper.Profile
			if strings.TrimSpace(wrapper.Revision) != "" {
				s.revision = strings.TrimSpace(wrapper.Revision)
			}
			s.loadedFrom = "file"
		} else if err := json.Unmarshal(content, &profile); err == nil {
			s.loadedFrom = "file"
		} else {
			profile = model.DefaultAgentProfile(string(model.AgentProfileTypeHostAgentDefault))
			s.loadedFrom = "corrupt-file-fallback"
		}
	}

	profile = normalizeHostAgentProfile(profile)
	s.profile = profile
	if s.revision == "" {
		s.revision = profileRevision(profile)
	}
	return s.persistLocked()
}

type hostAgentProfileFile struct {
	Revision  string             `json:"revision,omitempty"`
	UpdatedAt string             `json:"updatedAt,omitempty"`
	Profile   model.AgentProfile `json:"profile"`
}

func normalizeHostAgentProfile(profile model.AgentProfile) model.AgentProfile {
	profile = model.CompleteAgentProfile(profile)
	profile.ID = string(model.AgentProfileTypeHostAgentDefault)
	profile.Type = string(model.AgentProfileTypeHostAgentDefault)
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = "Host Agent Default"
	}
	if strings.TrimSpace(profile.Description) == "" {
		profile.Description = "Current profile for the host-agent runtime"
	}
	return profile
}

func (s *hostAgentProfileStore) snapshot() (model.AgentProfile, string, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneHostAgentProfile(s.profile), s.revision, append([]string(nil), s.unsupported...)
}

func (s *hostAgentProfileStore) applyUpdate(msg *agentrpc.Envelope) (hostAgentProfileAckMessage, error) {
	payload, err := decodeHostAgentProfileUpdate(msg)
	if err != nil {
		return hostAgentProfileAckMessage{}, err
	}
	profile := normalizeHostAgentProfile(payload.Profile)
	revision := strings.TrimSpace(payload.ProfileHash)
	if revision == "" {
		revision = profileRevision(profile)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if revision == s.revision && profilesEqual(s.profile, profile) {
		return hostAgentProfileAckMessage{
			ProfileID:     profile.ID,
			Revision:      s.revision,
			Status:        "unchanged",
			Summary:       "profile revision already applied",
			EnabledSkills: hostAgentEnabledSkillNames(profile),
			EnabledMCPs:   hostAgentEnabledMCPNames(profile),
			Unsupported:   append([]string(nil), s.unsupported...),
		}, nil
	}
	s.profile = profile
	s.revision = revision
	s.loadedFrom = "update"
	if err := s.persistLocked(); err != nil {
		return hostAgentProfileAckMessage{}, err
	}
	return hostAgentProfileAckMessage{
		ProfileID:     profile.ID,
		Revision:      s.revision,
		Status:        "applied",
		Summary:       hostAgentProfileSummary(profile, s.unsupported),
		EnabledSkills: hostAgentEnabledSkillNames(profile),
		EnabledMCPs:   hostAgentEnabledMCPNames(profile),
		Unsupported:   append([]string(nil), s.unsupported...),
	}, nil
}

func (s *hostAgentProfileStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload := hostAgentProfileFile{
		Revision:  s.revision,
		UpdatedAt: model.NowString(),
		Profile:   s.profile,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".host-agent-profile-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func (s *hostAgentProfileStore) capabilityState(capability string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hostAgentCapabilityStateFromProfile(s.profile, capability)
}

func (s *hostAgentProfileStore) ensureCapabilityAllowed(capability string) error {
	state := s.capabilityState(capability)
	if hostAgentCapabilityDisabled(state) {
		return fmt.Errorf("%s capability is disabled by the current host-agent profile", capability)
	}
	switch capability {
	case "fileRead", "fileSearch":
		if !s.mcpEnabled("host-files") {
			return fmt.Errorf("%s capability requires the host-files MCP to be enabled", capability)
		}
	case "fileChange":
		if !s.mcpEnabled("host-files") {
			return fmt.Errorf("%s capability requires the host-files MCP to be enabled", capability)
		}
		if s.mcpPermission("host-files") != model.AgentMCPPermissionReadwrite {
			return fmt.Errorf("%s capability requires host-files MCP readwrite access", capability)
		}
		if !s.skillEnabled("host-change-review") {
			return fmt.Errorf("%s capability requires the host-change-review skill to be enabled", capability)
		}
	case "terminal":
		if !s.skillEnabled("host-diagnostics") {
			return fmt.Errorf("%s capability requires the host-diagnostics skill to be enabled", capability)
		}
	}
	return nil
}

func (s *hostAgentProfileStore) effectiveCommandTimeoutSeconds(requested int, readonly bool) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	base := clampAgentExecTimeout(requested, readonly)
	limit := s.profile.CommandPermissions.DefaultTimeoutSeconds
	if limit <= 0 {
		return base
	}
	if base <= 0 {
		return limit
	}
	if base > limit {
		return limit
	}
	return base
}

func (s *hostAgentProfileStore) commandPolicy(command string) (commandPolicyDecision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return evaluateCommandPolicyForProfile(s.profile, command)
}

func (s *hostAgentProfileStore) ensureWritableRoots(paths []string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hostAgentEnsurePathsWithinRoots(paths, s.profile.CommandPermissions.AllowedWritableRoots, "the current host-agent profile")
}

func (s *hostAgentProfileStore) allowShellWrapper() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hostAgentBoolValue(s.profile.CommandPermissions.AllowShellWrapper, true)
}

func (s *hostAgentProfileStore) allowSudo() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hostAgentBoolValue(s.profile.CommandPermissions.AllowSudo, false)
}

func (s *hostAgentProfileStore) summary() string {
	profile, revision, unsupported := s.snapshot()
	return hostAgentProfileSummary(profile, unsupported) + " (rev=" + revision + ")"
}

func (s *hostAgentProfileStore) skillEnabled(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return skillEnabledByID(s.profile, id)
}

func (s *hostAgentProfileStore) mcpEnabled(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, enabled := mcpByID(s.profile, id)
	return enabled
}

func (s *hostAgentProfileStore) mcpPermission(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, enabled := mcpByID(s.profile, id)
	if !enabled {
		return ""
	}
	return model.NormalizeAgentMCPPermission(item.Permission)
}

func profileRevision(profile model.AgentProfile) string {
	data, err := json.Marshal(profile)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func profilesEqual(a, b model.AgentProfile) bool {
	return profileRevision(a) == profileRevision(b)
}

func cloneHostAgentProfile(profile model.AgentProfile) model.AgentProfile {
	out := profile
	out.CommandPermissions.Enabled = cloneBoolPtr(profile.CommandPermissions.Enabled)
	out.CommandPermissions.AllowShellWrapper = cloneBoolPtr(profile.CommandPermissions.AllowShellWrapper)
	out.CommandPermissions.AllowSudo = cloneBoolPtr(profile.CommandPermissions.AllowSudo)
	out.CommandPermissions.AllowedWritableRoots = append([]string(nil), profile.CommandPermissions.AllowedWritableRoots...)
	out.CommandPermissions.CategoryPolicies = cloneStringMap(profile.CommandPermissions.CategoryPolicies)
	out.Skills = append([]model.AgentSkill(nil), profile.Skills...)
	out.MCPs = append([]model.AgentMCP(nil), profile.MCPs...)
	return out
}

func hostAgentProfileSummary(profile model.AgentProfile, unsupported []string) string {
	sections := []string{
		fmt.Sprintf("profile=%s", profile.Name),
		fmt.Sprintf("type=%s", profile.Type),
		fmt.Sprintf("systemPrompt=%s", hostAgentYesNo(strings.TrimSpace(profile.SystemPrompt.Content) != "")),
		fmt.Sprintf("skills=%d[%s]", len(profile.Skills), strings.Join(hostAgentSkillStateLabels(profile), ",")),
		fmt.Sprintf("mcps=%d[%s]", len(profile.MCPs), strings.Join(hostAgentMCPStateLabels(profile), ",")),
		fmt.Sprintf("gates=%s", strings.Join(hostAgentRuntimeGateLabels(profile), ",")),
	}
	if len(unsupported) > 0 {
		sections = append(sections, "unsupported="+strings.Join(unsupported, ","))
	}
	return strings.Join(sections, " ")
}

func hostAgentEnabledSkillNames(profile model.AgentProfile) []string {
	names := make([]string, 0, len(profile.Skills))
	for _, item := range profile.Skills {
		if !skillEnabledByProfile(item) {
			continue
		}
		label := strings.TrimSpace(item.ID)
		if label == "" {
			label = strings.TrimSpace(item.Name)
		}
		if label != "" {
			names = append(names, label)
		}
	}
	sort.Strings(names)
	return names
}

func hostAgentEnabledMCPNames(profile model.AgentProfile) []string {
	names := make([]string, 0, len(profile.MCPs))
	for _, item := range profile.MCPs {
		if !item.Enabled {
			continue
		}
		label := strings.TrimSpace(item.ID)
		if label == "" {
			label = strings.TrimSpace(item.Name)
		}
		if label != "" {
			names = append(names, label)
		}
	}
	sort.Strings(names)
	return names
}

func decodeHostAgentProfileUpdate(msg *agentrpc.Envelope) (*agentrpc.ProfileUpdate, error) {
	if msg == nil {
		return nil, errors.New("profile update requires payload")
	}
	if msg.ProfileUpdate != nil {
		return msg.ProfileUpdate, nil
	}
	var legacyPayload struct {
		Revision string             `json:"revision,omitempty"`
		Profile  model.AgentProfile `json:"profile"`
	}
	if msg.Ack != nil && strings.TrimSpace(msg.Ack.Message) != "" {
		if err := json.Unmarshal([]byte(msg.Ack.Message), &legacyPayload); err == nil {
			return &agentrpc.ProfileUpdate{
				ConfigVersion: model.AgentProfileConfigVersion,
				ProfileHash:   strings.TrimSpace(legacyPayload.Revision),
				Profile:       legacyPayload.Profile,
			}, nil
		}
	}
	if strings.TrimSpace(msg.Error) != "" {
		if err := json.Unmarshal([]byte(msg.Error), &legacyPayload); err == nil {
			return &agentrpc.ProfileUpdate{
				ConfigVersion: model.AgentProfileConfigVersion,
				ProfileHash:   strings.TrimSpace(legacyPayload.Revision),
				Profile:       legacyPayload.Profile,
			}, nil
		}
	}
	return nil, errors.New("profile update payload missing or invalid")
}

func encodeHostAgentProfilePayload(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}

func profileUpdateEnvelope(profile model.AgentProfile, revision string) *agentrpc.Envelope {
	return &agentrpc.Envelope{
		Kind: hostAgentProfileUpdateKind,
		ProfileUpdate: &agentrpc.ProfileUpdate{
			ConfigVersion: model.AgentProfileConfigVersion,
			ProfileHash:   revision,
			Profile:       profile,
		},
	}
}

func profileAckEnvelope(ack hostAgentProfileAckMessage) *agentrpc.Envelope {
	rpcAck := &agentrpc.ProfileAck{
		ConfigVersion: model.AgentProfileConfigVersion,
		ProfileID:     ack.ProfileID,
		ProfileHash:   ack.Revision,
		LoadedAt:      model.NowString(),
		Status:        ack.Status,
		Summary:       ack.Summary,
		EnabledSkills: append([]string(nil), ack.EnabledSkills...),
		EnabledMCPs:   append([]string(nil), ack.EnabledMCPs...),
		Unsupported:   append([]string(nil), ack.Unsupported...),
	}
	return &agentrpc.Envelope{
		Kind:       hostAgentProfileAckKind,
		ProfileAck: rpcAck,
	}
}

func profileAckErrorEnvelope(message string) *agentrpc.Envelope {
	return &agentrpc.Envelope{
		Kind: hostAgentProfileAckKind,
		ProfileAck: &agentrpc.ProfileAck{
			ConfigVersion: model.AgentProfileConfigVersion,
			Status:        "error",
			Error:         strings.TrimSpace(message),
		},
		Error: strings.TrimSpace(message),
	}
}

func evaluateCommandPolicyForProfile(profile model.AgentProfile, command string) (commandPolicyDecision, error) {
	decision := commandPolicyDecision{
		Category: classifyHostAgentCommandCategory(command),
		Mode:     profileCommandMode(profile, classifyHostAgentCommandCategory(command)),
		Readonly: isHostAgentReadonlyCommand(command),
	}
	if hostAgentCommandUsesLogsCapability(command) {
		if !hostAgentMCPEnabled(profile, "host-logs") {
			return decision, fmt.Errorf("%s requires the host-logs MCP to be enabled", strings.TrimSpace(command))
		}
	} else if decision.Readonly {
		if !hostAgentSkillEnabled(profile, "host-diagnostics") {
			return decision, fmt.Errorf("%s commands require the host-diagnostics skill to be enabled", decision.Category)
		}
	} else {
		if !hostAgentSkillEnabled(profile, "host-change-review") {
			return decision, fmt.Errorf("%s commands require the host-change-review skill to be enabled", decision.Category)
		}
	}
	if !hostAgentBoolValue(profile.CommandPermissions.Enabled, true) {
		return decision, fmt.Errorf("command execution is disabled by the current host-agent profile")
	}
	if !hostAgentBoolValue(profile.CommandPermissions.AllowShellWrapper, true) && hostAgentCommandUsesShellWrapper(command) {
		return decision, fmt.Errorf("shell wrapper commands are disabled by the current host-agent profile")
	}
	if !hostAgentBoolValue(profile.CommandPermissions.AllowSudo, false) && hostAgentCommandUsesSudo(command) {
		return decision, fmt.Errorf("sudo commands are disabled by the current host-agent profile")
	}
	switch decision.Mode {
	case model.AgentPermissionModeDeny:
		return decision, fmt.Errorf("%s commands are denied by the current host-agent profile", decision.Category)
	case model.AgentPermissionModeReadonlyOnly:
		if !decision.Readonly {
			return decision, fmt.Errorf("%s commands are restricted to readonly_only by the current host-agent profile", decision.Category)
		}
	}
	return decision, nil
}

func skillEnabledByID(profile model.AgentProfile, id string) bool {
	want := strings.TrimSpace(strings.ToLower(id))
	if want == "" {
		return false
	}
	for _, item := range profile.Skills {
		if !skillEnabledByProfile(item) {
			continue
		}
		if strings.TrimSpace(strings.ToLower(item.ID)) == want {
			return true
		}
		if strings.TrimSpace(strings.ToLower(item.Name)) == want {
			return true
		}
	}
	return false
}

func mcpByID(profile model.AgentProfile, id string) (model.AgentMCP, bool) {
	want := strings.TrimSpace(strings.ToLower(id))
	if want == "" {
		return model.AgentMCP{}, false
	}
	for _, item := range profile.MCPs {
		if !item.Enabled {
			continue
		}
		if strings.TrimSpace(strings.ToLower(item.ID)) == want {
			return item, true
		}
		if strings.TrimSpace(strings.ToLower(item.Name)) == want {
			return item, true
		}
	}
	return model.AgentMCP{}, false
}

func hostAgentSkillEnabled(profile model.AgentProfile, id string) bool {
	return skillEnabledByID(profile, id)
}

func hostAgentMCPEnabled(profile model.AgentProfile, id string) bool {
	_, ok := mcpByID(profile, id)
	return ok
}

func skillEnabledByProfile(item model.AgentSkill) bool {
	if !item.Enabled {
		return false
	}
	return model.NormalizeAgentSkillActivationMode(item.ActivationMode) != model.AgentSkillActivationDisabled
}

func hostAgentSkillStateLabel(profile model.AgentProfile, id string, enabledLabel string) string {
	if hostAgentSkillEnabled(profile, id) {
		return enabledLabel
	}
	return enabledLabel + ":disabled"
}

func hostAgentSkillStateLabels(profile model.AgentProfile) []string {
	labels := make([]string, 0, len(profile.Skills))
	for _, item := range profile.Skills {
		label := strings.TrimSpace(item.Name)
		if label == "" {
			label = strings.TrimSpace(item.ID)
		}
		if label == "" {
			continue
		}
		state := "disabled"
		if skillEnabledByProfile(item) {
			state = "enabled"
		}
		labels = append(labels, fmt.Sprintf("%s:%s", label, state))
	}
	sort.Strings(labels)
	return labels
}

func hostAgentMCPStateLabels(profile model.AgentProfile) []string {
	labels := make([]string, 0, len(profile.MCPs))
	for _, item := range profile.MCPs {
		label := strings.TrimSpace(item.Name)
		if label == "" {
			label = strings.TrimSpace(item.ID)
		}
		if label == "" {
			continue
		}
		state := "disabled"
		if item.Enabled {
			state = model.NormalizeAgentMCPPermission(item.Permission)
		}
		labels = append(labels, fmt.Sprintf("%s:%s", label, state))
	}
	sort.Strings(labels)
	return labels
}

func hostAgentRuntimeGateLabels(profile model.AgentProfile) []string {
	gates := make([]string, 0, 4)
	for _, item := range []struct {
		id    string
		label string
	}{
		{id: "host-files", label: "host-files"},
		{id: "host-logs", label: "host-logs"},
		{id: "host-diagnostics", label: "host-diagnostics"},
		{id: "host-change-review", label: "host-change-review"},
	} {
		state := "disabled"
		switch item.id {
		case "host-files", "host-logs":
			if hostAgentMCPEnabled(profile, item.id) {
				state = "enabled"
			}
		case "host-diagnostics", "host-change-review":
			if hostAgentSkillEnabled(profile, item.id) {
				state = "enabled"
			}
		}
		gates = append(gates, fmt.Sprintf("%s=%s", item.label, state))
	}
	return gates
}

func hostAgentCommandUsesLogsCapability(command string) bool {
	normalized := strings.ToLower(strings.TrimSpace(command))
	if normalized == "" {
		return false
	}
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return false
	}
	program := filepath.Base(fields[0])
	if program == "journalctl" {
		return true
	}
	if program == "systemctl" && strings.Contains(normalized, " status ") {
		return true
	}
	if program == "service" && strings.Contains(normalized, " status ") {
		return true
	}
	if strings.Contains(normalized, "/var/log/") || strings.Contains(normalized, "/var/log ") {
		return true
	}
	if strings.Contains(normalized, "/journal/") || strings.Contains(normalized, "/journal ") {
		return true
	}
	return false
}

func hostAgentCapabilityStateFromProfile(profile model.AgentProfile, capability string) string {
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

func hostAgentCapabilityDisabled(state string) bool {
	return strings.TrimSpace(state) == model.AgentCapabilityDisabled
}

func profileCommandMode(profile model.AgentProfile, category string) string {
	mode := hostAgentNormalizePermissionMode(profile.CommandPermissions.DefaultMode)
	if profile.CommandPermissions.CategoryPolicies != nil {
		if override, ok := profile.CommandPermissions.CategoryPolicies[category]; ok && strings.TrimSpace(override) != "" {
			mode = hostAgentNormalizePermissionMode(override)
		}
	}
	return mode
}

func hostAgentNormalizePermissionMode(mode string) string {
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

func classifyHostAgentCommandCategory(command string) string {
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
		if program == "sed" && !isHostAgentReadonlyCommand(command) {
			return "filesystem_mutation"
		}
		if program == "find" && !isHostAgentReadonlyCommand(command) {
			return "filesystem_mutation"
		}
		return "file_read"
	case "systemctl", "service":
		if isHostAgentReadonlyCommand(command) {
			return "service_read"
		}
		return "service_mutation"
	case "ss", "netstat", "curl", "wget", "dig", "nslookup", "ping", "ip":
		return "network_read"
	case "docker", "kubectl", "git":
		if isHostAgentReadonlyCommand(command) {
			switch program {
			case "git":
				return "file_read"
			default:
				return "system_inspection"
			}
		}
		return "service_mutation"
	default:
		if isHostAgentReadonlyCommand(command) {
			return "system_inspection"
		}
		return "filesystem_mutation"
	}
}

func hostAgentEnsurePathsWithinRoots(paths, roots []string, profileLabel string) error {
	if len(roots) == 0 {
		return nil
	}
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if !hostAgentPathWithinRoots(trimmed, roots) {
			return fmt.Errorf("%s is outside the allowed writable roots for %s", trimmed, profileLabel)
		}
	}
	return nil
}

func hostAgentPathWithinRoots(path string, roots []string) bool {
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

func hostAgentBoolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func hostAgentYesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isHostAgentReadonlyCommand(command string) bool {
	return validateHostAgentReadonlyCommand(command) == nil
}

func validateHostAgentReadonlyCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return errors.New("empty command")
	}
	lower := strings.ToLower(trimmed)
	forbiddenFragments := []string{
		";", "&&", "||", ">>", ">", "<", "`", "$(",
		" sudo ", "\nsudo ", "\tsudo ", "rm ", " mv ", " cp ", " chmod ", " chown ", " mkdir ", " touch ",
		" systemctl start", " systemctl stop", " systemctl restart", " service ", " kill ", " pkill ", " killall ",
		" apt ", " apt-get ", " yum ", " dnf ", " apk ", " pip install", " npm install", " tee ",
	}
	padded := " " + lower + " "
	for _, fragment := range forbiddenFragments {
		if strings.Contains(padded, fragment) || strings.HasPrefix(lower, strings.TrimSpace(fragment)) {
			return errors.New("this request is not read-only. Use execute_system_mutation instead.")
		}
	}

	segments := strings.Split(trimmed, "|")
	for _, segment := range segments {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 {
			continue
		}
		if err := validateHostAgentReadonlyProgram(fields); err != nil {
			return err
		}
	}
	return nil
}

func validateHostAgentReadonlyProgram(fields []string) error {
	program := strings.ToLower(filepath.Base(fields[0]))
	allowed := map[string]bool{
		"cat": true, "ls": true, "find": true, "grep": true, "rg": true, "sed": true,
		"head": true, "tail": true, "wc": true, "cut": true, "sort": true, "uniq": true,
		"df": true, "du": true, "free": true, "uptime": true, "top": true, "ps": true,
		"ss": true, "netstat": true, "iostat": true, "vmstat": true, "journalctl": true,
		"dmesg": true, "uname": true, "env": true, "printenv": true, "which": true, "whereis": true,
		"hostname": true, "id": true, "whoami": true, "pwd": true, "date": true,
		"lsblk": true, "blkid": true,
		"docker": true, "kubectl": true, "git": true, "systemctl": true,
	}
	if !allowed[program] {
		return fmt.Errorf("`%s` is not allowed in readonly queries. Use execute_system_mutation instead.", program)
	}

	switch program {
	case "find":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "-delete",
				value == "-exec",
				value == "-execdir",
				value == "-ok",
				value == "-okdir",
				value == "-fprint",
				value == "-fprint0",
				value == "-fprintf",
				value == "-fls":
				return errors.New("find mutations must use execute_system_mutation")
			}
		}
		return nil
	case "sed":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "-i",
				strings.HasPrefix(value, "-i"),
				value == "--in-place",
				strings.HasPrefix(value, "--in-place="):
				return errors.New("sed in-place edits must use execute_system_mutation")
			}
		}
		return nil
	case "sort":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "-o",
				value == "--output",
				strings.HasPrefix(value, "-o="),
				strings.HasPrefix(value, "--output="):
				return errors.New("sort output redirection must use execute_system_mutation")
			}
		}
		return nil
	case "git":
		for _, arg := range fields[1:] {
			switch strings.ToLower(strings.TrimSpace(arg)) {
			case "checkout", "switch", "merge", "rebase", "reset", "clean", "commit", "push", "pull", "fetch":
				return errors.New("git mutation commands must use execute_system_mutation")
			}
		}
		return nil
	case "docker", "kubectl", "systemctl":
		for _, arg := range fields[1:] {
			switch strings.ToLower(strings.TrimSpace(arg)) {
			case "start", "stop", "restart", "rm", "kill", "delete", "apply", "replace", "rollout":
				return errors.New("mutation commands must use execute_system_mutation")
			}
		}
		return nil
	default:
		return nil
	}
}

func hostAgentCommandUsesShellWrapper(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	prefixes := []string{
		"/bin/sh -c", "sh -c", "/bin/bash -c", "/bin/bash -lc", "bash -c", "bash -lc",
		"/bin/zsh -c", "/bin/zsh -lc", "zsh -c", "zsh -lc", "fish -c",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.ContainsAny(command, "|;&<>`$()")
}

func hostAgentCommandUsesSudo(command string) bool {
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
