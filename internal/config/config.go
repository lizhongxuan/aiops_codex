package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                  string
	GRPCAddr                  string
	GRPCAdvertiseAddr         string
	GRPCTLSCertFile           string
	GRPCTLSKeyFile            string
	GRPCTLSClientCAFile       string
	UseBifrost                bool
	LLMProvider               string
	LLMModel                  string
	LLMAPIKey                 string
	LLMBaseURL                string
	LLMAPIKeys                []string
	LLMFallbackProvider       string
	LLMFallbackModel          string
	LLMFallbackAPIKey         string
	LLMCompactModel           string
	StatePath                 string
	AuditLogPath              string
	DefaultWorkspace          string
	SessionCookieName         string
	SessionSecret             string
	SessionCookieTTL          time.Duration
	HostAgentBootstrapToken   string
	HostAgentBootstrapTokens  []string
	AllowedAgentHostIDs       []string
	AllowedAgentCIDRs         []string
	HostAgentSecurityProfile  string
	AgentHeartbeatTimeout     time.Duration
	FrontendRedirectURL       string
	OAuthClientID             string
	OAuthClientSecret         string
	OAuthAuthURL              string
	OAuthTokenURL             string
	OAuthRedirectURL          string
	OAuthScopes               string
	OAuthUserInfoURL          string
	OAuthAccountID            string
	OAuthPlanType             string
	CorootBaseURL             string
	CorootToken               string
	CorootTimeout             time.Duration
	CorootPriority            string
	CorootFallbackEnabled     bool
	CorootHealthCheckInterval time.Duration
	CorootRCAEnabled          bool
	WorkspaceReActLoopEnabled bool

	// MCP configuration
	MCPConfigPaths []string // Paths to mcp.json files (user + workspace)

	// Skills configuration
	SkillRoots []string // Directories to scan for SKILL.md files

	// Session persistence
	SessionStorePath string // Directory for persisted agent sessions

	// ExecPolicy
	ExecPolicyPath string // Path to exec policy file (JSON/YAML)

	// Subagent
	SubagentEnabled  bool
	SubagentMaxDepth int
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
		HTTPAddr:                  env("AIOPS_HTTP_ADDR", "127.0.0.1:8080"),
		GRPCAddr:                  env("AIOPS_GRPC_ADDR", "127.0.0.1:19090"),
		GRPCAdvertiseAddr:         env("AIOPS_GRPC_ADVERTISE_ADDR", ""),
		GRPCTLSCertFile:           env("AIOPS_GRPC_TLS_CERT_FILE", ""),
		GRPCTLSKeyFile:            env("AIOPS_GRPC_TLS_KEY_FILE", ""),
		GRPCTLSClientCAFile:       env("AIOPS_GRPC_TLS_CLIENT_CA_FILE", ""),
		UseBifrost:                envBool("USE_BIFROST", true),
		LLMProvider:               env("LLM_PROVIDER", "openai"),
		LLMModel:                  env("LLM_MODEL", "gpt-4o-mini"),
		LLMAPIKey:                 firstEnv("LLM_API_KEY", "CODEX_API_KEY"),
		LLMBaseURL:                env("LLM_BASE_URL", ""),
		LLMAPIKeys:                llmAPIKeys(),
		LLMFallbackProvider:       env("LLM_FALLBACK_PROVIDER", ""),
		LLMFallbackModel:          env("LLM_FALLBACK_MODEL", ""),
		LLMFallbackAPIKey:         env("LLM_FALLBACK_API_KEY", ""),
		LLMCompactModel:           env("LLM_COMPACT_MODEL", "gpt-4o-mini"),
		StatePath:                 env("APP_STATE_PATH", statePath),
		AuditLogPath:              env("APP_AUDIT_LOG_PATH", auditLogPath),
		DefaultWorkspace:          env("DEFAULT_WORKSPACE", workspace),
		SessionCookieName:         env("APP_SESSION_COOKIE_NAME", "aiops_codex_session"),
		SessionSecret:             env("APP_SESSION_SECRET", "dev-insecure-session-secret"),
		SessionCookieTTL:          envDuration("APP_SESSION_TTL", 30*24*time.Hour),
		HostAgentBootstrapToken:   env("HOST_AGENT_BOOTSTRAP_TOKEN", "change-me"),
		HostAgentBootstrapTokens:  bootstrapTokens(),
		AllowedAgentHostIDs:       csvEnv("HOST_AGENT_ALLOWED_HOST_IDS"),
		AllowedAgentCIDRs:         csvEnv("HOST_AGENT_ALLOWED_CIDRS"),
		HostAgentSecurityProfile:  env("HOST_AGENT_SECURITY_PROFILE", "development"),
		AgentHeartbeatTimeout:     envDuration("AGENT_HEARTBEAT_TIMEOUT", 45*time.Second),
		FrontendRedirectURL:       env("FRONTEND_REDIRECT_URL", "http://127.0.0.1:5173/"),
		OAuthClientID:             env("GPT_OAUTH_CLIENT_ID", ""),
		OAuthClientSecret:         env("GPT_OAUTH_CLIENT_SECRET", ""),
		OAuthAuthURL:              env("GPT_OAUTH_AUTH_URL", ""),
		OAuthTokenURL:             env("GPT_OAUTH_TOKEN_URL", ""),
		OAuthRedirectURL:          env("GPT_OAUTH_REDIRECT_URL", ""),
		OAuthScopes:               env("GPT_OAUTH_SCOPES", "openid profile email"),
		OAuthUserInfoURL:          env("GPT_OAUTH_USERINFO_URL", ""),
		OAuthAccountID:            env("GPT_OAUTH_ACCOUNT_ID", ""),
		OAuthPlanType:             env("GPT_OAUTH_PLAN_TYPE", ""),
		CorootBaseURL:             env("COROOT_BASE_URL", ""),
		CorootToken:               env("COROOT_TOKEN", ""),
		CorootTimeout:             envDuration("COROOT_TIMEOUT", 30*time.Second),
		CorootPriority:            env("COROOT_PRIORITY", "coroot_first"),
		CorootFallbackEnabled:     envBool("COROOT_FALLBACK_ENABLED", true),
		CorootHealthCheckInterval: envDuration("COROOT_HEALTH_CHECK_INTERVAL", 30*time.Second),
		CorootRCAEnabled:          envBool("COROOT_RCA_ENABLED", false),
		WorkspaceReActLoopEnabled: envBool("WORKSPACE_REACT_LOOP_ENABLED", true),

		// MCP
		MCPConfigPaths: mcpConfigPaths(home, cwd),

		// Skills
		SkillRoots: skillRoots(home, cwd),

		// Session persistence
		SessionStorePath: env("SESSION_STORE_PATH", filepath.Join(workspace, "agent-sessions")),

		// ExecPolicy
		ExecPolicyPath: env("EXEC_POLICY_PATH", filepath.Join(workspace, "exec-policy.json")),

		// Subagent
		SubagentEnabled:  envBool("SUBAGENT_ENABLED", true),
		SubagentMaxDepth: envInt("SUBAGENT_MAX_DEPTH", 5),
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

func (c Config) CorootConfigured() bool {
	return c.CorootBaseURL != ""
}

// CorootFullConfigured returns true when all configuration items required
// for the complete Coroot integration are present (base URL + token).
func (c Config) CorootFullConfigured() bool {
	return c.CorootBaseURL != "" && c.CorootToken != ""
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
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

func llmAPIKeys() []string {
	keys := csvEnv("LLM_API_KEYS")
	primary := firstEnv("LLM_API_KEY", "CODEX_API_KEY")
	if primary == "" {
		return keys
	}

	out := []string{primary}
	for _, key := range keys {
		value := strings.TrimSpace(key)
		if value == "" || value == primary {
			continue
		}
		out = append(out, value)
	}
	return out
}

func (c Config) effectiveBootstrapTokens() []string {
	if len(c.HostAgentBootstrapTokens) > 0 {
		return c.HostAgentBootstrapTokens
	}
	return []string{c.HostAgentBootstrapToken}
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err == nil {
		return n
	}
	return fallback
}

func mcpConfigPaths(home, cwd string) []string {
	var paths []string
	// User-level config.
	if home != "" {
		userPath := filepath.Join(home, ".kiro", "settings", "mcp.json")
		paths = append(paths, userPath)
	}
	// Workspace-level config.
	if cwd != "" {
		wsPath := filepath.Join(cwd, ".kiro", "settings", "mcp.json")
		paths = append(paths, wsPath)
	}
	// Custom paths from env.
	if extra := env("MCP_CONFIG_PATHS", ""); extra != "" {
		for _, p := range strings.Split(extra, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

func skillRoots(home, cwd string) []string {
	var roots []string
	// User-level skills.
	if home != "" {
		roots = append(roots, filepath.Join(home, ".aiops_codex", "skills"))
	}
	// Workspace-level skills.
	if cwd != "" {
		roots = append(roots, filepath.Join(cwd, ".aiops_codex", "skills"))
	}
	// Custom roots from env.
	if extra := env("SKILL_ROOTS", ""); extra != "" {
		for _, p := range strings.Split(extra, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				roots = append(roots, p)
			}
		}
	}
	return roots
}
