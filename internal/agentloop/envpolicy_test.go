package agentloop

import (
	"os"
	"strings"
	"testing"
)

func TestFilterEnv_All(t *testing.T) {
	result := FilterEnv(EnvPolicyAll)
	osEnv := os.Environ()
	if len(result) != len(osEnv) {
		t.Errorf("EnvPolicyAll: expected %d vars, got %d", len(osEnv), len(result))
	}
}

func TestFilterEnv_None(t *testing.T) {
	result := FilterEnv(EnvPolicyNone)
	if result != nil {
		t.Errorf("EnvPolicyNone: expected nil, got %d vars", len(result))
	}
}

func TestFilterEnv_Core(t *testing.T) {
	// Set a sensitive env var for testing
	os.Setenv("TEST_SECRET_VALUE", "hidden")
	os.Setenv("TEST_API_KEY", "hidden")
	os.Setenv("TEST_SAFE_VAR", "visible")
	defer func() {
		os.Unsetenv("TEST_SECRET_VALUE")
		os.Unsetenv("TEST_API_KEY")
		os.Unsetenv("TEST_SAFE_VAR")
	}()

	result := FilterEnv(EnvPolicyCore)

	for _, env := range result {
		parts := strings.SplitN(env, "=", 2)
		name := strings.ToUpper(parts[0])
		if strings.Contains(name, "SECRET") || strings.Contains(name, "KEY") ||
			strings.Contains(name, "TOKEN") || strings.Contains(name, "PASSWORD") ||
			strings.Contains(name, "CREDENTIAL") {
			t.Errorf("sensitive var %s should be filtered", parts[0])
		}
	}

	// Verify safe var is present
	found := false
	for _, env := range result {
		if strings.HasPrefix(env, "TEST_SAFE_VAR=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("TEST_SAFE_VAR should be present in core-filtered env")
	}
}

func TestIsSensitive(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"AWS_SECRET_ACCESS_KEY", true},
		{"API_KEY", true},
		{"AUTH_TOKEN", true},
		{"DB_PASSWORD", true},
		{"GOOGLE_CREDENTIAL", true},
		{"HOME", false},
		{"PATH", false},
		{"USER", false},
		{"SHELL", false},
		{"GOPATH", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSensitive(tt.name)
			if got != tt.expected {
				t.Errorf("isSensitive(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
