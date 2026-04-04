package model

import "regexp"

// ApprovalGrantRecord represents a persisted approval grant that allows
// pre-approved execution of a specific command or file-change pattern.
type ApprovalGrantRecord struct {
	ID                    string         `json:"id"`
	HostID                string         `json:"hostId"`
	HostScope             string         `json:"hostScope"`             // host
	GrantType             string         `json:"grantType"`             // command | file_change
	Fingerprint           string         `json:"fingerprint"`
	Command               string         `json:"command,omitempty"`
	Cwd                   string         `json:"cwd,omitempty"`
	GrantRoot             string         `json:"grantRoot,omitempty"`
	CreatedFromApprovalID string         `json:"createdFromApprovalId"`
	CreatedFromSessionID  string         `json:"createdFromSessionId"`
	CreatedBy             string         `json:"createdBy"`
	CreatedAt             string         `json:"createdAt"`
	ExpiresAt             string         `json:"expiresAt,omitempty"`
	Status                string         `json:"status"`                // active | revoked | expired
	Reason                string         `json:"reason,omitempty"`
	Meta                  map[string]any `json:"meta,omitempty"`
}

// highRiskCommandPatterns lists regex patterns that match dangerous commands
// which should require additional confirmation before execution.
var highRiskCommandPatterns = []string{
	`rm\s+-rf\s+/`,
	`sudo\s+su`,
	`iptables\s+-F`,
	`mkfs\.`,
	`dd\s+if=`,
	`chmod\s+-R\s+777`,
	`shutdown`,
	`reboot`,
	`init\s+0`,
}

var compiledHighRiskPatterns []*regexp.Regexp

func init() {
	compiledHighRiskPatterns = make([]*regexp.Regexp, len(highRiskCommandPatterns))
	for i, p := range highRiskCommandPatterns {
		compiledHighRiskPatterns[i] = regexp.MustCompile(p)
	}
}

// IsHighRiskCommand returns true when the given command matches any of the
// known high-risk patterns (e.g. rm -rf /, sudo su, mkfs, etc.).
func IsHighRiskCommand(command string) bool {
	for _, re := range compiledHighRiskPatterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}
