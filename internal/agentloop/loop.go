package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/guardian"
	"github.com/lizhongxuan/aiops-codex/internal/hooks"
)

// ContextCompressor is the subset of compression behavior the loop depends on.
type ContextCompressor interface {
	ShouldCompress(estimatedTokens int) bool
	Compact(ctx context.Context, cm *ContextManager) error
}

// ApprovalRequest captures the information needed to request approval for a tool call.
type ApprovalRequest struct {
	ToolCall  bifrost.ToolCall
	Tool      ToolEntry
	Arguments map[string]interface{}
}

// ApprovalHandler bridges agentloop approval waits to the outer server/UI layer.
type ApprovalHandler interface {
	RequestToolApproval(ctx context.Context, session *Session, req ApprovalRequest) (string, error)
}

type toolExecutionOutcome struct {
	CallID string
	Result string
}

// Loop is the main agent loop that drives the ReAct cycle:
// reason (LLM call) → act (tool execution) → observe (append result) → repeat.
type Loop struct {
	gateway         *bifrost.Gateway
	toolReg         *ToolRegistry
	compressor      ContextCompressor
	approvalHandler ApprovalHandler
	streamObserver  StreamObserver
	hookRuntime     *hooks.Runtime
}

// NewLoop creates a new Loop with the given dependencies.
func NewLoop(gateway *bifrost.Gateway, toolReg *ToolRegistry, compressor ContextCompressor) *Loop {
	return &Loop{
		gateway:        gateway,
		toolReg:        toolReg,
		compressor:     compressor,
		streamObserver: noopStreamObserver{},
	}
}

// SetApprovalHandler wires an optional approval callback into the loop.
func (l *Loop) SetApprovalHandler(handler ApprovalHandler) *Loop {
	l.approvalHandler = handler
	return l
}

// SetStreamObserver wires an optional stream observer into the loop.
func (l *Loop) SetStreamObserver(observer StreamObserver) *Loop {
	if observer == nil {
		l.streamObserver = noopStreamObserver{}
		return l
	}
	l.streamObserver = observer
	return l
}

// SetHookRuntime wires an optional hook runtime into the loop.
func (l *Loop) SetHookRuntime(rt *hooks.Runtime) *Loop {
	l.hookRuntime = rt
	return l
}

// InitSession runs session_start hooks and appends any additional contexts
// to the session's context manager. Call this after creating a session and
// before the first RunTurn.
func (l *Loop) InitSession(session *Session) {
	if l.hookRuntime == nil {
		return
	}
	result := l.hookRuntime.ExecuteSessionStart()
	for _, ctx := range result.AdditionalContexts {
		session.ContextManager().AppendUser(ctx)
	}
}

// RunTurn executes a single user turn: it appends the user message, then
// iterates the ReAct loop (LLM call → tool execution → continue) until the
// model produces a final response with no tool calls or the iteration budget
// is exhausted.
func (l *Loop) RunTurn(ctx context.Context, session *Session, userInput string) error {
	// Execute prompt_submit hooks before processing user input.
	if l.hookRuntime != nil {
		psResult := l.hookRuntime.ExecutePromptSubmit(userInput)
		if psResult.Block {
			// Inform the model that the input was blocked.
			session.ContextManager().AppendUser(userInput)
			session.ContextManager().AppendAssistant(
				fmt.Sprintf("[Hook blocked prompt]: %s", psResult.BlockReason), nil)
			return nil
		}
		userInput = psResult.ModifiedInput
	}

	session.ContextManager().AppendUser(userInput)

	for iteration := 0; iteration < session.MaxIterations(); iteration++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("agent loop cancelled: %w", err)
		}

		if err := l.preSampleCompress(ctx, session); err != nil {
			log.Printf("[loop] pre-sample compression error: %v", err)
		}

		req := l.buildChatRequest(session)
		stream, err := l.gateway.StreamChatCompletion(ctx, req)
		if err != nil {
			return fmt.Errorf("stream chat completion failed: %w", err)
		}

		result, err := l.consumeStream(ctx, session, stream)
		if err != nil {
			return fmt.Errorf("consume stream failed: %w", err)
		}

		session.ContextManager().AppendAssistant(result.Content, result.ToolCalls)
		if len(result.ToolCalls) == 0 {
			// Auto-fallback: if model refused to search, do it automatically
			if shouldAutoWebSearch(result.Content, userInput) {
				searchResult, searchErr := l.autoWebSearch(ctx, session, userInput)
				if searchErr == nil && searchResult != "" {
					// Inject search results and continue the loop
					session.ContextManager().AppendUser("[System: web_search was executed automatically. Results below]\n" + searchResult)
					continue // Re-enter the ReAct loop with search results
				}
			}
			return nil
		}

		outcomes, err := l.executeToolBatch(ctx, session, result.ToolCalls)
		if err != nil && !IsPauseTurn(err) {
			return fmt.Errorf("execute tool batch failed: %w", err)
		}
		for _, outcome := range outcomes {
			session.ContextManager().AppendToolResult(outcome.CallID, outcome.Result)
			if err := l.postToolCompress(ctx, session); err != nil {
				log.Printf("[loop] post-tool compression error: %v", err)
			}
		}
		if IsPauseTurn(err) {
			return nil
		}
	}

	return fmt.Errorf("agent loop exceeded maximum iterations (%d)", session.MaxIterations())
}

// buildChatRequest constructs a bifrost.ChatRequest from the current session state.
func (l *Loop) buildChatRequest(session *Session) bifrost.ChatRequest {
	session.ContextManager().Sanitize()
	messages := session.ContextManager().Messages()

	if prompt := strings.TrimSpace(session.SystemPrompt()); prompt != "" {
		if len(messages) == 0 || messages[0].Role != "system" {
			messages = append([]bifrost.Message{{
				Role:    "system",
				Content: prompt,
			}}, messages...)
		}
	}

	log.Printf("[bifrost-debug] buildChatRequest: model=%s tools=%d enabledTools=%v", session.Model(), len(l.toolReg.Definitions(session.EnabledTools())), session.EnabledTools())
	return bifrost.ChatRequest{
		Model:    session.Model(),
		Messages: messages,
		Tools:    l.toolReg.Definitions(session.EnabledTools()),
		Stream:   true,
	}
}

// preSampleCompress checks whether the context needs compression before the
// next LLM call and runs the compressor if needed.
func (l *Loop) preSampleCompress(ctx context.Context, session *Session) error {
	return l.compressIfNeeded(ctx, session)
}

func (l *Loop) postToolCompress(ctx context.Context, session *Session) error {
	return l.compressIfNeeded(ctx, session)
}

func (l *Loop) compressIfNeeded(ctx context.Context, session *Session) error {
	if l.compressor == nil {
		return nil
	}
	estimatedTokens := session.ContextManager().EstimateTokens()
	if !l.compressor.ShouldCompress(estimatedTokens) {
		return nil
	}
	return l.compressor.Compact(ctx, session.ContextManager())
}

func (l *Loop) executeToolBatch(ctx context.Context, session *Session, toolCalls []bifrost.ToolCall) ([]toolExecutionOutcome, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	if !l.shouldParallelizeToolBatch(toolCalls) {
		outcomes := make([]toolExecutionOutcome, 0, len(toolCalls))
		for _, tc := range toolCalls {
			result, err := l.executeTool(ctx, session, tc)
			if err != nil {
				return outcomes, err
			}
			outcomes = append(outcomes, toolExecutionOutcome{CallID: tc.ID, Result: result})
		}
		return outcomes, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	outcomes := make([]toolExecutionOutcome, len(toolCalls))
	var (
		wg       sync.WaitGroup
		firstErr error
		errOnce  sync.Once
	)

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, toolCall bifrost.ToolCall) {
			defer wg.Done()
			result, err := l.executeTool(ctx, session, toolCall)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			outcomes[idx] = toolExecutionOutcome{CallID: toolCall.ID, Result: result}
		}(i, tc)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return outcomes, nil
}

func (l *Loop) shouldParallelizeToolBatch(toolCalls []bifrost.ToolCall) bool {
	if len(toolCalls) < 2 {
		return false
	}
	for _, tc := range toolCalls {
		entry, ok := l.toolReg.Get(tc.Function.Name)
		if !ok || entry == nil {
			return false
		}
		if entry.RequiresApproval || !entry.IsReadOnly {
			return false
		}
	}
	return true
}

// executeTool dispatches a single tool call through the tool registry.
func (l *Loop) executeTool(ctx context.Context, session *Session, tc bifrost.ToolCall) (string, error) {
	entry, ok := l.toolReg.Get(tc.Function.Name)
	if !ok {
		return fmt.Sprintf("Error executing tool %s: tool not found", tc.Function.Name), nil
	}

	args := parseToolArgs(tc.Function.Arguments)

	// Execute pre_tool_use hooks before dispatch.
	if l.hookRuntime != nil {
		preResult := l.hookRuntime.ExecutePreToolUse(hooks.PreToolUseRequest{
			ToolName:  tc.Function.Name,
			ToolInput: args,
		})
		// Append additional contexts from pre-hooks.
		for _, ctx := range preResult.AdditionalContexts {
			session.ContextManager().AppendUser(ctx)
		}
		if preResult.Block {
			return fmt.Sprintf("[Hook blocked tool %s]: %s", tc.Function.Name, preResult.BlockReason), nil
		}
	}

	if entry.RequiresApproval {
		approved, denialResult, err := l.awaitToolApproval(ctx, session, tc, entry, args)
		if err != nil {
			return "", err
		}
		if !approved {
			return denialResult, nil
		}
	}

	result, err := l.toolReg.Dispatch(ctx, session, tc, tc.Function.Name, args)
	if err != nil {
		return fmt.Sprintf("Error executing tool %s: %v", tc.Function.Name, err), nil
	}

	// Execute post_tool_use hooks after dispatch.
	if l.hookRuntime != nil {
		postResult := l.hookRuntime.ExecutePostToolUse(hooks.PostToolUseRequest{
			ToolName:   tc.Function.Name,
			ToolInput:  args,
			ToolResult: result,
		})
		// Append additional contexts from post-hooks.
		for _, ctx := range postResult.AdditionalContexts {
			session.ContextManager().AppendUser(ctx)
		}
	}

	return result, nil
}

func (l *Loop) awaitToolApproval(ctx context.Context, session *Session, tc bifrost.ToolCall, entry *ToolEntry, args map[string]interface{}) (bool, string, error) {
	// Build a guardian approval request for cache/review lookup.
	guardianReq := guardian.GuardianApprovalRequest{
		ToolName:    tc.Function.Name,
		Arguments:   tc.Function.Arguments,
		Description: entry.Description,
	}

	// Check the approval cache first.
	if cache := session.ApprovalCache(); cache != nil {
		if cached := guardian.CheckBeforeReview(cache, guardianReq); cached != nil {
			if cached.Outcome == guardian.OutcomeAllow {
				return true, "", nil
			}
			return false, fmt.Sprintf("Tool %s denied (cached): %s", tc.Function.Name, cached.Rationale), nil
		}
	}

	// If Guardian is enabled, perform an LLM-based review.
	if g := session.Guardian(); g != nil {
		messages := session.ContextManager().Messages()
		assessment, err := g.ReviewApproval(ctx, messages, guardianReq)
		if err != nil {
			log.Printf("[loop] guardian review error for %s: %v", tc.Function.Name, err)
		}
		if assessment != nil {
			// Cache the decision for future lookups.
			if cache := session.ApprovalCache(); cache != nil {
				guardian.CacheDecision(cache, guardianReq, assessment)
			}
			if assessment.Outcome == guardian.OutcomeAllow {
				return true, "", nil
			}
			return false, fmt.Sprintf("Tool %s denied by guardian: %s", tc.Function.Name, assessment.Rationale), nil
		}
	}

	// Fall back to the standard approval handler (user/UI approval).
	if l.approvalHandler == nil {
		return false, fmt.Sprintf("Approval required for tool %s but no approval handler is configured.", tc.Function.Name), nil
	}

	approvalID, err := l.approvalHandler.RequestToolApproval(ctx, session, ApprovalRequest{
		ToolCall:  tc,
		Tool:      *entry,
		Arguments: args,
	})
	if err != nil {
		return false, fmt.Sprintf("Failed to request approval for tool %s: %v", tc.Function.Name, err), nil
	}

	decision, err := session.WaitForApprovalID(ctx, approvalID)
	if err != nil {
		return false, "", err
	}
	if approvalDecisionApproved(decision.Decision) {
		return true, "", nil
	}

	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = strings.TrimSpace(decision.Decision)
	}
	if reason == "" {
		reason = "rejected"
	}
	return false, fmt.Sprintf("Tool %s was not approved: %s", tc.Function.Name, reason), nil
}

func approvalDecisionApproved(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved", "accept", "accept_session":
		return true
	default:
		return false
	}
}

// parseToolArgs attempts to parse a JSON arguments string into a map.
// Returns an empty map on parse failure.
func parseToolArgs(raw string) map[string]interface{} {
	if raw == "" {
		return make(map[string]interface{})
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return make(map[string]interface{})
	}
	return args
}

// shouldAutoWebSearch checks if the model refused to search and the user's query needs web search.
func shouldAutoWebSearch(response, userInput string) bool {
	refusalPhrases := []string{
		"不能联网", "无法搜索", "不能直接获取", "无法获取实时",
		"联网查询工具", "不可用", "没法替你", "不能直接联网",
		"cannot search", "can't search", "no internet",
		"unable to search", "cannot access the internet",
		"联网搜索", "暂时不可用", "无法直接取到",
	}
	hasRefusal := false
	responseLower := strings.ToLower(response)
	for _, phrase := range refusalPhrases {
		if strings.Contains(responseLower, strings.ToLower(phrase)) {
			hasRefusal = true
			break
		}
	}
	if !hasRefusal {
		return false
	}

	// Check if user input looks like a search query
	searchKeywords := []string{
		"行情", "指数", "股票", "新闻", "天气", "搜索", "查询", "价格",
		"实时", "最新", "今天", "今日", "查看", "帮我查", "帮我搜",
		"search", "find", "look up", "what is", "how to",
	}
	inputLower := strings.ToLower(userInput)
	for _, kw := range searchKeywords {
		if strings.Contains(inputLower, strings.ToLower(kw)) {
			return true
		}
	}

	// If the input is short (likely a search query), also trigger
	if len([]rune(userInput)) <= 20 {
		return true
	}
	return false
}

// autoWebSearch performs an automatic web search using the user's input.
func (l *Loop) autoWebSearch(ctx context.Context, session *Session, query string) (string, error) {
	entry, ok := l.toolReg.Get("web_search")
	if !ok || entry == nil || entry.Handler == nil {
		return "", fmt.Errorf("web_search tool not available")
	}
	dummyCall := bifrost.ToolCall{ID: "auto-web-search", Function: bifrost.FunctionCall{Name: "web_search"}}
	args := map[string]interface{}{"query": query}
	return entry.Handler(ctx, session, dummyCall, args)
}
