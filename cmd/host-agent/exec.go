package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
)

type agentExecSession struct {
	id      string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	doneCtx context.Context

	mu        sync.Mutex
	cancelled bool
	stdout    string
	stderr    string
}

type agentExecManager struct {
	sender  *agentStreamSender
	runtime *hostAgentRuntime

	mu    sync.Mutex
	execs map[string]*agentExecSession
}

func newAgentExecManager(sender *agentStreamSender, runtime ...*hostAgentRuntime) *agentExecManager {
	var hostRuntime *hostAgentRuntime
	if len(runtime) > 0 {
		hostRuntime = runtime[0]
	}
	return &agentExecManager{
		sender:  sender,
		runtime: hostRuntime,
		execs:   make(map[string]*agentExecSession),
	}
}

func (m *agentExecManager) start(req *agentrpc.ExecStart) error {
	if req == nil || req.ExecID == "" {
		return errors.New("exec start requires execId")
	}
	if req.Command == "" {
		return errors.New("exec start requires command")
	}
	if _, ok := m.session(req.ExecID); ok {
		return errors.New("exec session already exists")
	}

	cwd, err := resolveAgentExecCwd(req.Cwd)
	if err != nil {
		return err
	}
	decision := commandPolicyDecision{}
	if m.runtime != nil && m.runtime.profile != nil {
		decision, err = m.runtime.profile.commandPolicy(req.Command)
		if err != nil {
			return err
		}
		if isMutationCommandCategory(decision.Category) {
			if err := m.runtime.profile.ensureWritableRoots([]string{cwd}); err != nil {
				return err
			}
		}
	}
	timeout := clampAgentExecTimeout(req.TimeoutSec, req.Readonly)
	if m.runtime != nil && m.runtime.profile != nil {
		timeout = m.runtime.profile.effectiveCommandTimeoutSeconds(req.TimeoutSec, req.Readonly)
	}
	allowShellWrapper := m.runtime == nil || m.runtime.profile == nil || m.runtime.profile.allowShellWrapper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	cmd, err := buildAgentExecCommand(ctx, req.Command, req.Shell, allowShellWrapper)
	if err != nil {
		cancel()
		return err
	}
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	log.Printf(
		"exec started exec=%s pid=%d readonly=%t timeout=%ds cwd=%q category=%q mode=%q shell_wrapper=%t argv=%q",
		req.ExecID,
		cmd.Process.Pid,
		req.Readonly,
		timeout,
		cwd,
		decision.Category,
		decision.Mode,
		allowShellWrapper,
		cmd.Args,
	)

	session := &agentExecSession{
		id:      req.ExecID,
		cmd:     cmd,
		cancel:  cancel,
		doneCtx: ctx,
	}

	m.mu.Lock()
	m.execs[session.id] = session
	m.mu.Unlock()

	go m.streamOutput(session, "stdout", stdout)
	go m.streamOutput(session, "stderr", stderr)
	go m.waitExit(session)
	return nil
}

func (m *agentExecManager) cancel(req *agentrpc.ExecCancel) error {
	if req == nil || req.ExecID == "" {
		return errors.New("exec cancel requires execId")
	}
	session, ok := m.session(req.ExecID)
	if !ok {
		return nil
	}

	session.mu.Lock()
	session.cancelled = true
	cmd := session.cmd
	cancel := session.cancel
	session.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		log.Printf("exec cancelling exec=%s pid=%d", req.ExecID, cmd.Process.Pid)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	return nil
}

func (m *agentExecManager) shutdownAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.execs))
	for id := range m.execs {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.cancel(&agentrpc.ExecCancel{ExecID: id})
	}
}

func (m *agentExecManager) streamOutput(session *agentExecSession, stream string, reader io.Reader) {
	bufReader := bufio.NewReader(reader)
	buf := make([]byte, 4096)
	for {
		n, err := bufReader.Read(buf)
		if n > 0 {
			chunk := sanitizeTerminalChunk(string(buf[:n]))
			if chunk == "" {
				continue
			}
			session.mu.Lock()
			if stream == "stderr" {
				session.stderr += chunk
			} else {
				session.stdout += chunk
			}
			session.mu.Unlock()
			log.Printf("exec output exec=%s stream=%s bytes=%d data=%q", session.id, stream, len(chunk), summarizeLogText(chunk, 320))
			if sendErr := m.sender.send(&agentrpc.Envelope{
				Kind: "exec/output",
				ExecOutput: &agentrpc.ExecOutput{
					ExecID: session.id,
					Stream: stream,
					Data:   chunk,
				},
			}); sendErr != nil {
				log.Printf("send exec output failed: %v", sendErr)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("exec output stream error exec=%s err=%v", session.id, err)
			}
			return
		}
	}
}

func (m *agentExecManager) waitExit(session *agentExecSession) {
	exitCode := 0
	status := "completed"
	message := ""
	errorText := ""

	if err := session.cmd.Wait(); err != nil {
		exitCode = terminalExitCode(err)
		status = "failed"
		errorText = err.Error()
	}

	session.mu.Lock()
	cancelled := session.cancelled
	ctxErr := session.doneCtx.Err()
	cancel := session.cancel
	stdout := session.stdout
	stderr := session.stderr
	session.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if ctxErr == context.DeadlineExceeded {
		status = "timeout"
		message = "command timed out"
		if exitCode == 0 {
			exitCode = 124
		}
	} else if cancelled {
		status = "cancelled"
		message = "command cancelled"
		if exitCode == 0 {
			exitCode = 130
		}
	} else if exitCode != 0 {
		status = "failed"
	}

	m.mu.Lock()
	delete(m.execs, session.id)
	m.mu.Unlock()

	if err := m.sender.send(&agentrpc.Envelope{
		Kind: "exec/exit",
		ExecExit: &agentrpc.ExecExit{
			ExecID:    session.id,
			Code:      exitCode,
			ExitCode:  exitCode,
			Status:    status,
			Message:   message,
			Stdout:    stdout,
			Stderr:    stderr,
			Timeout:   status == "timeout",
			Cancelled: status == "cancelled",
			Error:     errorText,
		},
	}); err != nil {
		log.Printf("send exec exit failed: %v", err)
	}
	log.Printf(
		"exec finished exec=%s status=%s exit_code=%d timeout=%t cancelled=%t error=%q stdout=%q stderr=%q",
		session.id,
		status,
		exitCode,
		status == "timeout",
		status == "cancelled",
		errorText,
		summarizeLogText(stdout, 400),
		summarizeLogText(stderr, 400),
	)
}

func (m *agentExecManager) session(execID string) (*agentExecSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.execs[execID]
	return session, ok
}

func clampAgentExecTimeout(timeoutSec int, readonly bool) int {
	if readonly {
		if timeoutSec <= 0 {
			return 60
		}
		if timeoutSec > 120 {
			return 120
		}
		return timeoutSec
	}
	if timeoutSec <= 0 {
		return 180
	}
	if timeoutSec > 600 {
		return 600
	}
	return timeoutSec
}

func buildAgentExecCommand(ctx context.Context, command, shell string, allowShellWrapper bool) (*exec.Cmd, error) {
	if allowShellWrapper {
		resolvedShell := resolveAgentShell(shell)
		return exec.CommandContext(ctx, resolvedShell, "-c", command), nil
	}
	if hostAgentCommandUsesShellWrapper(command) {
		return nil, errors.New("shell wrapper commands are disabled by the current host-agent profile")
	}
	args, err := parseAgentDirectCommand(command)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, args[0], args[1:]...), nil
}

func parseAgentDirectCommand(command string) ([]string, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil, errors.New("exec start requires command")
	}
	if strings.ContainsAny(trimmed, "|;&<>`$()") {
		return nil, errors.New("shell wrapper commands are disabled by the current host-agent profile")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil, errors.New("exec start requires command")
	}
	return fields, nil
}

func isMutationCommandCategory(category string) bool {
	switch category {
	case "service_mutation", "filesystem_mutation", "package_mutation":
		return true
	default:
		return false
	}
}

func resolveAgentExecCwd(requested string) (string, error) {
	cwd := strings.TrimSpace(requested)
	if cwd == "" || cwd == "~" {
		cwd = "/tmp"
	}
	return resolveAgentTerminalCwd(cwd)
}
