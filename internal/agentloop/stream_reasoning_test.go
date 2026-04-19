package agentloop

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"pgregory.net/rapid"
)

// Feature: bifrost-provider-capabilities, Property 5: Interleaved stream event accumulation
// **Validates: Requirements 3.4, 3.5**
//
// For any interleaved sequence of content_delta and reasoning_delta events,
// consumeStream produces a StreamResult where Content equals the concatenation
// of all content_delta deltas AND ReasoningContent equals the concatenation of
// all reasoning_delta ReasoningContent values.
func TestProperty5_InterleavedStreamEventAccumulation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random interleaved sequence of content_delta and reasoning_delta events.
		numEvents := rapid.IntRange(0, 50).Draw(t, "numEvents")

		var expectedContent string
		var expectedReasoning string
		events := make([]bifrost.StreamEvent, 0, numEvents+1)

		for i := 0; i < numEvents; i++ {
			isReasoning := rapid.Bool().Draw(t, "isReasoning")
			delta := rapid.String().Draw(t, "delta")

			if isReasoning {
				events = append(events, bifrost.StreamEvent{
					Type:             "reasoning_delta",
					ReasoningContent: delta,
				})
				expectedReasoning += delta
			} else {
				events = append(events, bifrost.StreamEvent{
					Type:  "content_delta",
					Delta: delta,
				})
				expectedContent += delta
			}
		}

		// Append a done event to close the stream.
		events = append(events, bifrost.StreamEvent{Type: "done"})

		// Feed events into a channel.
		ch := make(chan bifrost.StreamEvent, len(events))
		for _, ev := range events {
			ch <- ev
		}
		close(ch)

		// Create a minimal loop with a noop observer.
		loop := &Loop{streamObserver: noopStreamObserver{}}

		result, err := loop.consumeStream(context.Background(), nil, ch)
		if err != nil {
			t.Fatalf("consumeStream returned error: %v", err)
		}

		if result.Content != expectedContent {
			t.Fatalf("Content mismatch:\n  got:  %q\n  want: %q", result.Content, expectedContent)
		}
		if result.ReasoningContent != expectedReasoning {
			t.Fatalf("ReasoningContent mismatch:\n  got:  %q\n  want: %q", result.ReasoningContent, expectedReasoning)
		}
	})
}
