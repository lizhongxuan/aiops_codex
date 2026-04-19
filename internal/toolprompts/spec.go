package toolprompts

import "strings"

type Spec struct {
	Name         string
	Capability   string
	Constraints  []string
	ResultShape  []string
	ApprovalNote string
}

func (s Spec) Description() string {
	parts := make([]string, 0, 4)
	if capability := strings.TrimSpace(s.Capability); capability != "" {
		parts = append(parts, capability)
	}
	if len(s.Constraints) > 0 {
		parts = append(parts, "Constraints: "+strings.Join(nonEmptyStrings(s.Constraints), " "))
	}
	if len(s.ResultShape) > 0 {
		parts = append(parts, "Result: "+strings.Join(nonEmptyStrings(s.ResultShape), " "))
	}
	if note := strings.TrimSpace(s.ApprovalNote); note != "" {
		parts = append(parts, note)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func nonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
