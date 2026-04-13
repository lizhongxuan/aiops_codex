package guardian

import (
	"encoding/json"
	"testing"
)

func TestRiskLevel_IsValid(t *testing.T) {
	tests := []struct {
		level RiskLevel
		valid bool
	}{
		{RiskLow, true},
		{RiskMedium, true},
		{RiskHigh, true},
		{RiskCritical, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.level.IsValid(); got != tt.valid {
			t.Errorf("RiskLevel(%q).IsValid() = %v, want %v", tt.level, got, tt.valid)
		}
	}
}

func TestUserAuthorization_IsValid(t *testing.T) {
	tests := []struct {
		auth  UserAuthorization
		valid bool
	}{
		{AuthExplicit, true},
		{AuthImplied, true},
		{AuthNone, true},
		{"other", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.auth.IsValid(); got != tt.valid {
			t.Errorf("UserAuthorization(%q).IsValid() = %v, want %v", tt.auth, got, tt.valid)
		}
	}
}

func TestAssessmentOutcome_IsValid(t *testing.T) {
	tests := []struct {
		outcome AssessmentOutcome
		valid   bool
	}{
		{OutcomeAllow, true},
		{OutcomeDeny, true},
		{"maybe", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.outcome.IsValid(); got != tt.valid {
			t.Errorf("AssessmentOutcome(%q).IsValid() = %v, want %v", tt.outcome, got, tt.valid)
		}
	}
}

func TestGuardianAssessment_Validate(t *testing.T) {
	tests := []struct {
		name    string
		a       *GuardianAssessment
		wantErr bool
	}{
		{
			name: "valid assessment",
			a: &GuardianAssessment{
				RiskLevel:         RiskLow,
				UserAuthorization: AuthExplicit,
				Outcome:           OutcomeAllow,
				Rationale:         "user explicitly requested this",
			},
			wantErr: false,
		},
		{
			name:    "nil assessment",
			a:       nil,
			wantErr: true,
		},
		{
			name: "invalid risk level",
			a: &GuardianAssessment{
				RiskLevel:         "extreme",
				UserAuthorization: AuthExplicit,
				Outcome:           OutcomeAllow,
				Rationale:         "reason",
			},
			wantErr: true,
		},
		{
			name: "invalid authorization",
			a: &GuardianAssessment{
				RiskLevel:         RiskLow,
				UserAuthorization: "maybe",
				Outcome:           OutcomeAllow,
				Rationale:         "reason",
			},
			wantErr: true,
		},
		{
			name: "invalid outcome",
			a: &GuardianAssessment{
				RiskLevel:         RiskLow,
				UserAuthorization: AuthExplicit,
				Outcome:           "skip",
				Rationale:         "reason",
			},
			wantErr: true,
		},
		{
			name: "empty rationale",
			a: &GuardianAssessment{
				RiskLevel:         RiskLow,
				UserAuthorization: AuthExplicit,
				Outcome:           OutcomeAllow,
				Rationale:         "",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.a.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseAssessment_Valid(t *testing.T) {
	input := GuardianAssessment{
		RiskLevel:         RiskMedium,
		UserAuthorization: AuthImplied,
		Outcome:           OutcomeAllow,
		Rationale:         "operation is within scope",
	}
	raw, _ := json.Marshal(input)

	got, err := ParseAssessment(raw)
	if err != nil {
		t.Fatalf("ParseAssessment() unexpected error: %v", err)
	}
	if got.RiskLevel != RiskMedium {
		t.Errorf("RiskLevel = %v, want %v", got.RiskLevel, RiskMedium)
	}
	if got.Outcome != OutcomeAllow {
		t.Errorf("Outcome = %v, want %v", got.Outcome, OutcomeAllow)
	}
}

func TestParseAssessment_EmptyInput(t *testing.T) {
	got, err := ParseAssessment([]byte{})
	if err == nil {
		t.Fatal("ParseAssessment() expected error for empty input")
	}
	if got.Outcome != OutcomeDeny {
		t.Errorf("expected fail-closed deny, got %v", got.Outcome)
	}
}

func TestParseAssessment_MalformedJSON(t *testing.T) {
	got, err := ParseAssessment([]byte("not json"))
	if err == nil {
		t.Fatal("ParseAssessment() expected error for malformed JSON")
	}
	if got.Outcome != OutcomeDeny {
		t.Errorf("expected fail-closed deny, got %v", got.Outcome)
	}
	if got.RiskLevel != RiskCritical {
		t.Errorf("expected fail-closed critical risk, got %v", got.RiskLevel)
	}
}

func TestParseAssessment_InvalidFields(t *testing.T) {
	raw := []byte(`{"risk_level":"extreme","user_authorization":"explicit","outcome":"allow","rationale":"test"}`)
	got, err := ParseAssessment(raw)
	if err == nil {
		t.Fatal("ParseAssessment() expected error for invalid risk_level")
	}
	if got.Outcome != OutcomeDeny {
		t.Errorf("expected fail-closed deny, got %v", got.Outcome)
	}
}
