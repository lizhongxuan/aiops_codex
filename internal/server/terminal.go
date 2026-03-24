package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type terminalCreateRequest struct {
	HostID string `json:"hostId"`
	Cwd    string `json:"cwd"`
	Shell  string `json:"shell"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

type terminalCreateResponse struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
	StartedAt string `json:"startedAt"`
}

type terminalEnvelope struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
	Code      int    `json:"code,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Shell     string `json:"shell,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

type terminalClientMessage struct {
	Type   string `json:"type"`
	Data   string `json:"data,omitempty"`
	Signal string `json:"signal,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

type terminalSession struct {
	ID             string
	OwnerSessionID string
	HostID         string
	Cwd            string
	Shell          string
	StartedAt      string
	Status         string
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	subscribers    map[*websocket.Conn]struct{}
	exited         bool
	mu             sync.Mutex
}

func (a *App) handleTerminalCreate(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req terminalCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.HostID == "" {
		req.HostID = model.ServerLocalHostID
	}
	if req.HostID != model.ServerLocalHostID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "MVP only supports server-local terminal"})
		return
	}

	host := a.findHost(req.HostID)
	if !host.Executable || host.Status != "online" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selected host is not available for terminal access"})
		return
	}

	terminal, err := a.createTerminalSession(sessionID, req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	a.audit("terminal.created", map[string]any{
		"sessionId":  sessionID,
		"terminalId": terminal.ID,
		"hostId":     terminal.HostID,
		"cwd":        terminal.Cwd,
		"shell":      terminal.Shell,
	})
	writeJSON(w, http.StatusOK, terminalCreateResponse{
		SessionID: terminal.ID,
		Cwd:       terminal.Cwd,
		Shell:     terminal.Shell,
		StartedAt: terminal.StartedAt,
	})
}

func (a *App) handleTerminalWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	terminalID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	if terminalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId is required"})
		return
	}

	terminal, ok := a.terminalSession(terminalID)
	if !ok || terminal.OwnerSessionID != sessionID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "terminal session not found"})
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	a.addTerminalSubscriber(terminal, conn)
	a.writeTerminalJSON(conn, terminalEnvelope{
		Type:      "ready",
		SessionID: terminal.ID,
		Status:    terminal.Status,
		Cwd:       terminal.Cwd,
		Shell:     terminal.Shell,
		StartedAt: terminal.StartedAt,
	})

	defer func() {
		a.removeTerminalSubscriber(terminal, conn)
		_ = conn.Close()
	}()

	for {
		var msg terminalClientMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "input":
			if err := a.writeTerminalInput(terminal, msg.Data); err != nil {
				a.writeTerminalJSON(conn, terminalEnvelope{Type: "error", Message: err.Error()})
			}
		case "signal":
			if err := a.signalTerminal(terminal, msg.Signal); err != nil {
				a.writeTerminalJSON(conn, terminalEnvelope{Type: "error", Message: err.Error()})
			}
		case "resize":
			a.broadcastTerminal(terminal.ID, terminalEnvelope{Type: "status", Status: terminal.Status})
		}
	}
}

func (a *App) createTerminalSession(ownerSessionID string, req terminalCreateRequest) (*terminalSession, error) {
	cwd, err := resolveTerminalCwd(req.Cwd, a.cfg.DefaultWorkspace)
	if err != nil {
		return nil, err
	}
	shell := strings.TrimSpace(req.Shell)
	if shell == "" {
		shell = "/bin/zsh"
	}
	if _, err := os.Stat(shell); err != nil {
		return nil, errors.New("shell not found")
	}

	cmd := exec.Command("/usr/bin/script", "-q", "/dev/null", shell, "-i")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	terminal := &terminalSession{
		ID:             model.NewID("term"),
		OwnerSessionID: ownerSessionID,
		HostID:         req.HostID,
		Cwd:            cwd,
		Shell:          shell,
		StartedAt:      model.NowString(),
		Status:         "connected",
		cmd:            cmd,
		stdin:          stdin,
		subscribers:    make(map[*websocket.Conn]struct{}),
	}

	a.terminalMu.Lock()
	a.terminals[terminal.ID] = terminal
	a.terminalMu.Unlock()

	go a.streamTerminalOutput(terminal, stdout)
	go a.streamTerminalOutput(terminal, stderr)
	go a.waitTerminalExit(terminal)
	go a.expireUnusedTerminalSession(terminal.ID, 20*time.Second)

	return terminal, nil
}

func (a *App) expireUnusedTerminalSession(terminalID string, ttl time.Duration) {
	timer := time.NewTimer(ttl)
	defer timer.Stop()

	<-timer.C
	terminal, ok := a.terminalSession(terminalID)
	if !ok {
		return
	}

	terminal.mu.Lock()
	noSubscribers := len(terminal.subscribers) == 0
	terminal.mu.Unlock()
	if noSubscribers {
		a.shutdownTerminalSession(terminalID)
	}
}

func (a *App) streamTerminalOutput(terminal *terminalSession, reader io.Reader) {
	bufReader := bufio.NewReader(reader)
	buf := make([]byte, 4096)
	for {
		n, err := bufReader.Read(buf)
		if n > 0 {
			a.broadcastTerminal(terminal.ID, terminalEnvelope{
				Type: "output",
				Data: sanitizeTerminalChunk(string(buf[:n])),
			})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				a.broadcastTerminal(terminal.ID, terminalEnvelope{
					Type:    "error",
					Message: err.Error(),
				})
			}
			return
		}
	}
}

func (a *App) waitTerminalExit(terminal *terminalSession) {
	exitCode := 0
	if err := terminal.cmd.Wait(); err != nil {
		exitCode = terminalExitCode(err)
	}

	terminal.mu.Lock()
	terminal.exited = true
	terminal.Status = "disconnected"
	terminal.mu.Unlock()

	a.broadcastTerminal(terminal.ID, terminalEnvelope{
		Type:   "exit",
		Code:   exitCode,
		Status: "disconnected",
	})
}

func (a *App) writeTerminalInput(terminal *terminalSession, data string) error {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	if terminal.exited {
		return errors.New("terminal session has already exited")
	}
	if terminal.stdin == nil {
		return errors.New("terminal input is unavailable")
	}
	_, err := io.WriteString(terminal.stdin, data)
	return err
}

func (a *App) signalTerminal(terminal *terminalSession, signalName string) error {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	if terminal.cmd == nil || terminal.cmd.Process == nil {
		return errors.New("terminal process is unavailable")
	}
	if terminal.exited {
		return errors.New("terminal session has already exited")
	}
	switch strings.ToUpper(strings.TrimSpace(signalName)) {
	case "", "SIGINT":
		return syscall.Kill(-terminal.cmd.Process.Pid, syscall.SIGINT)
	case "SIGTERM":
		return syscall.Kill(-terminal.cmd.Process.Pid, syscall.SIGTERM)
	default:
		return errors.New("unsupported signal")
	}
}

func (a *App) terminalSession(terminalID string) (*terminalSession, bool) {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	terminal, ok := a.terminals[terminalID]
	return terminal, ok
}

func (a *App) addTerminalSubscriber(terminal *terminalSession, conn *websocket.Conn) {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	terminal.subscribers[conn] = struct{}{}
}

func (a *App) removeTerminalSubscriber(terminal *terminalSession, conn *websocket.Conn) {
	shouldClose := false
	terminal.mu.Lock()
	delete(terminal.subscribers, conn)
	if len(terminal.subscribers) == 0 {
		shouldClose = true
	}
	terminal.mu.Unlock()
	if shouldClose {
		a.shutdownTerminalSession(terminal.ID)
	}
}

func (a *App) shutdownTerminalSession(terminalID string) {
	terminal, ok := a.terminalSession(terminalID)
	if !ok {
		return
	}

	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	if terminal.cmd != nil && terminal.cmd.Process != nil && !terminal.exited {
		_ = syscall.Kill(-terminal.cmd.Process.Pid, syscall.SIGHUP)
	}
	if terminal.stdin != nil {
		_ = terminal.stdin.Close()
		terminal.stdin = nil
	}
	a.terminalMu.Lock()
	delete(a.terminals, terminalID)
	a.terminalMu.Unlock()
}

func (a *App) broadcastTerminal(terminalID string, payload terminalEnvelope) {
	terminal, ok := a.terminalSession(terminalID)
	if !ok {
		return
	}

	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	if payload.Status != "" {
		terminal.Status = payload.Status
	}
	for conn := range terminal.subscribers {
		if err := a.writeTerminalJSON(conn, payload); err != nil {
			_ = conn.Close()
			delete(terminal.subscribers, conn)
		}
	}
}

func (a *App) writeTerminalJSON(conn *websocket.Conn, payload terminalEnvelope) error {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(payload)
}

func resolveTerminalCwd(requested, fallback string) (string, error) {
	cwd := strings.TrimSpace(requested)
	if cwd == "" || cwd == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home, nil
		}
		return filepath.Abs(fallback)
	}
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			cwd = filepath.Join(home, strings.TrimPrefix(cwd, "~/"))
		}
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(fallback, cwd)
	}
	resolved, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("cwd must be a directory")
	}
	return resolved, nil
}

func sanitizeTerminalChunk(chunk string) string {
	return strings.ReplaceAll(chunk, "^D\b\b", "")
}

func terminalExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func (a *App) stopAllTerminals(ctx context.Context) {
	a.terminalMu.Lock()
	ids := make([]string, 0, len(a.terminals))
	for id := range a.terminals {
		ids = append(ids, id)
	}
	a.terminalMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, id := range ids {
			a.shutdownTerminalSession(id)
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}
