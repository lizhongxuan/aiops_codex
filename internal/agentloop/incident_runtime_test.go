package agentloop

import "testing"

func TestValidateIncidentTransitionAcceptsMainPath(t *testing.T) {
	cases := []struct {
		prevMode  string
		nextMode  string
		prevStage string
		nextStage string
	}{
		{"", "analysis", "", "understanding"},
		{"analysis", "analysis", "understanding", "planning"},
		{"analysis", "analysis", "planning", "waiting_plan_approval"},
		{"analysis", "execute", "waiting_plan_approval", "executing"},
		{"execute", "execute", "executing", "verifying"},
		{"execute", "execute", "verifying", "completed"},
	}

	for _, tc := range cases {
		if err := ValidateIncidentTransition(tc.prevMode, tc.nextMode, tc.prevStage, tc.nextStage); err != nil {
			t.Fatalf("expected transition %s/%s -> %s/%s to pass, got %v", tc.prevMode, tc.prevStage, tc.nextMode, tc.nextStage, err)
		}
	}
}

func TestValidateIncidentTransitionRejectsInvalidModeStagePair(t *testing.T) {
	if err := ValidateIncidentTransition("analysis", "analysis", "planning", "executing"); err == nil {
		t.Fatalf("expected executing stage to be rejected in analysis mode")
	}
}

func TestValidateIncidentTransitionRejectsInvalidStageJump(t *testing.T) {
	if err := ValidateIncidentTransition("analysis", "analysis", "understanding", "waiting_action_approval"); err == nil {
		t.Fatalf("expected direct understanding -> waiting_action_approval to be rejected")
	}
}
