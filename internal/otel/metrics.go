package otel

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects operational metrics for the system.
type Metrics struct {
	llmLatency    *Histogram
	tokenUsage    *Counter
	toolExecCount *Counter
	errorRate     *Counter
	mu            sync.RWMutex
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		llmLatency:    NewHistogram("llm.latency", "LLM call latency in milliseconds"),
		tokenUsage:    NewCounter("llm.token_usage", "Total tokens used"),
		toolExecCount: NewCounter("tool.exec_count", "Total tool executions"),
		errorRate:     NewCounter("error.count", "Total errors"),
	}
}

// RecordLLMLatency records the latency of an LLM call.
func (m *Metrics) RecordLLMLatency(duration time.Duration) {
	m.llmLatency.Record(float64(duration.Milliseconds()))
}

// RecordTokenUsage records token usage for an LLM call.
func (m *Metrics) RecordTokenUsage(tokens int64) {
	m.tokenUsage.Add(tokens)
}

// RecordToolExec records a tool execution.
func (m *Metrics) RecordToolExec() {
	m.toolExecCount.Add(1)
}

// RecordError records an error occurrence.
func (m *Metrics) RecordError() {
	m.errorRate.Add(1)
}

// LLMLatency returns the LLM latency histogram.
func (m *Metrics) LLMLatency() *Histogram {
	return m.llmLatency
}

// TokenUsage returns the token usage counter.
func (m *Metrics) TokenUsage() *Counter {
	return m.tokenUsage
}

// ToolExecCount returns the tool execution counter.
func (m *Metrics) ToolExecCount() *Counter {
	return m.toolExecCount
}

// ErrorRate returns the error counter.
func (m *Metrics) ErrorRate() *Counter {
	return m.errorRate
}

// Counter is a simple monotonically increasing counter.
type Counter struct {
	name        string
	description string
	value       atomic.Int64
}

// NewCounter creates a new Counter.
func NewCounter(name, description string) *Counter {
	return &Counter{
		name:        name,
		description: description,
	}
}

// Add increments the counter by the given value.
func (c *Counter) Add(delta int64) {
	c.value.Add(delta)
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	return c.value.Load()
}

// Name returns the counter name.
func (c *Counter) Name() string {
	return c.name
}

// Histogram records distribution of values.
type Histogram struct {
	name        string
	description string
	mu          sync.Mutex
	values      []float64
	count       int64
	sum         float64
	min         float64
	max         float64
}

// NewHistogram creates a new Histogram.
func NewHistogram(name, description string) *Histogram {
	return &Histogram{
		name:        name,
		description: description,
	}
}

// Record adds a value to the histogram.
func (h *Histogram) Record(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.values = append(h.values, value)
	h.count++
	h.sum += value
	if h.count == 1 || value < h.min {
		h.min = value
	}
	if value > h.max {
		h.max = value
	}
}

// Count returns the number of recorded values.
func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// Sum returns the sum of all recorded values.
func (h *Histogram) Sum() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum
}

// Min returns the minimum recorded value.
func (h *Histogram) Min() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.min
}

// Max returns the maximum recorded value.
func (h *Histogram) Max() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.max
}

// Name returns the histogram name.
func (h *Histogram) Name() string {
	return h.name
}
