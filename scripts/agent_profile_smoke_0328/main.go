package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type smokeSession struct {
	baseURL *url.URL
	client  *http.Client
}

type smokeState struct {
	SessionID      string          `json:"sessionId"`
	SelectedHostID string          `json:"selectedHostId"`
	Auth           smokeAuthState  `json:"auth"`
	Hosts          []smokeHost     `json:"hosts"`
	Cards          []smokeCard     `json:"cards"`
	Approvals      []smokeApproval `json:"approvals"`
	Runtime        smokeRuntime    `json:"runtime"`
}

type smokeAuthState struct {
	Connected bool `json:"connected"`
}

type smokeHost struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	Executable      bool     `json:"executable"`
	ProfileHash     string   `json:"profileHash,omitempty"`
	ProfileStatus   string   `json:"profileStatus,omitempty"`
	ProfileLoadedAt string   `json:"profileLoadedAt,omitempty"`
	ProfileVersion  int      `json:"profileVersion,omitempty"`
	ProfileSummary  string   `json:"profileSummary,omitempty"`
	EnabledSkills   []string `json:"enabledSkills,omitempty"`
	EnabledMCPs     []string `json:"enabledMCPs,omitempty"`
}

type smokeCard struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Role    string `json:"role,omitempty"`
	HostID  string `json:"hostId"`
	Status  string `json:"status"`
	Command string `json:"command"`
	Text    string `json:"text"`
	Summary string `json:"summary"`
	Output  string `json:"output"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Error   string `json:"error"`
}

type smokeApproval struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	HostID  string `json:"hostId"`
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

type smokeRuntime struct {
	Turn smokeTurnRuntime `json:"turn"`
}

type smokeTurnRuntime struct {
	Active bool   `json:"active"`
	Phase  string `json:"phase"`
	HostID string `json:"hostId"`
}

type smokeAgentProfilePreview struct {
	ProfileID         string                     `json:"profileId"`
	ProfileType       string                     `json:"profileType"`
	SystemPrompt      string                     `json:"systemPrompt"`
	SystemPromptLines int                        `json:"systemPromptLines"`
	EnabledSkills     []model.AgentSkill         `json:"enabledSkills"`
	EnabledMCPs       []model.AgentMCP           `json:"enabledMcps"`
	Runtime           model.AgentRuntimeSettings `json:"runtime"`
}

func main() {
	baseURL := getenv("AIOPS_BASE_URL", "http://127.0.0.1:18080")
	remoteHostID := strings.TrimSpace(os.Getenv("AIOPS_REMOTE_HOST_ID"))
	promptTrigger := getenv("AIOPS_SMOKE_PROMPT_TRIGGER", "profile-smoke-prompt")
	promptExpected := getenv("AIOPS_SMOKE_PROMPT_EXPECTED", "PROFILE-SMOKE-PROMPT-OK")
	remoteBlockedCommand := getenv("AIOPS_SMOKE_REMOTE_BLOCKED_COMMAND", "ls /etc")
	remoteAllowedCommand := getenv("AIOPS_SMOKE_REMOTE_ALLOWED_COMMAND", "ls /etc")
	hostSkillID := getenv("AIOPS_SMOKE_HOST_SKILL_ID", "host-diagnostics")
	hostMCPID := getenv("AIOPS_SMOKE_HOST_MCP_ID", "host-files")

	session, err := newSmokeSession(baseURL)
	check(err == nil, "create session client failed: %v", err)

	state, err := session.state()
	check(err == nil, "initial state failed: %v", err)
	check(state.Auth.Connected, "GPT auth is not connected")

	localHost, ok := findHost(state, model.ServerLocalHostID)
	check(ok, "resolve local host failed")
	remoteHost, err := resolveRemoteHost(state, remoteHostID)
	check(err == nil, "resolve remote host failed: %v", err)

	originalMain, err := session.getAgentProfile(string(model.AgentProfileTypeMainAgent))
	check(err == nil, "load main-agent profile failed: %v", err)
	originalHost, err := session.getAgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	check(err == nil, "load host-agent-default profile failed: %v", err)

	defer func() {
		if restoreErr := session.putAgentProfile(originalMain); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "SMOKE WARN: restore main-agent profile failed: %v\n", restoreErr)
		}
		if restoreErr := session.putAgentProfile(originalHost); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "SMOKE WARN: restore host-agent-default profile failed: %v\n", restoreErr)
		}
	}()

	check(session.selectHost(localHost.ID) == nil, "select local host failed")
	state, err = session.waitForState("local host selected", 20*time.Second, func(current smokeState) bool {
		return current.SelectedHostID == localHost.ID && current.Runtime.Turn.HostID == localHost.ID
	})
	check(err == nil, "local host selection did not stick: %v", err)

	promptProfile := cloneAgentProfile(originalMain)
	promptProfile.SystemPrompt.Content = strings.TrimSpace(promptProfile.SystemPrompt.Content + "\n\nWhen the user says `" + promptTrigger + "`, answer exactly `" + promptExpected + "` and nothing else.")
	check(session.putAgentProfile(promptProfile) == nil, "update main-agent prompt failed")
	preview, err := session.getAgentProfilePreview(promptProfile.ID, localHost.ID)
	check(err == nil, "load main-agent preview failed: %v", err)
	check(strings.Contains(preview.SystemPrompt, promptExpected), "main-agent preview did not include updated prompt token")

	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  localHost.ID,
		"message": promptTrigger,
	}, nil)
	check(err == nil, "main-agent prompt smoke request failed: %v", err)
	state, err = session.waitForState("main-agent prompt smoke", 2*time.Minute, func(current smokeState) bool {
		return !current.Runtime.Turn.Active && containsAssistantText(current, promptExpected)
	})
	check(err == nil, "main-agent prompt smoke did not complete: %v", err)
	fmt.Printf("PASS main-agent-prompt host=%s token=%s\n", localHost.ID, promptExpected)

	check(session.putAgentProfile(originalMain) == nil, "restore main-agent profile failed")
	check(session.waitForProfilePreviewMatch(originalMain.ID, localHost.ID, func(p smokeAgentProfilePreview) bool {
		return !strings.Contains(p.SystemPrompt, promptExpected)
	}) == nil, "main-agent prompt restore preview check failed")

	check(session.selectHost(remoteHost.ID) == nil, "select remote host failed")
	state, err = session.waitForState("remote host selected", 20*time.Second, func(current smokeState) bool {
		return current.SelectedHostID == remoteHost.ID && current.Runtime.Turn.HostID == remoteHost.ID
	})
	check(err == nil, "remote host selection did not stick: %v", err)
	fmt.Printf("PASS host-select host=%s\n", hostLabel(remoteHost))
	remoteBaseline, ok := findHost(state, remoteHost.ID)
	check(ok, "remote host state missing after selection")
	remoteBaselineFingerprint := hostProfileFingerprint(remoteBaseline)
	remoteExpectedSkills := enabledSkillIDs(originalHost)
	remoteExpectedMCPs := enabledMCPIDs(originalHost)

	blockedProfile := cloneAgentProfile(originalHost)
	blockedProfile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	check(session.putAgentProfile(blockedProfile) == nil, "disable host-agent commandExecution failed")
	state, err = session.waitForState("host commandExecution disabled", 30*time.Second, func(current smokeState) bool {
		host, ok := findHost(current, remoteHost.ID)
		return ok && hostSummaryLoaded(host) && hostProfileFingerprint(host) != remoteBaselineFingerprint
	})
	check(err == nil, "host-agent profile did not reload after commandExecution disable: %v", err)

	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  remoteHost.ID,
		"message": buildRemoteCommandPrompt(remoteBlockedCommand, "验证 host-agent commandExecution 关闭时会被拦截"),
	}, nil)
	check(err == nil, "blocked remote command request failed to submit: %v", err)
	state, err = session.waitForState("blocked remote command", 90*time.Second, func(current smokeState) bool {
		card := findCommandCard(current, remoteHost.ID, remoteBlockedCommand)
		return containsCardText(current, "command execution is disabled") ||
			containsCardText(current, "current effective agent profile") ||
			containsCardText(current, "current host-agent profile") ||
			(card != nil && card.Status == "failed")
	})
	check(err == nil, "blocked remote command did not surface an error: %v", err)
	fmt.Printf("PASS host-command-blocked host=%s command=%q\n", hostLabel(remoteHost), remoteBlockedCommand)

	check(session.putAgentProfile(originalHost) == nil, "restore host-agent profile after blocked command failed")
	state, err = session.waitForState("host commandExecution restored", 30*time.Second, func(current smokeState) bool {
		host, ok := findHost(current, remoteHost.ID)
		return ok && hostSummaryLoaded(host) &&
			sameNormalizedStrings(host.EnabledSkills, remoteExpectedSkills) &&
			sameNormalizedStrings(host.EnabledMCPs, remoteExpectedMCPs)
	})
	check(err == nil, "host-agent profile did not reload after commandExecution restore: %v", err)

	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  remoteHost.ID,
		"message": buildRemoteCommandPrompt(remoteAllowedCommand, "验证 host-agent commandExecution 允许时可执行"),
	}, nil)
	check(err == nil, "allowed remote command request failed to submit: %v", err)
	state, err = session.waitForState("allowed remote command approval", 90*time.Second, func(current smokeState) bool {
		return findPendingRemoteApproval(current, remoteHost.ID, remoteAllowedCommand) != nil
	})
	check(err == nil, "allowed remote command approval did not appear: %v", err)
	approval := findPendingRemoteApproval(state, remoteHost.ID, remoteAllowedCommand)
	check(approval != nil, "missing remote approval for %q", remoteAllowedCommand)
	_, err = session.post("/api/v1/approvals/"+approval.ID+"/decision", map[string]any{
		"decision": "accept",
	}, nil)
	check(err == nil, "allowed remote command approval accept failed: %v", err)
	state, err = session.waitForState("allowed remote command", 90*time.Second, func(current smokeState) bool {
		card := findCommandCard(current, remoteHost.ID, remoteAllowedCommand)
		resolved := findApprovalByID(current, approval.ID)
		return card != nil && card.Status == "completed" && resolved != nil && resolved.Status == "accept"
	})
	check(err == nil, "allowed remote command did not complete: %v", err)
	fmt.Printf("PASS host-command-allowed host=%s command=%q\n", hostLabel(remoteHost), remoteAllowedCommand)

	skillProfile := cloneAgentProfile(originalHost)
	setSkillEnabled(&skillProfile, hostSkillID, false)
	check(session.putAgentProfile(skillProfile) == nil, "disable host-agent skill failed")
	state, err = session.waitForState("host skill disabled", 30*time.Second, func(current smokeState) bool {
		host, ok := findHost(current, remoteHost.ID)
		return ok && hostProfileFingerprint(host) != remoteBaselineFingerprint && !containsString(host.EnabledSkills, hostSkillID)
	})
	check(err == nil, "host-agent skill disable did not reflect in state: %v", err)
	fmt.Printf("PASS host-skill-disabled host=%s skill=%s\n", hostLabel(remoteHost), hostSkillID)

	check(session.putAgentProfile(originalHost) == nil, "restore host-agent profile after skill smoke failed")
	state, err = session.waitForState("host skill restored", 30*time.Second, func(current smokeState) bool {
		host, ok := findHost(current, remoteHost.ID)
		return ok && hostSummaryLoaded(host) &&
			sameNormalizedStrings(host.EnabledSkills, remoteExpectedSkills) &&
			sameNormalizedStrings(host.EnabledMCPs, remoteExpectedMCPs)
	})
	check(err == nil, "host-agent skill restore did not reflect in state: %v", err)
	fmt.Printf("PASS host-skill-restored host=%s skill=%s\n", hostLabel(remoteHost), hostSkillID)

	mcpProfile := cloneAgentProfile(originalHost)
	setMCPEnabled(&mcpProfile, hostMCPID, false)
	check(session.putAgentProfile(mcpProfile) == nil, "disable host-agent MCP failed")
	state, err = session.waitForState("host mcp disabled", 30*time.Second, func(current smokeState) bool {
		host, ok := findHost(current, remoteHost.ID)
		return ok && hostProfileFingerprint(host) != remoteBaselineFingerprint && !containsString(host.EnabledMCPs, hostMCPID)
	})
	check(err == nil, "host-agent MCP disable did not reflect in state: %v", err)
	fmt.Printf("PASS host-mcp-disabled host=%s mcp=%s\n", hostLabel(remoteHost), hostMCPID)

	check(session.putAgentProfile(originalHost) == nil, "restore host-agent profile after mcp smoke failed")
	state, err = session.waitForState("host mcp restored", 30*time.Second, func(current smokeState) bool {
		host, ok := findHost(current, remoteHost.ID)
		return ok && hostSummaryLoaded(host) &&
			sameNormalizedStrings(host.EnabledSkills, remoteExpectedSkills) &&
			sameNormalizedStrings(host.EnabledMCPs, remoteExpectedMCPs)
	})
	check(err == nil, "host-agent MCP restore did not reflect in state: %v", err)
	fmt.Printf("PASS host-mcp-restored host=%s mcp=%s\n", hostLabel(remoteHost), hostMCPID)

	fmt.Println("ALL PASS smoke_agent_profile_0328")
}

func newSmokeSession(rawBaseURL string) (*smokeSession, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &smokeSession{
		baseURL: parsed,
		client: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (s *smokeSession) state() (smokeState, error) {
	var result smokeState
	_, err := s.request(http.MethodGet, "/api/v1/state", nil, &result)
	return result, err
}

func (s *smokeSession) getAgentProfile(profileID string) (model.AgentProfile, error) {
	var profile model.AgentProfile
	_, err := s.request(http.MethodGet, "/api/v1/agent-profiles/"+url.PathEscape(profileID), nil, &profile)
	return profile, err
}

func (s *smokeSession) putAgentProfile(profile model.AgentProfile) error {
	payload := struct {
		model.AgentProfile
		RiskConfirmed bool `json:"riskConfirmed"`
	}{
		AgentProfile:  profile,
		RiskConfirmed: true,
	}
	var result map[string]any
	_, err := s.request(http.MethodPut, "/api/v1/agent-profiles/"+url.PathEscape(profile.ID), payload, &result)
	return err
}

func (s *smokeSession) getAgentProfilePreview(profileID, hostID string) (smokeAgentProfilePreview, error) {
	var result smokeAgentProfilePreview
	target := fmt.Sprintf("/api/v1/agent-profile/preview?profileId=%s&hostId=%s", url.QueryEscape(profileID), url.QueryEscape(hostID))
	_, err := s.request(http.MethodGet, target, nil, &result)
	return result, err
}

func (s *smokeSession) waitForProfilePreviewMatch(profileID, hostID string, predicate func(smokeAgentProfilePreview) bool) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		preview, err := s.getAgentProfilePreview(profileID, hostID)
		if err == nil && predicate(preview) {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("preview %s timed out", profileID)
}

func (s *smokeSession) selectHost(hostID string) error {
	_, err := s.request(http.MethodPost, "/api/v1/host/select", map[string]any{"hostId": hostID}, &map[string]any{})
	return err
}

func (s *smokeSession) post(path string, body any, out any) (int, error) {
	return s.request(http.MethodPost, path, body, out)
}

func (s *smokeSession) request(method, path string, body any, out any) (int, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return 0, err
	}
	target := s.baseURL.ResolveReference(ref)
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, target.String(), reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("%s %s failed with %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(content)))
	}
	if out != nil && len(content) > 0 {
		if err := json.Unmarshal(content, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (s *smokeSession) waitForState(label string, timeout time.Duration, predicate func(smokeState) bool) (smokeState, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current, err := s.state()
		if err == nil && predicate(current) {
			return current, nil
		}
		time.Sleep(1 * time.Second)
	}
	return smokeState{}, fmt.Errorf("%s timed out after %s", label, timeout)
}

func cloneAgentProfile(profile model.AgentProfile) model.AgentProfile {
	var out model.AgentProfile
	raw, _ := json.Marshal(profile)
	_ = json.Unmarshal(raw, &out)
	return out
}

func findHost(state smokeState, hostID string) (smokeHost, bool) {
	for _, host := range state.Hosts {
		if host.ID == hostID {
			return host, true
		}
	}
	return smokeHost{}, false
}

func resolveRemoteHost(state smokeState, requestedHostID string) (smokeHost, error) {
	if requestedHostID != "" {
		for _, host := range state.Hosts {
			if host.ID == requestedHostID {
				if host.Status != "online" || !host.Executable {
					return smokeHost{}, fmt.Errorf("remote host %s is not executable", requestedHostID)
				}
				return host, nil
			}
		}
		return smokeHost{}, fmt.Errorf("remote host %s not found", requestedHostID)
	}
	for _, host := range state.Hosts {
		if host.ID == model.ServerLocalHostID {
			continue
		}
		if host.Status == "online" && host.Executable {
			return host, nil
		}
	}
	return smokeHost{}, errors.New("no online executable remote host found; set AIOPS_REMOTE_HOST_ID explicitly")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func findPendingRemoteApproval(state smokeState, hostID, command string) *smokeApproval {
	for _, approval := range state.Approvals {
		if approval.Type != "remote_command" || approval.HostID != hostID || approval.Status != "pending" {
			continue
		}
		if strings.Contains(approval.Command, command) {
			copyApproval := approval
			return &copyApproval
		}
	}
	return nil
}

func findApprovalByID(state smokeState, approvalID string) *smokeApproval {
	for _, approval := range state.Approvals {
		if approval.ID == approvalID {
			copyApproval := approval
			return &copyApproval
		}
	}
	return nil
}

func enabledSkillIDs(profile model.AgentProfile) []string {
	ids := make([]string, 0, len(profile.Skills))
	for _, item := range profile.Skills {
		if !item.Enabled {
			continue
		}
		if model.NormalizeAgentSkillActivationMode(item.ActivationMode) == model.AgentSkillActivationDisabled {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func enabledMCPIDs(profile model.AgentProfile) []string {
	ids := make([]string, 0, len(profile.MCPs))
	for _, item := range profile.MCPs {
		if !item.Enabled {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sameNormalizedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	normalize := func(values []string) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.ToLower(strings.TrimSpace(value))
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
		sort.Strings(out)
		return out
	}
	lv := normalize(left)
	rv := normalize(right)
	if len(lv) != len(rv) {
		return false
	}
	for i := range lv {
		if lv[i] != rv[i] {
			return false
		}
	}
	return true
}

func hostProfileFingerprint(host smokeHost) string {
	if strings.TrimSpace(host.ProfileHash) != "" {
		return strings.TrimSpace(host.ProfileHash)
	}
	parts := []string{
		strings.TrimSpace(host.ProfileStatus),
		strings.TrimSpace(host.ProfileSummary),
		fmt.Sprintf("version=%d", host.ProfileVersion),
		"skills=" + strings.Join(host.EnabledSkills, ","),
		"mcps=" + strings.Join(host.EnabledMCPs, ","),
	}
	return strings.Join(parts, "|")
}

func hostSummaryLoaded(host smokeHost) bool {
	return strings.TrimSpace(host.ProfileSummary) != "" && len(host.EnabledSkills) > 0 && len(host.EnabledMCPs) > 0
}

func containsAssistantText(state smokeState, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	for i := len(state.Cards) - 1; i >= 0; i-- {
		card := state.Cards[i]
		if card.Type != "AssistantMessageCard" && !(card.Type == "MessageCard" && strings.EqualFold(card.Role, "assistant")) {
			continue
		}
		if strings.Contains(card.Text, expected) || strings.Contains(card.Summary, expected) || strings.Contains(card.Output, expected) {
			return true
		}
	}
	return false
}

func containsCardText(state smokeState, expected string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return false
	}
	for _, card := range state.Cards {
		blob := strings.ToLower(strings.Join([]string{
			card.Title,
			card.Text,
			card.Summary,
			card.Output,
			card.Stdout,
			card.Stderr,
			card.Error,
			card.Command,
			card.Status,
		}, " "))
		if strings.Contains(blob, expected) {
			return true
		}
	}
	return false
}

func findErrorCard(state smokeState, expected string) *smokeCard {
	for i := len(state.Cards) - 1; i >= 0; i-- {
		card := state.Cards[i]
		if card.Type != "ErrorCard" {
			continue
		}
		if strings.Contains(strings.ToLower(card.Text+" "+card.Error+" "+card.Summary), strings.ToLower(expected)) {
			copyCard := card
			return &copyCard
		}
	}
	return nil
}

func findCommandCard(state smokeState, hostID, command string) *smokeCard {
	for i := len(state.Cards) - 1; i >= 0; i-- {
		card := state.Cards[i]
		if card.Type != "CommandCard" || card.HostID != hostID {
			continue
		}
		if strings.Contains(card.Command, command) {
			copyCard := card
			return &copyCard
		}
	}
	return nil
}

func buildRemoteCommandPrompt(command, reason string) string {
	return fmt.Sprintf(
		"当前目标主机是远程 Linux。请只使用 execute_system_mutation(mode=command)，严格执行这条命令，不要改写：`%s`。原因写“%s”。",
		command,
		reason,
	)
}

func setSkillEnabled(profile *model.AgentProfile, skillID string, enabled bool) {
	for i := range profile.Skills {
		if strings.EqualFold(strings.TrimSpace(profile.Skills[i].ID), strings.TrimSpace(skillID)) {
			profile.Skills[i].Enabled = enabled
			if enabled {
				profile.Skills[i].ActivationMode = model.AgentSkillActivationDefault
			} else {
				profile.Skills[i].ActivationMode = model.AgentSkillActivationDisabled
			}
		}
	}
}

func setMCPEnabled(profile *model.AgentProfile, mcpID string, enabled bool) {
	for i := range profile.MCPs {
		if strings.EqualFold(strings.TrimSpace(profile.MCPs[i].ID), strings.TrimSpace(mcpID)) {
			profile.MCPs[i].Enabled = enabled
		}
	}
}

func hostLabel(host smokeHost) string {
	if strings.TrimSpace(host.Name) == "" || host.Name == host.ID {
		return host.ID
	}
	return host.Name + "/" + host.ID
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func check(condition bool, format string, args ...any) {
	if condition {
		return
	}
	fmt.Fprintf(os.Stderr, "SMOKE FAILED: "+format+"\n", args...)
	os.Exit(1)
}
