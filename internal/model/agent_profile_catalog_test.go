package model

import "testing"

func TestDefaultAgentProfileIncludesSkillAndMCPDefaults(t *testing.T) {
	mainProfile := DefaultAgentProfile(string(AgentProfileTypeMainAgent))
	if len(mainProfile.Skills) == 0 {
		t.Fatalf("expected main-agent default skills")
	}
	if len(mainProfile.MCPs) == 0 {
		t.Fatalf("expected main-agent default mcps")
	}
	hostProfile := DefaultAgentProfile(string(AgentProfileTypeHostAgentDefault))
	if len(hostProfile.Skills) == 0 {
		t.Fatalf("expected host-agent default skills")
	}
	if len(hostProfile.MCPs) == 0 {
		t.Fatalf("expected host-agent default mcps")
	}
}

func TestCompleteAgentProfileNormalizesLegacySkillAndMCPValues(t *testing.T) {
	profile := CompleteAgentProfile(AgentProfile{
		ID:   string(AgentProfileTypeMainAgent),
		Type: string(AgentProfileTypeMainAgent),
		Skills: []AgentSkill{
			{ID: "ops-triage", ActivationMode: "default"},
			{ID: "safe-change-review", ActivationMode: "explicit"},
		},
		MCPs: []AgentMCP{
			{ID: "filesystem", Permission: "read-only"},
			{ID: "metrics", Permission: "read-write"},
		},
	})
	if len(profile.Skills) != 2 {
		t.Fatalf("expected only provided skills to remain, got %#v", profile.Skills)
	}
	if profile.Skills[0].ActivationMode != AgentSkillActivationDefault {
		t.Fatalf("expected default activation mode, got %q", profile.Skills[0].ActivationMode)
	}
	if profile.Skills[1].ActivationMode != AgentSkillActivationExplicit {
		t.Fatalf("expected explicit activation mode, got %q", profile.Skills[1].ActivationMode)
	}
	if len(profile.MCPs) != 2 {
		t.Fatalf("expected only provided mcps to remain, got %#v", profile.MCPs)
	}
	if profile.MCPs[0].Permission != AgentMCPPermissionReadonly {
		t.Fatalf("expected readonly permission, got %q", profile.MCPs[0].Permission)
	}
	if profile.MCPs[1].Permission != AgentMCPPermissionReadwrite {
		t.Fatalf("expected readwrite permission, got %q", profile.MCPs[1].Permission)
	}
}
