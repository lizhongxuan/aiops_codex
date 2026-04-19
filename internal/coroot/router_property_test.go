package coroot

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
)

// ---------- Property-Based Tests for DataSourceRouter ----------
//
// **Validates: Requirements 1.1, 1.2**

// routeScenario captures the inputs that drive a single routing decision.
type routeScenario struct {
	QueryKindIdx    int  // index into allQueryKinds
	CorootSucceeds  bool // whether the coroot provider returns success
	FallbackSucceeds bool // whether the fallback provider returns success
	StrategyIdx     int  // index into allStrategies
}

var allQueryKinds = []QueryKind{
	QueryServices, QueryServiceOverview, QueryServiceMetrics,
	QueryServiceAlerts, QueryTopology, QueryIncidentTimeline,
	QueryRCA, QueryHostOverview,
}

var allStrategies = []PriorityStrategy{
	PriorityCorootFirst, PriorityLocalFirst, PriorityCorootOnly,
}

// Generate implements quick.Generator so testing/quick can produce random scenarios.
func (routeScenario) Generate(rand *rand.Rand, size int) reflect.Value {
	return reflect.ValueOf(routeScenario{
		QueryKindIdx:     rand.Intn(len(allQueryKinds)),
		CorootSucceeds:   rand.Intn(2) == 0,
		FallbackSucceeds: rand.Intn(2) == 0,
		StrategyIdx:      rand.Intn(len(allStrategies)),
	})
}

// buildRouter creates a router from a scenario, returning the router and whether
// we expect at least one provider to succeed.
func buildRouter(s routeScenario) (*DataSourceRouter, DataQuery) {
	strategy := allStrategies[s.StrategyIdx%len(allStrategies)]
	kind := allQueryKinds[s.QueryKindIdx%len(allQueryKinds)]

	var corootData any
	var corootErr error
	if s.CorootSucceeds {
		corootData = "coroot-ok"
	} else {
		corootErr = errors.New("coroot unavailable")
	}

	var fbData any
	var fbErr error
	if s.FallbackSucceeds {
		fbData = "fallback-ok"
	} else {
		fbErr = errors.New("fallback unavailable")
	}

	cp := &stubProvider{name: "coroot", data: corootData, err: corootErr}
	fb := &stubProvider{name: "fallback", data: fbData, err: fbErr}

	r := NewDataSourceRouter(cp, fb, strategy)
	q := DataQuery{Kind: kind, ServiceID: "svc-prop-test"}
	return r, q
}

// Property 1: 路由一致性 (Routing Consistency)
// For the same query and same provider state, the Router always returns the
// same DataSource. We call Route twice with identical inputs and assert the
// DataSource (and error presence) are identical.
//
// **Validates: Requirements 1.1, 1.2**
func TestProperty_RoutingConsistency(t *testing.T) {
	prop := func(s routeScenario) bool {
		// Build two identical routers from the same scenario.
		r1, q1 := buildRouter(s)
		r2, q2 := buildRouter(s)

		ctx := context.Background()

		res1, err1 := r1.Route(ctx, q1)
		res2, err2 := r2.Route(ctx, q2)

		// Both calls must agree on error presence.
		if (err1 == nil) != (err2 == nil) {
			return false
		}

		// When both succeed, the DataSource must be identical.
		if err1 == nil && err2 == nil {
			if res1.DataSource != res2.DataSource {
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_RoutingConsistency failed: %v", err)
	}
}

// Property 1b: DataSource annotation is always valid.
// Every successful Route call returns a DataSource in {coroot_mcp, bash_fallback}.
// This is a sub-property of routing consistency — the result set is closed.
//
// **Validates: Requirements 1.1, 1.2**
func TestProperty_DataSourceAlwaysAnnotated(t *testing.T) {
	prop := func(s routeScenario) bool {
		r, q := buildRouter(s)
		res, err := r.Route(context.Background(), q)
		if err != nil {
			// Errors are fine — no annotation to check.
			return true
		}
		return res.DataSource == DataSourceCoroot || res.DataSource == DataSourceBashFallback
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_DataSourceAlwaysAnnotated failed: %v", err)
	}
}

// Property 1c: Strategy-specific determinism.
// For coroot_first: if coroot succeeds, DataSource is always coroot_mcp.
// For local_first: if fallback succeeds, DataSource is always bash_fallback.
// For coroot_only: DataSource is always coroot_mcp (or error).
//
// **Validates: Requirements 1.1, 1.2**
func TestProperty_StrategyDeterminism(t *testing.T) {
	prop := func(s routeScenario) bool {
		strategy := allStrategies[s.StrategyIdx%len(allStrategies)]
		r, q := buildRouter(s)
		res, err := r.Route(context.Background(), q)

		switch strategy {
		case PriorityCorootFirst:
			if s.CorootSucceeds {
				// Must use coroot when it succeeds.
				return err == nil && res.DataSource == DataSourceCoroot
			}
			if !s.CorootSucceeds && s.FallbackSucceeds {
				// Must fall back.
				return err == nil && res.DataSource == DataSourceBashFallback
			}
			// Both fail — must error.
			return err != nil

		case PriorityLocalFirst:
			if s.FallbackSucceeds {
				// Must use fallback when it succeeds.
				return err == nil && res.DataSource == DataSourceBashFallback
			}
			if !s.FallbackSucceeds && s.CorootSucceeds {
				// Must fall back to coroot.
				return err == nil && res.DataSource == DataSourceCoroot
			}
			// Both fail — must error.
			return err != nil

		case PriorityCorootOnly:
			if s.CorootSucceeds {
				return err == nil && res.DataSource == DataSourceCoroot
			}
			// Coroot fails — must error (no fallback).
			return err != nil
		}

		return false
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_StrategyDeterminism failed: %v", err)
	}
}
