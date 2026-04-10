package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"google.golang.org/grpc/metadata"
)

type fakeAgentConnectClient struct {
	mu       sync.Mutex
	messages []*agentrpc.Envelope
}

func (f *fakeAgentConnectClient) Send(msg *agentrpc.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeAgentConnectClient) Recv() (*agentrpc.Envelope, error) {
	return nil, io.EOF
}

func (f *fakeAgentConnectClient) Header() (metadata.MD, error) {
	return metadata.MD{}, nil
}

func (f *fakeAgentConnectClient) Trailer() metadata.MD {
	return metadata.MD{}
}

func (f *fakeAgentConnectClient) CloseSend() error {
	return nil
}

func (f *fakeAgentConnectClient) Context() context.Context {
	return context.Background()
}

func (f *fakeAgentConnectClient) SendMsg(any) error {
	return nil
}

func (f *fakeAgentConnectClient) RecvMsg(any) error {
	return io.EOF
}

func (f *fakeAgentConnectClient) waitForExit(t *testing.T, timeout time.Duration) *agentrpc.ExecExit {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		messages := append([]*agentrpc.Envelope(nil), f.messages...)
		f.mu.Unlock()
		for _, msg := range messages {
			if msg.ExecExit != nil {
				return msg.ExecExit
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for exec exit")
	return nil
}

func (f *fakeAgentConnectClient) outputStreams() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	streams := make([]string, 0, len(f.messages))
	for _, msg := range f.messages {
		if msg.ExecOutput != nil {
			streams = append(streams, msg.ExecOutput.Stream)
		}
	}
	return streams
}

func TestAgentExecManagerStreamsStructuredExecOutputAndExit(t *testing.T) {
	stream := &fakeAgentConnectClient{}
	manager := newAgentExecManager(&agentStreamSender{stream: stream})

	if err := manager.start(&agentrpc.ExecStart{
		ExecID:   "exec-1",
		Command:  "printf 'stdout-line\\n'; printf 'stderr-line\\n' >&2; exit 7",
		Cwd:      "~",
		Shell:    "/bin/sh",
		Readonly: true,
	}); err != nil {
		t.Fatalf("start exec: %v", err)
	}

	exit := stream.waitForExit(t, 5*time.Second)
	if exit.ExitCode != 7 {
		t.Fatalf("expected exitCode 7, got %d", exit.ExitCode)
	}
	if exit.Status != "failed" {
		t.Fatalf("expected failed status, got %q", exit.Status)
	}
	if exit.Stdout != "stdout-line\n" {
		t.Fatalf("unexpected stdout %q", exit.Stdout)
	}
	if exit.Stderr != "stderr-line\n" {
		t.Fatalf("unexpected stderr %q", exit.Stderr)
	}
	if exit.Error == "" {
		t.Fatalf("expected non-empty error text")
	}

	streams := stream.outputStreams()
	if len(streams) < 2 {
		t.Fatalf("expected both stdout and stderr output envelopes, got %v", streams)
	}
	foundStdout := false
	foundStderr := false
	for _, streamName := range streams {
		if streamName == "stdout" {
			foundStdout = true
		}
		if streamName == "stderr" {
			foundStderr = true
		}
	}
	if !foundStdout || !foundStderr {
		t.Fatalf("expected stdout/stderr streams, got %v", streams)
	}
}

func TestBuildAgentExecCommandUsesNonLoginShellWrapper(t *testing.T) {
	cmd, err := buildAgentExecCommand(context.Background(), "uptime", "/bin/sh", true)
	if err != nil {
		t.Fatalf("buildAgentExecCommand: %v", err)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("expected 3 args, got %#v", cmd.Args)
	}
	if got := cmd.Args[1]; got != "-c" {
		t.Fatalf("expected non-login shell flag -c, got %q", got)
	}
	if got := cmd.Args[2]; got != "uptime" {
		t.Fatalf("unexpected command payload %q", got)
	}
}

func TestResolveAgentExecCwdDefaultsToTmp(t *testing.T) {
	cwd, err := resolveAgentExecCwd("")
	if err != nil {
		t.Fatalf("resolveAgentExecCwd: %v", err)
	}
	if cwd != "/tmp" {
		t.Fatalf("expected /tmp, got %q", cwd)
	}
}

func TestResolveAgentExecCwdResolvesRelativePathFromHome(t *testing.T) {
	home := t.TempDir()
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	t.Setenv("HOME", home)

	cwd, err := resolveAgentExecCwd("workspace")
	if err != nil {
		t.Fatalf("resolveAgentExecCwd relative: %v", err)
	}
	if cwd != workspace {
		t.Fatalf("expected %q, got %q", workspace, cwd)
	}
}
