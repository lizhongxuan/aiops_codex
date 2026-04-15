package guardian

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// mockProvider implements bifrost.Provider for testing.
type mockProvider struct {
	name     string
	response *bifrost.ChatResponse
	err      error
	delay    time.Duration
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) SupportsToolCalling() bool { return true }
func (m *mockProvider) Capabilities() bifrost.ProviderCapabilities {
	return bifrost.ProviderCapabilities{ToolCallingFormat: "openai_function"}
}
func (m *mockProvider) StreamChatCompletion(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	return nil, nil
}
func (m *mockProvider) ChatCompletion(ctx context.Context, req bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func newTestGateway(provider *mockProvider) *bifrost.Gateway {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{
		DefaultProvider: provider.name,
		DefaultModel:    DefaultGuardianModel,
	})
	gw.RegisterProvider(provider.name, provider)
	return gw
}

func TestNewGuardian_Defaults(t *testing.T) {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{})
	g := NewGuardian(gw)

	if g.preferredModel != DefaultGuardianModel {
		t.Errorf("preferredModel = %q, want %q", g.preferredModel, DefaultGuardianModel)
	}
	if g.timeout != DefaultGuardianTimeout {
		t.Errorf("timeout = %v, want %v", g.timeout, DefaultGuardianTimeout)
	}
	if g.sessionMgr == nil {
		t.Error("sessionMgr should not be nil")
	}
}

func TestGuardian_WithModel(t *testing.T) {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{})
	g := NewGuardian(gw).WithModel("custom-model")

	if g.preferredModel != "custom-model" {
		t.Errorf("preferredModel = %q, want %q", g.preferredModel, "custom-model")
	}
}

func TestGuardian_WithTimeout(t *testing.T) {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{})
	g := NewGuardian(gw).WithTimeout(30 * time.Second)

	if g.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want %v", g.timeout, 30*time.Second)
	}
}

func TestGuardian_ReviewApproval_Success(t *testing.T) {
	assessment := GuardianAssessment{
		RiskLevel:         RiskLow,
		UserAuthorization: AuthExplicit,
		Outcome:           OutcomeAllow,
		Rationale:         "user requested file read",
	}
	raw, _ := json.Marshal(assessment)

	provider := &mockProvider{
		name: "test",
		response: &bifrost.ChatResponse{
			Message: bifrost.Message{
				Role:    "assistant",
				Content: string(raw),
			},
		},
	}

	gw := newTestGateway(provider)
	g := NewGuardian(gw).WithModel("test/" + DefaultGuardianModel)

	messages := []bifrost.Message{
		{Role: "user", Content: "read my config file"},
	}
	request := GuardianApprovalRequest{
		ToolName:  "read_file",
		Arguments: "/etc/config.yaml",
	}

	result, err := g.ReviewApproval(context.Background(), messages, request)
	if err != nil {
		t.Fatalf("ReviewApproval() error = %v", err)
	}
	if result.Outcome != OutcomeAllow {
		t.Errorf("Outcome = %v, want allow", result.Outcome)
	}
	if result.RiskLevel != RiskLow {
		t.Errorf("RiskLevel = %v, want low", result.RiskLevel)
	}
}

func TestGuardian_ReviewApproval_Timeout(t *testing.T) {
	provider := &mockProvider{
		name:  "test",
		delay: 200 * time.Millisecond,
	}

	gw := newTestGateway(provider)
	g := NewGuardian(gw).WithModel("test/" + DefaultGuardianModel).WithTimeout(50 * time.Millisecond)

	messages := []bifrost.Message{
		{Role: "user", Content: "do something"},
	}
	request := GuardianApprovalRequest{ToolName: "exec"}

	result, err := g.ReviewApproval(context.Background(), messages, request)
	if err == nil {
		t.Fatal("ReviewApproval() expected timeout error")
	}
	if result.Outcome != OutcomeDeny {
		t.Errorf("expected fail-closed deny on timeout, got %v", result.Outcome)
	}
}

func TestGuardian_ReviewApproval_MalformedOutput(t *testing.T) {
	provider := &mockProvider{
		name: "test",
		response: &bifrost.ChatResponse{
			Message: bifrost.Message{
				Role:    "assistant",
				Content: "I think this is fine!",
			},
		},
	}

	gw := newTestGateway(provider)
	g := NewGuardian(gw).WithModel("test/" + DefaultGuardianModel)

	messages := []bifrost.Message{
		{Role: "user", Content: "delete everything"},
	}
	request := GuardianApprovalRequest{ToolName: "exec", Arguments: "rm -rf /"}

	result, err := g.ReviewApproval(context.Background(), messages, request)
	if err == nil {
		t.Fatal("ReviewApproval() expected error for malformed output")
	}
	if result.Outcome != OutcomeDeny {
		t.Errorf("expected fail-closed deny on malformed output, got %v", result.Outcome)
	}
}

func TestGuardian_ReviewApproval_ExecutionFailure(t *testing.T) {
	provider := &mockProvider{
		name: "test",
		err:  context.DeadlineExceeded,
	}

	gw := newTestGateway(provider)
	g := NewGuardian(gw).WithModel("test/" + DefaultGuardianModel)

	messages := []bifrost.Message{
		{Role: "user", Content: "test"},
	}
	request := GuardianApprovalRequest{ToolName: "test"}

	result, err := g.ReviewApproval(context.Background(), messages, request)
	if err == nil {
		t.Fatal("ReviewApproval() expected error on execution failure")
	}
	if result.Outcome != OutcomeDeny {
		t.Errorf("expected fail-closed deny on execution failure, got %v", result.Outcome)
	}
}
