package store

import "testing"

func TestToolEventStoreSessionEventsPreservesOrderAcrossWrap(t *testing.T) {
	events := NewToolEventStore(3)

	events.Append(ToolEventRecord{SessionID: "sess-1", Type: "started", EventID: "evt-1"})
	events.Append(ToolEventRecord{SessionID: "sess-2", Type: "started", EventID: "evt-2"})
	events.Append(ToolEventRecord{SessionID: "sess-1", Type: "completed", EventID: "evt-3"})
	events.Append(ToolEventRecord{SessionID: "sess-1", Type: "failed", EventID: "evt-4"})

	got := events.SessionEvents("sess-1")
	if len(got) != 2 {
		t.Fatalf("expected wrapped store to keep two sess-1 events, got %d", len(got))
	}
	if got[0].EventID != "evt-3" || got[0].Sequence != 3 {
		t.Fatalf("expected oldest remaining event to be evt-3/3, got %#v", got[0])
	}
	if got[1].EventID != "evt-4" || got[1].Sequence != 4 {
		t.Fatalf("expected newest event to be evt-4/4, got %#v", got[1])
	}
}

func TestToolEventStoreSessionEventsReturnsClonedMaps(t *testing.T) {
	events := NewToolEventStore(2)
	events.Append(ToolEventRecord{
		SessionID: "sess-1",
		Type:      "started",
		Payload:   map[string]any{"command": "ls"},
		Metadata:  map[string]any{"host": "linux-01"},
	})

	got := events.SessionEvents("sess-1")
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}

	got[0].Payload["command"] = "rm -rf /"
	got[0].Metadata["host"] = "mutated"

	again := events.SessionEvents("sess-1")
	if again[0].Payload["command"] != "ls" {
		t.Fatalf("expected payload clone to protect store contents, got %#v", again[0].Payload)
	}
	if again[0].Metadata["host"] != "linux-01" {
		t.Fatalf("expected metadata clone to protect store contents, got %#v", again[0].Metadata)
	}
}
