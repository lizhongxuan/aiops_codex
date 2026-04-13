package config

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"USE_BIFROST",
		"LLM_PROVIDER",
		"LLM_MODEL",
		"LLM_API_KEY",
		"LLM_API_KEYS",
		"LLM_BASE_URL",
		"LLM_FALLBACK_PROVIDER",
		"LLM_FALLBACK_MODEL",
		"LLM_FALLBACK_API_KEY",
		"LLM_COMPACT_MODEL",
		"CODEX_API_KEY",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_BifrostDefaults(t *testing.T) {
	clearLLMEnv(t)

	cfg := Load()

	if !cfg.UseBifrost {
		t.Fatal("expected UseBifrost default true")
	}
	if cfg.LLMProvider != "openai" {
		t.Fatalf("expected default LLMProvider openai, got %q", cfg.LLMProvider)
	}
	if cfg.LLMModel != "gpt-4o-mini" {
		t.Fatalf("expected default LLMModel gpt-4o-mini, got %q", cfg.LLMModel)
	}
	if cfg.LLMCompactModel != "gpt-4o-mini" {
		t.Fatalf("expected default LLMCompactModel gpt-4o-mini, got %q", cfg.LLMCompactModel)
	}
	if cfg.LLMAPIKey != "" {
		t.Fatalf("expected empty default LLMAPIKey, got %q", cfg.LLMAPIKey)
	}
}

func TestLoad_BifrostConfigFromEnv(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("USE_BIFROST", "false")
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("LLM_MODEL", "claude-sonnet-4-20250514")
	t.Setenv("LLM_API_KEY", "primary-key")
	t.Setenv("LLM_API_KEYS", "backup-1,backup-2")
	t.Setenv("LLM_BASE_URL", "https://example.test/v1")
	t.Setenv("LLM_FALLBACK_PROVIDER", "openai")
	t.Setenv("LLM_FALLBACK_MODEL", "gpt-4.1-mini")
	t.Setenv("LLM_FALLBACK_API_KEY", "fallback-key")
	t.Setenv("LLM_COMPACT_MODEL", "claude-3-5-haiku")

	cfg := Load()

	if cfg.UseBifrost {
		t.Fatal("expected UseBifrost=false from env")
	}
	if cfg.LLMProvider != "anthropic" || cfg.LLMModel != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected provider/model: %s %s", cfg.LLMProvider, cfg.LLMModel)
	}
	if cfg.LLMAPIKey != "primary-key" {
		t.Fatalf("expected primary key, got %q", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://example.test/v1" {
		t.Fatalf("unexpected LLMBaseURL %q", cfg.LLMBaseURL)
	}
	if cfg.LLMFallbackProvider != "openai" || cfg.LLMFallbackModel != "gpt-4.1-mini" || cfg.LLMFallbackAPIKey != "fallback-key" {
		t.Fatalf("unexpected fallback config: %#v", cfg)
	}
	if cfg.LLMCompactModel != "claude-3-5-haiku" {
		t.Fatalf("unexpected compact model %q", cfg.LLMCompactModel)
	}
	wantKeys := []string{"primary-key", "backup-1", "backup-2"}
	if !reflect.DeepEqual(cfg.LLMAPIKeys, wantKeys) {
		t.Fatalf("LLMAPIKeys = %#v, want %#v", cfg.LLMAPIKeys, wantKeys)
	}
}

func TestLoad_LLMAPIKeyFallsBackToCodexAPIKey(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("CODEX_API_KEY", "legacy-codex-key")

	cfg := Load()

	if cfg.LLMAPIKey != "legacy-codex-key" {
		t.Fatalf("expected LLMAPIKey to fall back to CODEX_API_KEY, got %q", cfg.LLMAPIKey)
	}
	if !reflect.DeepEqual(cfg.LLMAPIKeys, []string{"legacy-codex-key"}) {
		t.Fatalf("expected primary key promoted into LLMAPIKeys, got %#v", cfg.LLMAPIKeys)
	}
}

func TestLoad_CorootRoutingDefaults(t *testing.T) {
	// Clear any env vars that might interfere
	os.Unsetenv("COROOT_PRIORITY")
	os.Unsetenv("COROOT_FALLBACK_ENABLED")
	os.Unsetenv("COROOT_HEALTH_CHECK_INTERVAL")

	cfg := Load()

	if cfg.CorootPriority != "coroot_first" {
		t.Errorf("CorootPriority = %q, want %q", cfg.CorootPriority, "coroot_first")
	}
	if cfg.CorootFallbackEnabled != true {
		t.Errorf("CorootFallbackEnabled = %v, want true", cfg.CorootFallbackEnabled)
	}
	if cfg.CorootHealthCheckInterval != 30*time.Second {
		t.Errorf("CorootHealthCheckInterval = %v, want %v", cfg.CorootHealthCheckInterval, 30*time.Second)
	}
}

func TestLoad_CorootRoutingFromEnv(t *testing.T) {
	t.Setenv("COROOT_PRIORITY", "local_first")
	t.Setenv("COROOT_FALLBACK_ENABLED", "false")
	t.Setenv("COROOT_HEALTH_CHECK_INTERVAL", "1m")

	cfg := Load()

	if cfg.CorootPriority != "local_first" {
		t.Errorf("CorootPriority = %q, want %q", cfg.CorootPriority, "local_first")
	}
	if cfg.CorootFallbackEnabled != false {
		t.Errorf("CorootFallbackEnabled = %v, want false", cfg.CorootFallbackEnabled)
	}
	if cfg.CorootHealthCheckInterval != time.Minute {
		t.Errorf("CorootHealthCheckInterval = %v, want %v", cfg.CorootHealthCheckInterval, time.Minute)
	}
}

func TestLoad_CorootRCAEnabledDefault(t *testing.T) {
	os.Unsetenv("COROOT_RCA_ENABLED")

	cfg := Load()

	if cfg.CorootRCAEnabled != false {
		t.Errorf("CorootRCAEnabled = %v, want false", cfg.CorootRCAEnabled)
	}
}

func TestLoad_CorootRCAEnabledFromEnv(t *testing.T) {
	t.Setenv("COROOT_RCA_ENABLED", "true")

	cfg := Load()

	if cfg.CorootRCAEnabled != true {
		t.Errorf("CorootRCAEnabled = %v, want true", cfg.CorootRCAEnabled)
	}
}

func TestCorootFullConfigured(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		token   string
		want    bool
	}{
		{"both set", "http://coroot:8080", "secret-token", true},
		{"only base URL", "http://coroot:8080", "", false},
		{"only token", "", "secret-token", false},
		{"neither set", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				CorootBaseURL: tt.baseURL,
				CorootToken:   tt.token,
			}
			if got := cfg.CorootFullConfigured(); got != tt.want {
				t.Errorf("CorootFullConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}
