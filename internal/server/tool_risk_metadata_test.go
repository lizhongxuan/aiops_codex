package server

import (
	"context"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func toolRiskMetadataValue(t *testing.T, tool map[string]any) map[string]any {
	t.Helper()
	meta, ok := tool["riskMetadata"].(map[string]any)
	if !ok || len(meta) == 0 {
		t.Fatalf("expected riskMetadata on tool %#v", tool)
	}
	return meta
}

func toolByName(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if getStringAny(tool, "name") == name {
			return tool
		}
	}
	t.Fatalf("expected tool %q in %#v", name, tools)
	return nil
}

func hasStrategy(meta map[string]any, want string) bool {
	switch raw := meta["verify_strategies"].(type) {
	case []string:
		for _, item := range raw {
			if item == want {
				return true
			}
		}
	case []any:
		for _, item := range raw {
			if text, ok := item.(string); ok && text == want {
				return true
			}
		}
	}
	return false
}

func hasVerificationSource(meta map[string]any, want string) bool {
	switch raw := meta["verification_sources"].(type) {
	case []string:
		for _, item := range raw {
			if item == want {
				return true
			}
		}
	case []any:
		for _, item := range raw {
			if text, ok := item.(string); ok && text == want {
				return true
			}
		}
	}
	return false
}

func TestDynamicToolSchemasExposeRiskMetadata(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.corootClient = coroot.NewClient("http://coroot.internal:8080", "test-token", time.Second)

	requiredKeys := []string{
		"category",
		"risk_level",
		"readonly",
		"mutation",
		"dangerous",
		"requires_plan",
		"requires_approval",
		"dry_run_supported",
		"rollback_supported",
		"verify_strategies",
		"verification_sources",
		"verify_strategy_details",
	}

	allTools := make([]map[string]any, 0)
	allTools = append(allTools, app.localDynamicTools()...)
	allTools = append(allTools, app.remoteDynamicTools()...)
	allTools = append(allTools, app.corootDynamicTools()...)
	for _, tool := range allTools {
		meta := toolRiskMetadataValue(t, tool)
		for _, key := range requiredKeys {
			if _, ok := meta[key]; !ok {
				t.Fatalf("expected risk metadata key %q on tool %q: %#v", key, getStringAny(tool, "name"), meta)
			}
		}
	}

	readonlyMeta := toolRiskMetadataValue(t, toolByName(t, allTools, "execute_readonly_query"))
	if readonly, _ := readonlyMeta["readonly"].(bool); !readonly {
		t.Fatalf("expected execute_readonly_query to be readonly, got %#v", readonlyMeta)
	}
	if mutation, _ := readonlyMeta["mutation"].(bool); mutation {
		t.Fatalf("expected execute_readonly_query to be non-mutation, got %#v", readonlyMeta)
	}

	mutationMeta := toolRiskMetadataValue(t, toolByName(t, allTools, "execute_system_mutation"))
	if dangerous, _ := mutationMeta["dangerous"].(bool); !dangerous {
		t.Fatalf("expected execute_system_mutation to be dangerous, got %#v", mutationMeta)
	}
	if requiresPlan, _ := mutationMeta["requires_plan"].(bool); !requiresPlan {
		t.Fatalf("expected execute_system_mutation to require plan approval, got %#v", mutationMeta)
	}
	if requiresApproval, _ := mutationMeta["requires_approval"].(bool); !requiresApproval {
		t.Fatalf("expected execute_system_mutation to require approval, got %#v", mutationMeta)
	}

	serviceMeta := toolRiskMetadataValue(t, toolByName(t, allTools, serviceRestartToolName))
	if rollbackSupported, _ := serviceMeta["rollback_supported"].(bool); !rollbackSupported {
		t.Fatalf("expected service restart to support rollback metadata, got %#v", serviceMeta)
	}
	if !hasStrategy(serviceMeta, "service_health") {
		t.Fatalf("expected service restart verify strategies, got %#v", serviceMeta)
	}
	if !hasVerificationSource(serviceMeta, verificationSourceCorootHealth) {
		t.Fatalf("expected service restart verification sources, got %#v", serviceMeta)
	}

	configMeta := toolRiskMetadataValue(t, toolByName(t, allTools, configApplyToolName))
	if dryRunSupported, _ := configMeta["dry_run_supported"].(bool); !dryRunSupported {
		t.Fatalf("expected config_apply to expose dry-run support, got %#v", configMeta)
	}
	if !hasVerificationSource(configMeta, verificationSourceHealthProbe) {
		t.Fatalf("expected config_apply verification sources, got %#v", configMeta)
	}

	corootMeta := toolRiskMetadataValue(t, toolByName(t, allTools, corootToolIncidentTime))
	if readonly, _ := corootMeta["readonly"].(bool); !readonly {
		t.Fatalf("expected coroot incident timeline to be readonly, got %#v", corootMeta)
	}
}

func TestResolveVerificationPlanClassifiesOperationalMutations(t *testing.T) {
	tests := []struct {
		name         string
		ctx          verificationPlanContext
		wantPrimary  string
		wantSource   string
		wantCriteria string
	}{
		{
			name: "rollout restart",
			ctx: verificationPlanContext{
				ToolName: "execute_system_mutation",
				Command:  "kubectl rollout restart deployment/api -n prod",
			},
			wantPrimary:  "rollout_status",
			wantSource:   verificationSourceCorootHealth,
			wantCriteria: "rollout 状态达到 completed",
		},
		{
			name: "rollout rollback",
			ctx: verificationPlanContext{
				ToolName: "execute_system_mutation",
				Command:  "kubectl rollout undo deployment/api -n prod",
			},
			wantPrimary:  "rollback_stability",
			wantSource:   verificationSourceMetricCheck,
			wantCriteria: "回滚后的 workload 已稳定",
		},
		{
			name: "scale deployment",
			ctx: verificationPlanContext{
				ToolName: "execute_system_mutation",
				Command:  "kubectl scale deployment/api --replicas=5 -n prod",
			},
			wantPrimary:  "capacity_recovery",
			wantSource:   verificationSourceHealthProbe,
			wantCriteria: "目标副本或容量达到预期",
		},
		{
			name: "file change",
			ctx: verificationPlanContext{
				ToolName: "execute_system_mutation",
				Mode:     "file_change",
				FilePath: "/etc/nginx/nginx.conf",
			},
			wantPrimary:  "config_validation",
			wantSource:   verificationSourceLogCheck,
			wantCriteria: "配置语法或渲染校验通过",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := resolveVerificationPlan(tc.ctx)
			if got := plan.PrimaryStrategy(); got != tc.wantPrimary {
				t.Fatalf("expected primary strategy %q, got %#v", tc.wantPrimary, plan)
			}
			if !containsVerificationString(plan.SourcePriority(), tc.wantSource) {
				t.Fatalf("expected verification source %q, got %#v", tc.wantSource, plan.SourcePriority())
			}
			if !containsVerificationString(plan.SuccessCriteria(), tc.wantCriteria) {
				t.Fatalf("expected success criteria %q, got %#v", tc.wantCriteria, plan.SuccessCriteria())
			}
		})
	}
}

func containsVerificationString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestWorkspaceReActThreadSpecIncludesRiskMetadata(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-risk-metadata"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, "remote-risk-01")
	app.store.UpsertHost(model.Host{
		ID:         "remote-risk-01",
		Name:       "remote-risk-01",
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})

	spec := app.buildWorkspaceReActThreadStartSpec(context.Background(), sessionID, "remote-risk-01")
	askMeta := toolRiskMetadataValue(t, toolByName(t, spec.DynamicTools, "ask_user_question"))
	if category := getStringAny(askMeta, "category"); category != string(toolCategoryBlocking) {
		t.Fatalf("expected ask_user_question category %q, got %#v", toolCategoryBlocking, askMeta)
	}

	dispatchMeta := toolRiskMetadataValue(t, toolByName(t, spec.DynamicTools, "orchestrator_dispatch_tasks"))
	if requiresPlan, _ := dispatchMeta["requires_plan"].(bool); !requiresPlan {
		t.Fatalf("expected orchestrator_dispatch_tasks to require plan, got %#v", dispatchMeta)
	}
	if mutation, _ := dispatchMeta["mutation"].(bool); !mutation {
		t.Fatalf("expected orchestrator_dispatch_tasks to be marked mutation, got %#v", dispatchMeta)
	}
}
