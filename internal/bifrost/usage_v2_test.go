package bifrost

import "testing"

// ─── Task 11.15: Bifrost Usage V2 and Image Detail Tests ────────────────────

// --- 11.11: Token Usage Breakdown Tests ---

func TestUsageBreakdown_TotalTokens(t *testing.T) {
	u := UsageBreakdown{InputTokens: 100, OutputTokens: 50}
	if u.TotalTokens() != 150 {
		t.Errorf("expected 150, got %d", u.TotalTokens())
	}
}

func TestUsageBreakdown_EffectiveInputTokens(t *testing.T) {
	u := UsageBreakdown{InputTokens: 100, CachedTokens: 30}
	if u.EffectiveInputTokens() != 70 {
		t.Errorf("expected 70, got %d", u.EffectiveInputTokens())
	}

	// Edge case: cached > input (shouldn't happen but handle gracefully).
	u2 := UsageBreakdown{InputTokens: 10, CachedTokens: 20}
	if u2.EffectiveInputTokens() != 0 {
		t.Errorf("expected 0 for negative effective, got %d", u2.EffectiveInputTokens())
	}
}

func TestUsageBreakdown_CacheHitRatio(t *testing.T) {
	u := UsageBreakdown{InputTokens: 100, CachedTokens: 50}
	ratio := u.CacheHitRatio()
	if ratio != 0.5 {
		t.Errorf("expected 0.5, got %f", ratio)
	}

	// Zero input tokens.
	u2 := UsageBreakdown{InputTokens: 0, CachedTokens: 0}
	if u2.CacheHitRatio() != 0 {
		t.Errorf("expected 0 for zero input, got %f", u2.CacheHitRatio())
	}
}

func TestUsageTrackerV2_Record(t *testing.T) {
	tracker := NewUsageTrackerV2()
	tracker.Record(UsageRecord{
		Model:        "gpt-4o",
		PromptTokens: 1000,
		OutputTokens: 500,
	})

	breakdown := tracker.Breakdown()
	if breakdown == nil {
		t.Fatal("expected non-nil breakdown")
	}
	if breakdown.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", breakdown.InputTokens)
	}
	if breakdown.OutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", breakdown.OutputTokens)
	}
}

func TestUsageTrackerV2_RecordDetailed(t *testing.T) {
	tracker := NewUsageTrackerV2()
	tracker.RecordDetailed(UsageBreakdownRecord{
		UsageRecord: UsageRecord{
			Model:        "gpt-4o",
			PromptTokens: 2000,
			OutputTokens: 800,
		},
		ReasoningTokens: 200,
		CachedTokens:    500,
	})

	breakdown := tracker.Breakdown()
	if breakdown.ReasoningTokens != 200 {
		t.Errorf("expected 200 reasoning tokens, got %d", breakdown.ReasoningTokens)
	}
	if breakdown.CachedTokens != 500 {
		t.Errorf("expected 500 cached tokens, got %d", breakdown.CachedTokens)
	}
}

func TestUsageTrackerV2_SummaryV2(t *testing.T) {
	tracker := NewUsageTrackerV2()
	tracker.RecordDetailed(UsageBreakdownRecord{
		UsageRecord: UsageRecord{
			Model:        "gpt-4o",
			PromptTokens: 1000,
			OutputTokens: 500,
		},
		ReasoningTokens: 100,
		CachedTokens:    200,
	})
	tracker.RecordDetailed(UsageBreakdownRecord{
		UsageRecord: UsageRecord{
			Model:        "gpt-4o-mini",
			PromptTokens: 500,
			OutputTokens: 200,
		},
		CachedTokens: 100,
	})

	summary := tracker.SummaryV2()
	if summary.TotalInputTokens != 1500 {
		t.Errorf("expected 1500 total input, got %d", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens != 700 {
		t.Errorf("expected 700 total output, got %d", summary.TotalOutputTokens)
	}
	if summary.TotalReasoningTokens != 100 {
		t.Errorf("expected 100 total reasoning, got %d", summary.TotalReasoningTokens)
	}
	if summary.TotalCachedTokens != 300 {
		t.Errorf("expected 300 total cached, got %d", summary.TotalCachedTokens)
	}
	if len(summary.ByModel) != 2 {
		t.Errorf("expected 2 models, got %d", len(summary.ByModel))
	}
}

func TestUsageTrackerV2_BreakdownEmpty(t *testing.T) {
	tracker := NewUsageTrackerV2()
	if tracker.Breakdown() != nil {
		t.Error("expected nil breakdown for empty tracker")
	}
}

// --- 11.12: Image Detail Handling Tests ---

func TestApplyImageDetail(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: "describe this image"},
			{Type: "image_url", ImageURL: &ContentImageURL{URL: "https://example.com/img.png"}},
		},
	}

	ApplyImageDetail(&msg, ImageDetailHigh)

	blocks := msg.Content.([]ContentBlock)
	if blocks[1].ImageURL.Detail != "high" {
		t.Errorf("expected detail 'high', got %q", blocks[1].ImageURL.Detail)
	}
}

func TestApplyImageDetail_NonImageContent(t *testing.T) {
	msg := Message{Role: "user", Content: "plain text"}
	// Should not panic on non-image content.
	ApplyImageDetail(&msg, ImageDetailHigh)
	if msg.Content != "plain text" {
		t.Error("expected content to be unchanged")
	}
}

func TestNewImageContentBlock(t *testing.T) {
	block := NewImageContentBlock("https://example.com/img.png", ImageDetailLow)
	if block.Type != "image_url" {
		t.Errorf("expected type 'image_url', got %q", block.Type)
	}
	if block.ImageURL.URL != "https://example.com/img.png" {
		t.Errorf("unexpected URL: %q", block.ImageURL.URL)
	}
	if block.ImageURL.Detail != "low" {
		t.Errorf("expected detail 'low', got %q", block.ImageURL.Detail)
	}
}

func TestEstimateImageTokens(t *testing.T) {
	tests := []struct {
		detail   ImageDetailLevel
		width    int
		height   int
		expected int
	}{
		{ImageDetailLow, 0, 0, 85},
		{ImageDetailLow, 1024, 768, 85},
		{ImageDetailHigh, 512, 512, 255},  // 1 tile * 170 + 85
		{ImageDetailHigh, 1024, 1024, 765}, // 4 tiles * 170 + 85
		{ImageDetailAuto, 0, 0, 765},       // default for unknown dimensions
		{ImageDetailHigh, 0, 0, 765},       // default for unknown dimensions
	}

	for _, tt := range tests {
		got := EstimateImageTokens(tt.detail, tt.width, tt.height)
		if got != tt.expected {
			t.Errorf("EstimateImageTokens(%s, %d, %d) = %d, want %d",
				tt.detail, tt.width, tt.height, got, tt.expected)
		}
	}
}

func TestDefaultImageConfig(t *testing.T) {
	cfg := DefaultImageConfig()
	if cfg.DefaultDetail != ImageDetailAuto {
		t.Errorf("expected auto detail, got %v", cfg.DefaultDetail)
	}
	if cfg.MaxImageSize != 2048 {
		t.Errorf("expected max size 2048, got %d", cfg.MaxImageSize)
	}
	if !cfg.AllowBase64 {
		t.Error("expected AllowBase64 to be true")
	}
}
