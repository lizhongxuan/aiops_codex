package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeToolHandler struct {
	descriptor ToolDescriptor
	result     ToolExecutionResult
	err        error
	executeFn  func(context.Context, ToolInvocation) (ToolExecutionResult, error)
}

func (f fakeToolHandler) Descriptor() ToolDescriptor {
	return f.descriptor
}

func (f fakeToolHandler) Execute(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error) {
	if f.executeFn != nil {
		return f.executeFn(ctx, inv)
	}
	result := f.result
	if result.InvocationID == "" {
		result.InvocationID = inv.InvocationID
	}
	return result, f.err
}

type collectingToolSubscriber struct {
	events []ToolLifecycleEvent
}

func (c *collectingToolSubscriber) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	c.events = append(c.events, event)
	return nil
}

func TestToolDispatcherDispatchEmitsStartedAndCompleted(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.Register(fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:       "test.readonly",
			Kind:       "test",
			IsReadOnly: true,
			StartPhase: "thinking",
		},
		result: ToolExecutionResult{
			Status:     ToolRunStatusCompleted,
			OutputText: "ok",
		},
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-dispatch-success",
		ToolName:  "test.readonly",
		Arguments: map[string]any{"path": "/tmp/demo"},
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(subscriber.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(subscriber.events))
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted {
		t.Fatalf("expected first event started, got %#v", subscriber.events[0])
	}
	if subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected second event completed, got %#v", subscriber.events[1])
	}
}

func TestToolDispatcherDispatchEmitsFailedOnHandlerError(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.Register(fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:       "test.failure",
			Kind:       "test",
			IsReadOnly: true,
			StartPhase: "thinking",
		},
		err: errors.New("boom"),
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	_, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-dispatch-fail",
		ToolName:  "test.failure",
	})
	if err == nil {
		t.Fatal("expected dispatch error")
	}
	if len(subscriber.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(subscriber.events))
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted {
		t.Fatalf("expected first event started, got %#v", subscriber.events[0])
	}
	if subscriber.events[1].Type != ToolLifecycleEventFailed {
		t.Fatalf("expected second event failed, got %#v", subscriber.events[1])
	}
}

func TestToolDispatcherDispatchPausesWhenApprovalIsPending(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	handler := fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:             "test.mutation",
			Kind:             "mutation",
			RequiresApproval: true,
		},
		result: ToolExecutionResult{
			Status: ToolRunStatusCompleted,
		},
	}
	if err := registry.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	coord := NewToolApprovalCoordinator()
	coord.now = func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) }
	coord.nextID = func(prefix string) string { return prefix + "-pending" }

	dispatcher := newToolDispatcher(app, registry, bus, coord)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-dispatch-approval",
		ToolName:  "test.mutation",
		CallID:    "call-approval",
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusWaitingApproval {
		t.Fatalf("expected waiting approval result, got %#v", result)
	}
	if len(subscriber.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(subscriber.events))
	}
	if subscriber.events[0].Type != ToolLifecycleEventApprovalRequested {
		t.Fatalf("expected approval requested event, got %#v", subscriber.events[0])
	}
	if got := subscriber.events[0].ApprovalID; got != "approval-pending" {
		t.Fatalf("expected approval id to flow through event, got %q", got)
	}
}

func TestToolDispatcherDispatchAutoApprovalEmitsResolvedBeforeExecution(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.Register(fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:             "test.auto-approved",
			Kind:             "mutation",
			RequiresApproval: true,
		},
		result: ToolExecutionResult{
			Status:     ToolRunStatusCompleted,
			OutputText: "ok",
		},
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	coord := NewToolApprovalCoordinator(ToolApprovalRuleFunc{
		RuleName: "session-allow",
		Fn: func(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
			if req.SessionID != "session-dispatch-auto-approved" {
				return ApprovalResolution{}, false
			}
			return ApprovalResolution{
				Status:   ApprovalResolutionStatusApproved,
				RuleName: "session-allow",
				Reason:   "session grant allows execution",
			}, true
		},
	})
	coord.now = func() time.Time { return time.Date(2026, 4, 17, 12, 30, 0, 0, time.UTC) }
	coord.nextID = func(prefix string) string { return prefix + "-auto" }

	dispatcher := newToolDispatcher(app, registry, bus, coord)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-dispatch-auto-approved",
		ToolName:  "test.auto-approved",
		CallID:    "call-auto",
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(subscriber.events) != 4 {
		t.Fatalf("expected 4 lifecycle events, got %d", len(subscriber.events))
	}
	expectedTypes := []ToolLifecycleEventType{
		ToolLifecycleEventApprovalRequested,
		ToolLifecycleEventApprovalResolved,
		ToolLifecycleEventStarted,
		ToolLifecycleEventCompleted,
	}
	for i, expected := range expectedTypes {
		if subscriber.events[i].Type != expected {
			t.Fatalf("event %d expected %s, got %#v", i, expected, subscriber.events[i])
		}
	}
	if subscriber.events[1].ApprovalID != "approval-auto" {
		t.Fatalf("expected approval id to remain stable, got %q", subscriber.events[1].ApprovalID)
	}
}

func TestToolDispatcherDispatchEmitsProgressBetweenStartedAndCompleted(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.Register(fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:                      "test.progress",
			Kind:                      "test",
			IsReadOnly:                true,
			StartPhase:                "executing",
			SupportsStreamingProgress: true,
		},
		executeFn: func(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error) {
			if err := ReportToolProgress(ctx, ToolProgressUpdate{
				Message: "processed 1/2 chunks",
				Payload: map[string]any{
					"current": 1,
					"total":   2,
				},
			}); err != nil {
				return ToolExecutionResult{}, err
			}
			return ToolExecutionResult{
				Status:     ToolRunStatusCompleted,
				OutputText: "ok",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-dispatch-progress",
		ToolName:  "test.progress",
		Arguments: map[string]any{"path": "/tmp/demo"},
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(subscriber.events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(subscriber.events))
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted {
		t.Fatalf("expected started event first, got %#v", subscriber.events[0])
	}
	if subscriber.events[1].Type != ToolLifecycleEventProgress {
		t.Fatalf("expected progress event second, got %#v", subscriber.events[1])
	}
	if subscriber.events[2].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected completed event third, got %#v", subscriber.events[2])
	}
	if got, _ := getIntAny(subscriber.events[1].Payload, "current"); got != 1 {
		t.Fatalf("expected progress payload current=1, got %#v", subscriber.events[1].Payload)
	}
	if got, _ := getIntAny(subscriber.events[1].Payload, "total"); got != 2 {
		t.Fatalf("expected progress payload total=2, got %#v", subscriber.events[1].Payload)
	}
	if subscriber.events[1].Message != "processed 1/2 chunks" {
		t.Fatalf("expected progress message to flow through, got %#v", subscriber.events[1])
	}
}

func TestToolDispatcherDispatchDoesNotEmitProgressWhenDescriptorDoesNotSupportIt(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.Register(fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:       "test.progress.disabled",
			Kind:       "test",
			IsReadOnly: true,
			StartPhase: "executing",
		},
		executeFn: func(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error) {
			if err := ReportToolProgress(ctx, ToolProgressUpdate{
				Message: "should be ignored",
			}); err != nil {
				return ToolExecutionResult{}, err
			}
			return ToolExecutionResult{
				Status: ToolRunStatusCompleted,
			}, nil
		},
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	_, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-dispatch-progress-disabled",
		ToolName:  "test.progress.disabled",
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(subscriber.events) != 2 {
		t.Fatalf("expected only started/completed events, got %#v", subscriber.events)
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted || subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected started/completed events only, got %#v", subscriber.events)
	}
}

func TestToolDispatcherDispatchBatchUsesDescriptorApprovalCategory(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegister(fakeToolHandler{
		descriptor: ToolDescriptor{
			Name:             "test.batch.approval",
			Kind:             "mutation",
			RequiresApproval: true,
		},
	})

	dispatcher := newToolDispatcher(app, registry, nil, nil)
	results, blocked := dispatcher.dispatchBatch([]toolDispatchRequest{
		{
			CallID:   "call-batch-approval",
			ToolName: "test.batch.approval",
			Category: toolCategoryMutation,
		},
	})

	if !blocked {
		t.Fatalf("expected descriptor-driven approval category to block batch, got %#v", results)
	}
	if len(results) != 1 || !results[0].Blocking {
		t.Fatalf("expected blocking approval result, got %#v", results)
	}
}

func TestToolDispatcherDispatchUsesUnifiedPermissionInsteadOfDescriptor(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.RegisterUnifiedTool(scriptedUnifiedTool{
		name: "test.unified.approval",
		permissionFn: func(_ context.Context, req ToolCallRequest) (PermissionResult, error) {
			return PermissionResult{
				Allowed:           false,
				RequiresApproval:  true,
				ApprovalType:      "custom_mutation",
				ApprovalDecisions: []string{"approve_once", "decline"},
				Reason:            "unified permission requires approval",
				Metadata: map[string]any{
					"cardId": "approval-card-custom",
				},
			}, nil
		},
		callFn: func(context.Context, ToolCallRequest) (ToolCallResult, error) {
			t.Fatal("tool should not execute before approval")
			return ToolCallResult{}, nil
		},
		isReadOnly: func(ToolCallRequest) bool { return true },
	}); err != nil {
		t.Fatalf("register unified tool: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	coord := NewToolApprovalCoordinator()
	coord.now = func() time.Time { return time.Date(2026, 4, 18, 1, 0, 0, 0, time.UTC) }
	coord.nextID = func(prefix string) string { return prefix + "-custom" }

	dispatcher := newToolDispatcher(app, registry, bus, coord)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-unified-approval",
		ToolName:  "test.unified.approval",
		CallID:    "call-unified-approval",
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusWaitingApproval {
		t.Fatalf("expected waiting approval result, got %#v", result)
	}
	if len(subscriber.events) != 1 {
		t.Fatalf("expected single approval event, got %#v", subscriber.events)
	}
	event := subscriber.events[0]
	if event.Type != ToolLifecycleEventApprovalRequested {
		t.Fatalf("expected approval requested event, got %#v", event)
	}
	approval, ok := event.Payload["approval"].(map[string]any)
	if !ok {
		t.Fatalf("expected approval payload, got %#v", event.Payload)
	}
	if got := getStringAny(approval, "approvalType"); got != "custom_mutation" {
		t.Fatalf("expected unified approval type, got %#v", approval)
	}
	if got := getStringAny(approval, "cardId"); got != "approval-card-custom" {
		t.Fatalf("expected unified card id metadata to flow through, got %#v", approval)
	}
}

func TestToolDispatcherDispatchRejectsUnifiedDenyWithoutApproval(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.RegisterUnifiedTool(scriptedUnifiedTool{
		name: "test.unified.denied",
		permissionFn: func(_ context.Context, req ToolCallRequest) (PermissionResult, error) {
			return PermissionResult{
				Allowed:          false,
				RequiresApproval: false,
				Reason:           "blocked by unified permission",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register unified tool: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	_, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-unified-denied",
		ToolName:  "test.unified.denied",
	})
	if err == nil {
		t.Fatal("expected unified permission denial to fail dispatch")
	}
	if len(subscriber.events) != 1 || subscriber.events[0].Type != ToolLifecycleEventFailed {
		t.Fatalf("expected failed event only, got %#v", subscriber.events)
	}
	if subscriber.events[0].Error != "blocked by unified permission" {
		t.Fatalf("expected unified reason to surface, got %#v", subscriber.events[0])
	}
}

func TestToolDispatcherDispatchProjectsUnifiedDisplayResult(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	if err := registry.RegisterUnifiedTool(scriptedUnifiedTool{
		name: "test.unified.display",
		callFn: func(context.Context, ToolCallRequest) (ToolCallResult, error) {
			return ToolCallResult{
				Output: "ok",
				DisplayOutput: &ToolDisplayPayload{
					Summary:  "displayed result",
					Activity: "searching",
					Blocks: []ToolDisplayBlock{
						{
							Kind: ToolDisplayBlockSearchQueries,
							Items: []map[string]any{
								{"query": "btc price"},
							},
						},
					},
				},
			}, nil
		},
	}); err != nil {
		t.Fatalf("register unified tool: %v", err)
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-unified-display",
		ToolName:  "test.unified.display",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(subscriber.events) != 2 {
		t.Fatalf("expected started/completed events, got %#v", subscriber.events)
	}
	display, ok := subscriber.events[1].Payload["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected display payload on completed event, got %#v", subscriber.events[1].Payload)
	}
	if got := getStringAny(display, "summary"); got != "displayed result" {
		t.Fatalf("expected result display summary, got %#v", display)
	}
}

func TestToolDispatcherDispatchUsesUnifiedToolWithoutLegacyHandlerAdapter(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.handlers["test.unified.only"] = &toolRegistryEntry{
		descriptor: ToolDescriptor{
			Name:       "test.unified.only",
			Kind:       "test",
			IsReadOnly: true,
			StartPhase: "thinking",
		},
		unified: scriptedUnifiedTool{
			name: "test.unified.only",
			callFn: func(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
				if got := getStringAny(req.Input, "path"); got != "/tmp/demo" {
					t.Fatalf("unexpected input passed to unified tool: %#v", req.Input)
				}
				return ToolCallResult{
					Output: "unified execution ok",
				}, nil
			},
		},
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-unified-only",
		ToolName:  "test.unified.only",
		Arguments: map[string]any{"path": "/tmp/demo"},
		HostID:    "server-local",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if result.OutputText != "unified execution ok" {
		t.Fatalf("expected unified output text, got %#v", result)
	}
	if len(subscriber.events) != 2 {
		t.Fatalf("expected started/completed events, got %#v", subscriber.events)
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted || subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected started/completed events, got %#v", subscriber.events)
	}
}

func TestToolDispatcherDispatchHonorsUnifiedFailureMetadata(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.handlers["test.unified.failed"] = &toolRegistryEntry{
		descriptor: ToolDescriptor{
			Name:       "test.unified.failed",
			Kind:       "test",
			IsReadOnly: true,
			StartPhase: "thinking",
		},
		unified: scriptedUnifiedTool{
			name: "test.unified.failed",
			callFn: func(context.Context, ToolCallRequest) (ToolCallResult, error) {
				return ToolCallResult{
					Output: "validation failed",
					Metadata: map[string]any{
						"status":    "failed",
						"errorText": "validation failed",
					},
				}, nil
			},
		},
	}

	bus := NewInProcessToolEventBus()
	subscriber := &collectingToolSubscriber{}
	bus.Subscribe(subscriber)

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "session-unified-failed",
		ToolName:  "test.unified.failed",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != ToolRunStatusFailed {
		t.Fatalf("expected failed result, got %#v", result)
	}
	if result.ErrorText != "validation failed" {
		t.Fatalf("expected failure text to flow through, got %#v", result)
	}
	if len(subscriber.events) != 2 || subscriber.events[1].Type != ToolLifecycleEventFailed {
		t.Fatalf("expected failed event, got %#v", subscriber.events)
	}
}

func TestRegisterDefaultToolHandlersUsesUnifiedReadonlyTools(t *testing.T) {
	app := newTestApp(t)

	checks := map[string]func(UnifiedTool) bool{
		"query_ai_server_state": func(tool UnifiedTool) bool {
			_, ok := tool.(queryAIServerStateUnifiedTool)
			return ok
		},
		"execute_readonly_query": func(tool UnifiedTool) bool {
			_, ok := tool.(readonlyCommandUnifiedTool)
			return ok
		},
		"readonly_host_inspect": func(tool UnifiedTool) bool {
			_, ok := tool.(readonlyCommandUnifiedTool)
			return ok
		},
	}

	for name, wantType := range checks {
		desc, unified, ok := app.toolHandlerRegistry.LookupUnified(name)
		if !ok || unified == nil {
			t.Fatalf("expected unified tool %q to be registered, desc=%#v", name, desc)
		}
		if !wantType(unified) {
			t.Fatalf("expected %q to use concrete unified tool, got %T", name, unified)
		}
	}
}

func TestToolDispatcherDispatchBatchUsesUnifiedConcurrencyAndApprovalMetadata(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(scriptedUnifiedTool{
		name:              "test.batch.readonly.parallel",
		isReadOnly:        func(ToolCallRequest) bool { return true },
		isConcurrencySafe: func(ToolCallRequest) bool { return true },
	})
	registry.MustRegisterUnifiedTool(scriptedUnifiedTool{
		name:              "test.batch.readonly.serial",
		isReadOnly:        func(ToolCallRequest) bool { return true },
		isConcurrencySafe: func(ToolCallRequest) bool { return false },
	})
	registry.MustRegisterUnifiedTool(scriptedUnifiedTool{
		name: "test.batch.approval.unified",
		permissionFn: func(_ context.Context, req ToolCallRequest) (PermissionResult, error) {
			return PermissionResult{Allowed: false, RequiresApproval: true}, nil
		},
	})

	dispatcher := newToolDispatcher(app, registry, nil, nil)
	results, blocked := dispatcher.dispatchBatch([]toolDispatchRequest{
		{
			CallID:   "call-batch-readonly-parallel",
			ToolName: "test.batch.readonly.parallel",
			Input:    map[string]any{"path": "/tmp/a"},
		},
		{
			CallID:   "call-batch-readonly-serial",
			ToolName: "test.batch.readonly.serial",
			Input:    map[string]any{"path": "/tmp/b"},
		},
		{
			CallID:   "call-batch-approval-unified",
			ToolName: "test.batch.approval.unified",
		},
	})

	if !blocked {
		t.Fatalf("expected unified approval metadata to block batch, got %#v", results)
	}
	if len(results) != 1 || results[0].ToolName != "test.batch.approval.unified" || !results[0].Blocking {
		t.Fatalf("expected approval tool to short-circuit batch, got %#v", results)
	}

	results, blocked = dispatcher.dispatchBatch([]toolDispatchRequest{
		{
			CallID:   "call-batch-readonly-parallel-2",
			ToolName: "test.batch.readonly.parallel",
			Input:    map[string]any{"path": "/tmp/a"},
		},
		{
			CallID:   "call-batch-readonly-serial-2",
			ToolName: "test.batch.readonly.serial",
			Input:    map[string]any{"path": "/tmp/b"},
		},
	})
	if blocked {
		t.Fatalf("did not expect batch to block, got %#v", results)
	}
	if len(results) != 2 {
		t.Fatalf("expected both batch results, got %#v", results)
	}
}
