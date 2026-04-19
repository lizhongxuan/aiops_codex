package otel

import (
	"context"
	"testing"
	"time"
)

func TestTracer_SpanLLMCall(t *testing.T) {
	tracer := NewTracer("test-service")
	ctx := context.Background()

	_, span := tracer.SpanLLMCall(ctx, "gpt-4o")
	span.End()

	if span.Name != "llm.call" {
		t.Errorf("expected span name 'llm.call', got %s", span.Name)
	}
	if span.Attributes["llm.model"] != "gpt-4o" {
		t.Errorf("expected model attribute 'gpt-4o', got %s", span.Attributes["llm.model"])
	}
	if span.EndTime.IsZero() {
		t.Error("expected EndTime to be set after End()")
	}
}

func TestTracer_SpanToolExec(t *testing.T) {
	tracer := NewTracer("test-service")
	ctx := context.Background()

	_, span := tracer.SpanToolExec(ctx, "shell_command")
	span.SetStatus(SpanStatusOK)
	span.End()

	if span.Name != "tool.exec" {
		t.Errorf("expected span name 'tool.exec', got %s", span.Name)
	}
	if span.Attributes["tool.name"] != "shell_command" {
		t.Errorf("expected tool.name attribute")
	}
	if span.Status != SpanStatusOK {
		t.Errorf("expected status OK")
	}
}

func TestTracer_SpanAgentSpawn(t *testing.T) {
	tracer := NewTracer("test-service")
	ctx := context.Background()

	_, span := tracer.SpanAgentSpawn(ctx, "agent-123")
	span.End()

	if span.Attributes["agent.id"] != "agent-123" {
		t.Errorf("expected agent.id attribute")
	}
}

func TestTracer_Disabled(t *testing.T) {
	tracer := NewTracer("test-service")
	tracer.SetEnabled(false)

	ctx := context.Background()
	_, span := tracer.SpanLLMCall(ctx, "gpt-4o")
	span.End()

	spans := tracer.Spans()
	if len(spans) != 0 {
		t.Errorf("expected no spans when disabled, got %d", len(spans))
	}
}

func TestTracer_Reset(t *testing.T) {
	tracer := NewTracer("test-service")
	ctx := context.Background()

	tracer.SpanLLMCall(ctx, "gpt-4o")
	tracer.SpanToolExec(ctx, "shell")

	if len(tracer.Spans()) != 2 {
		t.Fatalf("expected 2 spans")
	}

	tracer.Reset()
	if len(tracer.Spans()) != 0 {
		t.Error("expected 0 spans after reset")
	}
}

func TestMetrics_RecordLLMLatency(t *testing.T) {
	m := NewMetrics()
	m.RecordLLMLatency(100 * time.Millisecond)
	m.RecordLLMLatency(200 * time.Millisecond)

	if m.LLMLatency().Count() != 2 {
		t.Errorf("expected 2 latency records, got %d", m.LLMLatency().Count())
	}
	if m.LLMLatency().Min() != 100 {
		t.Errorf("expected min 100, got %f", m.LLMLatency().Min())
	}
	if m.LLMLatency().Max() != 200 {
		t.Errorf("expected max 200, got %f", m.LLMLatency().Max())
	}
}

func TestMetrics_RecordTokenUsage(t *testing.T) {
	m := NewMetrics()
	m.RecordTokenUsage(500)
	m.RecordTokenUsage(300)

	if m.TokenUsage().Value() != 800 {
		t.Errorf("expected 800 tokens, got %d", m.TokenUsage().Value())
	}
}

func TestMetrics_RecordToolExec(t *testing.T) {
	m := NewMetrics()
	m.RecordToolExec()
	m.RecordToolExec()
	m.RecordToolExec()

	if m.ToolExecCount().Value() != 3 {
		t.Errorf("expected 3 tool execs, got %d", m.ToolExecCount().Value())
	}
}

func TestMetrics_RecordError(t *testing.T) {
	m := NewMetrics()
	m.RecordError()

	if m.ErrorRate().Value() != 1 {
		t.Errorf("expected 1 error, got %d", m.ErrorRate().Value())
	}
}

func TestInit_ValidExporters(t *testing.T) {
	for _, exp := range []ExporterType{ExporterOTLP, ExporterStdout, ExporterNoop} {
		ResetForTesting()
		err := Init(exp)
		if err != nil {
			t.Errorf("Init(%s) failed: %v", exp, err)
		}
	}
}

func TestInit_InvalidExporter(t *testing.T) {
	ResetForTesting()
	err := Init("invalid")
	if err == nil {
		t.Error("expected error for invalid exporter")
	}
}

func TestInit_GlobalTracer(t *testing.T) {
	ResetForTesting()
	Init(ExporterStdout)

	tracer := GlobalTracer()
	if tracer == nil {
		t.Fatal("expected non-nil global tracer")
	}
}

func TestInit_GlobalMetrics(t *testing.T) {
	ResetForTesting()
	Init(ExporterStdout)

	metrics := GlobalMetrics()
	if metrics == nil {
		t.Fatal("expected non-nil global metrics")
	}
}

func TestInit_NoopDisablesTracing(t *testing.T) {
	ResetForTesting()
	Init(ExporterNoop)

	tracer := GlobalTracer()
	ctx := context.Background()
	tracer.SpanLLMCall(ctx, "test")

	if len(tracer.Spans()) != 0 {
		t.Error("noop exporter should disable tracing")
	}
}
