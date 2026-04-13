package agentloop

import (
	"context"
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

type approvalHandlerFunc func(context.Context, *Session, ApprovalRequest) (string, error)

func (f approvalHandlerFunc) RequestToolApproval(ctx context.Context, session *Session, req ApprovalRequest) (string, error) {
	return f(ctx, session, req)
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
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, args map[string]interface{}) (string, error) {
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
	handler := func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
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
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
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
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
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
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
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
