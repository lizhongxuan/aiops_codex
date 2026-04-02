package agentrpc

import "github.com/lizhongxuan/aiops-codex/internal/model"

type ProfileUpdate struct {
	ConfigVersion int                `json:"configVersion,omitempty"`
	ProfileHash   string             `json:"profileHash,omitempty"`
	Profile       model.AgentProfile `json:"profile,omitempty"`
}

type ProfileAck struct {
	ConfigVersion int      `json:"configVersion,omitempty"`
	ProfileID     string   `json:"profileId,omitempty"`
	ProfileHash   string   `json:"profileHash,omitempty"`
	LoadedAt      string   `json:"loadedAt,omitempty"`
	Status        string   `json:"status,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	EnabledSkills []string `json:"enabledSkills,omitempty"`
	EnabledMCPs   []string `json:"enabledMCPs,omitempty"`
	Unsupported   []string `json:"unsupported,omitempty"`
	Error         string   `json:"error,omitempty"`
}
