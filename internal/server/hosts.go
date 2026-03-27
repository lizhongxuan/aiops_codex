package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type commandRunner func(context.Context, string, ...string) ([]byte, error)

type hostMutationRequest struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Address       string            `json:"address"`
	SSHUser       string            `json:"sshUser"`
	SSHPort       int               `json:"sshPort"`
	Labels        map[string]string `json:"labels"`
	InstallViaSSH bool              `json:"installViaSsh"`
}

type hostBatchTagRequest struct {
	HostIDs []string          `json:"hostIds"`
	Add     map[string]string `json:"add"`
	Remove  []string          `json:"remove"`
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (a *App) handleHosts(w http.ResponseWriter, r *http.Request, _ string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"items": a.store.Hosts(),
		})
	case http.MethodPost:
		a.handleHostCreate(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleHostByID(w http.ResponseWriter, r *http.Request, _ string) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/hosts/"), "/")
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	if path == "tags" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		a.handleHostBatchTags(w, r)
		return
	}

	parts := strings.Split(path, "/")
	hostID := strings.TrimSpace(parts[0])
	if hostID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			a.handleHostGet(w, r, hostID)
		case http.MethodPut:
			a.handleHostUpdate(w, r, hostID)
		case http.MethodDelete:
			a.handleHostDelete(w, r, hostID)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	if len(parts) == 2 && parts[1] == "sessions" {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		a.handleHostSessions(w, r, hostID)
		return
	}
	if len(parts) == 2 && parts[1] == "install" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		a.handleHostInstall(w, r, hostID)
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "host route not found"})
}

func (a *App) handleHostGet(w http.ResponseWriter, _ *http.Request, hostID string) {
	host, ok := a.store.Host(hostID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	writeJSON(w, http.StatusOK, host)
}

func (a *App) handleHostCreate(w http.ResponseWriter, r *http.Request) {
	var req hostMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	host, err := a.saveHostRecord(req, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.InstallViaSSH {
		if err := a.installHostAgent(r.Context(), r, host.ID); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": err.Error(),
				"host":  a.findHost(host.ID),
			})
			return
		}
		host = a.findHost(host.ID)
	}

	a.audit("host.create", map[string]any{
		"hostId":    host.ID,
		"name":      host.Name,
		"address":   host.Address,
		"sshUser":   host.SSHUser,
		"sshPort":   host.SSHPort,
		"labels":    host.Labels,
		"install":   req.InstallViaSSH,
		"kind":      host.Kind,
		"status":    host.Status,
		"transport": host.Transport,
	})
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":   true,
		"host": host,
	})
}

func (a *App) handleHostUpdate(w http.ResponseWriter, r *http.Request, hostID string) {
	var req hostMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	host, err := a.saveHostRecord(req, hostID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	a.audit("host.update", map[string]any{
		"hostId":    host.ID,
		"name":      host.Name,
		"address":   host.Address,
		"sshUser":   host.SSHUser,
		"sshPort":   host.SSHPort,
		"labels":    host.Labels,
		"status":    host.Status,
		"transport": host.Transport,
	})
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"host": host,
	})
}

func (a *App) handleHostDelete(w http.ResponseWriter, r *http.Request, hostID string) {
	if hostID == model.ServerLocalHostID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "server-local 不能删除"})
		return
	}
	host, ok := a.store.Host(hostID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	if !a.store.DeleteHost(hostID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	a.clearAgentConnection(hostID, nil)
	a.failRemoteTerminalsForHost(hostID, "host deleted")
	a.failRemoteExecsForHost(hostID, "host deleted")
	a.failAgentResponseWaitersForHost(hostID, "host deleted")
	a.notifyRemoteHostUnavailable(hostID, "主机已从清单移除", "主机记录已删除，如需继续执行，请重新添加或切回其他主机。")
	a.audit("host.delete", map[string]any{
		"hostId":  host.ID,
		"name":    host.Name,
		"address": host.Address,
	})
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleHostBatchTags(w http.ResponseWriter, r *http.Request) {
	var req hostBatchTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(req.HostIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hostIds is required"})
		return
	}
	if len(req.Add) == 0 && len(req.Remove) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "add/remove 至少填一个"})
		return
	}

	updated := a.store.BatchUpdateHostLabels(req.HostIDs, req.Add, req.Remove)
	a.audit("host.tags.batch", map[string]any{
		"hostIds": req.HostIDs,
		"add":     req.Add,
		"remove":  req.Remove,
	})
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": updated,
	})
}

func (a *App) handleHostSessions(w http.ResponseWriter, r *http.Request, hostID string) {
	if _, ok := a.store.Host(hostID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if limit <= 0 {
		limit = 8
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hostId": hostID,
		"items":  a.store.HostSessions(hostID, limit),
	})
}

func (a *App) handleHostInstall(w http.ResponseWriter, r *http.Request, hostID string) {
	if err := a.installHostAgent(r.Context(), r, hostID); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	host := a.findHost(hostID)
	a.audit("host.install", map[string]any{
		"hostId":  host.ID,
		"address": host.Address,
		"sshUser": host.SSHUser,
		"sshPort": host.SSHPort,
	})
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"host": host,
	})
}

func (a *App) saveHostRecord(req hostMutationRequest, overrideID string) (model.Host, error) {
	hostID := strings.TrimSpace(overrideID)
	if hostID == "" {
		hostID = strings.TrimSpace(req.ID)
	}
	if hostID == "" {
		return model.Host{}, errors.New("host id is required")
	}
	if hostID == model.ServerLocalHostID {
		return model.Host{}, errors.New("server-local 不能通过这个入口修改")
	}

	address := strings.TrimSpace(req.Address)
	sshUser := strings.TrimSpace(req.SSHUser)
	if address == "" {
		return model.Host{}, errors.New("target machine is required")
	}

	sshPort := req.SSHPort
	if sshPort <= 0 {
		sshPort = 22
	}

	host, ok := a.store.Host(hostID)
	if !ok {
		host = model.Host{
			ID:              hostID,
			Kind:            "inventory",
			Status:          "pending_install",
			Executable:      false,
			TerminalCapable: false,
			Transport:       "ssh_bootstrap",
			InstallState:    "pending_install",
			ControlMode:     "bootstrap_over_ssh",
		}
	}
	host.Name = defaultString(strings.TrimSpace(req.Name), hostID)
	host.Address = address
	host.SSHUser = sshUser
	host.SSHPort = sshPort
	host.Labels = cloneLabels(req.Labels)
	if host.Status == "" || host.Status == "inventory" {
		host.Status = "pending_install"
	}
	if host.InstallState == "" || host.InstallState == "inventory" {
		host.InstallState = "pending_install"
	}
	if host.Transport == "" {
		host.Transport = "ssh_bootstrap"
	}
	if host.ControlMode == "" {
		host.ControlMode = "bootstrap_over_ssh"
	}
	a.store.UpsertHost(host)
	return a.findHost(hostID), nil
}

func (a *App) installHostAgent(parent context.Context, r *http.Request, hostID string) error {
	host, ok := a.store.Host(hostID)
	if !ok {
		return errors.New("host not found")
	}
	if host.ID == model.ServerLocalHostID {
		return errors.New("server-local 不需要安装")
	}
	if strings.TrimSpace(host.Address) == "" {
		return errors.New("host address is required before install")
	}
	if strings.TrimSpace(a.cfg.GRPCTLSClientCAFile) != "" {
		return errors.New("当前启用了 mTLS，SSH 安装还不支持自动下发客户端证书，请先手动部署 host-agent")
	}

	host.InstallState = "installing"
	host.Status = "installing"
	host.LastError = ""
	a.store.UpsertHost(host)
	a.broadcastAllSnapshots()

	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()

	binaryPath, err := a.ensureHostAgentBinary(ctx)
	if err != nil {
		a.markHostInstallFailed(hostID, err)
		return err
	}

	grpcAddr, err := a.hostInstallGRPCAddress(r)
	if err != nil {
		a.markHostInstallFailed(hostID, err)
		return err
	}

	target := sshTarget(host)
	port := strconv.Itoa(host.SSHPort)
	tmpBin := fmt.Sprintf("/tmp/aiops-host-agent-%s", safeRemoteID(host.ID))
	tmpScript := fmt.Sprintf("/tmp/aiops-host-agent-install-%s.sh", safeRemoteID(host.ID))

	scriptPath, err := a.writeInstallScript(host, grpcAddr, tmpBin, tmpScript)
	if err != nil {
		a.markHostInstallFailed(hostID, err)
		return err
	}
	defer os.Remove(scriptPath)

	if err := a.runLocalCommand(ctx, "scp", "-P", port, binaryPath, target+":"+tmpBin); err != nil {
		a.markHostInstallFailed(hostID, fmt.Errorf("copy host-agent failed: %w", err))
		return err
	}
	if err := a.runLocalCommand(ctx, "scp", "-P", port, scriptPath, target+":"+tmpScript); err != nil {
		a.markHostInstallFailed(hostID, fmt.Errorf("copy install script failed: %w", err))
		return err
	}
	if err := a.runLocalCommand(ctx, "ssh", "-p", port, target, "sh", tmpScript); err != nil {
		a.markHostInstallFailed(hostID, fmt.Errorf("install host-agent over ssh failed: %w", err))
		return err
	}

	host = a.findHost(hostID)
	host.InstallState = "installed"
	host.Status = "connecting"
	host.Transport = "grpc_reverse"
	host.ControlMode = "persistent_stream"
	host.LastError = ""
	a.store.UpsertHost(host)
	return nil
}

func (a *App) ensureHostAgentBinary(ctx context.Context) (string, error) {
	if override := strings.TrimSpace(os.Getenv("AIOPS_HOST_AGENT_BIN")); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("configured AIOPS_HOST_AGENT_BIN not found: %w", err)
		}
		return override, nil
	}

	binPath := filepath.Join(".data", "bin", "host-agent")
	if info, err := os.Stat(binPath); err == nil && info.Mode().IsRegular() {
		return binPath, nil
	}

	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		return "", err
	}
	if err := a.runLocalCommand(ctx, "go", "build", "-trimpath", "-o", binPath, "./cmd/host-agent"); err != nil {
		return "", fmt.Errorf("build host-agent failed: %w", err)
	}
	return binPath, nil
}

func (a *App) hostInstallGRPCAddress(r *http.Request) (string, error) {
	if value := strings.TrimSpace(a.cfg.GRPCAdvertiseAddr); value != "" {
		return value, nil
	}

	host, port, err := net.SplitHostPort(strings.TrimSpace(a.cfg.GRPCAddr))
	if err != nil {
		return "", fmt.Errorf("invalid grpc address %q", a.cfg.GRPCAddr)
	}
	host = strings.TrimSpace(host)
	if host != "" && host != "0.0.0.0" && host != "::" && host != "[::]" && !isLoopbackHost(host) {
		return net.JoinHostPort(host, port), nil
	}

	requestHost := strings.TrimSpace(r.Host)
	if value, _, err := net.SplitHostPort(requestHost); err == nil {
		requestHost = value
	}
	requestHost = strings.TrimSpace(requestHost)
	if requestHost == "" {
		return "", errors.New("无法推导可供远程主机回连的 gRPC 地址，请设置 AIOPS_GRPC_ADVERTISE_ADDR")
	}
	return net.JoinHostPort(requestHost, port), nil
}

func (a *App) writeInstallScript(host model.Host, grpcAddr, tmpBin, tmpScript string) (string, error) {
	labels := encodeLabels(host.Labels)
	token := ""
	if tokens := a.cfg.HostAgentBootstrapTokens; len(tokens) > 0 {
		token = strings.TrimSpace(tokens[0])
	}
	if token == "" {
		token = strings.TrimSpace(a.cfg.HostAgentBootstrapToken)
	}
	if token == "" {
		return "", errors.New("bootstrap token is empty")
	}

	content := fmt.Sprintf(`#!/bin/sh
set -eu
sudo install -m 0755 %s /usr/local/bin/host-agent
sudo tee /etc/systemd/system/aiops-host-agent.service >/dev/null <<'EOF'
[Unit]
Description=AIOps Host Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/host-agent
Restart=always
RestartSec=3
Environment=%s
Environment=%s
Environment=%s
Environment=%s
Environment=%s
Environment=%s

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now aiops-host-agent
sudo systemctl restart aiops-host-agent
rm -f %s %s
`, tmpBin,
		systemdEnv("AIOPS_SERVER_GRPC_ADDR", grpcAddr),
		systemdEnv("AIOPS_AGENT_HOST_ID", host.ID),
		systemdEnv("AIOPS_AGENT_HOSTNAME", defaultString(host.Name, host.ID)),
		systemdEnv("AIOPS_AGENT_VERSION", defaultString(host.AgentVersion, "0.1.0")),
		systemdEnv("AIOPS_AGENT_BOOTSTRAP_TOKEN", token),
		systemdEnv("AIOPS_AGENT_LABELS", labels),
		tmpBin, tmpScript,
	)

	file, err := os.CreateTemp("", "aiops-host-agent-install-*.sh")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		return "", err
	}
	if err := file.Chmod(0o700); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func (a *App) runLocalCommand(ctx context.Context, name string, args ...string) error {
	output, err := a.commandRunner(ctx, name, args...)
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, truncate(text, 320))
	}
	return nil
}

func (a *App) markHostInstallFailed(hostID string, installErr error) {
	host := a.findHost(hostID)
	host.InstallState = "install_failed"
	host.Status = "pending_install"
	host.LastError = truncate(installErr.Error(), 320)
	a.store.UpsertHost(host)
	a.broadcastAllSnapshots()
}

func sshTarget(host model.Host) string {
	address := strings.TrimSpace(host.Address)
	if user := strings.TrimSpace(host.SSHUser); user != "" && !strings.Contains(address, "@") {
		return user + "@" + address
	}
	return address
}

func safeRemoteID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "host"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
}

func encodeLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(labels[key])
		if value == "" {
			parts = append(parts, strings.TrimSpace(key))
			continue
		}
		parts = append(parts, strings.TrimSpace(key)+"="+value)
	}
	return strings.Join(parts, ",")
}

func systemdEnv(key, value string) string {
	return fmt.Sprintf("%q", strings.TrimSpace(key)+"="+strings.ReplaceAll(strings.TrimSpace(value), `"`, `\"`))
}

func isLoopbackHost(host string) bool {
	value := strings.Trim(strings.TrimSpace(host), "[]")
	switch value {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
