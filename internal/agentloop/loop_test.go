package agentloop

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// testProvider is a minimal bifrost.Provider for testing the agent loop.
type testProvider struct {
	streamFn func(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error)
}

func (p *testProvider) Name() string { return "test" }
func (p *testProvider) ChatCompletion(_ context.Context, _ bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	return nil, nil
}
func (p *testProvider) StreamChatCompletion(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	return p.streamFn(ctx, req)
}
func (p *testProvider) SupportsToolCalling() bool { return true }
func (p *testProvider) Capabilities() bifrost.ProviderCapabilities {
	return bifrost.ProviderCapabilities{ToolCallingFormat: "openai_function", SupportsStreamingToolCalls: true}
}

type approvalHandlerFunc func(context.Context, *Session, ApprovalRequest) (string, error)

func (f approvalHandlerFunc) RequestToolApproval(ctx context.Context, session *Session, req ApprovalRequest) (string, error) {
	return f(ctx, session, req)
}

type completionValidatorFunc func(context.Context, *Session, string, string) TurnCompletionDecision

func (f completionValidatorFunc) ValidateTurnCompletion(ctx context.Context, session *Session, userInput, assistantContent string) TurnCompletionDecision {
	return f(ctx, session, userInput, assistantContent)
}

type fakeCompressor struct {
	mu           sync.Mutex
	decisions    []bool
	shouldCalls  int
	compactCalls int
}

func (f *fakeCompressor) ShouldCompress(_ int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shouldCalls++
	if len(f.decisions) == 0 {
		return false
	}
	decision := f.decisions[0]
	f.decisions = f.decisions[1:]
	return decision
}

func (f *fakeCompressor) Compact(_ context.Context, _ *ContextManager) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compactCalls++
	return nil
}

type recordingObserver struct {
	mu               sync.Mutex
	assistantDeltas  []string
	toolCallDeltas   []bifrost.ToolCall
	completedContent string
}

func (o *recordingObserver) OnAssistantDelta(_ context.Context, _ *Session, delta string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.assistantDeltas = append(o.assistantDeltas, delta)
	return nil
}

func (o *recordingObserver) OnToolCallDelta(_ context.Context, _ *Session, _ int, toolCall bifrost.ToolCall) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.toolCallDeltas = append(o.toolCallDeltas, toolCall)
	return nil
}

func (o *recordingObserver) OnStreamComplete(_ context.Context, _ *Session, result *StreamResult) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.completedContent = result.Content
	return nil
}

// makeStreamCh creates a channel that emits the given events then closes.
func makeStreamCh(events []bifrost.StreamEvent) <-chan bifrost.StreamEvent {
	ch := make(chan bifrost.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func newLoopWithProvider(tp *testProvider, compressor ContextCompressor) *Loop {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{DefaultProvider: "test"})
	gw.RegisterProvider("test", tp)
	return NewLoop(gw, NewToolRegistry(), compressor)
}

func TestNewLoop(t *testing.T) {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{DefaultProvider: "test"})
	reg := NewToolRegistry()
	l := NewLoop(gw, reg, nil)

	if l.gateway != gw {
		t.Error("gateway not set")
	}
	if l.toolReg != reg {
		t.Error("toolReg not set")
	}
	if l.compressor != nil {
		t.Error("compressor should be nil")
	}
}

func TestRunTurn_SimpleResponse(t *testing.T) {
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "Hello "},
				{Type: "content_delta", Delta: "world!"},
				{Type: "done"},
			}), nil
		},
	}

	observer := &recordingObserver{}
	loop := newLoopWithProvider(tp, nil).SetStreamObserver(observer)
	session := NewSession("test-session", SessionSpec{Model: "test-model"})

	if err := loop.RunTurn(context.Background(), session, "hi"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	msgs := session.ContextManager().Messages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected first message role=user, got %s", msgs[0].Role)
	}

	last := msgs[len(msgs)-1]
	if last.Role != "assistant" {
		t.Errorf("expected last message role=assistant, got %s", last.Role)
	}
	if last.Content != "Hello world!" {
		t.Errorf("expected content 'Hello world!', got %q", last.Content)
	}

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.assistantDeltas) != 2 {
		t.Fatalf("expected 2 assistant deltas, got %d", len(observer.assistantDeltas))
	}
	if observer.completedContent != "Hello world!" {
		t.Fatalf("completedContent = %q, want %q", observer.completedContent, "Hello world!")
	}
}

func TestRunTurn_WithToolCall(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "echo"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"msg":"hi"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "Done!"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	loop.toolReg.Register(ToolEntry{
		Name: "echo",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, args map[string]interface{}) (string, error) {
			return "echoed: " + args["msg"].(string), nil
		},
	})

	session := NewSession("test-session", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "call echo"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}

	msgs := session.ContextManager().Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[2].Role != "tool" || msgs[2].Content != "echoed: hi" {
		t.Errorf("msg[2] should be tool result, got role=%s content=%v", msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "Done!" {
		t.Errorf("msg[3] should be final assistant, got role=%s content=%v", msgs[3].Role, msgs[3].Content)
	}
}

func TestRunTurn_ProactivelyAutoSearchesTimeSensitiveSingleHostQuery(t *testing.T) {
	callCount := 0
	var requests []bifrost.ChatRequest
	searchCalls := 0

	tp := &testProvider{
		streamFn: func(_ context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			requests = append(requests, req)
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "content_delta", Delta: "BTC 当前约 74648 美元。"},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "已根据搜索结果整理。"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	loop.SetWebSearchMode("native")
	loop.toolReg.Register(ToolEntry{
		Name: "web_search",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, args map[string]interface{}) (string, error) {
			searchCalls++
			return "search results for " + args["query"].(string), nil
		},
	})

	session := NewSession("test-session", SessionSpec{
		Model:        "test-model",
		DynamicTools: []string{"web_search"},
	})
	session.Metadata = map[string]string{
		"prefer_explicit_web_search": "true",
	}

	if err := loop.RunTurn(context.Background(), session, "看下BTC行情"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if searchCalls != 1 {
		t.Fatalf("expected one explicit web_search fallback, got %d", searchCalls)
	}
	if callCount != 2 {
		t.Fatalf("expected two model passes (initial + post-search), got %d", callCount)
	}
	if len(requests) == 0 {
		t.Fatal("expected at least one captured chat request")
	}
	if requests[0].UseResponsesAPI {
		t.Fatal("single-host explicit web search should not switch to Responses API")
	}
	foundWebSearch := false
	for _, tool := range requests[0].Tools {
		if tool.Function.Name == "web_search" {
			foundWebSearch = true
			break
		}
	}
	if !foundWebSearch {
		t.Fatal("initial request should keep web_search as an explicit tool")
	}

	msgs := session.ContextManager().Messages()
	foundAutoSearchInjection := false
	for _, msg := range msgs {
		content, _ := msg.Content.(string)
		if strings.Contains(content, "[System: web_search was executed automatically. Results below]") {
			foundAutoSearchInjection = true
			break
		}
	}
	if !foundAutoSearchInjection {
		t.Fatal("expected auto web search results to be injected back into the loop")
	}
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || last.Content != "已根据搜索结果整理。" {
		t.Fatalf("unexpected final assistant message: role=%s content=%v", last.Role, last.Content)
	}
}

func TestRunTurn_ParallelReadonlyToolBatch(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "read_a"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"name":"a"}`},
					{Type: "tool_call_delta", ToolIndex: 1, ToolCallID: "call-2", FuncName: "read_b"},
					{Type: "tool_call_delta", ToolIndex: 1, FuncArgs: `{"name":"b"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "batch done"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	var active atomic.Int32
	var maxConcurrent atomic.Int32
	handler := func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
		current := active.Add(1)
		for {
			prev := maxConcurrent.Load()
			if current <= prev || maxConcurrent.CompareAndSwap(prev, current) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		active.Add(-1)
		return "ok", nil
	}
	loop.toolReg.Register(ToolEntry{Name: "read_a", IsReadOnly: true, Handler: handler})
	loop.toolReg.Register(ToolEntry{Name: "read_b", IsReadOnly: true, Handler: handler})

	session := NewSession("parallel-session", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "parallel"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if maxConcurrent.Load() < 2 {
		t.Fatalf("expected parallel execution, max concurrency = %d", maxConcurrent.Load())
	}
}

func TestRunTurn_ToolApprovalWaitsForDecision(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-approve", FuncName: "execute_command"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"command":"echo ok"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "approved"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	var executed atomic.Bool
	loop.toolReg.Register(ToolEntry{
		Name:             "execute_command",
		RequiresApproval: true,
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			executed.Store(true)
			return "command executed", nil
		},
	})
	loop.SetApprovalHandler(approvalHandlerFunc(func(_ context.Context, session *Session, req ApprovalRequest) (string, error) {
		if req.ToolCall.Function.Name != "execute_command" {
			t.Fatalf("unexpected tool approval request: %+v", req.ToolCall)
		}
		go func() {
			time.Sleep(10 * time.Millisecond)
			session.ResolveApproval(ApprovalDecision{
				ApprovalID: "approval-1",
				Decision:   "approve",
			})
		}()
		return "approval-1", nil
	}))

	session := NewSession("approval-session", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "approve this"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !executed.Load() {
		t.Fatal("expected approved tool to execute")
	}
}

func TestRunTurn_CompressesBeforeAndAfterToolExecution(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "echo"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"msg":"hi"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "done"},
				{Type: "done"},
			}), nil
		},
	}

	compressor := &fakeCompressor{decisions: []bool{true, true, false}}
	loop := newLoopWithProvider(tp, compressor)
	loop.toolReg.Register(ToolEntry{
		Name: "echo",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			return "echoed", nil
		},
	})

	session := NewSession("compress-session", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "compress"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	compressor.mu.Lock()
	defer compressor.mu.Unlock()
	if compressor.compactCalls != 2 {
		t.Fatalf("expected 2 compact calls, got %d", compressor.compactCalls)
	}
	if compressor.shouldCalls < 3 {
		t.Fatalf("expected at least 3 ShouldCompress checks, got %d", compressor.shouldCalls)
	}
}

func TestRunTurn_CancelledContext(t *testing.T) {
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return make(chan bifrost.StreamEvent), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	session := NewSession("test-session", SessionSpec{Model: "test-model"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := loop.RunTurn(ctx, session, "hi")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestRunTurn_MaxIterations(t *testing.T) {
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-x", FuncName: "noop"},
				{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{}`},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	loop.toolReg.Register(ToolEntry{
		Name: "noop",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			return "ok", nil
		},
	})

	session := NewSession("test-session", SessionSpec{
		Model:         "test-model",
		MaxIterations: 3,
	})

	err := loop.RunTurn(context.Background(), session, "loop forever")
	if err == nil {
		t.Fatal("expected max iterations error")
	}
	if err.Error() != "agent loop exceeded maximum iterations (3)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseToolArgs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"empty string", "", 0},
		{"valid json", `{"a":1,"b":"two"}`, 2},
		{"invalid json", `not json`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseToolArgs(tt.raw)
			if len(got) != tt.want {
				t.Errorf("parseToolArgs(%q) returned %d keys, want %d", tt.raw, len(got), tt.want)
			}
		})
	}
}

func TestBuildChatRequest(t *testing.T) {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{DefaultProvider: "test"})
	gw.RegisterProvider("test", &testProvider{})
	reg := NewToolRegistry()
	reg.Register(ToolEntry{Name: "tool_a", Description: "A tool"})
	reg.Register(ToolEntry{Name: "tool_b", Description: "B tool"})

	loop := NewLoop(gw, reg, nil)
	session := NewSession("test-session", SessionSpec{
		Model:        "test-model",
		DynamicTools: []string{"tool_a"},
	})
	session.ContextManager().AppendUser("hello")

	req := loop.buildChatRequest(session)

	if req.Model != "test-model" {
		t.Errorf("model=%s, want test-model", req.Model)
	}
	if !req.Stream {
		t.Error("expected Stream=true")
	}
	if len(req.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("first message role=%s, want system", req.Messages[0].Role)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "tool_a" {
		t.Fatalf("expected only tool_a in request, got %#v", req.Tools)
	}
}

func TestRunTurn_CompletionValidatorContinuesLoop(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "content_delta", Delta: "这里先直接回答。"},
					{Type: "done"},
				}), nil
			}
			if callCount == 2 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-search", FuncName: "web_search"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"query":"btc latest price"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "已根据搜索结果补全答案。"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	loop.toolReg.Register(ToolEntry{
		Name: "web_search",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, args map[string]interface{}) (string, error) {
			return "search results for " + args["query"].(string), nil
		},
	})
	var validations int
	loop.SetTurnCompletionValidator(completionValidatorFunc(func(_ context.Context, _ *Session, _ string, _ string) TurnCompletionDecision {
		validations++
		if validations == 1 {
			return TurnCompletionDecision{
				Action:        "continue",
				RepairMessage: "next_required_tool=web_search\nCall web_search next.",
			}
		}
		return TurnCompletionDecision{Action: "pass"}
	}))

	session := NewSession("completion-validator-session", SessionSpec{
		Model:        "test-model",
		DynamicTools: []string{"web_search"},
	})
	if err := loop.RunTurn(context.Background(), session, "最新 BTC 价格"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if callCount != 3 {
		t.Fatalf("expected completion validator to trigger repair + post-tool model pass, got %d", callCount)
	}
	msgs := session.ContextManager().Messages()
	foundRepair := false
	foundToolResult := false
	for _, msg := range msgs {
		content, _ := msg.Content.(string)
		if strings.Contains(content, "[Runtime policy repair]") {
			foundRepair = true
		}
		if msg.Role == "tool" && strings.Contains(content, "search results for btc latest price") {
			foundToolResult = true
		}
	}
	if !foundRepair {
		t.Fatalf("expected runtime policy repair message in context, got %#v", msgs)
	}
	if !foundToolResult {
		t.Fatalf("expected repaired loop to execute required tool, got %#v", msgs)
	}
	for _, msg := range msgs {
		content, _ := msg.Content.(string)
		if strings.Contains(content, "compact snapshot format") || strings.Contains(content, "1-2 sources") {
			t.Fatalf("expected generic repair without market-specific prose, got %#v", msgs)
		}
	}
}

// --- Mid-turn message injection tests ---

func TestRunTurn_MidTurnInjection(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				// First call: return a tool call
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "slow_tool"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{}`},
					{Type: "done"},
				}), nil
			}
			// Second call: model sees the injected message and responds
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "Acknowledged interrupt"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	loop.toolReg.Register(ToolEntry{
		Name: "slow_tool",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			return "tool done", nil
		},
	})

	session := NewSession("inject-session", SessionSpec{Model: "test-model"})

	// Inject a message before the second iteration picks it up.
	// We inject before RunTurn so it's available on iteration 1.
	session.InjectMessage("please stop what you're doing")

	if err := loop.RunTurn(context.Background(), session, "start working"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	// Verify the injected message appears in the conversation.
	msgs := session.ContextManager().Messages()
	found := false
	for _, m := range msgs {
		if m.Role == "user" {
			if s, ok := m.Content.(string); ok {
				if s == "[User interrupt]: please stop what you're doing" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Fatal("expected injected interrupt message in conversation history")
	}
}

func TestRunTurn_InterruptDuringToolBatch(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				// Return two sequential tool calls (non-parallel because one requires approval)
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "tool_a"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{}`},
					{Type: "tool_call_delta", ToolIndex: 1, ToolCallID: "call-2", FuncName: "tool_b"},
					{Type: "tool_call_delta", ToolIndex: 1, FuncArgs: `{}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "done after interrupt"},
				{Type: "done"},
			}), nil
		},
	}

	var toolACalled, toolBCalled atomic.Bool
	loop := newLoopWithProvider(tp, nil)
	loop.toolReg.Register(ToolEntry{
		Name:             "tool_a",
		RequiresApproval: true, // Forces sequential execution
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			toolACalled.Store(true)
			return "a done", nil
		},
	})
	loop.toolReg.Register(ToolEntry{
		Name: "tool_b",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			toolBCalled.Store(true)
			return "b done", nil
		},
	})
	loop.SetApprovalHandler(approvalHandlerFunc(func(_ context.Context, session *Session, _ ApprovalRequest) (string, error) {
		// Auto-approve, but inject an interrupt before tool_b runs
		session.InjectMessage("abort remaining tools")
		go func() {
			time.Sleep(5 * time.Millisecond)
			session.ResolveApproval(ApprovalDecision{Decision: "approve"})
		}()
		return "", nil
	}))

	session := NewSession("interrupt-batch", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "run both tools"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	// tool_a should have been called, tool_b should have been skipped due to interrupt
	if !toolACalled.Load() {
		t.Fatal("expected tool_a to be called")
	}
	if toolBCalled.Load() {
		t.Fatal("expected tool_b to be skipped due to interrupt")
	}
}

// --- Proactive compression tests ---

func TestIterationBudgetTracker_NoCompressBeforeThreshold(t *testing.T) {
	tracker := &iterationBudgetTracker{}
	for i := 0; i < ProactiveCompressIterationThreshold; i++ {
		tracker.record(1000 + i*500)
		if tracker.shouldForceCompress(i) {
			t.Fatalf("should not force compress at iteration %d", i)
		}
	}
}

func TestIterationBudgetTracker_CompressOnHighIterationWithDiminishingReturns(t *testing.T) {
	tracker := &iterationBudgetTracker{}

	// Record growing content for first iterations
	for i := 0; i < ProactiveCompressIterationThreshold; i++ {
		tracker.record(1000 + i*1000)
	}

	// Now record diminishing returns (very small deltas)
	lastLen := 1000 + (ProactiveCompressIterationThreshold-1)*1000
	for i := 0; i < DiminishingReturnsCheckWindow; i++ {
		lastLen += 10 // tiny delta, well below threshold
		tracker.record(lastLen)
	}

	iteration := ProactiveCompressIterationThreshold + DiminishingReturnsCheckWindow
	if !tracker.shouldForceCompress(iteration) {
		t.Fatal("expected force compress with diminishing returns at high iteration count")
	}
}

func TestIterationBudgetTracker_NoCompressWithGoodProgress(t *testing.T) {
	tracker := &iterationBudgetTracker{}

	// Record consistently growing content
	for i := 0; i <= ProactiveCompressIterationThreshold+DiminishingReturnsCheckWindow; i++ {
		tracker.record(1000 + i*1000) // 1000 chars per iteration — well above threshold
	}

	iteration := ProactiveCompressIterationThreshold + DiminishingReturnsCheckWindow
	if tracker.shouldForceCompress(iteration) {
		t.Fatal("should not force compress when progress is good")
	}
}

func TestTotalContentLength(t *testing.T) {
	msgs := []bifrost.Message{
		{Role: "user", Content: "hello"}, // 5
		{Role: "assistant", Content: "world", ToolCalls: []bifrost.ToolCall{{Function: bifrost.FunctionCall{Arguments: `{"a":1}`}}}}, // 5 + 7 = 12
		{Role: "tool", Content: "result"}, // 6
	}
	total := totalContentLength(msgs)
	expected := 5 + 5 + 7 + 6 // 23
	if total != expected {
		t.Fatalf("expected %d, got %d", expected, total)
	}
}

func TestTruncateLog(t *testing.T) {
	if truncateLog("short", 10) != "short" {
		t.Fatal("short string should not be truncated")
	}
	result := truncateLog("this is a long string", 10)
	if result != "this is a ..." {
		t.Fatalf("expected truncation, got %q", result)
	}
}

// --- Checkpoint integration with loop ---

func TestRunTurn_WithCheckpointStore(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir)

	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "echo"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"msg":"hi"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "Done!"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil).SetCheckpointStore(store)
	loop.toolReg.Register(ToolEntry{
		Name: "echo",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, args map[string]interface{}) (string, error) {
			return "echoed: " + args["msg"].(string), nil
		},
	})

	session := NewSession("checkpoint-session", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "test checkpoint"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	// After successful completion, checkpoints should be cleared.
	if store.LoadLatest("checkpoint-session") != nil {
		t.Fatal("expected checkpoints to be cleared after successful turn")
	}
}

func TestRunTurn_ProactiveCompressionTriggered(t *testing.T) {
	// Create a provider that always returns tool calls to force many iterations.
	iterCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			iterCount++
			if iterCount <= ProactiveCompressIterationThreshold+DiminishingReturnsCheckWindow+1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-x", FuncName: "noop"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "finally done"},
				{Type: "done"},
			}), nil
		},
	}

	compressor := &fakeCompressor{}
	loop := newLoopWithProvider(tp, compressor)
	loop.toolReg.Register(ToolEntry{
		Name: "noop",
		Handler: func(_ context.Context, _ ToolContext, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			return "ok", nil
		},
	})

	session := NewSession("proactive-session", SessionSpec{
		Model:         "test-model",
		MaxIterations: ProactiveCompressIterationThreshold + DiminishingReturnsCheckWindow + 5,
	})
	if err := loop.RunTurn(context.Background(), session, "keep going"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	// The proactive compression should have triggered Compact at least once
	// (via shouldForceCompress path, which calls l.compressor.Compact directly).
	// Note: the fakeCompressor.ShouldCompress returns false by default, so
	// preSampleCompress won't trigger. But the proactive path calls Compact directly.
	compressor.mu.Lock()
	defer compressor.mu.Unlock()
	if compressor.compactCalls < 1 {
		t.Fatalf("expected at least 1 proactive compact call, got %d", compressor.compactCalls)
	}
}
