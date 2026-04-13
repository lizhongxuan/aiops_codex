package bifrost

import "sync"

// modelPricing holds per-1M-token pricing for a model.
type modelPricing struct {
	InputPer1M  float64
	OutputPer1M float64
}

// pricingTable maps well-known model names to their token pricing.
var pricingTable = map[string]modelPricing{
	"gpt-4o":                     {InputPer1M: 2.50, OutputPer1M: 10.00},
	"gpt-4o-mini":                {InputPer1M: 0.15, OutputPer1M: 0.60},
	"gpt-4-turbo":                {InputPer1M: 10.00, OutputPer1M: 30.00},
	"claude-sonnet-4-20250514":   {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-3-5-haiku-20241022":  {InputPer1M: 1.00, OutputPer1M: 5.00},
	"claude-3-opus-20240229":     {InputPer1M: 15.00, OutputPer1M: 75.00},
}

// ModelUsage aggregates token usage and cost for a single model.
type ModelUsage struct {
	PromptTokens int
	OutputTokens int
	CostUSD      float64
	Calls        int
}

// UsageSummary is the aggregate view returned by UsageTracker.Summary().
type UsageSummary struct {
	TotalPromptTokens int
	TotalOutputTokens int
	TotalCostUSD      float64
	ByModel           map[string]ModelUsage
}

// UsageTracker records per-call token usage and computes cost.
// It implements the Tracker interface declared in gateway.go.
type UsageTracker struct {
	mu      sync.Mutex
	records []UsageRecord
}

// NewUsageTracker returns a ready-to-use UsageTracker.
func NewUsageTracker() *UsageTracker {
	return &UsageTracker{}
}

// Record appends a UsageRecord. If CostUSD is zero and the model is in the
// built-in pricing table, the cost is auto-calculated.
func (t *UsageTracker) Record(rec UsageRecord) {
	if rec.CostUSD == 0 {
		if p, ok := pricingTable[rec.Model]; ok {
			rec.CostUSD = float64(rec.PromptTokens)*p.InputPer1M/1_000_000 +
				float64(rec.OutputTokens)*p.OutputPer1M/1_000_000
		}
	}
	t.mu.Lock()
	t.records = append(t.records, rec)
	t.mu.Unlock()
}

// Summary returns an aggregate view of all recorded usage.
func (t *UsageTracker) Summary() UsageSummary {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := UsageSummary{ByModel: make(map[string]ModelUsage)}
	for _, r := range t.records {
		s.TotalPromptTokens += r.PromptTokens
		s.TotalOutputTokens += r.OutputTokens
		s.TotalCostUSD += r.CostUSD

		m := s.ByModel[r.Model]
		m.PromptTokens += r.PromptTokens
		m.OutputTokens += r.OutputTokens
		m.CostUSD += r.CostUSD
		m.Calls++
		s.ByModel[r.Model] = m
	}
	return s
}
