package server

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

type testAgentAddr string

func (a testAgentAddr) Network() string { return "tcp" }

func (a testAgentAddr) String() string { return string(a) }

type scriptedAgentConnectServer struct {
	ctx    context.Context
	mu     sync.Mutex
	recv   []*agentrpc.Envelope
	sent   []*agentrpc.Envelope
	header metadata.MD
}

func newScriptedAgentConnectServer(remoteAddr string, messages ...*agentrpc.Envelope) *scriptedAgentConnectServer {
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: testAgentAddr(remoteAddr)})
	return &scriptedAgentConnectServer{
		ctx:  ctx,
		recv: append([]*agentrpc.Envelope(nil), messages...),
	}
}

func (s *scriptedAgentConnectServer) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.header = md
	return nil
}

func (s *scriptedAgentConnectServer) SendHeader(metadata.MD) error { return nil }

func (s *scriptedAgentConnectServer) SetTrailer(metadata.MD) {}

func (s *scriptedAgentConnectServer) Context() context.Context { return s.ctx }

func (s *scriptedAgentConnectServer) Send(msg *agentrpc.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	return nil
}

func (s *scriptedAgentConnectServer) Recv() (*agentrpc.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recv) == 0 {
		return nil, io.EOF
	}
	msg := s.recv[0]
	s.recv = s.recv[1:]
	return msg, nil
}

func (s *scriptedAgentConnectServer) SendMsg(any) error { return nil }

func (s *scriptedAgentConnectServer) RecvMsg(any) error { return io.EOF }

func (s *scriptedAgentConnectServer) messages() []*agentrpc.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*agentrpc.Envelope(nil), s.sent...)
}

func testAgentRegistrationConfig() config.Config {
	return config.Config{
		HostAgentBootstrapTokens: []string{"bootstrap-token"},
		AllowedAgentHostIDs:      []string{"linux-01"},
		AllowedAgentCIDRs:        []string{"10.0.0.0/8"},
	}
}

func testRegistrationEnvelope(hostID, token string) *agentrpc.Envelope {
	return &agentrpc.Envelope{
		Kind: "register",
		Registration: &agentrpc.Registration{
			Token:        token,
			HostID:       hostID,
			Hostname:     "build-node-01",
			OS:           "linux",
			Arch:         "amd64",
			AgentVersion: "1.2.3",
			Labels: map[string]string{
				"role": "worker",
			},
		},
	}
}

func requireSingleErrorEnvelope(t *testing.T, messages []*agentrpc.Envelope) string {
	t.Helper()
	if len(messages) != 1 {
		t.Fatalf("expected 1 envelope, got %#v", messages)
	}
	if messages[0].Kind != "error" {
		t.Fatalf("expected error envelope, got %#v", messages[0])
	}
	return messages[0].Error
}

func TestConnectRejectsNonAllowlistedSource(t *testing.T) {
	app := New(testAgentRegistrationConfig())
	stream := newScriptedAgentConnectServer("203.0.113.10:5555", testRegistrationEnvelope("linux-01", "bootstrap-token"))

	err := app.Connect(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	if got := requireSingleErrorEnvelope(t, stream.messages()); !strings.Contains(got, "not allowed") {
		t.Fatalf("expected source rejection, got %q", got)
	}
	if _, ok := app.store.Host("linux-01"); ok {
		t.Fatalf("expected rejected host to stay absent")
	}
}

func TestConnectRejectsWrongBootstrapToken(t *testing.T) {
	app := New(testAgentRegistrationConfig())
	stream := newScriptedAgentConnectServer("10.1.2.3:5555", testRegistrationEnvelope("linux-01", "wrong-token"))

	err := app.Connect(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	if got := requireSingleErrorEnvelope(t, stream.messages()); got != "invalid bootstrap token" {
		t.Fatalf("expected bootstrap token rejection, got %q", got)
	}
	if _, ok := app.store.Host("linux-01"); ok {
		t.Fatalf("expected rejected host to stay absent")
	}
}

func TestConnectRejectsDisallowedHostID(t *testing.T) {
	app := New(testAgentRegistrationConfig())
	stream := newScriptedAgentConnectServer("10.1.2.3:5555", testRegistrationEnvelope("linux-02", "bootstrap-token"))

	err := app.Connect(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	if got := requireSingleErrorEnvelope(t, stream.messages()); got != "host id is not allowed" {
		t.Fatalf("expected host allowlist rejection, got %q", got)
	}
	if _, ok := app.store.Host("linux-02"); ok {
		t.Fatalf("expected rejected host to stay absent")
	}
}

func TestConnectRejectsEmptyHostID(t *testing.T) {
	app := New(testAgentRegistrationConfig())
	stream := newScriptedAgentConnectServer("10.1.2.3:5555", testRegistrationEnvelope("   ", "bootstrap-token"))

	err := app.Connect(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	if got := requireSingleErrorEnvelope(t, stream.messages()); got != "host id is required" {
		t.Fatalf("expected empty host id rejection, got %q", got)
	}
}

func TestConnectRegistersHostAndAck(t *testing.T) {
	app := New(testAgentRegistrationConfig())
	stream := newScriptedAgentConnectServer("10.1.2.3:5555", testRegistrationEnvelope("linux-01", "bootstrap-token"))

	err := app.Connect(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	messages := stream.messages()
	if len(messages) != 1 || messages[0].Kind != "ack" {
		t.Fatalf("expected ack envelope, got %#v", messages)
	}
	if messages[0].Ack == nil || messages[0].Ack.Message != "registered" {
		t.Fatalf("expected registered ack, got %#v", messages[0])
	}

	host, ok := app.store.Host("linux-01")
	if !ok {
		t.Fatalf("expected host to be written")
	}
	if host.ID != "linux-01" || host.Name != "build-node-01" || host.Kind != "agent" {
		t.Fatalf("expected registered host metadata, got %#v", host)
	}
	if !host.Executable || !host.TerminalCapable {
		t.Fatalf("expected host to be executable and terminal-capable, got %#v", host)
	}
	if host.OS != "linux" || host.Arch != "amd64" || host.AgentVersion != "1.2.3" {
		t.Fatalf("expected host runtime metadata to be written, got %#v", host)
	}
	if host.LastHeartbeat == "" {
		t.Fatalf("expected last heartbeat to be populated")
	}
}

func TestConnectRejectsHostIdentityDrift(t *testing.T) {
	app := New(testAgentRegistrationConfig())
	stream := newScriptedAgentConnectServer(
		"10.1.2.3:5555",
		testRegistrationEnvelope("linux-01", "bootstrap-token"),
		&agentrpc.Envelope{
			Kind: "heartbeat",
			Heartbeat: &agentrpc.Heartbeat{
				HostID: "linux-evil",
			},
		},
	)

	err := app.Connect(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	messages := stream.messages()
	if len(messages) != 2 {
		t.Fatalf("expected ack plus identity error, got %#v", messages)
	}
	if messages[0].Kind != "ack" {
		t.Fatalf("expected registration ack first, got %#v", messages[0])
	}
	if messages[1].Kind != "error" || messages[1].Error != "host identity mismatch" {
		t.Fatalf("expected identity mismatch error, got %#v", messages[1])
	}

	if _, ok := app.store.Host("linux-evil"); ok {
		t.Fatalf("expected drifting host identity to be rejected")
	}
	host, ok := app.store.Host("linux-01")
	if !ok {
		t.Fatalf("expected original host to remain registered")
	}
	if host.ID != "linux-01" {
		t.Fatalf("expected original host id to remain fixed, got %#v", host)
	}
}
