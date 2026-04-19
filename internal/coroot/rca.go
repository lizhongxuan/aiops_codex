package coroot

import (
	"context"
	"fmt"
	"time"
)

// RCAResult holds the outcome of a root-cause analysis for an incident.
type RCAResult struct {
	IncidentID  string           `json:"incidentId"`
	RootCauses  []map[string]any `json:"rootCauses,omitempty"`
	Suggestions []map[string]any `json:"suggestions,omitempty"`
	Timeline    []map[string]any `json:"timeline,omitempty"`
	AnalyzedAt  time.Time        `json:"analyzedAt"`
}

// RCAEngine is the interface for performing root-cause analysis on incidents.
type RCAEngine interface {
	// Analyze performs root-cause analysis for the given incident and returns
	// the combined RCA report and timeline.
	Analyze(ctx context.Context, incidentID string) (*RCAResult, error)
}

// CorootRCAEngine implements RCAEngine by calling the Coroot API via Client.
type CorootRCAEngine struct {
	client *Client
}

// NewCorootRCAEngine creates an RCAEngine backed by a live Coroot client.
func NewCorootRCAEngine(c *Client) *CorootRCAEngine {
	return &CorootRCAEngine{client: c}
}

// Analyze fetches the RCA report and incident timeline from Coroot and merges
// them into a single RCAResult.
func (e *CorootRCAEngine) Analyze(ctx context.Context, incidentID string) (*RCAResult, error) {
	if incidentID == "" {
		return nil, fmt.Errorf("rca: incidentID is required")
	}

	report, err := e.client.RCAReport(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("rca report: %w", err)
	}

	timeline, err := e.client.IncidentTimeline(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("rca timeline: %w", err)
	}

	return &RCAResult{
		IncidentID:  incidentID,
		RootCauses:  report.RootCauses,
		Suggestions: report.Suggestions,
		Timeline:    timeline.Events,
		AnalyzedAt:  time.Now(),
	}, nil
}

// StubRCAEngine implements RCAEngine with predefined mock data for development
// and testing purposes.
type StubRCAEngine struct{}

// NewStubRCAEngine creates a stub RCA engine that returns canned responses.
func NewStubRCAEngine() *StubRCAEngine {
	return &StubRCAEngine{}
}

// Analyze returns a fixed mock RCAResult without calling any external service.
func (e *StubRCAEngine) Analyze(_ context.Context, incidentID string) (*RCAResult, error) {
	if incidentID == "" {
		return nil, fmt.Errorf("rca: incidentID is required")
	}

	return &RCAResult{
		IncidentID: incidentID,
		RootCauses: []map[string]any{
			{
				"component":   "api-gateway",
				"description": "High memory usage caused OOM kills",
				"confidence":  0.92,
			},
		},
		Suggestions: []map[string]any{
			{
				"action":   "Increase memory limit to 2Gi",
				"priority": "high",
			},
			{
				"action":   "Add horizontal pod autoscaler",
				"priority": "medium",
			},
		},
		Timeline: []map[string]any{
			{
				"timestamp":   time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
				"event":       "Memory usage exceeded 90%",
				"component":   "api-gateway",
				"severity":    "warning",
			},
			{
				"timestamp":   time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
				"event":       "OOM kill detected",
				"component":   "api-gateway",
				"severity":    "critical",
			},
			{
				"timestamp":   time.Now().Add(-4 * time.Minute).Format(time.RFC3339),
				"event":       "Pod restarted",
				"component":   "api-gateway",
				"severity":    "info",
			},
		},
		AnalyzedAt: time.Now(),
	}, nil
}
