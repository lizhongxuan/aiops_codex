package agentloop

import (
	"testing"
)

func TestDiff_AllFieldsChanged(t *testing.T) {
	prev := EnvironmentContext{
		Cwd:            "/old",
		Shell:          "bash",
		OS:             "linux",
		Hostname:       "host1",
		Username:       "alice",
		CurrentDate:    "2025-01-01",
		Timezone:       "UTC",
		ActiveHost:     "h1",
		Subagents:      "none",
		NetworkAllowed: []string{"a.com"},
		NetworkDenied:  []string{"b.com"},
		CustomContext:   map[string]string{"k": "v1"},
	}
	curr := EnvironmentContext{
		Cwd:            "/new",
		Shell:          "zsh",
		OS:             "darwin",
		Hostname:       "host2",
		Username:       "bob",
		CurrentDate:    "2025-01-02",
		Timezone:       "PST",
		ActiveHost:     "h2",
		Subagents:      "sub1",
		NetworkAllowed: []string{"c.com"},
		NetworkDenied:  []string{"d.com"},
		CustomContext:   map[string]string{"k": "v2"},
	}

	diff := curr.Diff(prev)

	if diff.Cwd != "/new" {
		t.Errorf("Cwd: got %q, want /new", diff.Cwd)
	}
	if diff.Shell != "zsh" {
		t.Errorf("Shell: got %q, want zsh", diff.Shell)
	}
	if diff.OS != "darwin" {
		t.Errorf("OS: got %q, want darwin", diff.OS)
	}
	if diff.Hostname != "host2" {
		t.Errorf("Hostname: got %q, want host2", diff.Hostname)
	}
	if diff.Username != "bob" {
		t.Errorf("Username: got %q, want bob", diff.Username)
	}
	if diff.CurrentDate != "2025-01-02" {
		t.Errorf("CurrentDate: got %q", diff.CurrentDate)
	}
	if diff.Timezone != "PST" {
		t.Errorf("Timezone: got %q", diff.Timezone)
	}
	if diff.ActiveHost != "h2" {
		t.Errorf("ActiveHost: got %q", diff.ActiveHost)
	}
	if diff.Subagents != "sub1" {
		t.Errorf("Subagents: got %q", diff.Subagents)
	}
	if len(diff.NetworkAllowed) != 1 || diff.NetworkAllowed[0] != "c.com" {
		t.Errorf("NetworkAllowed: got %v", diff.NetworkAllowed)
	}
	if len(diff.NetworkDenied) != 1 || diff.NetworkDenied[0] != "d.com" {
		t.Errorf("NetworkDenied: got %v", diff.NetworkDenied)
	}
	if diff.CustomContext["k"] != "v2" {
		t.Errorf("CustomContext: got %v", diff.CustomContext)
	}
}

func TestDiff_NoChanges(t *testing.T) {
	ctx := EnvironmentContext{
		Cwd:      "/home",
		Shell:    "bash",
		OS:       "linux",
		Hostname: "host",
		Username: "user",
	}

	diff := ctx.Diff(ctx)

	if !diff.IsEmpty() {
		t.Errorf("expected empty diff when contexts are identical, got %+v", diff)
	}
}

func TestDiff_OnlyCwdChanged(t *testing.T) {
	prev := EnvironmentContext{Cwd: "/old", Shell: "bash", OS: "linux"}
	curr := EnvironmentContext{Cwd: "/new", Shell: "bash", OS: "linux"}

	diff := curr.Diff(prev)

	if diff.Cwd != "/new" {
		t.Errorf("Cwd: got %q, want /new", diff.Cwd)
	}
	if diff.Shell != "" {
		t.Errorf("Shell should be empty (unchanged), got %q", diff.Shell)
	}
	if diff.OS != "" {
		t.Errorf("OS should be empty (unchanged), got %q", diff.OS)
	}
}

func TestDiff_NetworkSliceChanges(t *testing.T) {
	prev := EnvironmentContext{NetworkAllowed: []string{"a.com", "b.com"}}
	curr := EnvironmentContext{NetworkAllowed: []string{"a.com", "c.com"}}

	diff := curr.Diff(prev)

	if len(diff.NetworkAllowed) != 2 {
		t.Errorf("expected NetworkAllowed in diff, got %v", diff.NetworkAllowed)
	}
}

func TestDiff_CustomContextChanges(t *testing.T) {
	prev := EnvironmentContext{CustomContext: map[string]string{"a": "1"}}
	curr := EnvironmentContext{CustomContext: map[string]string{"a": "1", "b": "2"}}

	diff := curr.Diff(prev)

	if diff.CustomContext == nil {
		t.Fatal("expected CustomContext in diff")
	}
	if diff.CustomContext["b"] != "2" {
		t.Errorf("expected new key in diff, got %v", diff.CustomContext)
	}
}

func TestIsEmpty_WithTimezone(t *testing.T) {
	ec := EnvironmentContext{Timezone: "UTC"}
	if ec.IsEmpty() {
		t.Error("context with Timezone set should not be empty")
	}
}

func TestIsEmpty_WithNetworkAllowed(t *testing.T) {
	ec := EnvironmentContext{NetworkAllowed: []string{"a.com"}}
	if ec.IsEmpty() {
		t.Error("context with NetworkAllowed set should not be empty")
	}
}

func TestIsEmpty_WithCustomContext(t *testing.T) {
	ec := EnvironmentContext{CustomContext: map[string]string{"k": "v"}}
	if ec.IsEmpty() {
		t.Error("context with CustomContext set should not be empty")
	}
}

func TestIsEmpty_TrulyEmpty(t *testing.T) {
	ec := EnvironmentContext{}
	if !ec.IsEmpty() {
		t.Error("zero-value context should be empty")
	}
}

func TestSlicesEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"equal", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different length", []string{"a"}, []string{"a", "b"}, false},
		{"different content", []string{"a", "b"}, []string{"a", "c"}, false},
		{"nil vs empty", nil, []string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slicesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("slicesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMapsEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"equal", map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		{"different value", map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		{"different keys", map[string]string{"a": "1"}, map[string]string{"b": "1"}, false},
		{"different length", map[string]string{"a": "1"}, map[string]string{"a": "1", "b": "2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("mapsEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestInjectEnvironmentContext_FirstTurnFullContext(t *testing.T) {
	s := NewSession("test", SessionSpec{Model: "test"})
	env := EnvironmentContext{
		Cwd:   "/workspace",
		Shell: "bash",
		OS:    "linux",
	}

	InjectEnvironmentContext(s, env)

	msgs := s.ContextManager().Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	content, ok := msgs[0].Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if content == "" {
		t.Fatal("expected non-empty environment context XML")
	}
}

func TestInjectEnvironmentContext_SubsequentTurnOnlyDiff(t *testing.T) {
	s := NewSession("test", SessionSpec{Model: "test"})
	env1 := EnvironmentContext{Cwd: "/old", Shell: "bash", OS: "linux"}
	env2 := EnvironmentContext{Cwd: "/new", Shell: "bash", OS: "linux"}

	InjectEnvironmentContext(s, env1)
	InjectEnvironmentContext(s, env2)

	msgs := s.ContextManager().Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Second message should only contain the cwd diff.
	content, ok := msgs[1].Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if content == "" {
		t.Fatal("expected non-empty diff XML")
	}
}

func TestInjectEnvironmentContext_NoDiffSkipsInjection(t *testing.T) {
	s := NewSession("test", SessionSpec{Model: "test"})
	env := EnvironmentContext{Cwd: "/same", Shell: "bash"}

	InjectEnvironmentContext(s, env)
	InjectEnvironmentContext(s, env)

	msgs := s.ContextManager().Messages()
	// Second injection should be skipped (diff is empty except CurrentDate which
	// we set identically here).
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (second injection skipped), got %d", len(msgs))
	}
}
