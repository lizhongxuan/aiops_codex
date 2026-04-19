package coroot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// DataSource identifies where a query result came from.
type DataSource string

const (
	DataSourceCoroot       DataSource = "coroot_mcp"
	DataSourceBashFallback DataSource = "bash_fallback"
)

// PriorityStrategy controls the order in which data sources are tried.
type PriorityStrategy string

const (
	PriorityCorootFirst PriorityStrategy = "coroot_first"
	PriorityLocalFirst  PriorityStrategy = "local_first"
	PriorityCorootOnly  PriorityStrategy = "coroot_only"
)

// ParsePriorityStrategy converts a raw string (typically from an env var)
// into a PriorityStrategy. Unrecognised values default to coroot_first.
func ParsePriorityStrategy(raw string) PriorityStrategy {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(PriorityLocalFirst):
		return PriorityLocalFirst
	case string(PriorityCorootOnly):
		return PriorityCorootOnly
	default:
		return PriorityCorootFirst
	}
}

// QueryKind describes the type of data being requested.
type QueryKind string

const (
	QueryServices        QueryKind = "services"
	QueryServiceOverview QueryKind = "service_overview"
	QueryServiceMetrics  QueryKind = "service_metrics"
	QueryServiceAlerts   QueryKind = "service_alerts"
	QueryTopology        QueryKind = "topology"
	QueryIncidentTimeline QueryKind = "incident_timeline"
	QueryRCA             QueryKind = "rca"
	QueryHostOverview    QueryKind = "host_overview"
)

// DataQuery represents a request for monitoring data.
type DataQuery struct {
	Kind      QueryKind         `json:"kind"`
	ServiceID string            `json:"serviceId,omitempty"`
	HostID    string            `json:"hostId,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

// DataResult holds the outcome of a routed data query.
type DataResult struct {
	DataSource DataSource     `json:"dataSource"`
	Data       any            `json:"data,omitempty"`
	Error      string         `json:"error,omitempty"`
	Latency    time.Duration  `json:"latency"`
}

// DataSourceProvider is the interface that both the Coroot backend and the
// local/bash fallback must satisfy.
type DataSourceProvider interface {
	// Query executes a data query and returns the raw result.
	Query(ctx context.Context, q DataQuery) (any, error)
	// Name returns a human-readable label for this provider.
	Name() string
}

// DataSourceRouter decides which data source to use for a given query,
// implements fallback logic, and annotates every result with its origin.
type DataSourceRouter struct {
	corootProvider DataSourceProvider
	fallback       DataSourceProvider
	strategy       PriorityStrategy

	mu            sync.RWMutex
	corootHealthy bool
	lastHealthAt  time.Time
}

// NewDataSourceRouter creates a router with the given providers and strategy.
// If strategy is empty it reads COROOT_PRIORITY from the environment.
func NewDataSourceRouter(corootProvider, fallback DataSourceProvider, strategy PriorityStrategy) *DataSourceRouter {
	if strategy == "" {
		strategy = ParsePriorityStrategy(os.Getenv("COROOT_PRIORITY"))
	}
	return &DataSourceRouter{
		corootProvider: corootProvider,
		fallback:       fallback,
		strategy:       strategy,
		corootHealthy:  true, // optimistic start
	}
}

// Strategy returns the current priority strategy.
func (r *DataSourceRouter) Strategy() PriorityStrategy {
	return r.strategy
}

// Route executes the query according to the configured priority strategy.
// Every returned DataResult carries a DataSource annotation.
func (r *DataSourceRouter) Route(ctx context.Context, q DataQuery) (DataResult, error) {
	start := time.Now()

	switch r.strategy {
	case PriorityLocalFirst:
		return r.routeLocalFirst(ctx, q, start)
	case PriorityCorootOnly:
		return r.routeCorootOnly(ctx, q, start)
	default: // coroot_first
		return r.routeCorootFirst(ctx, q, start)
	}
}

// HealthCheck probes the Coroot provider and updates the cached health state.
func (r *DataSourceRouter) HealthCheck(ctx context.Context) error {
	// Use a lightweight query (list services) to verify connectivity.
	_, err := r.corootProvider.Query(ctx, DataQuery{Kind: QueryServices})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastHealthAt = time.Now()
	if err != nil {
		r.corootHealthy = false
		return fmt.Errorf("coroot health check failed: %w", err)
	}
	r.corootHealthy = true
	return nil
}

// IsCorootHealthy returns the last known health state.
func (r *DataSourceRouter) IsCorootHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.corootHealthy
}

// ---------- routing strategies ----------

func (r *DataSourceRouter) routeCorootFirst(ctx context.Context, q DataQuery, start time.Time) (DataResult, error) {
	if r.corootProvider != nil {
		data, err := r.corootProvider.Query(ctx, q)
		if err == nil {
			r.setHealthy(true)
			return DataResult{
				DataSource: DataSourceCoroot,
				Data:       data,
				Latency:    time.Since(start),
			}, nil
		}
		// Coroot failed — log and fall through.
		r.setHealthy(false)
		log.Printf("coroot query failed (kind=%s), falling back: %v", q.Kind, err)
	}

	return r.queryFallback(ctx, q, start)
}

func (r *DataSourceRouter) routeLocalFirst(ctx context.Context, q DataQuery, start time.Time) (DataResult, error) {
	if r.fallback != nil {
		data, err := r.fallback.Query(ctx, q)
		if err == nil {
			return DataResult{
				DataSource: DataSourceBashFallback,
				Data:       data,
				Latency:    time.Since(start),
			}, nil
		}
		log.Printf("local query failed (kind=%s), trying coroot: %v", q.Kind, err)
	}

	if r.corootProvider != nil {
		data, err := r.corootProvider.Query(ctx, q)
		if err == nil {
			r.setHealthy(true)
			return DataResult{
				DataSource: DataSourceCoroot,
				Data:       data,
				Latency:    time.Since(start),
			}, nil
		}
		r.setHealthy(false)
		return DataResult{
			DataSource: DataSourceCoroot,
			Error:      err.Error(),
			Latency:    time.Since(start),
		}, fmt.Errorf("all data sources failed for query kind=%s: %w", q.Kind, err)
	}

	return DataResult{Latency: time.Since(start)}, fmt.Errorf("no data source available for query kind=%s", q.Kind)
}

func (r *DataSourceRouter) routeCorootOnly(ctx context.Context, q DataQuery, start time.Time) (DataResult, error) {
	if r.corootProvider == nil {
		return DataResult{Latency: time.Since(start)}, fmt.Errorf("coroot provider not configured (strategy=coroot_only)")
	}
	data, err := r.corootProvider.Query(ctx, q)
	if err != nil {
		r.setHealthy(false)
		return DataResult{
			DataSource: DataSourceCoroot,
			Error:      err.Error(),
			Latency:    time.Since(start),
		}, fmt.Errorf("coroot query failed (strategy=coroot_only, kind=%s): %w", q.Kind, err)
	}
	r.setHealthy(true)
	return DataResult{
		DataSource: DataSourceCoroot,
		Data:       data,
		Latency:    time.Since(start),
	}, nil
}

func (r *DataSourceRouter) queryFallback(ctx context.Context, q DataQuery, start time.Time) (DataResult, error) {
	if r.fallback == nil {
		return DataResult{Latency: time.Since(start)}, fmt.Errorf("no fallback data source configured for query kind=%s", q.Kind)
	}
	data, err := r.fallback.Query(ctx, q)
	if err != nil {
		return DataResult{
			DataSource: DataSourceBashFallback,
			Error:      err.Error(),
			Latency:    time.Since(start),
		}, fmt.Errorf("fallback query failed (kind=%s): %w", q.Kind, err)
	}
	return DataResult{
		DataSource: DataSourceBashFallback,
		Data:       data,
		Latency:    time.Since(start),
	}, nil
}

// ---------- helpers ----------

func (r *DataSourceRouter) setHealthy(healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.corootHealthy = healthy
	r.lastHealthAt = time.Now()
}
