package bifrost

import "sync"

// ─── Task 11.11: Token Usage Breakdown ──────────────────────────────────────

// UsageBreakdown provides a detailed breakdown of token usage for a single LLM call.
type UsageBreakdown struct {
	// InputTokens is the total number of input/prompt tokens.
	InputTokens int `json:"input_tokens"`
	// OutputTokens is the total number of output/completion tokens.
	OutputTokens int `json:"output_tokens"`
	// ReasoningTokens is the number of tokens used for chain-of-thought reasoning.
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	// CachedTokens is the number of tokens served from cache (prompt cache hit).
	CachedTokens int `json:"cached_tokens,omitempty"`
	// CostUSD is the computed cost for this call.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// TotalTokens returns the sum of input and output tokens.
func (u UsageBreakdown) TotalTokens() int {
	return u.InputTokens + u.OutputTokens
}

// EffectiveInputTokens returns input tokens minus cached tokens (actual compute).
func (u UsageBreakdown) EffectiveInputTokens() int {
	effective := u.InputTokens - u.CachedTokens
	if effective < 0 {
		return 0
	}
	return effective
}

// CacheHitRatio returns the fraction of input tokens that were cached.
func (u UsageBreakdown) CacheHitRatio() float64 {
	if u.InputTokens == 0 {
		return 0
	}
	return float64(u.CachedTokens) / float64(u.InputTokens)
}

// UsageBreakdownRecord extends UsageRecord with detailed breakdown fields.
type UsageBreakdownRecord struct {
	UsageRecord
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	CachedTokens    int `json:"cached_tokens,omitempty"`
}

// UsageTrackerV2 extends UsageTracker with detailed breakdown tracking.
type UsageTrackerV2 struct {
	mu      sync.Mutex
	records []UsageBreakdownRecord
}

// NewUsageTrackerV2 creates a V2 usage tracker.
func NewUsageTrackerV2() *UsageTrackerV2 {
	return &UsageTrackerV2{}
}

// Record implements the Tracker interface with basic fields.
func (t *UsageTrackerV2) Record(rec UsageRecord) {
	t.RecordDetailed(UsageBreakdownRecord{UsageRecord: rec})
}

// RecordDetailed records a usage entry with full breakdown.
func (t *UsageTrackerV2) RecordDetailed(rec UsageBreakdownRecord) {
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

// Breakdown returns the usage breakdown for the most recent call.
func (t *UsageTrackerV2) Breakdown() *UsageBreakdown {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.records) == 0 {
		return nil
	}
	last := t.records[len(t.records)-1]
	return &UsageBreakdown{
		InputTokens:     last.PromptTokens,
		OutputTokens:    last.OutputTokens,
		ReasoningTokens: last.ReasoningTokens,
		CachedTokens:    last.CachedTokens,
		CostUSD:         last.CostUSD,
	}
}

// SummaryV2 returns an aggregate view with breakdown details.
func (t *UsageTrackerV2) SummaryV2() UsageSummaryV2 {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := UsageSummaryV2{ByModel: make(map[string]ModelUsageV2)}
	for _, r := range t.records {
		s.TotalInputTokens += r.PromptTokens
		s.TotalOutputTokens += r.OutputTokens
		s.TotalReasoningTokens += r.ReasoningTokens
		s.TotalCachedTokens += r.CachedTokens
		s.TotalCostUSD += r.CostUSD

		m := s.ByModel[r.Model]
		m.InputTokens += r.PromptTokens
		m.OutputTokens += r.OutputTokens
		m.ReasoningTokens += r.ReasoningTokens
		m.CachedTokens += r.CachedTokens
		m.CostUSD += r.CostUSD
		m.Calls++
		s.ByModel[r.Model] = m
	}
	return s
}

// UsageSummaryV2 is the V2 aggregate usage view.
type UsageSummaryV2 struct {
	TotalInputTokens     int                    `json:"total_input_tokens"`
	TotalOutputTokens    int                    `json:"total_output_tokens"`
	TotalReasoningTokens int                    `json:"total_reasoning_tokens"`
	TotalCachedTokens    int                    `json:"total_cached_tokens"`
	TotalCostUSD         float64                `json:"total_cost_usd"`
	ByModel              map[string]ModelUsageV2 `json:"by_model"`
}

// ModelUsageV2 is per-model usage with breakdown.
type ModelUsageV2 struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	ReasoningTokens int     `json:"reasoning_tokens"`
	CachedTokens    int     `json:"cached_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	Calls           int     `json:"calls"`
}

// ─── Task 11.12: Image Detail Handling ──────────────────────────────────────

// ImageDetailLevel represents the detail level for image processing.
type ImageDetailLevel string

const (
	ImageDetailLow  ImageDetailLevel = "low"
	ImageDetailHigh ImageDetailLevel = "high"
	ImageDetailAuto ImageDetailLevel = "auto"
)

// ImageConfig holds configuration for image handling in messages.
type ImageConfig struct {
	// DefaultDetail is the default detail level for images.
	DefaultDetail ImageDetailLevel `json:"default_detail"`
	// MaxImageSize is the maximum image dimension (pixels) before downscaling.
	MaxImageSize int `json:"max_image_size,omitempty"`
	// AllowBase64 controls whether base64-encoded images are accepted.
	AllowBase64 bool `json:"allow_base64"`
}

// DefaultImageConfig returns sensible defaults for image handling.
func DefaultImageConfig() ImageConfig {
	return ImageConfig{
		DefaultDetail: ImageDetailAuto,
		MaxImageSize:  2048,
		AllowBase64:   true,
	}
}

// ApplyImageDetail sets the detail level on all image content blocks in a message.
func ApplyImageDetail(msg *Message, detail ImageDetailLevel) {
	blocks, ok := msg.Content.([]ContentBlock)
	if !ok {
		return
	}
	for i := range blocks {
		if blocks[i].Type == "image_url" && blocks[i].ImageURL != nil {
			blocks[i].ImageURL.Detail = string(detail)
		}
	}
	msg.Content = blocks
}

// NewImageContentBlock creates a content block for an image URL with the specified detail.
func NewImageContentBlock(url string, detail ImageDetailLevel) ContentBlock {
	return ContentBlock{
		Type: "image_url",
		ImageURL: &ContentImageURL{
			URL:    url,
			Detail: string(detail),
		},
	}
}

// EstimateImageTokens estimates token usage for an image based on detail level.
// Based on OpenAI's image token calculation:
// - low: 85 tokens
// - high: 170 tokens per 512x512 tile + 85 base
// - auto: assumes high for estimation
func EstimateImageTokens(detail ImageDetailLevel, width, height int) int {
	switch detail {
	case ImageDetailLow:
		return 85
	case ImageDetailHigh, ImageDetailAuto:
		if width == 0 || height == 0 {
			return 765 // default estimate for unknown dimensions
		}
		// Calculate tiles (512x512 each).
		tilesW := (width + 511) / 512
		tilesH := (height + 511) / 512
		return 170*tilesW*tilesH + 85
	default:
		return 85
	}
}
