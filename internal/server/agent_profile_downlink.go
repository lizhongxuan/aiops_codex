package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func agentProfileHash(profile model.AgentProfile) string {
	profile = model.CompleteAgentProfile(profile)
	payload, err := json.Marshal(profile)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (a *App) hostAgentProfileUpdateEnvelope() *agentrpc.Envelope {
	profile := a.hostAgentDefaultProfile()
	return &agentrpc.Envelope{
		Kind: "profile/update",
		ProfileUpdate: &agentrpc.ProfileUpdate{
			ConfigVersion: model.AgentProfileConfigVersion,
			ProfileHash:   agentProfileHash(profile),
			Profile:       profile,
		},
	}
}

func (a *App) pushHostAgentProfile(conn *agentConnection) error {
	if conn == nil {
		return nil
	}
	env := a.hostAgentProfileUpdateEnvelope()
	if env.ProfileUpdate == nil {
		return nil
	}
	if hash := strings.TrimSpace(env.ProfileUpdate.ProfileHash); hash != "" && conn.profileHash() == hash {
		return nil
	}
	if err := conn.send(env); err != nil {
		return err
	}
	conn.setProfileHash(env.ProfileUpdate.ProfileHash)
	return nil
}

func (a *App) pushHostAgentProfileToConnectedAgents() {
	a.agentMu.Lock()
	conns := make([]*agentConnection, 0, len(a.agents))
	for _, conn := range a.agents {
		conns = append(conns, conn)
	}
	a.agentMu.Unlock()

	for _, conn := range conns {
		if err := a.pushHostAgentProfile(conn); err != nil {
			log.Printf("push host-agent profile failed host=%s err=%v", conn.hostID, err)
		}
	}
}

func (a *App) handleAgentProfileAck(hostID string, payload *agentrpc.ProfileAck) {
	if strings.TrimSpace(hostID) == "" || payload == nil {
		return
	}

	host := a.findHost(hostID)
	host.ProfileVersion = payload.ConfigVersion
	host.ProfileHash = strings.TrimSpace(payload.ProfileHash)
	host.ProfileLoadedAt = defaultString(strings.TrimSpace(payload.LoadedAt), model.NowString())
	host.ProfileStatus = defaultString(strings.TrimSpace(payload.Status), "loaded")
	host.ProfileSummary = strings.TrimSpace(payload.Summary)
	host.EnabledSkills = append([]string(nil), payload.EnabledSkills...)
	host.EnabledMCPs = append([]string(nil), payload.EnabledMCPs...)
	host.LastError = strings.TrimSpace(payload.Error)
	a.store.UpsertHost(host)
	a.audit("agent.profile_loaded", map[string]any{
		"hostId":        hostID,
		"profileId":     payload.ProfileID,
		"profileHash":   payload.ProfileHash,
		"configVersion": payload.ConfigVersion,
		"status":        host.ProfileStatus,
		"summary":       payload.Summary,
		"enabledSkills": append([]string(nil), payload.EnabledSkills...),
		"enabledMCPs":   append([]string(nil), payload.EnabledMCPs...),
		"unsupported":   append([]string(nil), payload.Unsupported...),
		"error":         payload.Error,
	})
	a.broadcastAllSnapshots()
}
