package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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

// ToolExecutionObserver receives callbacks before and after tool execution.
// The server layer implements this to create ProcessLineCards and update activity.
type ToolExecutionObserver interface {
	OnToolStart(ctx context.Context, session *Session, toolName string, args map[string]interface{})
	OnToolComplete(ctx context.Context, session *Session, toolName string, args map[string]interface{}, result string, err error)
}

type TurnCompletionDecision struct {
	Action        string
	RepairMessage string
}

type TurnCompletionValidator interface {
	ValidateTurnCompletion(ctx context.Context, session *Session, userInput, assistantContent string) TurnCompletionDecision
}

type noopToolObserver struct{}

func (noopToolObserver) OnToolStart(context.Context, *Session, string, map[string]interface{}) {}
func (noopToolObserver) OnToolComplete(context.Context, *Session, string, map[string]interface{}, string, error) {
}

// Loop is the main agent loop that drives the ReAct cycle:
// reason (LLM call) → act (tool execution) → observe (append result) → repeat.
type Loop struct {
	gateway         *bifrost.Gateway
	toolReg         *ToolRegistry
	compressor      ContextCompressor
	approvalHandler ApprovalHandler
	streamObserver  StreamObserver
	toolObserver    ToolExecutionObserver
	completionGate  TurnCompletionValidator
	hookRuntime     *hooks.Runtime
	webSearchMode   string
	checkpointStore *CheckpointStore
}

// NewLoop creates a new Loop with the given dependencies.
func NewLoop(gateway *bifrost.Gateway, toolReg *ToolRegistry, compressor ContextCompressor) *Loop {
	return &Loop{
		gateway:        gateway,
		toolReg:        toolReg,
		compressor:     compressor,
		streamObserver: noopStreamObserver{},
		toolObserver:   noopToolObserver{},
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

// SetToolObserver wires an optional tool execution observer into the loop.
func (l *Loop) SetToolObserver(observer ToolExecutionObserver) *Loop {
	if observer == nil {
		l.toolObserver = noopToolObserver{}
		return l
	}
	l.toolObserver = observer
	return l
}

// SetTurnCompletionValidator wires an optional turn completion validator into the loop.
func (l *Loop) SetTurnCompletionValidator(validator TurnCompletionValidator) *Loop {
	l.completionGate = validator
	return l
}

// SetHookRuntime wires an optional hook runtime into the loop.
func (l *Loop) SetHookRuntime(rt *hooks.Runtime) *Loop {
	l.hookRuntime = rt
	return l
}

// SetWebSearchMode configures the web search mode for the loop.
// Valid values: "duckduckgo", "brave", "native", "disabled".
func (l *Loop) SetWebSearchMode(mode string) *Loop {
	l.webSearchMode = mode
	return l
}

// SetCheckpointStore wires an optional checkpoint store into the loop.
func (l *Loop) SetCheckpointStore(store *CheckpointStore) *Loop {
	l.checkpointStore = store
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
	if session.Metadata == nil {
		session.Metadata = make(map[string]string)
	}
	delete(session.Metadata, "market_snapshot_answer_nudged")
	delete(session.Metadata, "completion_gate_repairs")
	session.Metadata["market_snapshot_tool_base"] = strconv.Itoa(sessionToolResultCount(session))

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

	tracker := &iterationBudgetTracker{}
	completed := false

	for iteration := 0; iteration < session.MaxIterations(); iteration++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("agent loop cancelled: %w", err)
		}

		// Check for mid-turn injected messages (Claude Code-style interrupt)
		if injected := session.DrainInterrupt(); injected != "" {
			session.ContextManager().AppendUser("[User interrupt]: " + injected)
			log.Printf("[loop] mid-turn message injected: %s", truncateLog(injected, 100))
		}

		// Track content length for diminishing returns detection
		tracker.record(totalContentLength(session.ContextManager().Messages()))

		// Proactive compression: force compress if iterations are high and progress is stalling
		if tracker.shouldForceCompress(iteration) {
			log.Printf("[loop] proactive compression triggered at iteration %d", iteration)
			if l.compressor != nil {
				if err := l.compressor.Compact(ctx, session.ContextManager()); err != nil {
					log.Printf("[loop] proactive compression failed: %v", err)
				}
			}
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

		session.ContextManager().AppendAssistant(result.Content, result.ToolCalls, result.ReasoningContent)

		// Checkpoint after LLM response
		if l.checkpointStore != nil {
			l.checkpointStore.Save(IterationCheckpoint{
				SessionID: session.ID,
				Iteration: iteration,
				Messages:  session.ContextManager().Messages(),
				Phase:     "llm_call",
				ToolCalls: result.ToolCalls,
			})
		}

		if len(result.ToolCalls) == 0 {
			// Auto-fallback: if this is a time-sensitive search-style query and the
			// model answered without any tool call, force one explicit web search so
			// the UI can stream visible progress lines instead of silently waiting.
			if shouldAutoWebSearch(
				result.Content,
				userInput,
				sessionPrefersExplicitWebSearch(session),
				sessionHasAutoWebSearchResults(session),
			) {
				searchResult, searchErr := l.autoWebSearch(ctx, session, userInput)
				if searchErr == nil && searchResult != "" {
					// Inject search results and continue the loop
					session.ContextManager().AppendUser("[System: web_search was executed automatically. Results below]\n" + searchResult)
					continue // Re-enter the ReAct loop with search results
				}
			}
			if l.completionGate != nil {
				decision := l.completionGate.ValidateTurnCompletion(ctx, session, userInput, result.Content)
				switch strings.TrimSpace(decision.Action) {
				case "continue":
					repairCount := 0
					if session.Metadata != nil {
						if raw := strings.TrimSpace(session.Metadata["completion_gate_repairs"]); raw != "" {
							if parsed, err := strconv.Atoi(raw); err == nil {
								repairCount = parsed
							}
						}
						session.Metadata["completion_gate_repairs"] = strconv.Itoa(repairCount + 1)
					}
					if repairCount >= 2 {
						completed = true
						break
					}
					if message := strings.TrimSpace(decision.RepairMessage); message != "" {
						session.ContextManager().AppendUser("[Runtime policy repair]\n" + message)
					}
					continue
				case "abstain":
					if message := strings.TrimSpace(decision.RepairMessage); message != "" {
						session.ContextManager().AppendAssistant(message, nil)
					}
				}
			}
			completed = true
			break // successful completion
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
		if shouldNudgeCompactSnapshotAnswer(session, userInput) {
			session.ContextManager().AppendUser(compactSnapshotAnswerNudge)
			session.Metadata["market_snapshot_answer_nudged"] = "true"
		}

		// Checkpoint after tool execution
		if l.checkpointStore != nil {
			entries := make([]toolResultEntry, len(outcomes))
			for i, o := range outcomes {
				entries[i] = toolResultEntry{CallID: o.CallID, Result: o.Result}
			}
			l.checkpointStore.Save(IterationCheckpoint{
				SessionID:   session.ID,
				Iteration:   iteration,
				Messages:    session.ContextManager().Messages(),
				Phase:       "tool_exec",
				ToolResults: entries,
			})
		}

		if IsPauseTurn(err) {
			// Clear checkpoints on pause (partial success)
			if l.checkpointStore != nil {
				l.checkpointStore.Clear(session.ID)
			}
			return nil
		}
	}

	// Clear checkpoints on turn completion
	if l.checkpointStore != nil {
		l.checkpointStore.Clear(session.ID)
	}

	if !completed {
		return fmt.Errorf("agent loop exceeded maximum iterations (%d)", session.MaxIterations())
	}

	return nil
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

	tools := l.toolReg.Definitions(session.EnabledTools())
	caps := l.gateway.ProviderCapabilities(session.Model())

	req := bifrost.ChatRequest{
		Model:    session.Model(),
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}

	// If native search is supported and webSearchMode is "native", filter out
	// the web_search function tool and enable native search on the request.
	if l.webSearchMode == "native" && caps.SupportsNativeSearch && !sessionPrefersExplicitWebSearch(session) {
		req.Tools = filterOutTool(req.Tools, "web_search")
		req.WebSearchEnabled = true
		req.UseResponsesAPI = true
	}

	// If the provider doesn't support streaming tool calls and tools are present,
	// disable streaming to avoid partial tool call issues.
	if !caps.SupportsStreamingToolCalls && len(req.Tools) > 0 {
		req.Stream = false
	}

	log.Printf("[bifrost-debug] buildChatRequest: model=%s tools=%d enabledTools=%v", session.Model(), len(req.Tools), session.EnabledTools())
	return req
}

// filterOutTool returns a new slice of ToolDefinitions excluding the tool with
// the given name. The original slice is never mutated.
func filterOutTool(tools []bifrost.ToolDefinition, name string) []bifrost.ToolDefinition {
	filtered := make([]bifrost.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if t.Function.Name != name {
			filtered = append(filtered, t)
		}
	}
	return filtered
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
			// Check for mid-turn interrupt between sequential tool executions
			if injected := session.DrainInterrupt(); injected != "" {
				session.ContextManager().AppendUser("[User interrupt]: " + injected)
				log.Printf("[loop] mid-turn interrupt during tool batch: %s", truncateLog(injected, 100))
				break // Don't execute remaining tools — let the model re-evaluate
			}
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

	l.toolObserver.OnToolStart(ctx, session, tc.Function.Name, args)

	result, err := l.toolReg.Dispatch(ctx, session, tc, tc.Function.Name, args)
	if err != nil {
		l.toolObserver.OnToolComplete(ctx, session, tc.Function.Name, args, "", err)
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

	l.toolObserver.OnToolComplete(ctx, session, tc.Function.Name, args, result, nil)

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

func sessionPrefersExplicitWebSearch(session *Session) bool {
	if session == nil || session.Metadata == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(session.Metadata["prefer_explicit_web_search"])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func queryNeedsWebSearch(userInput string) bool {
	searchKeywords := []string{
		"行情", "指数", "股票", "新闻", "天气", "搜索", "查询", "价格",
		"实时", "最新", "今天", "今日", "查看", "帮我查", "帮我搜",
		"a股", "美股", "港股", "btc", "eth", "比特币", "以太坊", "加密",
		"search", "find", "look up", "what is", "how to", "price", "news", "market", "bitcoin", "crypto",
	}
	inputLower := strings.ToLower(strings.TrimSpace(userInput))
	for _, kw := range searchKeywords {
		if strings.Contains(inputLower, strings.ToLower(kw)) {
			return true
		}
	}
	return len([]rune(inputLower)) > 0 && len([]rune(inputLower)) <= 20
}

const compactSnapshotAnswerNudge = "[System: You already have enough market snapshot data to answer. Stop calling more tools unless there is a material conflict you cannot resolve with a price range or a brief timing-difference note. Answer now in compact snapshot format: time boundary + current price/index + 2-4 bullets + 1 short judgment + 1-2 sources.]"

func queryNeedsCompactSnapshotAnswer(userInput string) bool {
	keywords := []string{
		"行情", "指数", "价格", "报价", "最新", "实时", "今日", "今天",
		"btc", "eth", "比特币", "以太坊", "crypto", "bitcoin", "market", "price",
		"a股", "美股", "港股", "上证", "深证", "创业板",
	}
	inputLower := strings.ToLower(strings.TrimSpace(userInput))
	for _, kw := range keywords {
		if strings.Contains(inputLower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func sessionToolResultCount(session *Session) int {
	if session == nil {
		return 0
	}
	count := 0
	for _, msg := range session.ContextManager().Messages() {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "tool") {
			count++
		}
	}
	return count
}

func sessionMarketSnapshotToolBase(session *Session) int {
	if session == nil || session.Metadata == nil {
		return 0
	}
	base, err := strconv.Atoi(strings.TrimSpace(session.Metadata["market_snapshot_tool_base"]))
	if err != nil || base < 0 {
		return 0
	}
	return base
}

func sessionHasCompactSnapshotAnswerNudge(session *Session) bool {
	if session == nil || session.Metadata == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(session.Metadata["market_snapshot_answer_nudged"])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shouldNudgeCompactSnapshotAnswer(session *Session, userInput string) bool {
	if !queryNeedsCompactSnapshotAnswer(userInput) || session == nil {
		return false
	}
	if sessionHasCompactSnapshotAnswerNudge(session) {
		return false
	}
	if strings.TrimSpace(session.CurrentCardID()) != "" {
		return false
	}
	base := sessionMarketSnapshotToolBase(session)
	return sessionToolResultCount(session)-base >= 1
}

func sessionHasAutoWebSearchResults(session *Session) bool {
	if session == nil {
		return false
	}
	for _, msg := range session.ContextManager().Messages() {
		if msg.Role != "user" {
			continue
		}
		content, _ := msg.Content.(string)
		if strings.Contains(content, "[System: web_search was executed automatically. Results below]") {
			return true
		}
	}
	return false
}

// shouldAutoWebSearch checks if the loop should force a web search for a
// time-sensitive query when the model returned plain text without any tool calls.
func shouldAutoWebSearch(response, userInput string, preferExplicit bool, alreadySearched bool) bool {
	if alreadySearched || !queryNeedsWebSearch(userInput) {
		return false
	}
	if preferExplicit {
		return true
	}
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
	return true
}

// autoWebSearch performs an automatic web search using the user's input.
// It first tries the OpenAI Responses API for native high-quality search,
// then falls back to the DuckDuckGo-based web_search tool handler.
func (l *Loop) autoWebSearch(ctx context.Context, session *Session, query string) (string, error) {
	if sessionPrefersExplicitWebSearch(session) {
		entry, ok := l.toolReg.Get("web_search")
		if !ok || entry == nil || entry.Handler == nil {
			return "", fmt.Errorf("web_search tool not available")
		}
		dummyCall := bifrost.ToolCall{ID: "auto-web-search", Function: bifrost.FunctionCall{Name: "web_search"}}
		args := map[string]interface{}{"query": query}
		return entry.Handler(ctx, session, dummyCall, args)
	}

	// Try Responses API native search first (high quality, uses Bing).
	if l.gateway != nil {
		searchReq := bifrost.ChatRequest{
			Model: session.Model(),
			Messages: []bifrost.Message{
				{Role: "user", Content: query},
			},
			Stream:           true,
			UseResponsesAPI:  true,
			WebSearchEnabled: true,
		}
		stream, err := l.gateway.StreamChatCompletion(ctx, searchReq)
		if err == nil {
			var result strings.Builder
			for ev := range stream {
				if ev.Type == "content_delta" {
					result.WriteString(ev.Delta)
				}
			}
			if result.Len() > 0 {
				return result.String(), nil
			}
		}
		log.Printf("[auto-web-search] Responses API failed, falling back to DuckDuckGo: %v", err)
	}

	// Fallback to DuckDuckGo-based search.
	entry, ok := l.toolReg.Get("web_search")
	if !ok || entry == nil || entry.Handler == nil {
		return "", fmt.Errorf("web_search tool not available")
	}
	dummyCall := bifrost.ToolCall{ID: "auto-web-search", Function: bifrost.FunctionCall{Name: "web_search"}}
	args := map[string]interface{}{"query": query}
	return entry.Handler(ctx, session, dummyCall, args)
}

// ─── Proactive Compression (Optimization 3) ─────────────────────────────────

const (
	// ProactiveCompressIterationThreshold triggers forced compression after this many iterations.
	ProactiveCompressIterationThreshold = 8

	// DiminishingReturnsDeltaThreshold: if the last N iterations produced fewer than this many
	// new content characters, force compression.
	DiminishingReturnsDeltaThreshold = 200

	// DiminishingReturnsCheckWindow: number of consecutive low-delta iterations before triggering.
	DiminishingReturnsCheckWindow = 3
)

// iterationBudgetTracker monitors iteration progress and detects diminishing returns.
type iterationBudgetTracker struct {
	contentLengths []int // content length after each iteration
}

func (t *iterationBudgetTracker) record(contentLen int) {
	t.contentLengths = append(t.contentLengths, contentLen)
}

// shouldForceCompress returns true if iterations are high and progress is diminishing.
func (t *iterationBudgetTracker) shouldForceCompress(iteration int) bool {
	if iteration < ProactiveCompressIterationThreshold {
		return false
	}

	// Check for diminishing returns: last N iterations produced very little new content
	n := len(t.contentLengths)
	if n < DiminishingReturnsCheckWindow+1 {
		return true // High iteration count but not enough data — compress anyway
	}

	lowDeltaCount := 0
	for i := n - DiminishingReturnsCheckWindow; i < n; i++ {
		delta := t.contentLengths[i] - t.contentLengths[i-1]
		if delta < DiminishingReturnsDeltaThreshold {
			lowDeltaCount++
		}
	}
	return lowDeltaCount >= DiminishingReturnsCheckWindow
}

// totalContentLength sums the character length of all message content.
func totalContentLength(msgs []bifrost.Message) int {
	total := 0
	for _, m := range msgs {
		switch v := m.Content.(type) {
		case string:
			total += len(v)
		}
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Arguments)
		}
	}
	return total
}

// truncateLog truncates a string for log output.
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
