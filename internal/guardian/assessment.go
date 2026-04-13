package guardian

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RiskLevel represents the assessed risk of an operation.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// validRiskLevels is the set of allowed risk levels.
var validRiskLevels = map[RiskLevel]bool{
	RiskLow:      true,
	RiskMedium:   true,
	RiskHigh:     true,
	RiskCritical: true,
}

// IsValid returns true if the RiskLevel is a recognized value.
func (r RiskLevel) IsValid() bool {
	return validRiskLevels[r]
}

// UserAuthorization represents the user's authorization stance.
type UserAuthorization string

const (
	AuthExplicit UserAuthorization = "explicit"
	AuthImplied  UserAuthorization = "implied"
	AuthNone     UserAuthorization = "none"
)

// validUserAuthorizations is the set of allowed authorization values.
var validUserAuthorizations = map[UserAuthorization]bool{
	AuthExplicit: true,
	AuthImplied:  true,
	AuthNone:     true,
}

// IsValid returns true if the UserAuthorization is a recognized value.
func (u UserAuthorization) IsValid() bool {
	return validUserAuthorizations[u]
}

// AssessmentOutcome represents the final decision of the guardian.
type AssessmentOutcome string

const (
	OutcomeAllow AssessmentOutcome = "allow"
	OutcomeDeny  AssessmentOutcome = "deny"
)

// validOutcomes is the set of allowed outcomes.
var validOutcomes = map[AssessmentOutcome]bool{
	OutcomeAllow: true,
	OutcomeDeny:  true,
}

// IsValid returns true if the AssessmentOutcome is a recognized value.
func (o AssessmentOutcome) IsValid() bool {
	return validOutcomes[o]
}

// GuardianAssessment is the structured output from the guardian model.
type GuardianAssessment struct {
	RiskLevel         RiskLevel         `json:"risk_level"`
	UserAuthorization UserAuthorization `json:"user_authorization"`
	Outcome           AssessmentOutcome `json:"outcome"`
	Rationale         string            `json:"rationale"`
}

// Validate checks that all fields of the assessment contain valid values.
func (a *GuardianAssessment) Validate() error {
	if a == nil {
		return errors.New("guardian: assessment is nil")
	}
	if !a.RiskLevel.IsValid() {
		return fmt.Errorf("guardian: invalid risk_level %q", a.RiskLevel)
	}
	if !a.UserAuthorization.IsValid() {
		return fmt.Errorf("guardian: invalid user_authorization %q", a.UserAuthorization)
	}
	if !a.Outcome.IsValid() {
		return fmt.Errorf("guardian: invalid outcome %q", a.Outcome)
	}
	if a.Rationale == "" {
		return errors.New("guardian: rationale must not be empty")
	}
	return nil
}

// ParseAssessment parses raw JSON output from the guardian model into a
// GuardianAssessment. If the output is malformed or contains invalid values,
// it fails closed by returning a deny assessment with an error.
func ParseAssessment(raw []byte) (*GuardianAssessment, error) {
	if len(raw) == 0 {
		return failClosed("empty guardian output"), errors.New("guardian: empty output, failing closed")
	}

	var assessment GuardianAssessment
	if err := json.Unmarshal(raw, &assessment); err != nil {
		return failClosed("malformed JSON output"), fmt.Errorf("guardian: failed to parse output: %w", err)
	}

	if err := assessment.Validate(); err != nil {
		return failClosed(err.Error()), fmt.Errorf("guardian: validation failed: %w", err)
	}

	return &assessment, nil
}

// failClosed returns a deny assessment used when guardian output is malformed.
func failClosed(reason string) *GuardianAssessment {
	return &GuardianAssessment{
		RiskLevel:         RiskCritical,
		UserAuthorization: AuthNone,
		Outcome:           OutcomeDeny,
		Rationale:         fmt.Sprintf("fail-closed: %s", reason),
	}
}
