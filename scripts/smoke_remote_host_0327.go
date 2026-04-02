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
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type smokeSession struct {
	baseURL *url.URL
	client  *http.Client
}

type snapshot struct {
	SessionID      string          `json:"sessionId"`
	SelectedHostID string          `json:"selectedHostId"`
	Auth           authState       `json:"auth"`
	Hosts          []hostState     `json:"hosts"`
	Cards          []cardState     `json:"cards"`
	Approvals      []approvalState `json:"approvals"`
	Runtime        runtimeState    `json:"runtime"`
}

type authState struct {
	Connected bool `json:"connected"`
}

type hostState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Executable bool   `json:"executable"`
}

type cardState struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Title   string            `json:"title"`
	HostID  string            `json:"hostId"`
	Status  string            `json:"status"`
	Command string            `json:"command"`
	Text    string            `json:"text"`
	Summary string            `json:"summary"`
	Output  string            `json:"output"`
	Stdout  string            `json:"stdout"`
	Stderr  string            `json:"stderr"`
	Error   string            `json:"error"`
	Changes []fileChangeState `json:"changes"`
}

type approvalState struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Status  string            `json:"status"`
	HostID  string            `json:"hostId"`
	Command string            `json:"command"`
	Reason  string            `json:"reason"`
	Changes []fileChangeState `json:"changes"`
}

type fileChangeState struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Diff string `json:"diff"`
}

type runtimeState struct {
	Turn turnRuntime `json:"turn"`
}

type turnRuntime struct {
	Active bool   `json:"active"`
	Phase  string `json:"phase"`
	HostID string `json:"hostId"`
}

type terminalCreateResponse struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
	StartedAt string `json:"startedAt"`
}

type terminalEnvelope struct {
	Type      string `json:"type"`
	Data      string `json:"data"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Code      int    `json:"code"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
	StartedAt string `json:"startedAt"`
}

type filePreviewResponse struct {
	HostID    string `json:"hostId,omitempty"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

func main() {
	baseURL := getenv("AIOPS_BASE_URL", "http://127.0.0.1:18080")
	remoteHostID := strings.TrimSpace(os.Getenv("AIOPS_REMOTE_HOST_ID"))
	cpuCommand := getenv("AIOPS_REMOTE_CPU_COMMAND", "uptime")
	diskCommand := getenv("AIOPS_REMOTE_DISK_COMMAND", "df -h /")
	serviceCommand := getenv("AIOPS_REMOTE_SERVICE_COMMAND", "systemctl status ssh --no-pager")
	logCommand := getenv("AIOPS_REMOTE_LOG_COMMAND", "journalctl -n 20 --no-pager")
	stopCommand := getenv("AIOPS_REMOTE_STOP_COMMAND", "sleep 30")
	browsePath := getenv("AIOPS_REMOTE_BROWSE_PATH", "/etc")
	readPath := getenv("AIOPS_REMOTE_READ_PATH", "/etc/hosts")
	searchPath := getenv("AIOPS_REMOTE_SEARCH_PATH", "/etc")
	searchQuery := getenv("AIOPS_REMOTE_SEARCH_QUERY", "localhost")
	configReadPath := getenv("AIOPS_REMOTE_CONFIG_READ_PATH", "/etc/nginx/nginx.conf")
	configBrowsePath := getenv("AIOPS_REMOTE_CONFIG_BROWSE_PATH", filepath.Dir(configReadPath))
	configSearchPath := getenv("AIOPS_REMOTE_CONFIG_SEARCH_PATH", configBrowsePath)
	configSearchQuery := getenv("AIOPS_REMOTE_CONFIG_SEARCH_QUERY", "server_name")
	configService := getenv("AIOPS_REMOTE_CONFIG_SERVICE", "nginx")
	configChangePath := getenv("AIOPS_REMOTE_CONFIG_CHANGE_PATH", filepath.Join(configBrowsePath, "conf.d", "aiops-smoke.conf"))
	configChangeReason := getenv("AIOPS_REMOTE_CONFIG_REASON", "验证配置变更与重载链路")
	configWriteMode := getenv("AIOPS_REMOTE_CONFIG_WRITE_MODE", "overwrite")
	configChangeContent := getenv("AIOPS_REMOTE_CONFIG_CHANGE_CONTENT", buildConfigSmokeContent(configReadPath, configChangePath, configService))
	configReloadCommand := getenv("AIOPS_REMOTE_RELOAD_COMMAND", fmt.Sprintf("%s -t && (systemctl reload %s || systemctl restart %s) && systemctl status %s --no-pager --lines=5", configService, configService, configService, configService))
	configReloadReason := getenv("AIOPS_REMOTE_RELOAD_REASON", "验证重载后的服务状态")

	session, err := newSmokeSession(baseURL)
	check(err == nil, "create session client failed: %v", err)

	state, err := session.state()
	check(err == nil, "initial state failed: %v", err)
	check(state.Auth.Connected, "GPT auth is not connected")

	host, err := resolveRemoteHost(state, remoteHostID)
	check(err == nil, "resolve remote host failed: %v", err)

	_, err = session.post("/api/v1/host/select", map[string]any{"hostId": host.ID}, nil)
	check(err == nil, "host selection failed: %v", err)
	state, err = session.waitForState("host selected", 15*time.Second, func(current snapshot) bool {
		return current.SelectedHostID == host.ID && current.Runtime.Turn.HostID == host.ID
	})
	check(err == nil, "host selection did not stick: %v", err)
	fmt.Printf("PASS host-select host=%s\n", hostLabel(host))

	readonlyCommands := []string{cpuCommand, diskCommand, serviceCommand, logCommand}
	readonlyPrompt := buildReadonlyPrompt(readonlyCommands)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": readonlyPrompt,
	}, nil)
	check(err == nil, "readonly smoke request failed: %v", err)
	state, err = session.waitForState("readonly smoke", 2*time.Minute, func(current snapshot) bool {
		if current.Runtime.Turn.Active {
			return false
		}
		return hasReadonlyCommandCards(current, host.ID, readonlyCommands)
	})
	check(err == nil, "readonly smoke did not complete: %v", err)
	for _, command := range readonlyCommands {
		card := findCommandCard(state, host.ID, command)
		check(card != nil, "missing readonly command card for %q", command)
		check(card.Status != "inProgress" && card.Status != "pending", "readonly command still in progress for %q", command)
	}
	fmt.Printf("PASS readonly-smoke host=%s commands=%d\n", hostLabel(host), len(readonlyCommands))

	beforeBrowse := countResultSummaryCards(state)
	browsePrompt := buildFileBrowsePrompt(browsePath, readPath, searchPath, searchQuery)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": browsePrompt,
	}, nil)
	check(err == nil, "file browse smoke request failed: %v", err)
	state, err = session.waitForState("file browse smoke", 2*time.Minute, func(current snapshot) bool {
		if current.Runtime.Turn.Active {
			return false
		}
		return countResultSummaryCards(current) >= beforeBrowse+3 &&
			findResultSummaryCard(current, "远程文件列表") != nil &&
			findResultSummaryCard(current, "远程文件读取") != nil &&
			findResultSummaryCard(current, "远程搜索结果") != nil
	})
	check(err == nil, "file browse smoke did not complete: %v", err)
	fmt.Printf("PASS file-browse-smoke host=%s path=%s read=%s query=%s\n", hostLabel(host), browsePath, readPath, searchQuery)

	configBrowsePrompt := buildFileBrowsePrompt(configBrowsePath, configReadPath, configSearchPath, configSearchQuery)
	beforeConfigBrowse := countResultSummaryCards(state)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": configBrowsePrompt,
	}, nil)
	check(err == nil, "config browse smoke request failed: %v", err)
	state, err = session.waitForState("config browse smoke", 2*time.Minute, func(current snapshot) bool {
		if current.Runtime.Turn.Active {
			return false
		}
		return countResultSummaryCards(current) >= beforeConfigBrowse+3 &&
			findResultSummaryCard(current, "远程文件列表") != nil &&
			findResultSummaryCard(current, "远程文件读取") != nil &&
			findResultSummaryCard(current, "远程搜索结果") != nil
	})
	check(err == nil, "config browse smoke did not complete: %v", err)
	fmt.Printf("PASS config-read-smoke host=%s path=%s read=%s query=%s\n", hostLabel(host), configBrowsePath, configReadPath, configSearchQuery)

	configChangePrompt := buildFileChangePrompt(configChangePath, configChangeContent, configWriteMode, configChangeReason)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": configChangePrompt,
	}, nil)
	check(err == nil, "config change smoke request failed: %v", err)
	state, err = session.waitForState("config file change approval pending", 90*time.Second, func(current snapshot) bool {
		return findPendingRemoteFileApproval(current, host.ID, configChangePath) != nil
	})
	check(err == nil, "config file change approval did not appear: %v", err)
	configFileApproval := findPendingRemoteFileApproval(state, host.ID, configChangePath)
	check(configFileApproval != nil, "missing config file change approval for %s", configChangePath)

	_, err = session.post("/api/v1/approvals/"+configFileApproval.ID+"/decision", map[string]any{"decision": "accept"}, nil)
	check(err == nil, "config file change approval accept failed: %v", err)
	state, err = session.waitForState("config file change completed", 90*time.Second, func(current snapshot) bool {
		card := findFileChangeCard(current, configChangePath)
		return !current.Runtime.Turn.Active && card != nil && card.Status == "completed"
	})
	check(err == nil, "config file change did not complete: %v", err)
	configFileCard := findFileChangeCard(state, configChangePath)
	check(configFileCard != nil, "missing config file change card for %s", configChangePath)
	check(strings.Contains(configFileCard.Text+configFileCard.Summary, configChangePath), "config file change card does not mention path %s", configChangePath)
	configContent, configTruncated, err := session.previewFile(host.ID, configChangePath)
	check(err == nil, "preview config file failed: %v", err)
	check(!configTruncated, "config file preview unexpectedly truncated")
	check(strings.Contains(configContent, configChangeContent), "config file content mismatch: %q", configContent)
	fmt.Printf("PASS config-change-smoke host=%s path=%s service=%s\n", hostLabel(host), configChangePath, configService)

	configReloadPrompt := buildCommandApprovalPrompt(configReloadCommand, configReloadReason)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": configReloadPrompt,
	}, nil)
	check(err == nil, "config reload smoke request failed: %v", err)
	state, err = session.waitForState("config reload approval pending", 90*time.Second, func(current snapshot) bool {
		approval := findPendingRemoteApproval(current, host.ID, configReloadCommand)
		return approval != nil
	})
	check(err == nil, "config reload approval did not appear: %v", err)
	configReloadApproval := findPendingRemoteApproval(state, host.ID, configReloadCommand)
	check(configReloadApproval != nil, "missing config reload approval for %q", configReloadCommand)

	_, err = session.post("/api/v1/approvals/"+configReloadApproval.ID+"/decision", map[string]any{"decision": "accept"}, nil)
	check(err == nil, "config reload approval accept failed: %v", err)
	state, err = session.waitForState("config reload completed", 2*time.Minute, func(current snapshot) bool {
		card := findCommandCard(current, host.ID, configReloadCommand)
		return card != nil && card.Status == "completed"
	})
	check(err == nil, "config reload did not complete: %v", err)
	configReloadCard := findCommandCard(state, host.ID, configReloadCommand)
	check(configReloadCard != nil, "missing config reload command card for %q", configReloadCommand)
	check(strings.TrimSpace(configReloadCard.Output+configReloadCard.Stdout+configReloadCard.Stderr) != "", "config reload command produced no output")
	fmt.Printf("PASS config-reload-smoke host=%s service=%s command=%q\n", hostLabel(host), configService, configReloadCommand)

	stopPrompt := fmt.Sprintf(
		"当前目标主机是远程 Linux。请只使用 execute_system_mutation(mode=command)，严格执行这条命令，不要改写：`%s`。原因写“验证远程停止任务链路”。",
		stopCommand,
	)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": stopPrompt,
	}, nil)
	check(err == nil, "stop smoke request failed: %v", err)

	state, err = session.waitForState("stop approval pending", 90*time.Second, func(current snapshot) bool {
		approval := findPendingRemoteApproval(current, host.ID, stopCommand)
		return approval != nil
	})
	check(err == nil, "stop approval did not appear: %v", err)
	stopApproval := findPendingRemoteApproval(state, host.ID, stopCommand)
	check(stopApproval != nil, "missing stop approval for %q", stopCommand)

	_, err = session.post("/api/v1/approvals/"+stopApproval.ID+"/decision", map[string]any{"decision": "accept"}, nil)
	check(err == nil, "stop approval accept failed: %v", err)
	state, err = session.waitForState("remote stop running", 30*time.Second, func(current snapshot) bool {
		card := findCommandCard(current, host.ID, stopCommand)
		return card != nil && (card.Status == "inProgress" || card.Status == "executing")
	})
	check(err == nil, "stop command did not start running: %v", err)

	_, err = session.post("/api/v1/chat/stop", map[string]any{}, nil)
	check(err == nil, "stop api failed: %v", err)
	state, err = session.waitForState("remote stop cancelled", 90*time.Second, func(current snapshot) bool {
		card := findCommandCard(current, host.ID, stopCommand)
		return !current.Runtime.Turn.Active && current.Runtime.Turn.Phase == "aborted" && card != nil && card.Status == "cancelled"
	})
	check(err == nil, "remote stop did not cancel command: %v", err)
	fmt.Printf("PASS stop-smoke host=%s command=%q\n", hostLabel(host), stopCommand)

	mutationPath := fmt.Sprintf("/tmp/aiops-remote-smoke-%d", time.Now().UnixNano())
	mutationCommand := "touch " + mutationPath
	mutationPrompt := fmt.Sprintf(
		"当前目标主机是远程 Linux。请只使用 execute_system_mutation(mode=command)，严格执行这条命令，不要改写：`%s`。原因写“验证远程审批链路”。",
		mutationCommand,
	)
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": mutationPrompt,
	}, nil)
	check(err == nil, "mutation smoke request failed: %v", err)

	state, err = session.waitForState("remote approval pending", 90*time.Second, func(current snapshot) bool {
		approval := findPendingRemoteApproval(current, host.ID, mutationCommand)
		return approval != nil
	})
	check(err == nil, "remote approval did not appear: %v", err)
	approval := findPendingRemoteApproval(state, host.ID, mutationCommand)
	check(approval != nil, "missing remote approval for %q", mutationCommand)

	_, err = session.post("/api/v1/approvals/"+approval.ID+"/decision", map[string]any{"decision": "accept"}, nil)
	check(err == nil, "approval accept failed: %v", err)

	state, err = session.waitForState("remote mutation completed", 90*time.Second, func(current snapshot) bool {
		if current.Runtime.Turn.Active {
			return false
		}
		resolved := findApprovalByID(current, approval.ID)
		card := findCommandCard(current, host.ID, mutationCommand)
		return resolved != nil && resolved.Status == "accept" && card != nil && card.Status == "completed"
	})
	check(err == nil, "remote mutation did not complete: %v", err)
	fmt.Printf("PASS approval-smoke host=%s approval=%s\n", hostLabel(host), approval.ID)

	fileChangePath := getenv("AIOPS_REMOTE_FILE_CHANGE_PATH", fmt.Sprintf("/tmp/aiops-remote-file-change-%d.conf", time.Now().UnixNano()))
	fileChangeContent := fmt.Sprintf("smoke=remote-file-change\nhost=%s\nstamp=%d\n", host.ID, time.Now().Unix())
	fileChangePrompt := buildFileChangePrompt(fileChangePath, fileChangeContent, "overwrite", "验证远程文件修改审批链路")
	_, err = session.post("/api/v1/chat/message", map[string]any{
		"hostId":  host.ID,
		"message": fileChangePrompt,
	}, nil)
	check(err == nil, "file change smoke request failed: %v", err)

	state, err = session.waitForState("file change approval pending", 90*time.Second, func(current snapshot) bool {
		return findPendingRemoteFileApproval(current, host.ID, fileChangePath) != nil
	})
	check(err == nil, "file change approval did not appear: %v", err)
	fileApproval := findPendingRemoteFileApproval(state, host.ID, fileChangePath)
	check(fileApproval != nil, "missing file change approval for %s", fileChangePath)

	_, err = session.post("/api/v1/approvals/"+fileApproval.ID+"/decision", map[string]any{"decision": "accept"}, nil)
	check(err == nil, "file change approval accept failed: %v", err)
	state, err = session.waitForState("file change completed", 90*time.Second, func(current snapshot) bool {
		card := findFileChangeCard(current, fileChangePath)
		return !current.Runtime.Turn.Active && card != nil && card.Status == "completed"
	})
	check(err == nil, "file change did not complete: %v", err)
	fileCard := findFileChangeCard(state, fileChangePath)
	check(fileCard != nil, "missing file change card for %s", fileChangePath)
	check(strings.Contains(fileCard.Text+fileCard.Summary, fileChangePath), "file change card does not mention path %s", fileChangePath)
	content, truncated, err := session.previewFile(host.ID, fileChangePath)
	check(err == nil, "preview changed file failed: %v", err)
	check(!truncated, "changed file preview unexpectedly truncated")
	check(strings.Contains(content, fileChangeContent), "changed file content mismatch: %q", content)
	fmt.Printf("PASS file-change-smoke host=%s path=%s\n", hostLabel(host), fileChangePath)

	var terminal terminalCreateResponse
	_, err = session.post("/api/v1/terminal/sessions", map[string]any{
		"hostId": host.ID,
		"cwd":    "~",
		"shell":  "/bin/sh",
		"cols":   120,
		"rows":   36,
	}, &terminal)
	check(err == nil, "terminal session create failed: %v", err)
	check(terminal.SessionID != "", "terminal session id is empty")

	outputSeen, ready, exit, err := session.runTerminalSmoke(terminal.SessionID, mutationPath)
	check(err == nil, "terminal smoke failed: %v", err)
	check(ready.SessionID == terminal.SessionID, "terminal ready session mismatch: %q != %q", ready.SessionID, terminal.SessionID)
	check(exit.Type == "exit", "terminal exit message missing")
	check(outputSeen, "terminal produced no output after pwd/cleanup")

	state, err = session.state()
	check(err == nil, "final state failed: %v", err)
	check(state.SelectedHostID == host.ID, "selected host drifted after terminal flow: %q", state.SelectedHostID)
	fmt.Printf("PASS terminal-smoke host=%s session=%s\n", hostLabel(host), terminal.SessionID)

	fmt.Println("ALL PASS smoke_remote_host_0327")
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

func (s *smokeSession) state() (snapshot, error) {
	var result snapshot
	_, err := s.request(http.MethodGet, "/api/v1/state", nil, &result)
	return result, err
}

func (s *smokeSession) previewFile(hostID, path string) (string, bool, error) {
	target := fmt.Sprintf("/api/v1/files/preview?hostId=%s&path=%s", url.QueryEscape(hostID), url.QueryEscape(path))
	var result filePreviewResponse
	_, err := s.request(http.MethodGet, target, nil, &result)
	if err != nil {
		return "", false, err
	}
	return result.Content, result.Truncated, nil
}

func (s *smokeSession) post(path string, body any, out any) (int, error) {
	return s.request(http.MethodPost, path, body, out)
}

func (s *smokeSession) request(method, path string, body any, out any) (int, error) {
	target := s.baseURL.ResolveReference(&url.URL{Path: path})
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

func (s *smokeSession) waitForState(label string, timeout time.Duration, predicate func(snapshot) bool) (snapshot, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current, err := s.state()
		if err == nil && predicate(current) {
			return current, nil
		}
		time.Sleep(1 * time.Second)
	}
	return snapshot{}, fmt.Errorf("%s timed out after %s", label, timeout)
}

func (s *smokeSession) runTerminalSmoke(terminalSessionID, cleanupPath string) (bool, terminalEnvelope, terminalEnvelope, error) {
	wsURL := websocketURL(s.baseURL, "/api/v1/terminal/ws?sessionId="+url.QueryEscape(terminalSessionID))
	header := http.Header{}
	if cookieHeader := s.cookieHeader(); cookieHeader != "" {
		header.Set("Cookie", cookieHeader)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return false, terminalEnvelope{}, terminalEnvelope{}, err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))

	var ready terminalEnvelope
	var exit terminalEnvelope
	outputSeen := false
	readyReceived := false
	sentCommands := false

	for {
		var msg terminalEnvelope
		if err := conn.ReadJSON(&msg); err != nil {
			return outputSeen, ready, exit, err
		}
		switch msg.Type {
		case "ready":
			ready = msg
			readyReceived = true
			if !sentCommands {
				if err := conn.WriteJSON(map[string]any{"type": "input", "data": "pwd\n"}); err != nil {
					return outputSeen, ready, exit, err
				}
				if err := conn.WriteJSON(map[string]any{"type": "input", "data": "rm -f " + cleanupPath + "\n"}); err != nil {
					return outputSeen, ready, exit, err
				}
				if err := conn.WriteJSON(map[string]any{"type": "input", "data": "exit\n"}); err != nil {
					return outputSeen, ready, exit, err
				}
				sentCommands = true
			}
		case "output":
			if strings.TrimSpace(msg.Data) != "" {
				outputSeen = true
			}
		case "error":
			return outputSeen, ready, exit, errors.New(msg.Message)
		case "exit":
			exit = msg
			if !readyReceived {
				return outputSeen, ready, exit, errors.New("terminal exited before ready")
			}
			return outputSeen, ready, exit, nil
		}
	}
}

func (s *smokeSession) cookieHeader() string {
	cookies := s.client.Jar.Cookies(s.baseURL)
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func resolveRemoteHost(state snapshot, requestedHostID string) (hostState, error) {
	if requestedHostID != "" {
		for _, host := range state.Hosts {
			if host.ID == requestedHostID {
				if host.Status != "online" || !host.Executable {
					return hostState{}, fmt.Errorf("remote host %s is not executable", requestedHostID)
				}
				return host, nil
			}
		}
		return hostState{}, fmt.Errorf("remote host %s not found", requestedHostID)
	}

	for _, host := range state.Hosts {
		if host.ID == "server-local" {
			continue
		}
		if host.Status == "online" && host.Executable {
			return host, nil
		}
	}
	return hostState{}, errors.New("no online executable remote host found; set AIOPS_REMOTE_HOST_ID explicitly")
}

func hasReadonlyCommandCards(state snapshot, hostID string, commands []string) bool {
	for _, command := range commands {
		card := findCommandCard(state, hostID, command)
		if card == nil {
			return false
		}
	}
	return true
}

func findCommandCard(state snapshot, hostID, command string) *cardState {
	for _, card := range state.Cards {
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

func findPendingRemoteApproval(state snapshot, hostID, command string) *approvalState {
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

func findPendingRemoteFileApproval(state snapshot, hostID, path string) *approvalState {
	for _, approval := range state.Approvals {
		if approval.Type != "remote_file_change" || approval.HostID != hostID || approval.Status != "pending" {
			continue
		}
		for _, change := range approval.Changes {
			if change.Path == path {
				copyApproval := approval
				return &copyApproval
			}
		}
	}
	return nil
}

func findFileChangeCard(state snapshot, path string) *cardState {
	for _, card := range state.Cards {
		if card.Type != "FileChangeCard" {
			continue
		}
		for _, change := range card.Changes {
			if change.Path == path {
				copyCard := card
				return &copyCard
			}
		}
	}
	return nil
}

func findResultSummaryCard(state snapshot, title string) *cardState {
	for _, card := range state.Cards {
		if card.Type == "ResultSummaryCard" && card.Title == title {
			copyCard := card
			return &copyCard
		}
	}
	return nil
}

func countResultSummaryCards(state snapshot) int {
	count := 0
	for _, card := range state.Cards {
		if card.Type == "ResultSummaryCard" {
			count++
		}
	}
	return count
}

func findApprovalByID(state snapshot, approvalID string) *approvalState {
	for _, approval := range state.Approvals {
		if approval.ID == approvalID {
			copyApproval := approval
			return &copyApproval
		}
	}
	return nil
}

func buildReadonlyPrompt(commands []string) string {
	lines := make([]string, 0, len(commands)+2)
	lines = append(lines, "当前目标主机是远程 Linux。请严格只使用 execute_readonly_query，不要使用本地 commandExecution，也不要改写下面命令：")
	for index, command := range commands {
		lines = append(lines, fmt.Sprintf("%d. `%s`", index+1, command))
	}
	lines = append(lines, "每条命令执行后保留输出，最后用一句话总结。")
	return strings.Join(lines, "\n")
}

func buildFileBrowsePrompt(browsePath, readPath, searchPath, query string) string {
	lines := []string{
		"当前目标主机是远程 Linux。请只使用远程文件工具，不要使用本地 fileSearch/fileRead，也不要用 commandExecution。",
		fmt.Sprintf("1. 先用 list_remote_files 列出 `%s`。", browsePath),
		fmt.Sprintf("2. 再用 read_remote_file 读取 `%s`。", readPath),
		fmt.Sprintf("3. 最后用 search_remote_files 在 `%s` 中搜索 `%s`。", searchPath, query),
		"每一步都保留结构化文件结果卡，最后再简短总结。",
	}
	return strings.Join(lines, "\n")
}

func buildFileChangePrompt(path, content, writeMode, reason string) string {
	return fmt.Sprintf(
		"当前目标主机是远程 Linux。请只使用 execute_system_mutation(mode=file_change) 修改远程文件，不要使用 shell echo/cat/tee，也不要改写路径或内容。\n目标路径：`%s`\nwrite_mode：`%s`\nreason：`%s`\n写入内容如下：\n```text\n%s```\n完成后等待审批。",
		path,
		writeMode,
		reason,
		content,
	)
}

func buildCommandApprovalPrompt(command, reason string) string {
	return fmt.Sprintf(
		"当前目标主机是远程 Linux。请只使用 execute_system_mutation(mode=command)，严格执行这条命令，不要改写：`%s`。原因写“%s”。",
		command,
		reason,
	)
}

func buildConfigSmokeContent(readPath, changePath, serviceName string) string {
	return strings.Join([]string{
		"# aiops smoke managed snippet",
		fmt.Sprintf("# source: %s", readPath),
		fmt.Sprintf("# target: %s", changePath),
		fmt.Sprintf("# service: %s", serviceName),
		fmt.Sprintf("# stamp: %d", time.Now().Unix()),
		"",
	}, "\n")
}

func websocketURL(base *url.URL, path string) string {
	relative, err := url.Parse(path)
	if err != nil {
		relative = &url.URL{Path: path}
	}
	target := base.ResolveReference(relative)
	switch target.Scheme {
	case "https":
		target.Scheme = "wss"
	default:
		target.Scheme = "ws"
	}
	return target.String()
}

func hostLabel(host hostState) string {
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
