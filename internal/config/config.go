package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                string
	GRPCAddr                string
	CodexPath               string
	CodexHome               string
	StatePath               string
	DefaultWorkspace        string
	SessionCookieName       string
	SessionSecret           string
	SessionCookieTTL        time.Duration
	HostAgentBootstrapToken string
	AgentHeartbeatTimeout   time.Duration
	FrontendRedirectURL     string
	OAuthClientID           string
	OAuthClientSecret       string
	OAuthAuthURL            string
	OAuthTokenURL           string
	OAuthRedirectURL        string
	OAuthScopes             string
	OAuthUserInfoURL        string
	OAuthAccountID          string
	OAuthPlanType           string
}

func Load() Config {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	workspace := filepath.Join(home, ".aiops_codex")
	statePath := filepath.Join(".data", "ai-server-state.json")
	if err == nil {
		statePath = filepath.Join(cwd, ".data", "ai-server-state.json")
	}

	return Config{
		HTTPAddr:                env("AIOPS_HTTP_ADDR", "127.0.0.1:8080"),
		GRPCAddr:                env("AIOPS_GRPC_ADDR", "127.0.0.1:19090"),
		CodexPath:               env("CODEX_APP_SERVER_PATH", "codex"),
		CodexHome:               env("CODEX_HOME", filepath.Join(home, ".codex")),
		StatePath:               env("APP_STATE_PATH", statePath),
		DefaultWorkspace:        env("DEFAULT_WORKSPACE", workspace),
		SessionCookieName:       env("APP_SESSION_COOKIE_NAME", "aiops_codex_session"),
		SessionSecret:           env("APP_SESSION_SECRET", "dev-insecure-session-secret"),
		SessionCookieTTL:        envDuration("APP_SESSION_TTL", 30*24*time.Hour),
		HostAgentBootstrapToken: env("HOST_AGENT_BOOTSTRAP_TOKEN", "change-me"),
		AgentHeartbeatTimeout:   envDuration("AGENT_HEARTBEAT_TIMEOUT", 45*time.Second),
		FrontendRedirectURL:     env("FRONTEND_REDIRECT_URL", "http://127.0.0.1:5173/"),
		OAuthClientID:           env("GPT_OAUTH_CLIENT_ID", ""),
		OAuthClientSecret:       env("GPT_OAUTH_CLIENT_SECRET", ""),
		OAuthAuthURL:            env("GPT_OAUTH_AUTH_URL", ""),
		OAuthTokenURL:           env("GPT_OAUTH_TOKEN_URL", ""),
		OAuthRedirectURL:        env("GPT_OAUTH_REDIRECT_URL", ""),
		OAuthScopes:             env("GPT_OAUTH_SCOPES", "openid profile email"),
		OAuthUserInfoURL:        env("GPT_OAUTH_USERINFO_URL", ""),
		OAuthAccountID:          env("GPT_OAUTH_ACCOUNT_ID", ""),
		OAuthPlanType:           env("GPT_OAUTH_PLAN_TYPE", ""),
	}
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
