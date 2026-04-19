package coroot

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------- test helpers ----------

type stubProvider struct {
	name    string
	data    any
	err     error
	called  int
}

func (s *stubProvider) Query(_ context.Context, _ DataQuery) (any, error) {
	s.called++
	return s.data, s.err
}

func (s *stubProvider) Name() string { return s.name }

// ---------- ParsePriorityStrategy ----------

func TestParsePriorityStrategy(t *testing.T) {
	tests := []struct {
		input string
		want  PriorityStrategy
	}{
		{"coroot_first", PriorityCorootFirst},
		{"COROOT_FIRST", PriorityCorootFirst},
		{"local_first", PriorityLocalFirst},
		{"coroot_only", PriorityCorootOnly},
		{"", PriorityCorootFirst},
		{"unknown", PriorityCorootFirst},
		{"  coroot_only  ", PriorityCorootOnly},
	}
	for _, tt := range tests {
		got := ParsePriorityStrategy(tt.input)
		if got != tt.want {
			t.Errorf("ParsePriorityStrategy(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------- coroot_first strategy ----------

func TestRouteCorootFirst_CorootSucceeds(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "coroot-data"}
	fb := &stubProvider{name: "fallback", data: "fallback-data"}
	r := NewDataSourceRouter(cp, fb, PriorityCorootFirst)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceCoroot {
		t.Errorf("DataSource = %q, want %q", res.DataSource, DataSourceCoroot)
	}
	if res.Data != "coroot-data" {
		t.Errorf("Data = %v, want %q", res.Data, "coroot-data")
	}
	if cp.called != 1 {
		t.Errorf("coroot called %d times, want 1", cp.called)
	}
	if fb.called != 0 {
		t.Errorf("fallback called %d times, want 0", fb.called)
	}
}

func TestRouteCorootFirst_CorootFails_FallbackSucceeds(t *testing.T) {
	cp := &stubProvider{name: "coroot", err: errors.New("timeout")}
	fb := &stubProvider{name: "fallback", data: "fallback-data"}
	r := NewDataSourceRouter(cp, fb, PriorityCorootFirst)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServiceOverview, ServiceID: "svc-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceBashFallback {
		t.Errorf("DataSource = %q, want %q", res.DataSource, DataSourceBashFallback)
	}
	if res.Data != "fallback-data" {
		t.Errorf("Data = %v, want %q", res.Data, "fallback-data")
	}
	if cp.called != 1 {
		t.Errorf("coroot called %d times, want 1", cp.called)
	}
	if fb.called != 1 {
		t.Errorf("fallback called %d times, want 1", fb.called)
	}
}

func TestRouteCorootFirst_BothFail(t *testing.T) {
	cp := &stubProvider{name: "coroot", err: errors.New("coroot down")}
	fb := &stubProvider{name: "fallback", err: errors.New("fallback down")}
	r := NewDataSourceRouter(cp, fb, PriorityCorootFirst)

	_, err := r.Route(context.Background(), DataQuery{Kind: QueryTopology})
	if err == nil {
		t.Fatal("expected error when both sources fail")
	}
}

// ---------- coroot_only strategy ----------

func TestRouteCorootOnly_Succeeds(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "coroot-data"}
	r := NewDataSourceRouter(cp, nil, PriorityCorootOnly)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceCoroot {
		t.Errorf("DataSource = %q, want %q", res.DataSource, DataSourceCoroot)
	}
}

func TestRouteCorootOnly_Fails(t *testing.T) {
	cp := &stubProvider{name: "coroot", err: errors.New("unavailable")}
	r := NewDataSourceRouter(cp, nil, PriorityCorootOnly)

	_, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err == nil {
		t.Fatal("expected error when coroot_only and coroot fails")
	}
}

func TestRouteCorootOnly_NilProvider(t *testing.T) {
	r := NewDataSourceRouter(nil, nil, PriorityCorootOnly)

	_, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err == nil {
		t.Fatal("expected error when coroot provider is nil")
	}
}

// ---------- local_first strategy ----------

func TestRouteLocalFirst_FallbackSucceeds(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "coroot-data"}
	fb := &stubProvider{name: "fallback", data: "local-data"}
	r := NewDataSourceRouter(cp, fb, PriorityLocalFirst)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServiceAlerts, ServiceID: "svc-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceBashFallback {
		t.Errorf("DataSource = %q, want %q", res.DataSource, DataSourceBashFallback)
	}
	if cp.called != 0 {
		t.Errorf("coroot should not be called when local succeeds, called %d", cp.called)
	}
}

func TestRouteLocalFirst_FallbackFails_CorootSucceeds(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "coroot-data"}
	fb := &stubProvider{name: "fallback", err: errors.New("local fail")}
	r := NewDataSourceRouter(cp, fb, PriorityLocalFirst)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServiceMetrics})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceCoroot {
		t.Errorf("DataSource = %q, want %q", res.DataSource, DataSourceCoroot)
	}
}

// ---------- HealthCheck ----------

func TestHealthCheck_Success(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: []Service{}}
	r := NewDataSourceRouter(cp, nil, PriorityCorootFirst)

	err := r.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.IsCorootHealthy() {
		t.Error("expected coroot to be healthy after successful check")
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	cp := &stubProvider{name: "coroot", err: errors.New("connection refused")}
	r := NewDataSourceRouter(cp, nil, PriorityCorootFirst)

	err := r.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error from failed health check")
	}
	if r.IsCorootHealthy() {
		t.Error("expected coroot to be unhealthy after failed check")
	}
}

// ---------- DataResult annotation ----------

func TestDataResultAlwaysAnnotated(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "ok"}
	fb := &stubProvider{name: "fallback", data: "ok"}
	r := NewDataSourceRouter(cp, fb, PriorityCorootFirst)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceCoroot && res.DataSource != DataSourceBashFallback {
		t.Errorf("DataSource = %q, want one of {coroot_mcp, bash_fallback}", res.DataSource)
	}
	if res.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestDataResultLatencyPositive(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "ok"}
	r := NewDataSourceRouter(cp, nil, PriorityCorootFirst)

	res, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Latency < 0 {
		t.Errorf("Latency = %v, want >= 0", res.Latency)
	}
}

// ---------- health state transitions ----------

func TestHealthStateTransitions(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "ok"}
	r := NewDataSourceRouter(cp, nil, PriorityCorootFirst)

	// Initially healthy (optimistic).
	if !r.IsCorootHealthy() {
		t.Error("expected initially healthy")
	}

	// Fail the provider.
	cp.err = errors.New("down")
	cp.data = nil
	_ = r.HealthCheck(context.Background())
	if r.IsCorootHealthy() {
		t.Error("expected unhealthy after failed check")
	}

	// Recover.
	cp.err = nil
	cp.data = "ok"
	_ = r.HealthCheck(context.Background())
	if !r.IsCorootHealthy() {
		t.Error("expected healthy after successful check")
	}
}

// ---------- edge: nil fallback with coroot_first ----------

func TestRouteCorootFirst_NilFallback_CorootFails(t *testing.T) {
	cp := &stubProvider{name: "coroot", err: errors.New("fail")}
	r := NewDataSourceRouter(cp, nil, PriorityCorootFirst)

	_, err := r.Route(context.Background(), DataQuery{Kind: QueryServices})
	if err == nil {
		t.Fatal("expected error when coroot fails and no fallback")
	}
}

// ---------- verify strategy getter ----------

func TestStrategyGetter(t *testing.T) {
	r := NewDataSourceRouter(nil, nil, PriorityLocalFirst)
	if r.Strategy() != PriorityLocalFirst {
		t.Errorf("Strategy() = %q, want %q", r.Strategy(), PriorityLocalFirst)
	}
}

// ---------- context cancellation ----------

func TestRouteRespectsContextCancellation(t *testing.T) {
	slowProvider := &stubProvider{name: "coroot"}
	// Simulate a provider that would succeed but context is already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	slowProvider.err = ctx.Err()
	fb := &stubProvider{name: "fallback", data: "fallback-ok"}
	r := NewDataSourceRouter(slowProvider, fb, PriorityCorootFirst)

	res, err := r.Route(ctx, DataQuery{Kind: QueryServices})
	// Should fall back since coroot "failed" (context cancelled).
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DataSource != DataSourceBashFallback {
		t.Errorf("DataSource = %q, want %q", res.DataSource, DataSourceBashFallback)
	}
}

// ---------- concurrent safety ----------

func TestConcurrentRouting(t *testing.T) {
	cp := &stubProvider{name: "coroot", data: "ok"}
	fb := &stubProvider{name: "fallback", data: "ok"}
	r := NewDataSourceRouter(cp, fb, PriorityCorootFirst)

	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = r.Route(context.Background(), DataQuery{Kind: QueryServices})
			_ = r.HealthCheck(context.Background())
			_ = r.IsCorootHealthy()
		}()
	}

	timeout := time.After(5 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("concurrent test timed out")
		}
	}
}
