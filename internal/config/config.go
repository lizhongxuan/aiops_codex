package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                 string
	GRPCAddr                 string
	GRPCAdvertiseAddr        string
	GRPCTLSCertFile          string
	GRPCTLSKeyFile           string
	GRPCTLSClientCAFile      string
	CodexPath                string
	CodexHome                string
	StatePath                string
	AuditLogPath             string
	DefaultWorkspace         string
	SessionCookieName        string
	SessionSecret            string
	SessionCookieTTL         time.Duration
	HostAgentBootstrapToken  string
	HostAgentBootstrapTokens []string
	AllowedAgentHostIDs      []string
	AllowedAgentCIDRs        []string
	HostAgentSecurityProfile string
	AgentHeartbeatTimeout    time.Duration
	FrontendRedirectURL      string
	OAuthClientID            string
	OAuthClientSecret        string
	OAuthAuthURL             string
	OAuthTokenURL            string
	OAuthRedirectURL         string
	OAuthScopes              string
	OAuthUserInfoURL         string
	OAuthAccountID           string
	OAuthPlanType            string
}

func Load() Config {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	workspace := filepath.Join(home, ".aiops_codex")
	statePath := filepath.Join(".data", "ai-server-state.json")
	auditLogPath := filepath.Join(".data", "ai-audit.log")
	if err == nil {
		statePath = filepath.Join(cwd, ".data", "ai-server-state.json")
		auditLogPath = filepath.Join(cwd, ".data", "ai-audit.log")
	}

	return Config{
		HTTPAddr:                 env("AIOPS_HTTP_ADDR", "127.0.0.1:8080"),
		GRPCAddr:                 env("AIOPS_GRPC_ADDR", "127.0.0.1:19090"),
		GRPCAdvertiseAddr:        env("AIOPS_GRPC_ADVERTISE_ADDR", ""),
		GRPCTLSCertFile:          env("AIOPS_GRPC_TLS_CERT_FILE", ""),
		GRPCTLSKeyFile:           env("AIOPS_GRPC_TLS_KEY_FILE", ""),
		GRPCTLSClientCAFile:      env("AIOPS_GRPC_TLS_CLIENT_CA_FILE", ""),
		CodexPath:                env("CODEX_APP_SERVER_PATH", "codex"),
		CodexHome:                env("CODEX_HOME", filepath.Join(home, ".codex")),
		StatePath:                env("APP_STATE_PATH", statePath),
		AuditLogPath:             env("APP_AUDIT_LOG_PATH", auditLogPath),
		DefaultWorkspace:         env("DEFAULT_WORKSPACE", workspace),
		SessionCookieName:        env("APP_SESSION_COOKIE_NAME", "aiops_codex_session"),
		SessionSecret:            env("APP_SESSION_SECRET", "dev-insecure-session-secret"),
		SessionCookieTTL:         envDuration("APP_SESSION_TTL", 30*24*time.Hour),
		HostAgentBootstrapToken:  env("HOST_AGENT_BOOTSTRAP_TOKEN", "change-me"),
		HostAgentBootstrapTokens: bootstrapTokens(),
		AllowedAgentHostIDs:      csvEnv("HOST_AGENT_ALLOWED_HOST_IDS"),
		AllowedAgentCIDRs:        csvEnv("HOST_AGENT_ALLOWED_CIDRS"),
		HostAgentSecurityProfile: env("HOST_AGENT_SECURITY_PROFILE", "development"),
		AgentHeartbeatTimeout:    envDuration("AGENT_HEARTBEAT_TIMEOUT", 45*time.Second),
		FrontendRedirectURL:      env("FRONTEND_REDIRECT_URL", "http://127.0.0.1:5173/"),
		OAuthClientID:            env("GPT_OAUTH_CLIENT_ID", ""),
		OAuthClientSecret:        env("GPT_OAUTH_CLIENT_SECRET", ""),
		OAuthAuthURL:             env("GPT_OAUTH_AUTH_URL", ""),
		OAuthTokenURL:            env("GPT_OAUTH_TOKEN_URL", ""),
		OAuthRedirectURL:         env("GPT_OAUTH_REDIRECT_URL", ""),
		OAuthScopes:              env("GPT_OAUTH_SCOPES", "openid profile email"),
		OAuthUserInfoURL:         env("GPT_OAUTH_USERINFO_URL", ""),
		OAuthAccountID:           env("GPT_OAUTH_ACCOUNT_ID", ""),
		OAuthPlanType:            env("GPT_OAUTH_PLAN_TYPE", ""),
	}
}

func (c Config) ValidAgentBootstrapToken(token string) bool {
	value := strings.TrimSpace(token)
	if value == "" {
		return false
	}
	for _, candidate := range c.effectiveBootstrapTokens() {
		if value == strings.TrimSpace(candidate) {
			return true
		}
	}
	return false
}

func (c Config) AgentHostAllowed(hostID string) bool {
	if len(c.AllowedAgentHostIDs) == 0 {
		return true
	}
	value := strings.TrimSpace(hostID)
	if value == "" {
		return false
	}
	return slices.Contains(c.AllowedAgentHostIDs, value)
}

func (c Config) OAuthConfigured() bool {
	return c.OAuthClientID != "" &&
		c.OAuthClientSecret != "" &&
		c.OAuthAuthURL != "" &&
		c.OAuthTokenURL != "" &&
		c.OAuthRedirectURL != "" &&
		c.OAuthAccountID != ""
}

func (c Config) OAuthScopeList() []string {
	raw := strings.Fields(c.OAuthScopes)
	if len(raw) == 0 {
		return []string{"openid", "profile", "email"}
	}
	return raw
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		duration, err := time.ParseDuration(value)
		if err == nil {
			return duration
		}
	}
	return fallback
}

func csvEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func bootstrapTokens() []string {
	tokens := csvEnv("HOST_AGENT_BOOTSTRAP_TOKENS")
	if single, ok := os.LookupEnv("HOST_AGENT_BOOTSTRAP_TOKEN"); ok {
		value := strings.TrimSpace(single)
		if value != "" && !slices.Contains(tokens, value) {
			tokens = append(tokens, value)
		}
		return tokens
	}
	if len(tokens) == 0 {
		tokens = append(tokens, "change-me")
	}
	return tokens
}

func (c Config) effectiveBootstrapTokens() []string {
	if len(c.HostAgentBootstrapTokens) > 0 {
		return c.HostAgentBootstrapTokens
	}
	return []string{c.HostAgentBootstrapToken}
}
