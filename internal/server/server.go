package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/codex"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	cfg         config.Config
	store       *store.Store
	codex       *codex.Client
	upgrader    websocket.Upgrader
	wsMu        sync.Mutex
	wsClients   map[string]map[*websocket.Conn]struct{}
	turnMu      sync.Mutex
	turnCancels map[string]context.CancelFunc
	terminalMu  sync.Mutex
	terminals   map[string]*terminalSession
	oauthMu     sync.Mutex
	oauthStates map[string]string
	auditMu     sync.Mutex
	httpServer  *http.Server
	grpcServer  *grpc.Server
}

type authLoginRequest struct {
	Mode             string `json:"mode"`
	APIKey           string `json:"apiKey"`
	AccessToken      string `json:"accessToken"`
	ChatGPTAccountID string `json:"chatgptAccountId"`
	ChatGPTPlanType  string `json:"chatgptPlanType"`
	Email            string `json:"email"`
}

type chatRequest struct {
	Message string `json:"message"`
	HostID  string `json:"hostId"`
}

type approvalDecisionRequest struct {
	Decision string `json:"decision"`
}

type choiceAnswerInput struct {
	Value   string `json:"value"`
	Label   string `json:"label,omitempty"`
	IsOther bool   `json:"isOther,omitempty"`
}

type choiceAnswerRequest struct {
	Answers []choiceAnswerInput `json:"answers"`
}

type loginResponse struct {
	AuthURL string `json:"authUrl,omitempty"`
}

func New(cfg config.Config) *App {
	st := store.New()
	st.UpsertHost(model.Host{
		ID:         model.ServerLocalHostID,
		Name:       "server-local",
		Kind:       "server_local",
		Status:     "online",
		Executable: true,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
	})

	app := &App{
		cfg:   cfg,
		store: st,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		wsClients:   make(map[string]map[*websocket.Conn]struct{}),
		turnCancels: make(map[string]context.CancelFunc),
		terminals:   make(map[string]*terminalSession),
		oauthStates: make(map[string]string),
	}
	app.codex = codex.New(cfg.CodexPath, app.handleCodexNotification, app.handleCodexServerRequest)
	return app
}

func (a *App) Start(ctx context.Context) error {
	if err := os.MkdirAll(a.cfg.DefaultWorkspace, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.cfg.StatePath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.cfg.AuditLogPath), 0o755); err != nil {
		return err
	}
	absWorkspace, err := filepath.Abs(a.cfg.DefaultWorkspace)
	if err == nil {
		a.cfg.DefaultWorkspace = absWorkspace
	}
	if absStatePath, err := filepath.Abs(a.cfg.StatePath); err == nil {
		a.cfg.StatePath = absStatePath
	}
	a.store.SetStatePath(a.cfg.StatePath)
	if err := a.store.LoadStableState(a.cfg.StatePath); err != nil {
		return fmt.Errorf("load state store: %w", err)
	}
	a.store.UpsertHost(model.Host{
		ID:         model.ServerLocalHostID,
		Name:       "server-local",
		Kind:       "server_local",
		Status:     "online",
		Executable: true,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
	})

	if err := a.codex.Start(ctx); err != nil {
		return err
	}

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/api/v1/healthz", a.handleHealthz)
	httpMux.HandleFunc("/api/v1/sessions", a.withBrowserSession(a.handleSessions))
	httpMux.HandleFunc("/api/v1/sessions/", a.withBrowserSession(a.handleSessionActivation))
	httpMux.HandleFunc("/api/v1/state", a.withSession(a.handleState))
	httpMux.HandleFunc("/api/v1/thread/reset", a.withSession(a.handleThreadReset))
	httpMux.HandleFunc("/api/v1/auth/login", a.withSession(a.handleAuthLogin))
	httpMux.HandleFunc("/api/v1/auth/logout", a.withSession(a.handleAuthLogout))
	httpMux.HandleFunc("/api/v1/auth/oauth/start", a.withSession(a.handleOAuthStart))
	httpMux.HandleFunc("/api/v1/auth/oauth/callback", a.withSession(a.handleOAuthCallback))
	httpMux.HandleFunc("/api/v1/chat/message", a.withSession(a.handleChatMessage))
	httpMux.HandleFunc("/api/v1/chat/stop", a.withSession(a.handleChatStop))
	httpMux.HandleFunc("/api/v1/approvals/", a.withSession(a.handleApprovalDecision))
	httpMux.HandleFunc("/api/v1/choices/", a.withSession(a.handleChoiceAnswer))
	httpMux.HandleFunc("/api/v1/terminal/sessions", a.withSession(a.handleTerminalCreate))
	httpMux.HandleFunc("/api/v1/terminal/ws", a.withSession(a.handleTerminalWS))
	httpMux.HandleFunc("/ws", a.withSession(a.handleWS))
	httpMux.Handle("/", a.serveFrontend())

	a.httpServer = &http.Server{
		Addr:    a.cfg.HTTPAddr,
		Handler: httpMux,
	}

	a.grpcServer = grpc.NewServer()
	agentrpc.RegisterAgentServiceServer(a.grpcServer, a)

	go a.monitorHosts(ctx)

	return nil
}

func (a *App) Run(ctx context.Context) error {
	httpErrCh := make(chan error, 1)
	grpcErrCh := make(chan error, 1)

	go func() {
		log.Printf("http server listening on %s", a.cfg.HTTPAddr)
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
		}
	}()

	go func() {
		listener, err := net.Listen("tcp", a.cfg.GRPCAddr)
		if err != nil {
			grpcErrCh <- err
			return
		}
		log.Printf("grpc server listening on %s", a.cfg.GRPCAddr)
		if err := a.grpcServer.Serve(listener); err != nil {
			grpcErrCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.stopAllTerminals(shutdownCtx)
		_ = a.httpServer.Shutdown(shutdownCtx)
		a.grpcServer.GracefulStop()
		return ctx.Err()
	case err := <-httpErrCh:
		return err
	case err := <-grpcErrCh:
		return err
	}
}

func (a *App) Connect(stream agentrpc.AgentService_ConnectServer) error {
	var hostID string

	defer func() {
		if hostID != "" {
			a.store.MarkHostOffline(hostID)
			a.audit("agent.disconnect", map[string]any{
				"hostId": hostID,
			})
			a.broadcastAllSnapshots()
		}
	}()

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		switch msg.Kind {
		case "register":
			if msg.Registration == nil {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "missing registration payload",
				})
				continue
			}
			if msg.Registration.Token != a.cfg.HostAgentBootstrapToken {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "invalid bootstrap token",
				})
				continue
			}

			hostID = msg.Registration.HostID
			log.Printf("host-agent register host_id=%s hostname=%s", msg.Registration.HostID, msg.Registration.Hostname)
			a.audit("agent.register", map[string]any{
				"hostId":       msg.Registration.HostID,
				"hostname":     msg.Registration.Hostname,
				"os":           msg.Registration.OS,
				"arch":         msg.Registration.Arch,
				"agentVersion": msg.Registration.AgentVersion,
			})
			a.store.UpsertHost(model.Host{
				ID:            msg.Registration.HostID,
				Name:          msg.Registration.Hostname,
				Kind:          "agent",
				Status:        "online",
				Executable:    false,
				OS:            msg.Registration.OS,
				Arch:          msg.Registration.Arch,
				AgentVersion:  msg.Registration.AgentVersion,
				Labels:        msg.Registration.Labels,
				LastHeartbeat: model.NowString(),
			})
			a.broadcastAllSnapshots()

			_ = stream.Send(&agentrpc.Envelope{
				Kind: "ack",
				Ack: &agentrpc.Ack{
					Message:   "registered",
					Timestamp: time.Now().Unix(),
				},
			})
		case "heartbeat":
			if msg.Heartbeat == nil {
				continue
			}
			hostID = msg.Heartbeat.HostID
			log.Printf("host-agent heartbeat host_id=%s", msg.Heartbeat.HostID)
			host := a.findHost(hostID)
			host.Status = "online"
			host.LastHeartbeat = model.NowString()
			a.store.UpsertHost(host)
			a.broadcastAllSnapshots()
			_ = stream.Send(&agentrpc.Envelope{
				Kind: "ack",
				Ack: &agentrpc.Ack{
					Message:   "heartbeat",
					Timestamp: time.Now().Unix(),
				},
			})
		case "ping":
			_ = stream.Send(&agentrpc.Envelope{
				Kind: "pong",
				Ack: &agentrpc.Ack{
					Message:   "pong",
					Timestamp: time.Now().Unix(),
				},
			})
		}
	}
}

func DialAgent(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	)
}

func (a *App) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	status := http.StatusOK
	if !a.codex.Alive() {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":            status == http.StatusOK,
		"codexAlive":    a.codex.Alive(),
		"codexLastExit": a.codex.LastExitError(),
	})
}

func (a *App) handleSessions(w http.ResponseWriter, r *http.Request, browserID string) {
	activeSessionID := a.store.EnsureActiveSession(browserID)
	switch r.Method {
	case http.MethodGet:
		a.syncAccountState(r.Context(), activeSessionID)
		writeJSON(w, http.StatusOK, map[string]any{
			"activeSessionId": activeSessionID,
			"sessions":        a.store.SessionSummaries(browserID),
		})
	case http.MethodPost:
		if current := a.store.Session(activeSessionID); current != nil && current.Runtime.Turn.Active {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "当前任务执行中，暂时无法新建会话"})
			return
		}
		session := a.store.CreateSession(browserID)
		a.syncAccountState(r.Context(), session.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"activeSessionId": session.ID,
			"sessions":        a.store.SessionSummaries(browserID),
			"snapshot":        a.snapshot(session.ID),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleSessionActivation(w http.ResponseWriter, r *http.Request, browserID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	targetSessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	targetSessionID = strings.TrimSuffix(targetSessionID, "/activate")
	targetSessionID = strings.TrimSpace(targetSessionID)
	if targetSessionID == "" || !strings.HasSuffix(r.URL.Path, "/activate") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	currentSessionID := a.store.EnsureActiveSession(browserID)
	if currentSessionID != targetSessionID {
		if current := a.store.Session(currentSessionID); current != nil && current.Runtime.Turn.Active {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "当前任务执行中，完成后再切换会话"})
			return
		}
	}
	if err := a.store.ActivateSession(browserID, targetSessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	a.syncAccountState(r.Context(), targetSessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"activeSessionId": targetSessionID,
		"sessions":        a.store.SessionSummaries(browserID),
		"snapshot":        a.snapshot(targetSessionID),
	})
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request, sessionID string) {
	a.store.EnsureSession(sessionID)
	a.store.TouchSession(sessionID)
	a.syncAccountState(r.Context(), sessionID)
	writeJSON(w, http.StatusOK, a.snapshot(sessionID))
}

func (a *App) handleThreadReset(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	a.store.ResetConversation(sessionID)
	a.audit("thread.reset", map[string]any{
		"sessionId": sessionID,
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAuthLogin(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch req.Mode {
	case "apiKey":
		if req.APIKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "apiKey is required"})
			return
		}
		var result map[string]any
		log.Printf("auth login session=%s mode=apiKey", sessionID)
		if err := a.codex.Request(ctx, "account/login/start", map[string]any{
			"type":   "apiKey",
			"apiKey": req.APIKey,
		}, &result); err != nil {
			a.audit("auth.login_failed", map[string]any{
				"sessionId": sessionID,
				"mode":      req.Mode,
				"error":     err.Error(),
			})
			a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
			a.broadcastAllSnapshots()
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		a.store.SetAuth(sessionID, model.AuthState{
			Connected: true,
			Mode:      "apikey",
			Email:     req.Email,
		}, model.ExternalAuthTokens{Email: req.Email})
		a.audit("auth.login_started", map[string]any{
			"sessionId": sessionID,
			"mode":      req.Mode,
		})
		a.broadcastAllSnapshots()
		writeJSON(w, http.StatusOK, loginResponse{})
	case "chatgpt":
		var result struct {
			Type    string `json:"type"`
			AuthURL string `json:"authUrl"`
			LoginID string `json:"loginId"`
		}
		log.Printf("auth login session=%s mode=chatgpt", sessionID)
		if err := a.codex.Request(ctx, "account/login/start", map[string]any{
			"type": "chatgpt",
		}, &result); err != nil {
			a.audit("auth.login_failed", map[string]any{
				"sessionId": sessionID,
				"mode":      req.Mode,
				"error":     err.Error(),
			})
			a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
			a.broadcastAllSnapshots()
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		a.store.SetPendingLogin(sessionID, result.LoginID)
		a.audit("auth.login_started", map[string]any{
			"sessionId": sessionID,
			"mode":      req.Mode,
			"loginId":   result.LoginID,
		})
		a.broadcastAllSnapshots()
		writeJSON(w, http.StatusOK, loginResponse{AuthURL: result.AuthURL})
	case "chatgptAuthTokens":
		accountID := req.ChatGPTAccountID
		if accountID == "" {
			accountID = a.cfg.OAuthAccountID
		}
		planType := req.ChatGPTPlanType
		if planType == "" {
			planType = a.cfg.OAuthPlanType
		}
		if req.AccessToken == "" || accountID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "accessToken and chatgptAccountId are required"})
			return
		}
		var result map[string]any
		log.Printf("auth login session=%s mode=chatgptAuthTokens", sessionID)
		if err := a.codex.Request(ctx, "account/login/start", map[string]any{
			"type":             "chatgptAuthTokens",
			"accessToken":      req.AccessToken,
			"chatgptAccountId": accountID,
			"chatgptPlanType":  emptyToNil(planType),
		}, &result); err != nil {
			a.audit("auth.login_failed", map[string]any{
				"sessionId": sessionID,
				"mode":      req.Mode,
				"error":     err.Error(),
			})
			a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
			a.broadcastAllSnapshots()
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		a.store.SetAuth(sessionID, model.AuthState{
			Connected: true,
			Mode:      "chatgptAuthTokens",
			PlanType:  planType,
			Email:     req.Email,
		}, model.ExternalAuthTokens{
			AccessToken:      req.AccessToken,
			ChatGPTAccountID: accountID,
			ChatGPTPlanType:  planType,
			Email:            req.Email,
		})
		a.audit("auth.login_started", map[string]any{
			"sessionId": sessionID,
			"mode":      req.Mode,
			"planType":  planType,
		})
		a.broadcastAllSnapshots()
		writeJSON(w, http.StatusOK, loginResponse{})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mode"})
	}
}

func (a *App) handleAuthLogout(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var result map[string]any
	_ = a.codex.Request(ctx, "account/logout", map[string]any{}, &result)
	log.Printf("auth logout session=%s", sessionID)
	a.audit("auth.logout", map[string]any{
		"sessionId": sessionID,
	})
	a.store.ClearAuth(sessionID)
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleOAuthStart(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !a.cfg.OAuthConfigured() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth is not configured"})
		return
	}

	state := model.NewID("oauth")
	a.oauthMu.Lock()
	a.oauthStates[sessionID] = state
	a.oauthMu.Unlock()

	values := url.Values{}
	values.Set("client_id", a.cfg.OAuthClientID)
	values.Set("redirect_uri", a.cfg.OAuthRedirectURL)
	values.Set("response_type", "code")
	values.Set("scope", strings.Join(a.cfg.OAuthScopeList(), " "))
	values.Set("state", state)

	target := a.cfg.OAuthAuthURL
	if strings.Contains(target, "?") {
		target += "&" + values.Encode()
	} else {
		target += "?" + values.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !a.cfg.OAuthConfigured() {
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=oauth_not_configured", http.StatusFound)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		a.store.SetAuth(sessionID, model.AuthState{LastError: errParam}, model.ExternalAuthTokens{})
		a.broadcastAllSnapshots()
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=missing_code", http.StatusFound)
		return
	}

	a.oauthMu.Lock()
	expectedState := a.oauthStates[sessionID]
	delete(a.oauthStates, sessionID)
	a.oauthMu.Unlock()
	if state != expectedState {
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=invalid_state", http.StatusFound)
		return
	}

	tokenResp, err := a.exchangeOAuthCode(r.Context(), code)
	if err != nil {
		a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
		a.broadcastAllSnapshots()
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=exchange_failed", http.StatusFound)
		return
	}

	email := tokenResp.Email
	if email == "" {
		email = a.fetchOAuthEmail(r.Context(), tokenResp.AccessToken)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var result map[string]any
	err = a.codex.Request(ctx, "account/login/start", map[string]any{
		"type":             "chatgptAuthTokens",
		"accessToken":      tokenResp.AccessToken,
		"chatgptAccountId": a.cfg.OAuthAccountID,
		"chatgptPlanType":  emptyToNil(a.cfg.OAuthPlanType),
	}, &result)
	if err != nil {
		a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
		a.broadcastAllSnapshots()
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=codex_login_failed", http.StatusFound)
		return
	}

	a.store.SetAuth(sessionID, model.AuthState{
		Connected: true,
		Mode:      "chatgptAuthTokens",
		PlanType:  a.cfg.OAuthPlanType,
		Email:     email,
	}, model.ExternalAuthTokens{
		IDToken:          tokenResp.IDToken,
		AccessToken:      tokenResp.AccessToken,
		ChatGPTAccountID: a.cfg.OAuthAccountID,
		ChatGPTPlanType:  a.cfg.OAuthPlanType,
		Email:            email,
	})
	a.broadcastAllSnapshots()
	http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=success", http.StatusFound)
}

func (a *App) handleChatMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	if req.HostID == "" {
		req.HostID = model.ServerLocalHostID
	}

	a.syncAccountState(r.Context(), sessionID)
	auth := a.store.Auth(sessionID)
	if !auth.Connected {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "请先登录 GPT 账号"})
		return
	}
	if req.HostID != model.ServerLocalHostID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "MVP only supports server-local execution"})
		return
	}

	a.store.EnsureSession(sessionID)
	a.store.TouchSession(sessionID)
	a.store.SetSelectedHost(sessionID, req.HostID)
	log.Printf("chat message session=%s host=%s text=%q", sessionID, req.HostID, truncate(req.Message, 120))
	a.audit("chat.message", map[string]any{
		"sessionId": sessionID,
		"hostId":    req.HostID,
		"text":      truncate(req.Message, 400),
	})

	userCard := model.Card{
		ID:        model.NewID("msg"),
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      req.Message,
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	a.store.UpsertCard(sessionID, userCard)
	a.startRuntimeTurn(sessionID, req.HostID)
	a.broadcastSnapshot(sessionID)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	a.setTurnCancel(sessionID, cancel)
	defer func() {
		a.clearTurnCancel(sessionID)
		cancel()
	}()

	err := a.startTurn(ctx, sessionID, req)
	if err != nil {
		if errors.Is(err, context.Canceled) && a.turnWasInterrupted(sessionID) {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"accepted":    false,
				"interrupted": true,
			})
			return
		}
		a.finishRuntimeTurn(sessionID, "failed")
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("error"),
			Type:      "ErrorCard",
			Title:     "Turn failed",
			Message:   err.Error(),
			Text:      err.Error(),
			Status:    "failed",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(sessionID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

func (a *App) handleChatStop(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	session := a.store.Session(sessionID)
	if session == nil || !session.Runtime.Turn.Active {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "当前没有可中断的任务"})
		return
	}

	threadID := session.ThreadID
	turnID := session.TurnID
	cancelledPending := a.cancelTurnStart(sessionID)

	if threadID == "" && !cancelledPending {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "当前任务尚未进入可中断状态"})
		return
	}

	if threadID != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		params := map[string]any{
			"threadId":                   threadID,
			"clean_background_terminals": true,
		}
		if turnID != "" {
			params["turnId"] = turnID
		}
		var result map[string]any
		if err := a.codex.Request(ctx, "turn/interrupt", params, &result); err != nil && !cancelledPending {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}

	a.cleanBackgroundTerminals(threadID)
	a.markTurnInterrupted(sessionID, turnID)
	a.audit("chat.stop", map[string]any{
		"sessionId": sessionID,
		"threadId":  threadID,
		"turnId":    turnID,
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) startTurn(ctx context.Context, sessionID string, req chatRequest) error {
	threadID, err := a.ensureThread(ctx, sessionID)
	if err != nil {
		return err
	}

	err = a.requestTurn(ctx, sessionID, threadID, req)
	if err == nil {
		return nil
	}
	if !isThreadNotFoundError(err) {
		return err
	}

	log.Printf("stale codex thread detected session=%s thread=%s err=%s", sessionID, threadID, truncate(err.Error(), 200))
	a.store.ClearThread(sessionID)
	a.appendThreadResetCard(sessionID)
	a.broadcastSnapshot(sessionID)

	threadID, err = a.ensureThread(ctx, sessionID)
	if err != nil {
		return err
	}
	return a.requestTurn(ctx, sessionID, threadID, req)
}

func (a *App) requestTurn(ctx context.Context, sessionID, threadID string, req chatRequest) error {
	var result map[string]any
	err := a.codex.Request(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"cwd":            a.cfg.DefaultWorkspace,
		"approvalPolicy": "untrusted",
		"developerInstructions": fmt.Sprintf(
			"Current selected host is %s. Operate only on this host. The default writable workspace is %s. Do not assume access outside the workspace unless explicitly requested and approved.",
			req.HostID,
			a.cfg.DefaultWorkspace,
		),
		"sandboxPolicy": map[string]any{
			"type":          "workspaceWrite",
			"writableRoots": []string{a.cfg.DefaultWorkspace},
		},
		"input": []map[string]any{
			{"type": "text", "text": req.Message},
		},
	}, &result)
	if err != nil {
		return err
	}
	if turnID := getTurnID(result); turnID != "" {
		a.store.SetTurn(sessionID, turnID)
	}
	return nil
}

func (a *App) appendThreadResetCard(sessionID string) {
	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("notice"),
		Type:      "NoticeCard",
		Title:     "Thread restarted",
		Text:      "The previous Codex thread was no longer available, so this request is continuing in a fresh thread.",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func isThreadNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "thread not found")
}

func (a *App) handleApprovalDecision(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	approvalID := strings.TrimPrefix(r.URL.Path, "/api/v1/approvals/")
	approvalID = strings.TrimSuffix(approvalID, "/decision")
	approval, ok := a.store.Approval(sessionID, approvalID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval not found"})
		return
	}

	var req approvalDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	decision := req.Decision
	if decision == "" {
		decision = "accept"
	}
	log.Printf("approval decision session=%s approval=%s decision=%s", sessionID, approvalID, decision)
	if decision == "accept_session" {
		a.store.AddApprovalGrant(sessionID, approvalGrantFromApproval(approval))
	}

	codexDecision := mapApprovalDecision(decision, approval)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := a.codex.Respond(ctx, approval.RequestIDRaw, map[string]any{
		"decision": codexDecision,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	cardStatus := approvalStatusFromDecision(decision)
	a.store.ResolveApproval(sessionID, approvalID, cardStatus, model.NowString())
	nextPhase := "thinking"
	if a.hasPendingApprovals(sessionID) {
		nextPhase = "waiting_approval"
	} else if decision == "accept" || decision == "accept_session" {
		nextPhase = "executing"
	}
	a.setRuntimeTurnPhase(sessionID, nextPhase)
	a.audit("approval.decision", map[string]any{
		"sessionId":  sessionID,
		"approvalId": approvalID,
		"type":       approval.Type,
		"hostId":     approval.HostID,
		"decision":   cardStatus,
	})
	if approval.Type == "command" {
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Status = cardStatus
			card.UpdatedAt = model.NowString()
		})
	} else {
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Status = cardStatus
			card.UpdatedAt = model.NowString()
		})
	}
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleChoiceAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	choiceID := strings.TrimPrefix(r.URL.Path, "/api/v1/choices/")
	choiceID = strings.TrimSuffix(choiceID, "/answer")
	choice, ok := a.store.Choice(sessionID, choiceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "choice not found"})
		return
	}

	var req choiceAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(req.Answers) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answers are required"})
		return
	}
	if len(choice.Questions) > 0 && len(req.Answers) != len(choice.Questions) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answers count does not match questions"})
		return
	}

	codexAnswers := make([]map[string]any, 0, len(req.Answers))
	for _, answer := range req.Answers {
		value := strings.TrimSpace(answer.Value)
		if value == "" {
			value = strings.TrimSpace(answer.Label)
		}
		if value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "all answers must be non-empty"})
			return
		}
		codexAnswer := map[string]any{
			"value": value,
			"label": emptyToNil(strings.TrimSpace(answer.Label)),
		}
		if answer.IsOther {
			codexAnswer["isOther"] = true
		}
		codexAnswers = append(codexAnswers, codexAnswer)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := a.codex.Respond(ctx, choice.RequestIDRaw, map[string]any{
		"answers": codexAnswers,
	}); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	now := model.NowString()
	a.store.ResolveChoice(sessionID, choiceID, "completed", now)
	a.store.UpdateCard(sessionID, choice.ItemID, func(card *model.Card) {
		card.Status = "completed"
		card.AnswerSummary = choiceAnswerSummary(choice.Questions, req.Answers)
		card.UpdatedAt = now
	})
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.audit("choice.answer", map[string]any{
		"sessionId": sessionID,
		"choiceId":  choiceID,
		"answers":   len(req.Answers),
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	a.wsMu.Lock()
	conns := a.wsClients[sessionID]
	if conns == nil {
		conns = make(map[*websocket.Conn]struct{})
		a.wsClients[sessionID] = conns
	}
	conns[conn] = struct{}{}
	a.wsMu.Unlock()

	_ = conn.WriteJSON(a.snapshot(sessionID))

	defer func() {
		a.wsMu.Lock()
		delete(a.wsClients[sessionID], conn)
		a.wsMu.Unlock()
		_ = conn.Close()
	}()

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var incoming struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &incoming); err != nil {
			continue
		}
		if incoming.Type == "ping" {
			a.wsMu.Lock()
			writeErr := conn.WriteJSON(map[string]string{"type": "heartbeat"})
			a.wsMu.Unlock()
			if writeErr != nil {
				return
			}
		}
	}
}

func (a *App) handleCodexNotification(method string, params json.RawMessage) {
	var payload map[string]any
	_ = json.Unmarshal(params, &payload)

	switch method {
	case "account/updated":
		authMode := getString(payload, "authMode")
		planType := getString(payload, "planType")
		log.Printf("codex notification method=%s authMode=%q planType=%q", method, authMode, planType)
		for _, sessionID := range a.store.SessionIDs() {
			a.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
				auth.Connected = true
				auth.Pending = false
				auth.Mode = authMode
				auth.PlanType = planType
				auth.LastError = ""
			})
			a.broadcastSnapshot(sessionID)
		}
	case "account/login/completed":
		loginID := getString(payload, "loginId")
		success := getBool(payload, "success")
		log.Printf("codex notification method=%s loginId=%q success=%t error=%q", method, loginID, success, getString(payload, "error"))
		sessionID := a.store.SessionIDByLogin(loginID)
		targetSessionIDs := make([]string, 0, 1)
		if sessionID != "" {
			targetSessionIDs = append(targetSessionIDs, sessionID)
		} else {
			targetSessionIDs = append(targetSessionIDs, a.store.PendingSessionIDs()...)
			if len(targetSessionIDs) == 0 {
				log.Printf("codex login completion ignored because no session matched loginId=%q", loginID)
				break
			}
		}

		for _, targetSessionID := range targetSessionIDs {
			a.store.UpdateAuth(targetSessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
				auth.Pending = false
				if success {
					auth.Connected = true
					if auth.Mode == "" {
						auth.Mode = "chatgpt"
					}
					auth.LastError = ""
					return
				}
				auth.Connected = false
				auth.LastError = getString(payload, "error")
			})
			a.broadcastSnapshot(targetSessionID)
		}
	case "turn/started":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		if a.shouldIgnoreTurnPayload(sessionID, payload) {
			return
		}
		a.bindTurnToSession(sessionID, payload)
	case "turn/plan/updated":
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		if a.shouldIgnoreTurnPayload(sessionID, payload) {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		a.setRuntimeTurnPhase(sessionID, "planning")
		cardID := "plan-" + getString(payload, "turnId")
		planItems := toPlanItems(payload["plan"])
		card := model.Card{
			ID:        cardID,
			Type:      "PlanCard",
			Title:     "Plan",
			Items:     planItems,
			Status:    "inProgress",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		}
		a.store.UpsertCard(sessionID, card)
		a.broadcastSnapshot(sessionID)
	case "item/started":
		a.handleItemStarted(payload)
	case "item/agentMessage/delta":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		if a.shouldIgnoreTurnPayload(sessionID, payload) {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		itemID := getString(payload, "itemId")
		if a.cardIsFinal(sessionID, itemID) {
			return
		}
		if session := a.store.Session(sessionID); session != nil {
			exists := false
			for _, card := range session.Cards {
				if card.ID == itemID {
					exists = true
					break
				}
			}
			if !exists {
				now := model.NowString()
				a.store.UpsertCard(sessionID, model.Card{
					ID:        itemID,
					Type:      "AssistantMessageCard",
					Role:      "assistant",
					Status:    "inProgress",
					CreatedAt: now,
					UpdatedAt: now,
				})
			}
		}
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Text += getString(payload, "delta")
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
	case "item/commandExecution/outputDelta":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		if a.shouldIgnoreTurnPayload(sessionID, payload) {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		itemID := getString(payload, "itemId")
		if a.cardIsFinal(sessionID, itemID) {
			return
		}
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Output += getString(payload, "delta")
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
	case "item/fileChange/outputDelta":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		if a.shouldIgnoreTurnPayload(sessionID, payload) {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		itemID := getString(payload, "itemId")
		if a.cardIsFinal(sessionID, itemID) {
			return
		}
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Output += getString(payload, "delta")
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
	case "item/completed":
		a.handleItemCompleted(payload)
	case "serverRequest/resolved":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		a.broadcastSnapshot(sessionID)
	case "turn/completed":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		if a.shouldIgnoreTurnPayload(sessionID, payload) {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		turn := getMap(payload, "turn")
		turnStatus := getString(turn, "status")
		a.store.UpdateCard(sessionID, "plan-"+getString(turn, "id"), func(card *model.Card) {
			card.Status = normalizeCardStatus(turnStatus)
			card.UpdatedAt = model.NowString()
		})
		if normalizeCardStatus(turnStatus) == "completed" {
			a.finishRuntimeTurn(sessionID, "completed")
		} else {
			a.finishRuntimeTurn(sessionID, "failed")
		}
		a.finalizeOpenTurnCards(sessionID, normalizeCardStatus(turnStatus))
		a.cleanBackgroundTerminals(getStringAny(payload, "threadId", "thread_id"))
		log.Printf("turn completed session=%s turn=%s status=%s", sessionID, getString(turn, "id"), turnStatus)
		a.broadcastSnapshot(sessionID)
	case "turn/aborted":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		a.cleanBackgroundTerminals(getStringAny(payload, "threadId", "thread_id"))
		a.markTurnInterrupted(sessionID, getTurnID(payload))
		a.broadcastSnapshot(sessionID)
	case "error":
		errorPayload := getMap(payload, "error")
		text := getString(errorPayload, "message")
		threadID := getString(payload, "threadId")
		sessionIDs := a.store.SessionIDs()
		if threadID != "" {
			if sessionID := a.store.SessionIDByThread(threadID); sessionID != "" {
				sessionIDs = []string{sessionID}
			}
		}
		for _, id := range sessionIDs {
			a.finishRuntimeTurn(id, "failed")
			a.store.UpsertCard(id, model.Card{
				ID:        model.NewID("error"),
				Type:      "ErrorCard",
				Title:     "Error",
				Message:   text,
				Text:      text,
				Status:    "failed",
				CreatedAt: model.NowString(),
				UpdatedAt: model.NowString(),
			})
			a.broadcastSnapshot(id)
		}
	}
}

func (a *App) handleCodexServerRequest(rawID json.RawMessage, method string, params json.RawMessage) {
	var payload map[string]any
	_ = json.Unmarshal(params, &payload)

	switch method {
	case "account/chatgptAuthTokens/refresh":
		tokens := a.store.TokensForRefresh()
		if tokens.AccessToken == "" || tokens.ChatGPTAccountID == "" {
			_ = a.codex.RespondError(context.Background(), string(rawID), -32000, "no external tokens available")
			return
		}
		_ = a.codex.Respond(context.Background(), string(rawID), map[string]any{
			"accessToken":      tokens.AccessToken,
			"chatgptAccountId": tokens.ChatGPTAccountID,
			"chatgptPlanType":  emptyToNil(tokens.ChatGPTPlanType),
		})
	case "item/commandExecution/requestApproval":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		hostID := model.ServerLocalHostID
		if session := a.store.Session(sessionID); session != nil && session.SelectedHostID != "" {
			hostID = session.SelectedHostID
		}
		approval := model.ApprovalRequest{
			ID:           model.NewID("approval"),
			RequestIDRaw: string(rawID),
			HostID:       hostID,
			Fingerprint:  approvalFingerprintForCommand(hostID, getString(payload, "command"), getString(payload, "cwd")),
			Type:         "command",
			Status:       "pending",
			ThreadID:     getStringAny(payload, "threadId", "thread_id"),
			TurnID:       getStringAny(payload, "turnId", "turn_id"),
			ItemID:       getString(payload, "itemId"),
			Command:      getString(payload, "command"),
			Cwd:          getString(payload, "cwd"),
			Reason:       getString(payload, "reason"),
			Decisions:    toStringSlice(payload["availableDecisions"]),
			RequestedAt:  model.NowString(),
		}
		if a.autoApproveBySessionGrant(sessionID, approval) {
			return
		}
		log.Printf("approval requested type=command session=%s item=%s command=%q", sessionID, approval.ItemID, approval.Command)
		a.audit("approval.requested", map[string]any{
			"sessionId":  sessionID,
			"approvalId": approval.ID,
			"type":       approval.Type,
			"hostId":     approval.HostID,
			"command":    approval.Command,
			"cwd":        approval.Cwd,
		})
		a.store.AddApproval(sessionID, approval)
		card := model.Card{
			ID:      approval.ItemID,
			Type:    "CommandApprovalCard",
			Title:   "Command approval required",
			Command: approval.Command,
			Cwd:     approval.Cwd,
			Text:    approval.Reason,
			Status:  "pending",
			Approval: &model.ApprovalRef{
				RequestID: approval.ID,
				Type:      approval.Type,
				Decisions: approval.Decisions,
			},
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		}
		a.store.UpsertCard(sessionID, card)
		a.broadcastSnapshot(sessionID)
	case "item/fileChange/requestApproval":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			return
		}
		a.bindTurnToSession(sessionID, payload)
		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		itemID := getString(payload, "itemId")
		cachedItem := a.store.Item(sessionID, itemID)
		hostID := model.ServerLocalHostID
		if session := a.store.Session(sessionID); session != nil && session.SelectedHostID != "" {
			hostID = session.SelectedHostID
		}
		changes := toChanges(cachedItem["changes"])
		approval := model.ApprovalRequest{
			ID:           model.NewID("approval"),
			RequestIDRaw: string(rawID),
			HostID:       hostID,
			Fingerprint:  approvalFingerprintForFileChange(hostID, getString(payload, "grantRoot"), changes),
			Type:         "file_change",
			Status:       "pending",
			ThreadID:     getStringAny(payload, "threadId", "thread_id"),
			TurnID:       getStringAny(payload, "turnId", "turn_id"),
			ItemID:       itemID,
			Reason:       getString(payload, "reason"),
			GrantRoot:    getString(payload, "grantRoot"),
			Changes:      changes,
			Decisions:    []string{"accept", "decline"},
			RequestedAt:  model.NowString(),
		}
		if a.autoApproveBySessionGrant(sessionID, approval) {
			return
		}
		log.Printf("approval requested type=file_change session=%s item=%s", sessionID, itemID)
		a.audit("approval.requested", map[string]any{
			"sessionId":  sessionID,
			"approvalId": approval.ID,
			"type":       approval.Type,
			"hostId":     approval.HostID,
			"grantRoot":  approval.GrantRoot,
		})
		a.store.AddApproval(sessionID, approval)
		card := model.Card{
			ID:      itemID,
			Type:    "FileChangeApprovalCard",
			Title:   "File change approval required",
			Text:    approval.Reason,
			Status:  "pending",
			Changes: approval.Changes,
			Approval: &model.ApprovalRef{
				RequestID: approval.ID,
				Type:      approval.Type,
				Decisions: approval.Decisions,
			},
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		}
		a.store.UpsertCard(sessionID, card)
		a.broadcastSnapshot(sessionID)
	case "request_user_input":
		sessionID := a.sessionIDFromPayload(payload)
		if sessionID == "" {
			_ = a.codex.RespondError(context.Background(), string(rawID), -32000, "session not found for request_user_input")
			return
		}
		a.bindTurnToSession(sessionID, payload)
		questions := toChoiceQuestions(payload["questions"])
		if len(questions) == 0 {
			_ = a.codex.RespondError(context.Background(), string(rawID), -32602, "request_user_input requires questions")
			return
		}
		a.setRuntimeTurnPhase(sessionID, "waiting_input")
		now := model.NowString()
		choiceID := model.NewID("choice")
		choice := model.ChoiceRequest{
			ID:           choiceID,
			RequestIDRaw: string(rawID),
			ThreadID:     getStringAny(payload, "threadId", "thread_id"),
			TurnID:       getStringAny(payload, "turnId", "turn_id"),
			ItemID:       choiceID,
			Status:       "pending",
			Questions:    questions,
			RequestedAt:  now,
		}
		card := model.Card{
			ID:        choice.ItemID,
			Type:      "ChoiceCard",
			Title:     choiceCardTitle(questions),
			RequestID: choice.ID,
			Question:  questions[0].Question,
			Options:   questions[0].Options,
			Questions: questions,
			Status:    "pending",
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.AddChoice(sessionID, choice)
		a.store.UpsertCard(sessionID, card)
		a.audit("choice.requested", map[string]any{
			"sessionId": sessionID,
			"choiceId":  choice.ID,
			"questions": len(questions),
		})
		a.broadcastSnapshot(sessionID)
	}
}

func (a *App) handleItemStarted(payload map[string]any) {
	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		return
	}
	if a.shouldIgnoreTurnPayload(sessionID, payload) {
		return
	}
	a.bindTurnToSession(sessionID, payload)
	item := getMap(payload, "item")
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	a.store.RememberItem(sessionID, itemID, item)
	a.updateActivityFromItem(sessionID, item, false)
	a.syncProcessLineCard(sessionID, itemID, item, false)

	now := model.NowString()
	switch itemType {
	case "commandExecution":
		a.setRuntimeTurnPhase(sessionID, "executing")
		a.incrementCommandCount(sessionID)
		card := model.Card{
			ID:        itemID,
			Type:      "CommandCard",
			Title:     "Command execution",
			Command:   getString(item, "command"),
			Cwd:       getString(item, "cwd"),
			Status:    normalizeCardStatus(getString(item, "status")),
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
	case "fileChange":
		a.setRuntimeTurnPhase(sessionID, "executing")
		card := model.Card{
			ID:        itemID,
			Type:      "FileChangeCard",
			Title:     "File change",
			Status:    normalizeCardStatus(getString(item, "status")),
			Changes:   toChanges(item["changes"]),
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
	case "agentMessage":
		a.setRuntimeTurnPhase(sessionID, "finalizing")
		card := model.Card{
			ID:        itemID,
			Type:      "AssistantMessageCard",
			Role:      "assistant",
			Status:    "inProgress",
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
		a.scheduleFinalizingExecutionCleanup(sessionID, getStringAny(payload, "threadId", "thread_id"))
		a.scheduleSilentTurnCompletionCheck(sessionID, 6*time.Second)
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) handleItemCompleted(payload map[string]any) {
	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		return
	}
	if a.shouldIgnoreTurnPayload(sessionID, payload) {
		return
	}
	a.bindTurnToSession(sessionID, payload)
	item := getMap(payload, "item")
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	a.store.RememberItem(sessionID, itemID, item)
	a.updateActivityFromItem(sessionID, item, true)
	a.syncProcessLineCard(sessionID, itemID, item, true)

	now := model.NowString()
	durationMS := a.cardDurationMS(sessionID, itemID, now)

	switch itemType {
	case "agentMessage":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = "completed"
			if card.Text == "" {
				card.Text = getString(item, "text")
			}
			card.DurationMS = durationMS
			card.UpdatedAt = now
			if isTaskCompletionText(card.Text) {
				card.Type = "TaskDividerCard"
				card.Role = ""
				card.Text = ""
				card.Title = ""
				card.Status = "completed"
			}
		})
	case "commandExecution":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			output := card.Output
			if aggregated := getString(item, "aggregatedOutput"); aggregated != "" && len(aggregated) >= len(output) {
				output = aggregated
			}
			card.Output = output
			card.Status = completedCommandStatus(item, output)
			if itemDuration, ok := getIntAny(item, "durationMs", "duration_ms"); ok && itemDuration > 0 {
				card.DurationMS = int64(itemDuration)
			} else {
				card.DurationMS = durationMS
			}
			card.UpdatedAt = now
		})
		a.resumeThinkingAfterExecution(sessionID)
	case "fileChange":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = completedItemStatus(item)
			card.Changes = toChanges(item["changes"])
			card.DurationMS = durationMS
			card.UpdatedAt = now
		})
		a.resumeThinkingAfterExecution(sessionID)
	}
	if itemType == "agentMessage" {
		a.scheduleSilentTurnCompletionCheck(sessionID, 6*time.Second)
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) autoApproveBySessionGrant(sessionID string, approval model.ApprovalRequest) bool {
	if approval.Fingerprint == "" {
		return false
	}
	if _, ok := a.store.ApprovalGrant(sessionID, approval.Fingerprint); !ok {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.codex.Respond(ctx, approval.RequestIDRaw, map[string]any{
		"decision": "accept",
	}); err != nil {
		log.Printf("auto approval failed session=%s approval=%s err=%s", sessionID, approval.ID, truncate(err.Error(), 200))
		return false
	}

	now := model.NowString()
	approval.Status = "accepted_for_session_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved for session",
		Text:      autoApprovalNoticeText(approval),
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	log.Printf("approval auto accepted by session grant session=%s approval=%s type=%s", sessionID, approval.ID, approval.Type)
	a.audit("approval.auto_accepted", map[string]any{
		"sessionId":   sessionID,
		"approvalId":  approval.ID,
		"type":        approval.Type,
		"hostId":      approval.HostID,
		"fingerprint": approval.Fingerprint,
	})
	a.broadcastSnapshot(sessionID)
	return true
}

func (a *App) startRuntimeTurn(sessionID, hostID string) {
	startedAt := model.NowString()
	a.store.ClearTurn(sessionID)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = true
		runtime.Turn.Phase = "thinking"
		runtime.Turn.HostID = defaultHostID(hostID)
		runtime.Turn.StartedAt = startedAt
		runtime.Activity = model.ActivityRuntime{
			ViewedFiles:        make([]model.ActivityEntry, 0),
			SearchedWebQueries: make([]model.ActivityEntry, 0),
		}
	})
}

func (a *App) setRuntimeTurnPhase(sessionID, phase string) {
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = phase != "" && phase != "idle" && phase != "completed" && phase != "failed" && phase != "aborted"
		runtime.Turn.Phase = phase
		if runtime.Turn.StartedAt == "" && runtime.Turn.Active {
			runtime.Turn.StartedAt = model.NowString()
		}
		if runtime.Turn.HostID == "" {
			runtime.Turn.HostID = model.ServerLocalHostID
		}
	})
}

func (a *App) finishRuntimeTurn(sessionID, phase string) {
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = false
		runtime.Turn.Phase = phase
		runtime.Activity.CurrentReadingFile = ""
		runtime.Activity.CurrentWebSearchQuery = ""
	})
}

func (a *App) resumeThinkingAfterExecution(sessionID string) {
	session := a.store.Session(sessionID)
	if session == nil || !session.Runtime.Turn.Active {
		return
	}
	if session.Runtime.Turn.Phase != "executing" {
		return
	}
	a.setRuntimeTurnPhase(sessionID, "thinking")
}

func (a *App) hasPendingApprovals(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, approval := range session.Approvals {
		if approval.Status == "pending" {
			return true
		}
	}
	return false
}

func (a *App) hasPendingChoices(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, choice := range session.Choices {
		if choice.Status == "pending" {
			return true
		}
	}
	return false
}

func (a *App) hasInProgressExecutionCards(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if normalizeCardStatus(card.Status) != "inProgress" {
			continue
		}
		switch card.Type {
		case "CommandCard", "FileChangeCard", "ProcessLineCard":
			return true
		}
	}
	return false
}

func (a *App) hasInProgressCards(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if normalizeCardStatus(card.Status) == "inProgress" || card.Status == "pending" {
			return true
		}
	}
	return false
}

func (a *App) hasCompletedAssistantMessage(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if card.Type == "AssistantMessageCard" && normalizeCardStatus(card.Status) == "completed" {
			return true
		}
	}
	return false
}

func (a *App) finalizeLingeringExecutionCards(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}

	now := model.NowString()
	changed := false
	for _, existing := range session.Cards {
		if normalizeCardStatus(existing.Status) != "inProgress" {
			continue
		}

		switch existing.Type {
		case "CommandCard":
			item := a.store.Item(sessionID, existing.ID)
			output := existing.Output
			if aggregated := getString(item, "aggregatedOutput"); aggregated != "" && len(aggregated) >= len(output) {
				output = aggregated
			}
			durationMS := existing.DurationMS
			if durationMS == 0 {
				if itemDuration, ok := getIntAny(item, "durationMs", "duration_ms"); ok && itemDuration > 0 {
					durationMS = int64(itemDuration)
				} else {
					durationMS = durationBetween(existing.CreatedAt, now)
				}
			}
			status := completedCommandStatus(item, output)
			a.store.UpdateCard(sessionID, existing.ID, func(card *model.Card) {
				card.Output = output
				card.Status = status
				card.DurationMS = durationMS
				card.UpdatedAt = now
			})
			changed = true
		case "FileChangeCard", "ProcessLineCard":
			durationMS := existing.DurationMS
			if durationMS == 0 {
				durationMS = durationBetween(existing.CreatedAt, now)
			}
			a.store.UpdateCard(sessionID, existing.ID, func(card *model.Card) {
				card.Status = "completed"
				card.DurationMS = durationMS
				card.UpdatedAt = now
			})
			changed = true
		}
	}
	return changed
}

func (a *App) scheduleFinalizingExecutionCleanup(sessionID, threadID string) {
	session := a.store.Session(sessionID)
	if session == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	turnID := session.TurnID

	go func() {
		timer := time.NewTimer(1500 * time.Millisecond)
		defer timer.Stop()
		<-timer.C

		current := a.store.Session(sessionID)
		if current == nil || current.TurnID != turnID {
			return
		}
		if !current.Runtime.Turn.Active || current.Runtime.Turn.Phase != "finalizing" {
			return
		}
		if a.hasPendingApprovals(sessionID) || a.hasPendingChoices(sessionID) {
			return
		}
		if !a.hasInProgressExecutionCards(sessionID) {
			return
		}

		changed := a.finalizeLingeringExecutionCards(sessionID)
		a.cleanBackgroundTerminalsWithTimeout(threadID, 15*time.Second)
		if changed {
			log.Printf("finalizing cleanup resolved lingering execution cards session=%s turn=%s", sessionID, turnID)
			a.broadcastSnapshot(sessionID)
		}
	}()
}

func (a *App) scheduleSilentTurnCompletionCheck(sessionID string, delay time.Duration) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	turnID := session.TurnID

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C

		current := a.store.Session(sessionID)
		if current == nil || current.TurnID != turnID {
			return
		}
		if !current.Runtime.Turn.Active || current.Runtime.Turn.Phase != "finalizing" {
			return
		}
		if a.hasPendingApprovals(sessionID) || a.hasPendingChoices(sessionID) || a.hasInProgressCards(sessionID) {
			return
		}
		if !a.hasCompletedAssistantMessage(sessionID) {
			return
		}

		lastActivityAt, err := time.Parse(time.RFC3339, current.LastActivityAt)
		if err != nil || time.Since(lastActivityAt) < delay {
			return
		}

		a.finishRuntimeTurn(sessionID, "completed")
		a.finalizeOpenTurnCards(sessionID, "completed")
		log.Printf("auto completed silent finalizing turn session=%s turn=%s", sessionID, turnID)
		a.broadcastSnapshot(sessionID)
	}()
}

func (a *App) setTurnCancel(sessionID string, cancel context.CancelFunc) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	a.turnCancels[sessionID] = cancel
}

func (a *App) clearTurnCancel(sessionID string) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	if _, ok := a.turnCancels[sessionID]; ok {
		delete(a.turnCancels, sessionID)
	}
}

func (a *App) cancelTurnStart(sessionID string) bool {
	a.turnMu.Lock()
	cancel := a.turnCancels[sessionID]
	a.turnMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (a *App) turnWasInterrupted(sessionID string) bool {
	session := a.store.Session(sessionID)
	return session != nil && session.Runtime.Turn.Phase == "aborted"
}

func (a *App) incrementCommandCount(sessionID string) {
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Activity.CommandsRun++
	})
}

func (a *App) bindTurnToSession(sessionID string, payload map[string]any) {
	turnID := getTurnID(payload)
	if sessionID == "" || turnID == "" {
		return
	}
	a.store.SetTurn(sessionID, turnID)
}

func (a *App) shouldIgnoreTurnPayload(sessionID string, payload map[string]any) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	if session.Runtime.Turn.Active || session.Runtime.Turn.Phase != "aborted" {
		return false
	}
	turnID := getTurnID(payload)
	if turnID != "" && session.TurnID != "" {
		return turnID == session.TurnID
	}
	threadID := getStringAny(payload, "threadId", "thread_id")
	return threadID != "" && threadID == session.ThreadID
}

func (a *App) cardIsFinal(sessionID, cardID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if card.ID != cardID {
			continue
		}
		return normalizeCardStatus(card.Status) != "inProgress"
	}
	return false
}

func (a *App) sessionIDFromPayload(payload map[string]any) string {
	if sessionID := a.store.SessionIDByThread(getStringAny(payload, "threadId", "thread_id")); sessionID != "" {
		return sessionID
	}
	if sessionID := a.store.SessionIDByTurn(getTurnID(payload)); sessionID != "" {
		return sessionID
	}
	activeSessionID := ""
	for _, sessionID := range a.store.SessionIDs() {
		session := a.store.Session(sessionID)
		if session == nil || !session.Runtime.Turn.Active {
			continue
		}
		if activeSessionID != "" {
			return ""
		}
		activeSessionID = sessionID
	}
	return activeSessionID
}

func (a *App) updateActivityFromItem(sessionID string, item map[string]any, completed bool) {
	kind, entry, currentLabel, ok := detectActivitySignal(item)
	if !ok {
		return
	}

	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		switch kind {
		case "file_read":
			if completed {
				if runtime.Activity.CurrentReadingFile == currentLabel {
					runtime.Activity.CurrentReadingFile = ""
				}
				appendUniqueActivityEntry(&runtime.Activity.ViewedFiles, entry, func(existing, next model.ActivityEntry) bool {
					return existing.Path != "" && existing.Path == next.Path
				})
				runtime.Activity.FilesViewed = len(runtime.Activity.ViewedFiles)
				return
			}
			runtime.Activity.CurrentReadingFile = currentLabel
		case "web_search":
			if completed {
				if runtime.Activity.CurrentWebSearchQuery == currentLabel {
					runtime.Activity.CurrentWebSearchQuery = ""
				}
				appendUniqueActivityEntry(&runtime.Activity.SearchedWebQueries, entry, func(existing, next model.ActivityEntry) bool {
					return existing.Query != "" && existing.Query == next.Query
				})
				runtime.Activity.SearchCount = len(runtime.Activity.SearchedWebQueries)
				return
			}
			runtime.Activity.CurrentWebSearchQuery = currentLabel
		case "list":
			if completed {
				runtime.Activity.ListCount++
			}
		}
	})
}

func (a *App) syncProcessLineCard(sessionID, itemID string, item map[string]any, completed bool) {
	kind, entry, currentLabel, ok := detectActivitySignal(item)
	if !ok {
		return
	}

	cardID := "process-" + itemID
	now := model.NowString()
	existing := a.cardByID(sessionID, cardID)
	createdAt := now
	if existing != nil && existing.CreatedAt != "" {
		createdAt = existing.CreatedAt
	}

	status := "inProgress"
	durationMS := int64(0)
	if completed {
		status = "completed"
		durationMS = durationBetween(createdAt, now)
	}

	a.store.UpsertCard(sessionID, model.Card{
		ID:         cardID,
		Type:       "ProcessLineCard",
		Text:       processLineText(kind, entry, currentLabel, completed),
		Status:     status,
		DurationMS: durationMS,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	})

	if completed {
		return
	}
}

func (a *App) markTurnInterrupted(sessionID, turnID string) {
	now := model.NowString()
	a.finishRuntimeTurn(sessionID, "aborted")
	a.finalizeOpenTurnCards(sessionID, "failed")
	a.resolvePendingTurnRequests(sessionID, now)
	cardID := model.NewID("notice")
	if turnID != "" {
		cardID = "turn-aborted-" + turnID
	}
	a.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "NoticeCard",
		Title:     "任务已中断",
		Text:      "任务已中断",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (a *App) cleanBackgroundTerminals(threadID string) {
	a.cleanBackgroundTerminalsWithTimeout(threadID, 5*time.Second)
}

func (a *App) cleanBackgroundTerminalsWithTimeout(threadID string, timeout time.Duration) {
	if strings.TrimSpace(threadID) == "" {
		return
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var result map[string]any
	if err := a.codex.Request(ctx, "thread/backgroundTerminals/clean", map[string]any{
		"threadId": threadID,
	}, &result); err != nil {
		log.Printf("background terminal cleanup skipped thread=%s err=%s", threadID, truncate(err.Error(), 200))
	}
}

func (a *App) finalizeOpenTurnCards(sessionID, finalStatus string) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}

	now := model.NowString()
	for _, existing := range session.Cards {
		if normalizeCardStatus(existing.Status) != "inProgress" && existing.Status != "pending" {
			continue
		}
		switch existing.Type {
		case "CommandCard", "FileChangeCard", "ProcessLineCard", "CommandApprovalCard", "FileChangeApprovalCard", "ChoiceCard":
			cardID := existing.ID
			durationMS := durationBetween(existing.CreatedAt, now)
			a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
				card.Status = finalStatus
				if card.DurationMS == 0 {
					card.DurationMS = durationMS
				}
				card.UpdatedAt = now
			})
		}
	}
}

func (a *App) resolvePendingTurnRequests(sessionID, resolvedAt string) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	for approvalID, approval := range session.Approvals {
		if approval.Status == "pending" {
			a.store.ResolveApproval(sessionID, approvalID, "cancelled", resolvedAt)
		}
	}
	for choiceID, choice := range session.Choices {
		if choice.Status == "pending" {
			a.store.ResolveChoice(sessionID, choiceID, "cancelled", resolvedAt)
		}
	}
}

func (a *App) cardByID(sessionID, cardID string) *model.Card {
	session := a.store.Session(sessionID)
	if session == nil {
		return nil
	}
	for _, card := range session.Cards {
		if card.ID == cardID {
			copyCard := card
			return &copyCard
		}
	}
	return nil
}

func (a *App) cardDurationMS(sessionID, cardID, endedAt string) int64 {
	card := a.cardByID(sessionID, cardID)
	if card == nil {
		return 0
	}
	return durationBetween(card.CreatedAt, endedAt)
}

func processLineText(kind string, entry model.ActivityEntry, currentLabel string, completed bool) string {
	if completed {
		switch kind {
		case "file_read":
			return "已浏览 " + currentLabel
		case "web_search":
			return "已搜索网页（" + currentLabel + "）"
		case "web_open":
			return "已打开网页（" + currentLabel + "）"
		case "web_find":
			return "已页内查找（" + currentLabel + "）"
		case "list":
			return "已列出 " + currentLabel
		default:
			return strings.TrimSpace(entry.Label)
		}
	}
	switch kind {
	case "file_read":
		return "现在浏览 " + currentLabel
	case "web_search":
		return "现在搜索网页（" + currentLabel + "）"
	case "web_open":
		return "现在打开网页（" + currentLabel + "）"
	case "web_find":
		return "现在页内查找（" + currentLabel + "）"
	case "list":
		return "现在列出 " + currentLabel
	default:
		return strings.TrimSpace(entry.Label)
	}
}

func isTaskCompletionText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(value, "- ")))
	switch normalized {
	case "status: completed", "completed", "turn completed":
		return true
	default:
		return false
	}
}

func durationBetween(startedAt, endedAt string) int64 {
	if startedAt == "" || endedAt == "" {
		return 0
	}
	startTime, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return 0
	}
	endTime, err := time.Parse(time.RFC3339, endedAt)
	if err != nil {
		return 0
	}
	if endTime.Before(startTime) {
		return 0
	}
	return endTime.Sub(startTime).Milliseconds()
}

func autoApprovalNoticeText(approval model.ApprovalRequest) string {
	if approval.Type == "command" && approval.Command != "" {
		return fmt.Sprintf("已自动批准本会话内同类命令：%s", truncate(approval.Command, 72))
	}
	if approval.Type == "file_change" {
		return "已自动批准本会话内同类文件修改。"
	}
	return "已自动批准本会话内同类操作。"
}

func approvalGrantFromApproval(approval model.ApprovalRequest) model.ApprovalGrant {
	return model.ApprovalGrant{
		ID:          model.NewID("grant"),
		HostID:      approval.HostID,
		Type:        approval.Type,
		Fingerprint: approval.Fingerprint,
		Command:     approval.Command,
		Cwd:         approval.Cwd,
		CreatedAt:   model.NowString(),
	}
}

func mapApprovalDecision(decision string, approval model.ApprovalRequest) string {
	switch decision {
	case "accept", "accept_session":
		return "accept"
	case "decline":
		if slices.Contains(approval.Decisions, "decline") {
			return "decline"
		}
		if slices.Contains(approval.Decisions, "cancel") {
			return "cancel"
		}
	}
	return decision
}

func approvalStatusFromDecision(decision string) string {
	if decision == "accept_session" {
		return "accepted_for_session"
	}
	return decision
}

func approvalFingerprintForCommand(hostID, command, cwd string) string {
	return strings.Join([]string{"command", hostID, cwd, command}, "|")
}

func approvalFingerprintForFileChange(hostID, grantRoot string, changes []model.FileChange) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, change.Path+":"+change.Kind)
	}
	slices.Sort(parts)
	return strings.Join([]string{"file_change", hostID, grantRoot, strings.Join(parts, ",")}, "|")
}

func (a *App) ensureThread(ctx context.Context, sessionID string) (string, error) {
	session := a.store.EnsureSession(sessionID)
	if session.ThreadID != "" {
		return session.ThreadID, nil
	}
	selectedHostID := session.SelectedHostID
	if selectedHostID == "" {
		selectedHostID = model.ServerLocalHostID
	}

	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	err := a.codex.Request(ctx, "thread/start", map[string]any{
		"model":          "gpt-5.4",
		"cwd":            a.cfg.DefaultWorkspace,
		"approvalPolicy": "untrusted",
		"sandbox":        "workspace-write",
		"developerInstructions": fmt.Sprintf(strings.TrimSpace(`
You are embedded inside a web AI ops console.
Operate only on the selected host %q.
Use the working directory as the default root and keep writes inside it unless the user explicitly requests otherwise.
Summarize command results clearly for the web UI.
`), selectedHostID),
	}, &result)
	if err != nil {
		return "", err
	}
	a.store.SetThread(sessionID, result.Thread.ID)
	a.broadcastSnapshot(sessionID)
	return result.Thread.ID, nil
}

func (a *App) snapshot(sessionID string) model.Snapshot {
	snapshot := a.store.Snapshot(sessionID, model.UIConfig{
		OAuthConfigured: a.cfg.OAuthConfigured(),
		CodexAlive:      a.codex.Alive(),
	})
	snapshot.Runtime.Codex.RetryMax = 5
	if a.codex.Alive() {
		snapshot.Runtime.Codex.Status = "connected"
		snapshot.Runtime.Codex.LastError = ""
	} else {
		snapshot.Runtime.Codex.Status = "stopped"
		snapshot.Runtime.Codex.LastError = a.codex.LastExitError()
	}
	if snapshot.Runtime.Turn.Phase == "" {
		snapshot.Runtime.Turn.Phase = "idle"
	}
	if snapshot.Runtime.Turn.HostID == "" {
		snapshot.Runtime.Turn.HostID = snapshot.SelectedHostID
	}
	return snapshot
}

func (a *App) broadcastSnapshot(sessionID string) {
	snapshot := a.snapshot(sessionID)
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	for conn := range a.wsClients[sessionID] {
		if err := conn.WriteJSON(snapshot); err != nil {
			_ = conn.Close()
			delete(a.wsClients[sessionID], conn)
		}
	}
}

func (a *App) broadcastAllSnapshots() {
	for _, sessionID := range a.store.SessionIDs() {
		a.broadcastSnapshot(sessionID)
	}
}

func (a *App) serveFrontend() http.Handler {
	distPath := filepath.Join("web", "dist")
	if info, err := os.Stat(distPath); err == nil && info.IsDir() {
		return http.FileServer(http.Dir(distPath))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "frontend build not found; run `cd web && npm install && npm run dev` for development",
		})
	})
}

func (a *App) withSession(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		browserID := a.getOrCreateBrowserSessionID(w, r)
		sessionID := a.store.EnsureActiveSession(browserID)
		next(w, r, sessionID)
	}
}

func (a *App) withBrowserSession(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		browserID := a.getOrCreateBrowserSessionID(w, r)
		next(w, r, browserID)
	}
}

func (a *App) getOrCreateBrowserSessionID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(a.cfg.SessionCookieName); err == nil && cookie.Value != "" {
		if browserID, ok := a.verifySessionCookie(cookie.Value); ok {
			if a.store.BrowserSessionExists(browserID) {
				a.store.EnsureBrowserSession(browserID)
				return browserID
			}
			if a.store.SessionExists(browserID) {
				a.store.AttachLegacySessionToBrowser(browserID, browserID)
				return browserID
			}
			a.store.EnsureBrowserSession(browserID)
			return browserID
		}
	}
	browserID := model.NewID("browser")
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.SessionCookieName,
		Value:    a.signSessionCookie(browserID),
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(a.cfg.SessionCookieTTL),
		MaxAge:   int(a.cfg.SessionCookieTTL / time.Second),
		SameSite: http.SameSiteLaxMode,
	})
	a.store.EnsureBrowserSession(browserID)
	return browserID
}

func (a *App) syncAccountState(ctx context.Context, sessionID string) {
	if !a.codex.Alive() {
		return
	}
	currentAuth := a.store.Auth(sessionID)
	currentTokens := a.store.Tokens(sessionID)
	if !currentAuth.Connected && currentAuth.Mode != "apiKey" && currentTokens.AccessToken == "" {
		if imported, err := a.importLocalCodexAuth(ctx, sessionID); err != nil {
			log.Printf("local codex auth import skipped session=%s err=%s", sessionID, truncate(err.Error(), 200))
		} else if imported {
			currentAuth = a.store.Auth(sessionID)
			currentTokens = a.store.Tokens(sessionID)
		}
	}
	if !currentAuth.Connected && currentAuth.Mode != "apiKey" && currentTokens.AccessToken != "" {
		if restored, err := a.restoreStoredCodexAuth(ctx, sessionID, currentTokens, currentAuth.Mode); err != nil {
			log.Printf("stored codex auth restore skipped session=%s err=%s", sessionID, truncate(err.Error(), 200))
		} else if restored {
			currentAuth = a.store.Auth(sessionID)
			currentTokens = a.store.Tokens(sessionID)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result struct {
		Account            map[string]any `json:"account"`
		RequiresOpenAIAuth bool           `json:"requiresOpenaiAuth"`
	}
	refreshToken := currentAuth.Pending || !currentAuth.Connected
	if err := a.codex.Request(ctx, "account/read", map[string]any{"refreshToken": refreshToken}, &result); err != nil {
		log.Printf("account sync skipped session=%s err=%s", sessionID, truncate(err.Error(), 200))
		return
	}

	if result.RequiresOpenAIAuth || result.Account == nil {
		if !currentAuth.Connected && currentTokens.AccessToken != "" && currentAuth.Mode != "apiKey" {
			if restored, err := a.restoreStoredCodexAuth(ctx, sessionID, currentTokens, currentAuth.Mode); err != nil {
				log.Printf("stored codex auth retry failed session=%s err=%s", sessionID, truncate(err.Error(), 200))
			} else if restored {
				var retryResult struct {
					Account            map[string]any `json:"account"`
					RequiresOpenAIAuth bool           `json:"requiresOpenaiAuth"`
				}
				if err := a.codex.Request(ctx, "account/read", map[string]any{"refreshToken": false}, &retryResult); err == nil {
					result = retryResult
				}
			}
		}
	}

	if result.RequiresOpenAIAuth || result.Account == nil {
		if currentTokens.AccessToken != "" && currentAuth.Mode != "apiKey" {
			a.store.UpdateAuth(sessionID, func(auth *model.AuthState, tokens *model.ExternalAuthTokens) {
				auth.Connected = true
				auth.Pending = false
				if auth.Mode == "" {
					auth.Mode = "chatgptAuthTokens"
				}
				auth.LastError = ""
				if tokens.AccessToken == "" {
					*tokens = currentTokens
				}
			})
			return
		}
		if currentAuth.Pending {
			return
		}
		a.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
			auth.Connected = false
			auth.Pending = false
			auth.LastError = ""
		})
		return
	}

	accountType := getString(result.Account, "type")
	email := getString(result.Account, "email")
	planType := getString(result.Account, "planType")
	log.Printf("account sync session=%s connected=true type=%q email=%q planType=%q refreshToken=%t", sessionID, accountType, email, planType, refreshToken)
	a.store.UpdateAuth(sessionID, func(auth *model.AuthState, tokens *model.ExternalAuthTokens) {
		auth.Connected = true
		auth.Pending = false
		if accountType != "" {
			auth.Mode = accountType
		}
		if planType != "" {
			auth.PlanType = planType
		}
		if email != "" {
			auth.Email = email
			tokens.Email = email
		}
		auth.LastError = ""
	})
}

func (a *App) monitorHosts(ctx context.Context) {
	interval := a.cfg.AgentHeartbeatTimeout / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := a.store.MarkStaleHosts(a.cfg.AgentHeartbeatTimeout)
			if len(changed) == 0 {
				continue
			}
			for _, hostID := range changed {
				log.Printf("host-agent timeout host_id=%s marked offline", hostID)
				a.audit("agent.timeout", map[string]any{
					"hostId": hostID,
				})
			}
			a.broadcastAllSnapshots()
		}
	}
}

func (a *App) importLocalCodexAuth(ctx context.Context, sessionID string) (bool, error) {
	authPath := filepath.Join(a.cfg.CodexHome, "auth.json")
	content, err := os.ReadFile(authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	var payload struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
			IDToken     string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return false, err
	}
	if payload.Tokens.AccessToken == "" || payload.Tokens.AccountID == "" {
		return false, nil
	}

	mode := payload.AuthMode
	if mode == "" {
		mode = "chatgptAuthTokens"
	}
	return a.restoreStoredCodexAuth(ctx, sessionID, model.ExternalAuthTokens{
		IDToken:          payload.Tokens.IDToken,
		AccessToken:      payload.Tokens.AccessToken,
		ChatGPTAccountID: payload.Tokens.AccountID,
	}, mode)
}

func (a *App) restoreStoredCodexAuth(ctx context.Context, sessionID string, tokens model.ExternalAuthTokens, mode string) (bool, error) {
	if tokens.AccessToken == "" || tokens.ChatGPTAccountID == "" {
		return false, nil
	}

	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result map[string]any
	if err := a.codex.Request(requestCtx, "account/login/start", map[string]any{
		"type":             "chatgptAuthTokens",
		"accessToken":      tokens.AccessToken,
		"chatgptAccountId": tokens.ChatGPTAccountID,
		"chatgptPlanType":  emptyToNil(tokens.ChatGPTPlanType),
	}, &result); err != nil {
		return false, err
	}

	if mode == "" {
		mode = "chatgptAuthTokens"
	}
	a.store.SetAuth(sessionID, model.AuthState{
		Connected: true,
		Mode:      mode,
		PlanType:  tokens.ChatGPTPlanType,
		Email:     tokens.Email,
	}, tokens)
	log.Printf("local codex auth imported session=%s codexHome=%s", sessionID, a.cfg.CodexHome)
	return true, nil
}

func (a *App) signSessionCookie(sessionID string) string {
	return sessionID + "." + a.signatureForSession(sessionID)
}

func (a *App) verifySessionCookie(value string) (string, bool) {
	sessionID, signature, ok := strings.Cut(value, ".")
	if !ok || sessionID == "" || signature == "" {
		return "", false
	}
	expected := a.signatureForSession(sessionID)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return "", false
	}
	return sessionID, true
}

func (a *App) signatureForSession(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	_, _ = mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) audit(event string, fields map[string]any) {
	if a.cfg.AuditLogPath == "" {
		return
	}
	record := map[string]any{
		"ts":    model.NowString(),
		"event": event,
	}
	for key, value := range fields {
		record[key] = value
	}

	content, err := json.Marshal(record)
	if err != nil {
		log.Printf("audit marshal failed event=%s err=%s", event, truncate(err.Error(), 200))
		return
	}

	a.auditMu.Lock()
	defer a.auditMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(a.cfg.AuditLogPath), 0o755); err != nil {
		log.Printf("audit mkdir failed path=%s err=%s", a.cfg.AuditLogPath, truncate(err.Error(), 200))
		return
	}
	file, err := os.OpenFile(a.cfg.AuditLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("audit open failed path=%s err=%s", a.cfg.AuditLogPath, truncate(err.Error(), 200))
		return
	}
	defer file.Close()

	if _, err := file.Write(append(content, '\n')); err != nil {
		log.Printf("audit write failed path=%s err=%s", a.cfg.AuditLogPath, truncate(err.Error(), 200))
	}
}

type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	Email       string `json:"email"`
}

func (a *App) exchangeOAuthCode(ctx context.Context, code string) (oauthTokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", a.cfg.OAuthClientID)
	values.Set("client_secret", a.cfg.OAuthClientSecret)
	values.Set("redirect_uri", a.cfg.OAuthRedirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.OAuthTokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return oauthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	if resp.StatusCode >= 300 {
		return oauthTokenResponse{}, fmt.Errorf("oauth token exchange failed: %s", bytes.TrimSpace(body))
	}
	var tokenResp oauthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return oauthTokenResponse{}, err
	}
	if tokenResp.AccessToken == "" {
		return oauthTokenResponse{}, errors.New("oauth token response missing access_token")
	}
	return tokenResp, nil
}

func (a *App) fetchOAuthEmail(ctx context.Context, accessToken string) string {
	if a.cfg.OAuthUserInfoURL == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.OAuthUserInfoURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "upn"} {
		if value := getString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func (a *App) findHost(hostID string) model.Host {
	for _, host := range a.store.Hosts() {
		if host.ID == hostID {
			return host
		}
	}
	return model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: false,
	}
}

func toPlanItems(raw any) []model.PlanItem {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	items := make([]model.PlanItem, 0, len(list))
	for _, entry := range list {
		stepMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, model.PlanItem{
			Step:   getString(stepMap, "step"),
			Status: getString(stepMap, "status"),
		})
	}
	return items
}

func toChoiceQuestions(raw any) []model.ChoiceQuestion {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	questions := make([]model.ChoiceQuestion, 0, len(list))
	for _, entry := range list {
		questionMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		questions = append(questions, model.ChoiceQuestion{
			Header:   getString(questionMap, "header"),
			Question: getString(questionMap, "question"),
			IsOther:  getBool(questionMap, "isOther"),
			IsSecret: getBool(questionMap, "isSecret"),
			Options:  toChoiceOptions(questionMap["options"]),
		})
	}
	return questions
}

func toChoiceOptions(raw any) []model.ChoiceOption {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	options := make([]model.ChoiceOption, 0, len(list))
	for _, entry := range list {
		switch value := entry.(type) {
		case string:
			options = append(options, model.ChoiceOption{
				Label: value,
				Value: value,
			})
		case map[string]any:
			label := getString(value, "label")
			if label == "" {
				label = getString(value, "value")
			}
			optionValue := getString(value, "value")
			if optionValue == "" {
				optionValue = label
			}
			options = append(options, model.ChoiceOption{
				Label:       label,
				Value:       optionValue,
				Description: getString(value, "description"),
			})
		}
	}
	return options
}

func toChanges(raw any) []model.FileChange {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	changes := make([]model.FileChange, 0, len(list))
	for _, entry := range list {
		changeMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		changes = append(changes, model.FileChange{
			Path: getString(changeMap, "path"),
			Kind: kindLabel(changeMap["kind"]),
			Diff: getString(changeMap, "diff"),
		})
	}
	return changes
}

func choiceCardTitle(questions []model.ChoiceQuestion) string {
	if len(questions) == 0 {
		return "需要你的输入"
	}
	if len(questions) == 1 {
		if questions[0].Header != "" {
			return questions[0].Header
		}
	}
	return "需要你的输入"
}

func choiceAnswerSummary(questions []model.ChoiceQuestion, answers []choiceAnswerInput) []string {
	summary := make([]string, 0, len(answers))
	for index, answer := range answers {
		label := strings.TrimSpace(answer.Label)
		if label == "" {
			label = strings.TrimSpace(answer.Value)
		}
		if label == "" {
			continue
		}
		if index < len(questions) && questions[index].Header != "" {
			summary = append(summary, questions[index].Header+": "+label)
			continue
		}
		summary = append(summary, label)
	}
	return summary
}

func getTurnID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turnID := getStringAny(payload, "turnId", "turn_id"); turnID != "" {
		return turnID
	}
	turn := getMap(payload, "turn")
	return getString(turn, "id")
}

func kindLabel(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]any:
		for key := range value {
			return key
		}
	}
	return ""
}

func defaultHostID(hostID string) string {
	if hostID == "" {
		return model.ServerLocalHostID
	}
	return hostID
}

func normalizeCardStatus(status string) string {
	switch status {
	case "", "running":
		return "inProgress"
	case "in_progress", "inProgress", "pending":
		return "inProgress"
	case "completed", "success", "accepted", "accepted_for_session", "accepted_for_session_auto":
		return "completed"
	case "failed", "error", "decline", "declined", "cancelled", "canceled", "aborted", "interrupted":
		return "failed"
	default:
		return status
	}
}

func completedItemStatus(item map[string]any) string {
	status := normalizeCardStatus(getString(item, "status"))
	if status != "inProgress" {
		return status
	}
	return "completed"
}

func completedCommandStatus(item map[string]any, output string) string {
	exitCode, ok := getIntAny(item, "exitCode", "exit_code")
	if ok && exitCode != 0 {
		return "failed"
	}
	if commandOutputLooksFailed(output) {
		return "failed"
	}
	return completedItemStatus(item)
}

func commandOutputLooksFailed(output string) bool {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)
	strongSignals := []string{
		"operation not permitted",
		"permission denied",
		"command not found",
		"no such file or directory",
		"is not recognized as an internal or external command",
		"unknown option",
		"illegal option",
		"invalid option",
		"traceback (most recent call last):",
	}
	for _, signal := range strongSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}

	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "zsh:") || strings.HasPrefix(line, "bash:") || strings.HasPrefix(line, "sh:") {
			return true
		}
		if strings.HasPrefix(line, "python: can't open file") || strings.HasPrefix(line, "npm err!") {
			return true
		}
	}

	return false
}

type stringHit struct {
	Key   string
	Value string
}

func detectActivitySignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	if protocolKind, protocolEntry, protocolLabel, protocolOK := detectProtocolActivitySignal(item); protocolOK {
		return protocolKind, protocolEntry, protocolLabel, true
	}

	hits := make([]stringHit, 0, 24)
	collectStringHits("", item, &hits)

	descriptors := make([]string, 0, len(hits))
	var filePath string
	var query string
	for _, hit := range hits {
		key := strings.ToLower(hit.Key)
		value := strings.TrimSpace(hit.Value)
		if value == "" {
			continue
		}
		lowerValue := strings.ToLower(value)
		if isDescriptorKey(key) {
			descriptors = append(descriptors, lowerValue)
		}
		if filePath == "" && isFilePathKey(key) && looksLikePath(value) {
			filePath = value
		}
		if query == "" && isQueryKey(key) {
			query = value
		}
		if query == "" && (strings.Contains(lowerValue, "search the web:") || strings.Contains(lowerValue, "search_query")) {
			query = strings.TrimSpace(strings.TrimPrefix(value, "Search the web:"))
		}
	}

	descriptorText := strings.Join(descriptors, " | ")
	switch {
	case query != "" && isWebSearchDescriptor(descriptorText):
		return "web_search", model.ActivityEntry{
			Label: "Search the web: " + query,
			Query: query,
		}, query, true
	case filePath != "" && isListDescriptor(descriptorText):
		return "list", model.ActivityEntry{
			Label: "List " + filePath,
			Path:  filePath,
		}, filepath.Base(filePath), true
	case filePath != "" && isReadDescriptor(descriptorText):
		display := filepath.Base(filePath)
		if display == "." || display == "/" || display == "" {
			display = filePath
		}
		return "file_read", model.ActivityEntry{
			Label: "Read " + display,
			Path:  filePath,
		}, display, true
	default:
		return "", model.ActivityEntry{}, "", false
	}
}

func detectProtocolActivitySignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	switch strings.ToLower(getString(item, "type")) {
	case "websearch":
		return detectWebSearchSignal(item)
	default:
		return "", model.ActivityEntry{}, "", false
	}
}

func detectWebSearchSignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	action := getMap(item, "action")
	actionType := strings.ToLower(getString(action, "type"))
	query := strings.TrimSpace(getString(action, "query"))
	if query == "" {
		query = strings.TrimSpace(getString(item, "query"))
	}
	if query == "" {
		query = firstNonEmptyString(toStringSlice(action["queries"]))
	}

	switch actionType {
	case "", "search":
		if query == "" {
			return "", model.ActivityEntry{}, "", false
		}
		return "web_search", model.ActivityEntry{
			Label: "Search the web: " + query,
			Query: query,
		}, query, true
	case "openpage":
		rawURL := strings.TrimSpace(getString(action, "url"))
		if rawURL == "" {
			return "", model.ActivityEntry{}, "", false
		}
		display := summarizeWebLocation(rawURL)
		return "web_open", model.ActivityEntry{
			Label: "Open web page: " + rawURL,
			Query: rawURL,
		}, display, true
	case "findinpage":
		pattern := strings.TrimSpace(getString(action, "pattern"))
		rawURL := strings.TrimSpace(getString(action, "url"))
		if pattern == "" && rawURL == "" {
			return "", model.ActivityEntry{}, "", false
		}
		display := pattern
		if display == "" {
			display = summarizeWebLocation(rawURL)
		}
		label := "Find in page: " + display
		if rawURL != "" && pattern != "" {
			label = "Find in page: " + pattern + " @ " + rawURL
		}
		return "web_find", model.ActivityEntry{
			Label: label,
			Query: display,
		}, display, true
	default:
		if query == "" {
			return "", model.ActivityEntry{}, "", false
		}
		return "web_search", model.ActivityEntry{
			Label: "Search the web: " + query,
			Query: query,
		}, query, true
	}
}

func collectStringHits(prefix string, raw any, hits *[]stringHit) {
	switch value := raw.(type) {
	case map[string]any:
		for key, entry := range value {
			nextKey := key
			if prefix != "" {
				nextKey = prefix + "." + key
			}
			collectStringHits(nextKey, entry, hits)
		}
	case []any:
		for _, entry := range value {
			collectStringHits(prefix, entry, hits)
		}
	case string:
		*hits = append(*hits, stringHit{Key: prefix, Value: value})
	}
}

func appendUniqueActivityEntry(entries *[]model.ActivityEntry, entry model.ActivityEntry, match func(model.ActivityEntry, model.ActivityEntry) bool) {
	for _, existing := range *entries {
		if match(existing, entry) {
			return
		}
	}
	*entries = append(*entries, entry)
}

func isDescriptorKey(key string) bool {
	return strings.HasSuffix(key, "type") ||
		strings.HasSuffix(key, "title") ||
		strings.HasSuffix(key, "kind") ||
		strings.HasSuffix(key, "name") ||
		strings.HasSuffix(key, "label") ||
		strings.HasSuffix(key, "action") ||
		strings.HasSuffix(key, "tool") ||
		strings.HasSuffix(key, "toolname") ||
		strings.HasSuffix(key, "method")
}

func isFilePathKey(key string) bool {
	return (strings.Contains(key, "path") || strings.Contains(key, "file") || strings.Contains(key, "filename")) &&
		!strings.Contains(key, "cwd") &&
		!strings.Contains(key, "grantroot")
}

func isQueryKey(key string) bool {
	return strings.HasSuffix(key, "query") ||
		strings.HasSuffix(key, "searchquery") ||
		strings.HasSuffix(key, ".q") ||
		strings.HasSuffix(key, "pattern")
}

func looksLikePath(value string) bool {
	return strings.Contains(value, "/") ||
		strings.HasPrefix(value, "~") ||
		(strings.Contains(filepath.Base(value), ".") && !strings.Contains(value, " "))
}

func isWebSearchDescriptor(text string) bool {
	return strings.Contains(text, "search the web") ||
		strings.Contains(text, "search_query") ||
		strings.Contains(text, "websearch") ||
		strings.Contains(text, "web_search") ||
		strings.Contains(text, "web search")
}

func isListDescriptor(text string) bool {
	return descriptorHasToken(text, "list") ||
		descriptorHasToken(text, "glob") ||
		descriptorHasToken(text, "directory") ||
		descriptorHasToken(text, "ls")
}

func isReadDescriptor(text string) bool {
	if strings.Contains(text, "filechange") || strings.Contains(text, "file change") || strings.Contains(text, "edit") || strings.Contains(text, "write") {
		return false
	}
	return descriptorHasToken(text, "read") ||
		descriptorHasToken(text, "open") ||
		descriptorHasToken(text, "view")
}

func descriptorHasToken(text, token string) bool {
	return slices.ContainsFunc(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), func(field string) bool {
		return field == token
	})
}

func firstNonEmptyString(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func summarizeWebLocation(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return parsed.Host
	}
	return parsed.Host + "/" + path
}

func getMap(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return value
}

func getString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func getStringAny(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := getString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func getBool(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func getFloat(payload map[string]any, key string) float64 {
	value, _ := payload[key].(float64)
	return value
}

func getIntAny(payload map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case int:
			return value, true
		case int32:
			return int(value), true
		case int64:
			return int(value), true
		case float64:
			return int(value), true
		}
	}
	return 0, false
}

func toStringSlice(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, entry := range list {
		if value, ok := entry.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
