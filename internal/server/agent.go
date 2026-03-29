package server

import (
	"errors"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type agentConnection struct {
	hostID          string
	stream          agentrpc.AgentService_ConnectServer
	sendMu          sync.Mutex
	stateMu         sync.Mutex
	lastProfileHash string
}

func (c *agentConnection) send(msg *agentrpc.Envelope) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.stream.Send(msg)
}

func (c *agentConnection) profileHash() string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.lastProfileHash
}

func (c *agentConnection) setProfileHash(hash string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.lastProfileHash = strings.TrimSpace(hash)
}

func (a *App) setAgentConnection(hostID string, conn *agentConnection) {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	a.agents[hostID] = conn
}

func (a *App) clearAgentConnection(hostID string, conn *agentConnection) {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	current, ok := a.agents[hostID]
	if ok && (conn == nil || current == conn) {
		delete(a.agents, hostID)
	}
}

func (a *App) agentConnection(hostID string) (*agentConnection, bool) {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	conn, ok := a.agents[hostID]
	return conn, ok
}

func (a *App) sendAgentEnvelope(hostID string, payload *agentrpc.Envelope) error {
	conn, ok := a.agentConnection(hostID)
	if !ok {
		return errors.New("remote host agent is not connected")
	}
	return conn.send(payload)
}

func (a *App) handleAgentTerminalReady(hostID string, payload *agentrpc.TerminalReady) {
	if payload == nil {
		return
	}
	terminal, ok := a.terminalSession(payload.SessionID)
	if !ok || terminal.HostID != hostID {
		return
	}

	terminal.mu.Lock()
	terminal.Cwd = payload.Cwd
	terminal.Shell = payload.Shell
	terminal.StartedAt = payload.StartedAt
	terminal.Status = defaultString(payload.Status, "connected")
	terminal.mu.Unlock()

	a.broadcastTerminal(payload.SessionID, terminalEnvelope{
		Type:      "ready",
		SessionID: payload.SessionID,
		Cwd:       payload.Cwd,
		Shell:     payload.Shell,
		StartedAt: payload.StartedAt,
		Status:    defaultString(payload.Status, "connected"),
	})
}

func (a *App) handleAgentTerminalOutput(hostID string, payload *agentrpc.TerminalOutput) {
	if payload == nil {
		return
	}
	terminal, ok := a.terminalSession(payload.SessionID)
	if !ok || terminal.HostID != hostID {
		return
	}
	a.broadcastTerminal(payload.SessionID, terminalEnvelope{
		Type: "output",
		Data: sanitizeTerminalChunk(payload.Data),
	})
}

func (a *App) handleAgentTerminalExit(hostID string, payload *agentrpc.TerminalExit) {
	if payload == nil {
		return
	}
	terminal, ok := a.terminalSession(payload.SessionID)
	if !ok || terminal.HostID != hostID {
		return
	}

	terminal.mu.Lock()
	terminal.exited = true
	terminal.Status = defaultString(payload.Status, "disconnected")
	terminal.mu.Unlock()

	a.broadcastTerminal(payload.SessionID, terminalEnvelope{
		Type:   "exit",
		Code:   payload.Code,
		Status: defaultString(payload.Status, "disconnected"),
	})
	go a.reapExitedTerminalSession(payload.SessionID, terminalExitRetention)
}

func (a *App) handleAgentTerminalStatus(hostID string, payload *agentrpc.TerminalStatus) {
	if payload == nil {
		return
	}
	terminal, ok := a.terminalSession(payload.SessionID)
	if !ok || terminal.HostID != hostID {
		return
	}

	terminal.mu.Lock()
	currentStatus := defaultString(payload.Status, terminal.Status)
	terminal.Status = currentStatus
	terminal.mu.Unlock()

	if payload.Message != "" {
		a.broadcastTerminal(payload.SessionID, terminalEnvelope{
			Type:    "error",
			Status:  currentStatus,
			Message: payload.Message,
		})
		return
	}
	a.broadcastTerminal(payload.SessionID, terminalEnvelope{
		Type:   "status",
		Status: currentStatus,
	})
}

func (a *App) failRemoteTerminalsForHost(hostID, message string) {
	a.terminalMu.Lock()
	ids := make([]string, 0, len(a.terminals))
	for id, terminal := range a.terminals {
		if terminal.HostID == hostID && terminal.Remote {
			ids = append(ids, id)
		}
	}
	a.terminalMu.Unlock()

	for _, terminalID := range ids {
		terminal, ok := a.terminalSession(terminalID)
		if !ok {
			continue
		}

		terminal.mu.Lock()
		terminal.exited = true
		terminal.Status = "disconnected"
		terminal.mu.Unlock()

		a.broadcastTerminal(terminalID, terminalEnvelope{
			Type:    "error",
			Status:  "disconnected",
			Message: message,
		})
		a.broadcastTerminal(terminalID, terminalEnvelope{
			Type:   "exit",
			Code:   255,
			Status: "disconnected",
		})
		a.shutdownTerminalSession(terminalID)
	}
}

func hostSupportsTerminal(host model.Host) bool {
	return host.Status == "online" && (host.TerminalCapable || host.ID == model.ServerLocalHostID)
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
