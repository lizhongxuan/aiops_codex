package store

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestUpsertHostPreservesInventoryFieldsAcrossAgentRegistration(t *testing.T) {
	st := New()
	st.UpsertHost(model.Host{
		ID:           "web-01",
		Name:         "web-01",
		Kind:         "inventory",
		Address:      "10.0.0.21",
		SSHUser:      "ubuntu",
		SSHPort:      2222,
		InstallState: "pending_install",
		Status:       "pending_install",
		Labels: map[string]string{
			"env":  "prod",
			"role": "web",
		},
	})

	st.UpsertHost(model.Host{
		ID:              "web-01",
		Name:            "web-01.internal",
		Kind:            "agent",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
		AgentVersion:    "0.1.0",
	})

	host, ok := st.Host("web-01")
	if !ok {
		t.Fatalf("expected host to exist")
	}
	if host.Address != "10.0.0.21" {
		t.Fatalf("expected address to be preserved, got %q", host.Address)
	}
	if host.SSHUser != "ubuntu" || host.SSHPort != 2222 {
		t.Fatalf("expected ssh metadata to be preserved, got user=%q port=%d", host.SSHUser, host.SSHPort)
	}
	if !host.Executable || !host.TerminalCapable || host.Status != "online" {
		t.Fatalf("expected host to become remotely executable, got %#v", host)
	}
	if host.InstallState != "pending_install" {
		t.Fatalf("expected explicit install state to be preserved until installer updates it, got %q", host.InstallState)
	}
	if host.Transport != "grpc_reverse" {
		t.Fatalf("expected transport to become grpc_reverse, got %q", host.Transport)
	}
}

func TestHostSessionsBuildTaskAndReplySummaries(t *testing.T) {
	st := New()
	sessionID := "sess-host-1"
	st.EnsureSession(sessionID)
	st.SetSelectedHost(sessionID, "web-01")
	st.UpsertCard(sessionID, model.Card{
		ID:        "user-1",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "检查 nginx 配置并在异常时回滚",
		CreatedAt: "2026-03-27T10:00:00Z",
		UpdatedAt: "2026-03-27T10:00:00Z",
	})
	st.UpsertCard(sessionID, model.Card{
		ID:        "assistant-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "已检查 nginx -t，通过后准备 reload。",
		CreatedAt: "2026-03-27T10:00:10Z",
		UpdatedAt: "2026-03-27T10:00:10Z",
	})
	st.UpsertCard(sessionID, model.Card{
		ID:        "result-1",
		Type:      "ResultSummaryCard",
		Summary:   "reload 已完成，健康检查全部通过。",
		CreatedAt: "2026-03-27T10:00:20Z",
		UpdatedAt: "2026-03-27T10:00:20Z",
	})

	items := st.HostSessions("web-01", 10)
	if len(items) != 1 {
		t.Fatalf("expected exactly one host session summary, got %d", len(items))
	}
	got := items[0]
	if got.TaskSummary != "检查 nginx 配置并在异常时回滚" {
		t.Fatalf("unexpected task summary %q", got.TaskSummary)
	}
	if got.ReplySummary != "reload 已完成，健康检查全部通过。" {
		t.Fatalf("unexpected reply summary %q", got.ReplySummary)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("expected 3 message excerpts, got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[1].Role != "assistant" || got.Messages[2].Role != "system" {
		t.Fatalf("unexpected roles in message excerpts: %#v", got.Messages)
	}
}

func TestBatchUpdateHostLabelsAddsAndRemovesKeys(t *testing.T) {
	st := New()
	st.UpsertHost(model.Host{
		ID:     "web-01",
		Name:   "web-01",
		Status: "pending_install",
		Labels: map[string]string{"env": "prod", "owner": "ops"},
	})

	updated := st.BatchUpdateHostLabels([]string{"web-01"}, map[string]string{"batch": "blue"}, []string{"owner"})
	if len(updated) != 1 {
		t.Fatalf("expected one updated host, got %d", len(updated))
	}
	host, ok := st.Host("web-01")
	if !ok {
		t.Fatalf("expected host to exist")
	}
	if host.Labels["batch"] != "blue" {
		t.Fatalf("expected batch label to be set, got %#v", host.Labels)
	}
	if _, ok := host.Labels["owner"]; ok {
		t.Fatalf("expected owner label to be removed, got %#v", host.Labels)
	}
}
