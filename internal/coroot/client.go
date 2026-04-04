package coroot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is an HTTP client for the Coroot monitoring API.
type Client struct {
	baseURL    string
	auth       *TokenManager
	httpClient *http.Client
}

// NewClient creates a new Coroot API client.
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    NewTokenManager(token),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Auth returns the underlying TokenManager so callers (e.g. the reverse proxy)
// can inject credentials into arbitrary requests.
func (c *Client) Auth() *TokenManager { return c.auth }

// BaseURL returns the configured Coroot base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// ---------- domain types ----------

// Service represents a Coroot service entry.
type Service struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

// ServiceOverviewResult holds the overview data for a single service.
type ServiceOverviewResult struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Status  string         `json:"status,omitempty"`
	Summary map[string]any `json:"summary,omitempty"`
}

// TimeRange describes a time window for metric queries.
type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// MetricsResult holds metric query results.
type MetricsResult struct {
	Metrics []map[string]any `json:"metrics,omitempty"`
}

// Alert represents a single Coroot alert.
type Alert struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Severity string `json:"severity,omitempty"`
	Status   string `json:"status,omitempty"`
}

// TopologyResult holds the service topology graph.
type TopologyResult struct {
	Nodes []map[string]any `json:"nodes,omitempty"`
	Edges []map[string]any `json:"edges,omitempty"`
}

// IncidentTimelineResult holds the timeline for an incident.
type IncidentTimelineResult struct {
	ID     string           `json:"id"`
	Events []map[string]any `json:"events,omitempty"`
}

// RCAReportResult holds a root-cause analysis report.
type RCAReportResult struct {
	ID          string           `json:"id"`
	RootCauses  []map[string]any `json:"rootCauses,omitempty"`
	Suggestions []map[string]any `json:"suggestions,omitempty"`
}

// ---------- API methods ----------

// ListServices returns all services known to Coroot.
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	var out []Service
	if err := c.get(ctx, "/api/v1/services", &out); err != nil {
		return nil, fmt.Errorf("coroot list services: %w", err)
	}
	return out, nil
}

// ServiceOverview returns the overview for a single service.
func (c *Client) ServiceOverview(ctx context.Context, serviceID string) (*ServiceOverviewResult, error) {
	var out ServiceOverviewResult
	if err := c.get(ctx, "/api/v1/services/"+serviceID+"/overview", &out); err != nil {
		return nil, fmt.Errorf("coroot service overview: %w", err)
	}
	return &out, nil
}

// ServiceMetrics returns metrics for a service within the given time range.
func (c *Client) ServiceMetrics(ctx context.Context, serviceID string, tr TimeRange) (*MetricsResult, error) {
	path := fmt.Sprintf("/api/v1/services/%s/metrics?from=%s&to=%s", serviceID, tr.From, tr.To)
	var out MetricsResult
	if err := c.get(ctx, path, &out); err != nil {
		return nil, fmt.Errorf("coroot service metrics: %w", err)
	}
	return &out, nil
}

// ServiceAlerts returns alerts for a service.
func (c *Client) ServiceAlerts(ctx context.Context, serviceID string) ([]Alert, error) {
	var out []Alert
	if err := c.get(ctx, "/api/v1/services/"+serviceID+"/alerts", &out); err != nil {
		return nil, fmt.Errorf("coroot service alerts: %w", err)
	}
	return out, nil
}

// Topology returns the full service topology graph.
func (c *Client) Topology(ctx context.Context) (*TopologyResult, error) {
	var out TopologyResult
	if err := c.get(ctx, "/api/v1/topology", &out); err != nil {
		return nil, fmt.Errorf("coroot topology: %w", err)
	}
	return &out, nil
}

// IncidentTimeline returns the timeline for a specific incident.
func (c *Client) IncidentTimeline(ctx context.Context, incidentID string) (*IncidentTimelineResult, error) {
	var out IncidentTimelineResult
	if err := c.get(ctx, "/api/v1/incidents/"+incidentID+"/timeline", &out); err != nil {
		return nil, fmt.Errorf("coroot incident timeline: %w", err)
	}
	return &out, nil
}

// RCAReport returns the root-cause analysis report for an incident.
func (c *Client) RCAReport(ctx context.Context, incidentID string) (*RCAReportResult, error) {
	var out RCAReportResult
	if err := c.get(ctx, "/api/v1/incidents/"+incidentID+"/rca", &out); err != nil {
		return nil, fmt.Errorf("coroot rca report: %w", err)
	}
	return &out, nil
}

// ---------- internal helpers ----------

func (c *Client) get(ctx context.Context, path string, dest any) error {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.auth.InjectAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("coroot api %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}
