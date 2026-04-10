package config

import (
	"os"
	"testing"
	"time"
)

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
