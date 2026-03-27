package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
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
	sender *agentStreamSender

	mu    sync.Mutex
	execs map[string]*agentExecSession
}

func newAgentExecManager(sender *agentStreamSender) *agentExecManager {
	return &agentExecManager{
		sender: sender,
		execs:  make(map[string]*agentExecSession),
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

	cwd, err := resolveAgentTerminalCwd(req.Cwd)
	if err != nil {
		return err
	}
	shell := resolveAgentShell(req.Shell)
	timeout := clampAgentExecTimeout(req.TimeoutSec, req.Readonly)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	cmd := exec.CommandContext(ctx, shell, "-lc", req.Command)
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
