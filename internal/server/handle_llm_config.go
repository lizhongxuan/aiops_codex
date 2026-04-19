package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
)

// llmConfigResponse is the public shape of the LLM configuration.
// API keys are masked for security.
type llmConfigResponse struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	APIKeySet        bool   `json:"apiKeySet"`
	APIKeyMasked     string `json:"apiKeyMasked"`
	BaseURL          string `json:"baseURL"`
	FallbackProvider string `json:"fallbackProvider"`
	FallbackModel    string `json:"fallbackModel"`
	FallbackKeySet   bool   `json:"fallbackKeySet"`
	CompactModel     string `json:"compactModel"`
	BifrostActive    bool   `json:"bifrostActive"`
}

// llmConfigUpdateRequest is the shape of a PUT /api/v1/llm-config body.
type llmConfigUpdateRequest struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	APIKey           string `json:"apiKey"`
	BaseURL          string `json:"baseURL"`
	FallbackProvider string `json:"fallbackProvider"`
	FallbackModel    string `json:"fallbackModel"`
	FallbackAPIKey   string `json:"fallbackApiKey"`
	CompactModel     string `json:"compactModel"`
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func (a *App) handleLLMConfig(w http.ResponseWriter, r *http.Request, _ string) {
	switch r.Method {
	case http.MethodGet:
		a.handleLLMConfigGet(w, r)
	case http.MethodPut:
		a.handleLLMConfigUpdate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleLLMConfigGet(w http.ResponseWriter, _ *http.Request) {
	resp := llmConfigResponse{
		Provider:         a.cfg.LLMProvider,
		Model:            a.cfg.LLMModel,
		APIKeySet:        strings.TrimSpace(a.cfg.LLMAPIKey) != "",
		APIKeyMasked:     maskAPIKey(a.cfg.LLMAPIKey),
		BaseURL:          a.cfg.LLMBaseURL,
		FallbackProvider: a.cfg.LLMFallbackProvider,
		FallbackModel:    a.cfg.LLMFallbackModel,
		FallbackKeySet:   strings.TrimSpace(a.cfg.LLMFallbackAPIKey) != "",
		CompactModel:     a.cfg.LLMCompactModel,
		BifrostActive:    a.useBifrost(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleLLMConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req llmConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Update config fields (only non-empty values).
	if p := strings.TrimSpace(req.Provider); p != "" {
		a.cfg.LLMProvider = p
	}
	if m := strings.TrimSpace(req.Model); m != "" {
		a.cfg.LLMModel = m
	}
	if k := strings.TrimSpace(req.APIKey); k != "" {
		a.cfg.LLMAPIKey = k
		a.cfg.LLMAPIKeys = []string{k}
	}
	if u := strings.TrimSpace(req.BaseURL); u != "" {
		a.cfg.LLMBaseURL = u
	} else if req.BaseURL == "" && r.ContentLength > 0 {
		// Allow explicitly clearing base URL by sending empty string.
		a.cfg.LLMBaseURL = ""
	}
	if fp := strings.TrimSpace(req.FallbackProvider); fp != "" {
		a.cfg.LLMFallbackProvider = fp
	}
	if fm := strings.TrimSpace(req.FallbackModel); fm != "" {
		a.cfg.LLMFallbackModel = fm
	}
	if fk := strings.TrimSpace(req.FallbackAPIKey); fk != "" {
		a.cfg.LLMFallbackAPIKey = fk
	}
	if cm := strings.TrimSpace(req.CompactModel); cm != "" {
		a.cfg.LLMCompactModel = cm
	}

	// Ensure UseBifrost is on.
	a.cfg.UseBifrost = true

	// Persist the LLM config to disk.
	if err := a.saveLLMConfig(); err != nil {
		log.Printf("[llm-config] failed to persist config: %v", err)
	}

	// Reinitialize the Bifrost runtime with the new config.
	a.bifrostMu.Lock()
	// Clear existing sessions so they pick up the new gateway.
	a.bifrostSessions = make(map[string]*agentloop.Session)
	a.workspaceRuntimes = make(map[string]*agentloop.WorkspaceRuntime)
	a.agentLoop = nil
	a.bifrostGateway = nil
	a.bifrostMu.Unlock()

	if err := a.initBifrostRuntime(); err != nil {
		log.Printf("[llm-config] failed to reinitialize bifrost: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to initialize LLM runtime: " + err.Error(),
		})
		return
	}

	active := a.useBifrost()
	if !active {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      false,
			"message": "Config saved but Bifrost runtime could not start. Check provider and API key.",
		})
		return
	}

	log.Printf("[llm-config] bifrost runtime reinitialized: provider=%s model=%s", a.cfg.LLMProvider, a.cfg.LLMModel)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "LLM configuration updated and runtime restarted.",
	})
}

// ---------- LLM config persistence ----------

type llmConfigFile struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	APIKey           string `json:"apiKey"`
	BaseURL          string `json:"baseURL"`
	FallbackProvider string `json:"fallbackProvider"`
	FallbackModel    string `json:"fallbackModel"`
	FallbackAPIKey   string `json:"fallbackApiKey"`
	CompactModel     string `json:"compactModel"`
}

func (a *App) llmConfigPath() string {
	dir := filepath.Dir(a.cfg.StatePath)
	return filepath.Join(dir, "llm-config.json")
}

func (a *App) saveLLMConfig() error {
	cfg := llmConfigFile{
		Provider:         a.cfg.LLMProvider,
		Model:            a.cfg.LLMModel,
		APIKey:           a.cfg.LLMAPIKey,
		BaseURL:          a.cfg.LLMBaseURL,
		FallbackProvider: a.cfg.LLMFallbackProvider,
		FallbackModel:    a.cfg.LLMFallbackModel,
		FallbackAPIKey:   a.cfg.LLMFallbackAPIKey,
		CompactModel:     a.cfg.LLMCompactModel,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := a.llmConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (a *App) loadLLMConfig() {
	path := a.llmConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return // No saved config, use env defaults.
	}
	var cfg llmConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[llm-config] failed to parse saved config: %v", err)
		return
	}
	// Only override if the saved value is non-empty and the env var wasn't explicitly set.
	if cfg.Provider != "" {
		a.cfg.LLMProvider = cfg.Provider
	}
	if cfg.Model != "" {
		a.cfg.LLMModel = cfg.Model
	}
	if cfg.APIKey != "" {
		a.cfg.LLMAPIKey = cfg.APIKey
		a.cfg.LLMAPIKeys = []string{cfg.APIKey}
	}
	if cfg.BaseURL != "" {
		a.cfg.LLMBaseURL = cfg.BaseURL
	}
	if cfg.FallbackProvider != "" {
		a.cfg.LLMFallbackProvider = cfg.FallbackProvider
	}
	if cfg.FallbackModel != "" {
		a.cfg.LLMFallbackModel = cfg.FallbackModel
	}
	if cfg.FallbackAPIKey != "" {
		a.cfg.LLMFallbackAPIKey = cfg.FallbackAPIKey
	}
	if cfg.CompactModel != "" {
		a.cfg.LLMCompactModel = cfg.CompactModel
	}
	a.cfg.UseBifrost = true
	log.Printf("[llm-config] loaded saved config: provider=%s model=%s keySet=%v", cfg.Provider, cfg.Model, cfg.APIKey != "")
}
