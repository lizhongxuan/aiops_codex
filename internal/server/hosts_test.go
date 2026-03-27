package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestHandleHostsCreatePersistsInventoryRecord(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "aiops_codex_session",
		SessionSecret:     "test-session-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleHosts)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hosts", strings.NewReader(`{
	  "id": "web-01",
	  "name": "web-01",
	  "address": "10.0.0.21",
	  "sshUser": "ubuntu",
	  "sshPort": 22,
	  "labels": {"env":"prod","role":"web"}
	}`))
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected host create to return 201, got %d", recorder.Code)
	}

	var response struct {
		Host model.Host `json:"host"`
	}
	if err := json.NewDecoder(recorder.Result().Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Host.ID != "web-01" || response.Host.Address != "10.0.0.21" {
		t.Fatalf("unexpected created host: %#v", response.Host)
	}

	host, ok := app.store.Host("web-01")
	if !ok {
		t.Fatalf("expected created host to exist in store")
	}
	if host.Status != "pending_install" || host.Transport != "ssh_bootstrap" {
		t.Fatalf("unexpected persisted host state: %#v", host)
	}
}

func TestHandleHostBatchTagsUpdatesInventory(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "aiops_codex_session",
		SessionSecret:     "test-session-secret",
		SessionCookieTTL:  time.Hour,
	})
	app.store.UpsertHost(model.Host{
		ID:      "web-01",
		Name:    "web-01",
		Address: "10.0.0.21",
		Status:  "pending_install",
		Labels:  map[string]string{"env": "prod", "owner": "ops"},
	})

	handler := app.withSession(app.handleHostByID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hosts/tags", strings.NewReader(`{
	  "hostIds": ["web-01"],
	  "add": {"batch":"blue"},
	  "remove": ["owner"]
	}`))
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected batch tags to return 200, got %d", recorder.Code)
	}

	host, ok := app.store.Host("web-01")
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

func TestHandleHostInstallMarksHostConnectingAndRunsSSHFlow(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "host-agent")
	if err := os.WriteFile(binPath, []byte("fake-binary"), 0o755); err != nil {
		t.Fatalf("write fake host-agent binary: %v", err)
	}
	t.Setenv("AIOPS_HOST_AGENT_BIN", binPath)

	app := New(config.Config{
		SessionCookieName:       "aiops_codex_session",
		SessionSecret:           "test-session-secret",
		SessionCookieTTL:        time.Hour,
		GRPCAddr:                "127.0.0.1:19090",
		GRPCAdvertiseAddr:       "10.0.0.1:19090",
		HostAgentBootstrapToken: "bootstrap-1",
		HostAgentBootstrapTokens: []string{
			"bootstrap-1",
		},
	})
	app.store.UpsertHost(model.Host{
		ID:           "web-01",
		Name:         "web-01",
		Address:      "10.0.0.21",
		SSHUser:      "ubuntu",
		SSHPort:      22,
		Status:       "pending_install",
		InstallState: "pending_install",
	})

	var commands [][]string
	app.commandRunner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := append([]string{name}, args...)
		commands = append(commands, call)
		return []byte("ok"), nil
	}

	handler := app.withSession(app.handleHostByID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hosts/web-01/install", nil)
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected install to return 200, got %d", recorder.Code)
	}

	if len(commands) != 3 {
		t.Fatalf("expected scp/scp/ssh command sequence, got %#v", commands)
	}
	if commands[0][0] != "scp" || commands[1][0] != "scp" || commands[2][0] != "ssh" {
		t.Fatalf("unexpected command flow %#v", commands)
	}

	host, ok := app.store.Host("web-01")
	if !ok {
		t.Fatalf("expected host to exist after install")
	}
	if host.Status != "connecting" || host.InstallState != "installed" {
		t.Fatalf("expected host to be waiting for agent reconnect, got %#v", host)
	}
	if host.Transport != "grpc_reverse" {
		t.Fatalf("expected transport to switch to grpc_reverse, got %q", host.Transport)
	}
}
