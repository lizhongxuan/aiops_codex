package coroot

import (
	"context"
	"fmt"
)

// ClientProvider adapts a *Client to the DataSourceProvider interface so it
// can be used as the Coroot backend inside a DataSourceRouter.
type ClientProvider struct {
	client *Client
}

// NewClientProvider wraps an existing Coroot HTTP client as a DataSourceProvider.
func NewClientProvider(c *Client) *ClientProvider {
	return &ClientProvider{client: c}
}

// Name implements DataSourceProvider.
func (p *ClientProvider) Name() string { return "coroot_mcp" }

// Query implements DataSourceProvider by dispatching to the appropriate
// Client method based on DataQuery.Kind.
func (p *ClientProvider) Query(ctx context.Context, q DataQuery) (any, error) {
	switch q.Kind {
	case QueryServices:
		return p.client.ListServices(ctx)
	case QueryServiceOverview:
		if q.ServiceID == "" {
			return nil, fmt.Errorf("serviceId is required for %s", q.Kind)
		}
		return p.client.ServiceOverview(ctx, q.ServiceID)
	case QueryServiceMetrics:
		if q.ServiceID == "" {
			return nil, fmt.Errorf("serviceId is required for %s", q.Kind)
		}
		tr := TimeRange{
			From: q.Params["from"],
			To:   q.Params["to"],
		}
		return p.client.ServiceMetrics(ctx, q.ServiceID, tr)
	case QueryServiceAlerts:
		if q.ServiceID == "" {
			return nil, fmt.Errorf("serviceId is required for %s", q.Kind)
		}
		return p.client.ServiceAlerts(ctx, q.ServiceID)
	case QueryTopology:
		return p.client.Topology(ctx)
	case QueryIncidentTimeline:
		incidentID := q.Params["incidentId"]
		if incidentID == "" {
			return nil, fmt.Errorf("incidentId param is required for %s", q.Kind)
		}
		return p.client.IncidentTimeline(ctx, incidentID)
	case QueryRCA:
		incidentID := q.Params["incidentId"]
		if incidentID == "" {
			return nil, fmt.Errorf("incidentId param is required for %s", q.Kind)
		}
		return p.client.RCAReport(ctx, incidentID)
	case QueryHostOverview:
		if q.HostID == "" {
			return nil, fmt.Errorf("hostId is required for %s", q.Kind)
		}
		return p.client.HostOverview(ctx, q.HostID)
	default:
		return nil, fmt.Errorf("unsupported query kind: %s", q.Kind)
	}
}
