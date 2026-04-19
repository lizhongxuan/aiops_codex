// Package otel provides OpenTelemetry tracing and metrics abstractions.
// It implements a minimal abstraction layer that can be wired to the full
// go.opentelemetry.io/otel SDK when dependencies are added.
package otel

import (
	"context"
	"sync"
	"time"
)

// Span represents a trace span for an operation.
type Span struct {
	Name       string
	TraceID    string
	SpanID     string
	StartTime  time.Time
	EndTime    time.Time
	Attributes map[string]string
	Status     SpanStatus
	mu         sync.Mutex
}

// SpanStatus represents the status of a span.
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// End marks the span as complete.
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndTime = time.Now()
}

// SetStatus sets the span status.
func (s *Span) SetStatus(status SpanStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

// SetAttribute sets a span attribute.
func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Attributes == nil {
		s.Attributes = make(map[string]string)
	}
	s.Attributes[key] = value
}

// Tracer wraps tracing functionality for key operations.
type Tracer struct {
	serviceName string
	enabled     bool
	mu          sync.Mutex
	spans       []*Span
}

// NewTracer creates a new Tracer instance.
func NewTracer(serviceName string) *Tracer {
	return &Tracer{
		serviceName: serviceName,
		enabled:     true,
	}
}

// SetEnabled enables or disables tracing.
func (t *Tracer) SetEnabled(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enabled = enabled
}

// SpanLLMCall creates a span for an LLM call.
func (t *Tracer) SpanLLMCall(ctx context.Context, model string) (context.Context, *Span) {
	span := t.startSpan("llm.call")
	span.SetAttribute("llm.model", model)
	span.SetAttribute("service.name", t.serviceName)
	return ctx, span
}

// SpanToolExec creates a span for tool execution.
func (t *Tracer) SpanToolExec(ctx context.Context, toolName string) (context.Context, *Span) {
	span := t.startSpan("tool.exec")
	span.SetAttribute("tool.name", toolName)
	span.SetAttribute("service.name", t.serviceName)
	return ctx, span
}

// SpanAgentSpawn creates a span for agent spawn.
func (t *Tracer) SpanAgentSpawn(ctx context.Context, agentID string) (context.Context, *Span) {
	span := t.startSpan("agent.spawn")
	span.SetAttribute("agent.id", agentID)
	span.SetAttribute("service.name", t.serviceName)
	return ctx, span
}

// startSpan creates and records a new span.
func (t *Tracer) startSpan(name string) *Span {
	span := &Span{
		Name:       name,
		TraceID:    generateID(),
		SpanID:     generateID(),
		StartTime:  time.Now(),
		Attributes: make(map[string]string),
	}

	t.mu.Lock()
	if t.enabled {
		t.spans = append(t.spans, span)
	}
	t.mu.Unlock()

	return span
}

// Spans returns all recorded spans (for testing/debugging).
func (t *Tracer) Spans() []*Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*Span, len(t.spans))
	copy(out, t.spans)
	return out
}

// Reset clears all recorded spans.
func (t *Tracer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.spans = nil
}
