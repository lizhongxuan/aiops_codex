package server

import "strings"

const (
	ToolDisplayBlockText            = "text"
	ToolDisplayBlockKVList          = "kv_list"
	ToolDisplayBlockCommand         = "command"
	ToolDisplayBlockFilePreview     = "file_preview"
	ToolDisplayBlockFileDiffSummary = "file_diff_summary"
	ToolDisplayBlockSearchQueries   = "search_queries"
	ToolDisplayBlockLinkList        = "link_list"
	ToolDisplayBlockResultStats     = "result_stats"
	ToolDisplayBlockWarning         = "warning"
)

// ToolProgressEvent represents progress updates in the unified display contract.
type ToolProgressEvent struct {
	Invocation ToolInvocation
	Update     ToolProgressUpdate
}

// ToolDisplayAdapter turns tool use/progress/result states into a Vue-compatible
// display payload that can later be projected into durable cards/snapshots.
type ToolDisplayAdapter interface {
	RenderUse(req ToolCallRequest) *ToolDisplayPayload
	RenderProgress(progress ToolProgressEvent) *ToolDisplayPayload
	RenderResult(result ToolCallResult) *ToolDisplayPayload
}

// ToolDisplayPayload is the durable display intent emitted by a tool.
type ToolDisplayPayload struct {
	Summary   string
	Activity  string
	Blocks    []ToolDisplayBlock
	FinalCard *ToolFinalCardDescriptor
	SkipCards bool
	Metadata  map[string]any
}

// Clone returns a deep-enough copy for descriptor persistence and testing.
func (payload ToolDisplayPayload) Clone() ToolDisplayPayload {
	payload.Summary = strings.TrimSpace(payload.Summary)
	payload.Activity = strings.TrimSpace(payload.Activity)
	if payload.Blocks != nil {
		cloned := make([]ToolDisplayBlock, 0, len(payload.Blocks))
		for _, block := range payload.Blocks {
			cloned = append(cloned, block.Clone())
		}
		payload.Blocks = cloned
	}
	if payload.FinalCard != nil {
		cloned := payload.FinalCard.Clone()
		payload.FinalCard = &cloned
	}
	payload.Metadata = cloneNestedAnyMap(payload.Metadata)
	return payload
}

// ToolDisplayBlock is a structured UI block consumed by the Vue renderer.
type ToolDisplayBlock struct {
	Kind     string
	Title    string
	Text     string
	Items    []map[string]any
	Metadata map[string]any
}

// Clone returns a copy with copied nested items/maps.
func (block ToolDisplayBlock) Clone() ToolDisplayBlock {
	block.Kind = strings.TrimSpace(block.Kind)
	block.Title = strings.TrimSpace(block.Title)
	block.Text = strings.TrimSpace(block.Text)
	if block.Items != nil {
		items := make([]map[string]any, 0, len(block.Items))
		for _, item := range block.Items {
			items = append(items, cloneNestedAnyMap(item))
		}
		block.Items = items
	}
	block.Metadata = cloneNestedAnyMap(block.Metadata)
	return block
}

// ToolFinalCardDescriptor describes a final projected card emitted by a tool.
type ToolFinalCardDescriptor struct {
	CardID    string
	CardType  string
	Title     string
	Text      string
	Summary   string
	Status    string
	Command   string
	Cwd       string
	HostID    string
	HostName  string
	Detail    map[string]any
	CreatedAt string
	UpdatedAt string
}

// Clone returns a copy with copied detail maps.
func (card ToolFinalCardDescriptor) Clone() ToolFinalCardDescriptor {
	card.CardID = strings.TrimSpace(card.CardID)
	card.CardType = strings.TrimSpace(card.CardType)
	card.Title = strings.TrimSpace(card.Title)
	card.Text = strings.TrimSpace(card.Text)
	card.Summary = strings.TrimSpace(card.Summary)
	card.Status = strings.TrimSpace(card.Status)
	card.Command = strings.TrimSpace(card.Command)
	card.Cwd = strings.TrimSpace(card.Cwd)
	card.HostID = strings.TrimSpace(card.HostID)
	card.HostName = strings.TrimSpace(card.HostName)
	card.CreatedAt = strings.TrimSpace(card.CreatedAt)
	card.UpdatedAt = strings.TrimSpace(card.UpdatedAt)
	card.Detail = cloneNestedAnyMap(card.Detail)
	return card
}

func cloneNestedAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneNestedAnyValue(value)
	}
	return out
}

func cloneNestedAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneNestedAnyMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneNestedAnyMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneNestedAnyValue(item))
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	default:
		return value
	}
}
