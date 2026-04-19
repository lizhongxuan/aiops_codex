package bifrost

import (
	"math"
	"sync"
	"testing"
)

// almostEqual compares two floats within a small epsilon.
func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

// --- Record and Summary basic ---

func TestUsageTracker_RecordAndSummary(t *testing.T) {
	tr := NewUsageTracker()
	tr.Record(UsageRecord{
		SessionID:    "s1",
		Provider:     "openai",
		Model:        "gpt-4o",
		PromptTokens: 1000,
		OutputTokens: 500,
		CostUSD:      0.123,
	})

	s := tr.Summary()
	if s.TotalPromptTokens != 1000 {
		t.Errorf("TotalPromptTokens: got %d, want 1000", s.TotalPromptTokens)
	}
	if s.TotalOutputTokens != 500 {
		t.Errorf("TotalOutputTokens: got %d, want 500", s.TotalOutputTokens)
	}
	if !almostEqual(s.TotalCostUSD, 0.123, 1e-9) {
		t.Errorf("TotalCostUSD: got %f, want 0.123", s.TotalCostUSD)
	}
}

// --- Auto-calculate cost from pricing table ---

func TestUsageTracker_AutoCalculateCost(t *testing.T) {
	tr := NewUsageTracker()
	tr.Record(UsageRecord{
		Provider:     "openai",
		Model:        "gpt-4o",
		PromptTokens: 1_000_000,
		OutputTokens: 1_000_000,
		// CostUSD left at 0 → auto-calculate
	})

	s := tr.Summary()
	// gpt-4o: input $2.50/1M + output $10.00/1M = $12.50
	want := 12.50
	if !almostEqual(s.TotalCostUSD, want, 1e-6) {
		t.Errorf("auto-calc cost: got %f, want %f", s.TotalCostUSD, want)
	}
}

// --- Unknown model (cost stays 0) ---

func TestUsageTracker_UnknownModelCostZero(t *testing.T) {
	tr := NewUsageTracker()
	tr.Record(UsageRecord{
		Provider:     "custom",
		Model:        "my-custom-model",
		PromptTokens: 500,
		OutputTokens: 200,
	})

	s := tr.Summary()
	if s.TotalCostUSD != 0 {
		t.Errorf("unknown model cost: got %f, want 0", s.TotalCostUSD)
	}
}

// --- Multiple records aggregation ---

func TestUsageTracker_MultipleRecordsAggregation(t *testing.T) {
	tr := NewUsageTracker()
	tr.Record(UsageRecord{
		Provider:     "openai",
		Model:        "gpt-4o-mini",
		PromptTokens: 1_000_000,
		OutputTokens: 1_000_000,
	})
	tr.Record(UsageRecord{
		Provider:     "openai",
		Model:        "gpt-4o-mini",
		PromptTokens: 2_000_000,
		OutputTokens: 500_000,
	})

	s := tr.Summary()
	if s.TotalPromptTokens != 3_000_000 {
		t.Errorf("TotalPromptTokens: got %d, want 3000000", s.TotalPromptTokens)
	}
	if s.TotalOutputTokens != 1_500_000 {
		t.Errorf("TotalOutputTokens: got %d, want 1500000", s.TotalOutputTokens)
	}
	// gpt-4o-mini: input $0.15/1M, output $0.60/1M
	// rec1: 1M*0.15 + 1M*0.60 = 0.75
	// rec2: 2M*0.15 + 0.5M*0.60 = 0.30 + 0.30 = 0.60
	// total = 1.35
	if !almostEqual(s.TotalCostUSD, 1.35, 1e-6) {
		t.Errorf("TotalCostUSD: got %f, want 1.35", s.TotalCostUSD)
	}
}

// --- Per-model breakdown ---

func TestUsageTracker_PerModelBreakdown(t *testing.T) {
	tr := NewUsageTracker()
	tr.Record(UsageRecord{
		Provider:     "openai",
		Model:        "gpt-4o",
		PromptTokens: 100,
		OutputTokens: 50,
		CostUSD:      0.01,
	})
	tr.Record(UsageRecord{
		Provider:     "anthropic",
		Model:        "claude-sonnet-4-20250514",
		PromptTokens: 200,
		OutputTokens: 100,
		CostUSD:      0.02,
	})
	tr.Record(UsageRecord{
		Provider:     "openai",
		Model:        "gpt-4o",
		PromptTokens: 300,
		OutputTokens: 150,
		CostUSD:      0.03,
	})

	s := tr.Summary()

	gpt := s.ByModel["gpt-4o"]
	if gpt.Calls != 2 {
		t.Errorf("gpt-4o calls: got %d, want 2", gpt.Calls)
	}
	if gpt.PromptTokens != 400 {
		t.Errorf("gpt-4o prompt: got %d, want 400", gpt.PromptTokens)
	}
	if gpt.OutputTokens != 200 {
		t.Errorf("gpt-4o output: got %d, want 200", gpt.OutputTokens)
	}
	if !almostEqual(gpt.CostUSD, 0.04, 1e-9) {
		t.Errorf("gpt-4o cost: got %f, want 0.04", gpt.CostUSD)
	}

	claude := s.ByModel["claude-sonnet-4-20250514"]
	if claude.Calls != 1 {
		t.Errorf("claude calls: got %d, want 1", claude.Calls)
	}
	if claude.PromptTokens != 200 {
		t.Errorf("claude prompt: got %d, want 200", claude.PromptTokens)
	}
}

// --- Concurrent access ---

func TestUsageTracker_ConcurrentAccess(t *testing.T) {
	tr := NewUsageTracker()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Record(UsageRecord{
				Provider:     "openai",
				Model:        "gpt-4o",
				PromptTokens: 10,
				OutputTokens: 5,
			})
		}()
	}

	// Concurrent reads while writing
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tr.Summary()
		}()
	}

	wg.Wait()

	s := tr.Summary()
	if s.TotalPromptTokens != 1000 {
		t.Errorf("concurrent TotalPromptTokens: got %d, want 1000", s.TotalPromptTokens)
	}
	if s.TotalOutputTokens != 500 {
		t.Errorf("concurrent TotalOutputTokens: got %d, want 500", s.TotalOutputTokens)
	}
}
