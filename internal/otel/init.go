package otel

import (
	"fmt"
	"sync"
)

// ExporterType configures the telemetry exporter.
type ExporterType string

const (
	ExporterOTLP   ExporterType = "otlp"
	ExporterStdout ExporterType = "stdout"
	ExporterNoop   ExporterType = "noop"
)

// Provider holds the global telemetry provider state.
type Provider struct {
	tracer   *Tracer
	metrics  *Metrics
	exporter ExporterType
	mu       sync.RWMutex
}

var (
	globalProvider *Provider
	globalOnce     sync.Once
)

// Init initializes OpenTelemetry with the configured exporter.
// It sets up the global tracer and metrics provider.
func Init(exporter ExporterType) error {
	switch exporter {
	case ExporterOTLP, ExporterStdout, ExporterNoop:
		// Valid exporter types
	default:
		return fmt.Errorf("otel: unknown exporter type: %s", exporter)
	}

	globalOnce.Do(func() {
		globalProvider = &Provider{
			tracer:   NewTracer("aiops-codex"),
			metrics:  NewMetrics(),
			exporter: exporter,
		}

		if exporter == ExporterNoop {
			globalProvider.tracer.SetEnabled(false)
		}
	})

	return nil
}

// GlobalTracer returns the global tracer instance.
// Returns nil if Init has not been called.
func GlobalTracer() *Tracer {
	if globalProvider == nil {
		return nil
	}
	globalProvider.mu.RLock()
	defer globalProvider.mu.RUnlock()
	return globalProvider.tracer
}

// GlobalMetrics returns the global metrics instance.
// Returns nil if Init has not been called.
func GlobalMetrics() *Metrics {
	if globalProvider == nil {
		return nil
	}
	globalProvider.mu.RLock()
	defer globalProvider.mu.RUnlock()
	return globalProvider.metrics
}

// Shutdown gracefully shuts down the telemetry provider.
func Shutdown() error {
	if globalProvider == nil {
		return nil
	}
	globalProvider.mu.Lock()
	defer globalProvider.mu.Unlock()
	globalProvider.tracer.SetEnabled(false)
	return nil
}

// ResetForTesting resets the global provider (for testing only).
func ResetForTesting() {
	globalProvider = nil
	globalOnce = sync.Once{}
}
