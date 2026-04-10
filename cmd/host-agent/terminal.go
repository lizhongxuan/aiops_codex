package main

import (
	"bufio"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
)

var agentUserHomeDir = os.UserHomeDir

type agentStreamSender struct {
	stream agentrpc.AgentService_ConnectClient
	sendMu sync.Mutex
}

func (s *agentStreamSender) send(msg *agentrpc.Envelope) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.stream.Send(msg)
}

type agentTerminalSession struct {
	id        string
	cwd       string
	shell     string
	startedAt string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	exited    bool
	mu        sync.Mutex
}

type agentTerminalManager struct {
	sender    *agentStreamSender
	runtime   *hostAgentRuntime
	mu        sync.Mutex
	terminals map[string]*agentTerminalSession
}

func newAgentTerminalManager(sender *agentStreamSender, runtime ...*hostAgentRuntime) *agentTerminalManager {
	var hostRuntime *hostAgentRuntime
	if len(runtime) > 0 {
		hostRuntime = runtime[0]
	}
	return &agentTerminalManager{
		sender:    sender,
		runtime:   hostRuntime,
		terminals: make(map[string]*agentTerminalSession),
	}
}

func (m *agentTerminalManager) open(req *agentrpc.TerminalOpen) error {
	if req == nil || strings.TrimSpace(req.SessionID) == "" {
		return errors.New("terminal open requires sessionId")
	}
	if _, ok := m.session(req.SessionID); ok {
		return errors.New("terminal session already exists")
	}

	cwd, err := resolveAgentTerminalCwd(req.Cwd)
	if err != nil {
		return err
	}
	if m.runtime != nil && m.runtime.profile != nil {
		if err := m.runtime.profile.ensureCapabilityAllowed("terminal"); err != nil {
			return err
		}
		if !m.runtime.profile.allowShellWrapper() {
			return errors.New("terminal sessions require shell wrapper support by the current host-agent profile")
		}
		if err := m.runtime.profile.ensureWritableRoots([]string{cwd}); err != nil {
			return err
		}
	}
	shell := resolveAgentShell(req.Shell)
	scriptBinary, err := exec.LookPath("script")
	if err != nil {
		return errors.New("script binary not found; install util-linux in the container")
	}

	cmd := exec.Command(scriptBinary, "-q", "-f", "-c", shell+" -i", "/dev/null")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	session := &agentTerminalSession{
		id:        req.SessionID,
		cwd:       cwd,
		shell:     shell,
		startedAt: time.Now().UTC().Format(time.RFC3339),
		cmd:       cmd,
		stdin:     stdin,
	}

	m.mu.Lock()
	m.terminals[session.id] = session
	m.mu.Unlock()

	if err := m.sender.send(&agentrpc.Envelope{
		Kind: "terminal/ready",
		TerminalReady: &agentrpc.TerminalReady{
			SessionID: session.id,
			Cwd:       session.cwd,
			Shell:     session.shell,
			StartedAt: session.startedAt,
			Status:    "connected",
		},
	}); err != nil {
		log.Printf("send terminal ready failed: %v", err)
	}

	go m.streamOutput(session.id, stdout)
	go m.streamOutput(session.id, stderr)
	go m.waitExit(session)
	return nil
}

func (m *agentTerminalManager) input(req *agentrpc.TerminalInput) error {
	session, ok := m.session(req.SessionID)
	if !ok {
		return errors.New("terminal session not found")
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.exited {
		return errors.New("terminal session has already exited")
	}
	if session.stdin == nil {
		return errors.New("terminal input is unavailable")
	}
	_, err := io.WriteString(session.stdin, req.Data)
	return err
}

func (m *agentTerminalManager) signal(req *agentrpc.TerminalSignal) error {
	session, ok := m.session(req.SessionID)
	if !ok {
		return errors.New("terminal session not found")
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.exited {
		return errors.New("terminal session has already exited")
	}
	if session.cmd == nil || session.cmd.Process == nil {
		return errors.New("terminal process is unavailable")
	}

	switch strings.ToUpper(strings.TrimSpace(req.Signal)) {
	case "", "SIGINT":
		return syscall.Kill(-session.cmd.Process.Pid, syscall.SIGINT)
	case "SIGTERM":
		return syscall.Kill(-session.cmd.Process.Pid, syscall.SIGTERM)
	default:
		return errors.New("unsupported signal")
	}
}

func (m *agentTerminalManager) resize(req *agentrpc.TerminalResize) error {
	if _, ok := m.session(req.SessionID); !ok {
		return errors.New("terminal session not found")
	}
	return m.sender.send(&agentrpc.Envelope{
		Kind: "terminal/status",
		TerminalStatus: &agentrpc.TerminalStatus{
			SessionID: req.SessionID,
			Status:    "connected",
		},
	})
}

func (m *agentTerminalManager) close(req *agentrpc.TerminalClose) error {
	if req == nil {
		return errors.New("terminal close requires payload")
	}
	return m.closeSession(req.SessionID)
}

func (m *agentTerminalManager) closeSession(sessionID string) error {
	session, ok := m.session(sessionID)
	if !ok {
		return nil
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.cmd != nil && session.cmd.Process != nil && !session.exited {
		_ = syscall.Kill(-session.cmd.Process.Pid, syscall.SIGHUP)
	}
	if session.stdin != nil {
		_ = session.stdin.Close()
		session.stdin = nil
	}
	session.exited = true

	m.mu.Lock()
	delete(m.terminals, sessionID)
	m.mu.Unlock()
	return nil
}

func (m *agentTerminalManager) shutdownAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.terminals))
	for id := range m.terminals {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, sessionID := range ids {
		_ = m.closeSession(sessionID)
	}
}

func (m *agentTerminalManager) streamOutput(sessionID string, reader io.Reader) {
	bufReader := bufio.NewReader(reader)
	buf := make([]byte, 4096)
	for {
		n, err := bufReader.Read(buf)
		if n > 0 {
			if sendErr := m.sender.send(&agentrpc.Envelope{
				Kind: "terminal/output",
				TerminalOutput: &agentrpc.TerminalOutput{
					SessionID: sessionID,
					Data:      sanitizeTerminalChunk(string(buf[:n])),
				},
			}); sendErr != nil {
				log.Printf("send terminal output failed: %v", sendErr)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = m.sender.send(&agentrpc.Envelope{
					Kind: "terminal/status",
					TerminalStatus: &agentrpc.TerminalStatus{
						SessionID: sessionID,
						Status:    "error",
						Message:   err.Error(),
					},
				})
			}
			return
		}
	}
}

func (m *agentTerminalManager) waitExit(session *agentTerminalSession) {
	exitCode := 0
	if err := session.cmd.Wait(); err != nil {
		exitCode = terminalExitCode(err)
	}

	session.mu.Lock()
	session.exited = true
	if session.stdin != nil {
		_ = session.stdin.Close()
		session.stdin = nil
	}
	session.mu.Unlock()

	m.mu.Lock()
	delete(m.terminals, session.id)
	m.mu.Unlock()

	if err := m.sender.send(&agentrpc.Envelope{
		Kind: "terminal/exit",
		TerminalExit: &agentrpc.TerminalExit{
			SessionID: session.id,
			Code:      exitCode,
			Status:    "disconnected",
		},
	}); err != nil {
		log.Printf("send terminal exit failed: %v", err)
	}
}

func (m *agentTerminalManager) session(sessionID string) (*agentTerminalSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.terminals[sessionID]
	return session, ok
}

func resolveAgentShell(requested string) string {
	candidates := []string{strings.TrimSpace(requested), strings.TrimSpace(os.Getenv("SHELL")), "/bin/bash", "/bin/sh"}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "/bin/sh"
}

func resolveAgentTerminalCwd(requested string) (string, error) {
	cwd := strings.TrimSpace(requested)
	if cwd != "" && cwd != "~" && filepath.IsAbs(cwd) {
		resolved, err := filepath.Abs(filepath.Clean(cwd))
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

	home, err := agentUserHomeDir()
	if err != nil || home == "" {
		home = "/tmp"
	}

	if cwd == "" || cwd == "~" {
		cwd = home
	}
	if strings.HasPrefix(cwd, "~/") {
		cwd = filepath.Join(home, strings.TrimPrefix(cwd, "~/"))
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(home, cwd)
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
