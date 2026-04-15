package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- convertMessagesToInput ---

func TestConvertMessagesToInput_Basic(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}
	input := convertMessagesToInput(msgs)
	if len(input) != 2 {
		t.Fatalf("len: got %d, want 2", len(input))
	}
	// system → developer
	m0 := input[0].(map[string]interface{})
	if m0["role"] != "developer" {
		t.Errorf("input[0].role: got %q, want developer", m0["role"])
	}
	if m0["content"] != "You are helpful." {
		t.Errorf("input[0].content: got %v", m0["content"])
	}
	// user stays user
	m1 := input[1].(map[string]interface{})
	if m1["role"] != "user" {
		t.Errorf("input[1].role: got %q, want user", m1["role"])
	}
}

func TestConvertMessagesToInput_ToolCallID(t *testing.T) {
	msgs := []Message{
		{Role: "tool", Content: "result", ToolCallID: "call_abc"},
	}
	input := convertMessagesToInput(msgs)
	m := input[0].(map[string]interface{})
	if m["tool_call_id"] != "call_abc" {
		t.Errorf("tool_call_id: got %v, want call_abc", m["tool_call_id"])
	}
}

// --- buildResponsesTools ---

func TestBuildResponsesTools_WebSearchEnabled(t *testing.T) {
	tools := []ToolDefinition{
		{Type: "function", Function: FunctionSpec{Name: "web_search", Description: "search"}},
		{Type: "function", Function: FunctionSpec{Name: "execute_query", Description: "run query", Parameters: map[string]interface{}{"type": "object"}}},
	}
	result := buildResponsesTools(tools, true)

	// Should have: native web_search + execute_query (web_search function skipped)
	if len(result) != 2 {
		t.Fatalf("len: got %d, want 2", len(result))
	}

	// First should be native web_search
	ws := result[0].(map[string]interface{})
	if ws["type"] != "web_search" {
		t.Errorf("result[0].type: got %v, want web_search", ws["type"])
	}
	// Should NOT have "name" field (it's a native tool)
	if _, ok := ws["name"]; ok {
		t.Error("native web_search should not have 'name' field")
	}

	// Second should be the function tool
	fn := result[1].(map[string]interface{})
	if fn["type"] != "function" {
		t.Errorf("result[1].type: got %v, want function", fn["type"])
	}
	if fn["name"] != "execute_query" {
		t.Errorf("result[1].name: got %v, want execute_query", fn["name"])
	}
}

func TestBuildResponsesTools_WebSearchDisabled(t *testing.T) {
	tools := []ToolDefinition{
		{Type: "function", Function: FunctionSpec{Name: "get_weather"}},
	}
	result := buildResponsesTools(tools, false)

	if len(result) != 1 {
		t.Fatalf("len: got %d, want 1", len(result))
	}
	fn := result[0].(map[string]interface{})
	if fn["type"] != "function" {
		t.Errorf("type: got %v, want function", fn["type"])
	}
}

func TestBuildResponsesTools_SkipsWebSearchFunction(t *testing.T) {
	tools := []ToolDefinition{
		{Type: "function", Function: FunctionSpec{Name: "web_search"}},
	}
	result := buildResponsesTools(tools, true)

	// Only the native web_search, the function one is skipped
	if len(result) != 1 {
		t.Fatalf("len: got %d, want 1", len(result))
	}
	ws := result[0].(map[string]interface{})
	if ws["type"] != "web_search" {
		t.Errorf("type: got %v, want web_search", ws["type"])
	}
}

// --- streamResponsesAPI (integration with httptest) ---

func TestStreamResponsesAPI_TextDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it hits /responses endpoint
		if r.URL.Path != "/responses" {
			t.Errorf("path: got %q, want /responses", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type: got %q", r.Header.Get("Content-Type"))
		}

		// Verify request body
		var reqBody responsesAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if reqBody.Model != "gpt-4o" {
			t.Errorf("model: got %q, want gpt-4o", reqBody.Model)
		}
		if !reqBody.Stream {
			t.Error("stream: got false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"Hello"}`+"\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":" world"}`+"\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{}}`+"\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:           "gpt-4o",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		UseResponsesAPI: true,
		WebSearchEnabled: true,
	}

	ch, err := p.streamResponsesAPI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("events count: got %d, want 3", len(events))
	}
	if events[0].Type != "content_delta" || events[0].Delta != "Hello" {
		t.Errorf("event[0]: got %+v", events[0])
	}
	if events[1].Type != "content_delta" || events[1].Delta != " world" {
		t.Errorf("event[1]: got %+v", events[1])
	}
	if events[2].Type != "done" {
		t.Errorf("event[2]: got %+v, want done", events[2])
	}
}

func TestStreamResponsesAPI_FunctionCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "event: response.output_item.done\n")
		fmt.Fprint(w, `data: {"type":"response.output_item.done","item":{"id":"fc_abc","type":"function_call","name":"execute_query","arguments":"{\"command\":\"uptime\"}","call_id":"call_123"}}`+"\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{}}`+"\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:           "gpt-4o",
		Messages:        []Message{{Role: "user", Content: "run uptime"}},
		UseResponsesAPI: true,
	}

	ch, err := p.streamResponsesAPI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("events count: got %d, want 2", len(events))
	}
	if events[0].Type != "tool_call_delta" {
		t.Errorf("event[0].Type: got %q, want tool_call_delta", events[0].Type)
	}
	if events[0].ToolCallID != "call_123" {
		t.Errorf("event[0].ToolCallID: got %q, want call_123", events[0].ToolCallID)
	}
	if events[0].FuncName != "execute_query" {
		t.Errorf("event[0].FuncName: got %q, want execute_query", events[0].FuncName)
	}
	if events[0].FuncArgs != `{"command":"uptime"}` {
		t.Errorf("event[0].FuncArgs: got %q", events[0].FuncArgs)
	}
	if events[1].Type != "done" {
		t.Errorf("event[1].Type: got %q, want done", events[1].Type)
	}
}

func TestStreamResponsesAPI_WebSearchThenText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Web search events (informational, should not produce StreamEvents)
		fmt.Fprint(w, "event: response.web_search_call.searching\n")
		fmt.Fprint(w, `data: {"type":"response.web_search_call.searching"}`+"\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: response.web_search_call.completed\n")
		fmt.Fprint(w, `data: {"type":"response.web_search_call.completed"}`+"\n\n")
		flusher.Flush()

		// Text output after search
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"Search result: 3200 points"}`+"\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{}}`+"\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:            "gpt-4o",
		Messages:         []Message{{Role: "user", Content: "上证指数"}},
		UseResponsesAPI:  true,
		WebSearchEnabled: true,
	}

	ch, err := p.streamResponsesAPI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Only text delta + done (web search events are informational)
	if len(events) != 2 {
		t.Fatalf("events count: got %d, want 2", len(events))
	}
	if events[0].Type != "content_delta" {
		t.Errorf("event[0].Type: got %q, want content_delta", events[0].Type)
	}
	if events[1].Type != "done" {
		t.Errorf("event[1].Type: got %q, want done", events[1].Type)
	}
}

func TestStreamResponsesAPI_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid model",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:           "bad-model",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		UseResponsesAPI: true,
	}

	_, err := p.streamResponsesAPI(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if got := err.Error(); !contains(got, "400") || !contains(got, "Invalid model") {
		t.Errorf("error: got %q", got)
	}
}

func TestStreamResponsesAPI_FailedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "event: response.failed\n")
		fmt.Fprint(w, `data: {"type":"response.failed"}`+"\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:           "gpt-4o",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		UseResponsesAPI: true,
	}

	ch, err := p.streamResponsesAPI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 1 {
		t.Fatalf("events count: got %d, want 1", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("event[0].Type: got %q, want error", events[0].Type)
	}
}

// TestStreamChatCompletion_DispatchesToResponsesAPI verifies that
// StreamChatCompletion dispatches to the Responses API when UseResponsesAPI is set.
func TestStreamChatCompletion_DispatchesToResponsesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("expected /responses path, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"ok"}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{}}`+"\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:           "gpt-4o",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		UseResponsesAPI: true,
	}

	ch, err := p.StreamChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("events count: got %d, want 2", len(events))
	}
	if events[0].Delta != "ok" {
		t.Errorf("event[0].Delta: got %q, want ok", events[0].Delta)
	}
}
