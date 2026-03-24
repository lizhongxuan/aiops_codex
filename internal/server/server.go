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
	"strings"
	"sync"
	"time"

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
	oauthMu     sync.Mutex
	oauthStates map[string]string
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
	httpMux.HandleFunc("/api/v1/state", a.withSession(a.handleState))
	httpMux.HandleFunc("/api/v1/auth/login", a.withSession(a.handleAuthLogin))
	httpMux.HandleFunc("/api/v1/auth/logout", a.withSession(a.handleAuthLogout))
	httpMux.HandleFunc("/api/v1/auth/oauth/start", a.withSession(a.handleOAuthStart))
	httpMux.HandleFunc("/api/v1/auth/oauth/callback", a.withSession(a.handleOAuthCallback))
	httpMux.HandleFunc("/api/v1/chat/message", a.withSession(a.handleChatMessage))
	httpMux.HandleFunc("/api/v1/approvals/", a.withSession(a.handleApprovalDecision))
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

func (a *App) handleState(w http.ResponseWriter, r *http.Request, sessionID string) {
	a.store.EnsureSession(sessionID)
	a.store.TouchSession(sessionID)
	a.syncAccountState(r.Context(), sessionID)
	writeJSON(w, http.StatusOK, a.snapshot(sessionID))
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
			a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
			a.broadcastAllSnapshots()
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		a.store.SetPendingLogin(sessionID, result.LoginID)
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

	userCard := model.Card{
		ID:        model.NewID("msg"),
		Type:      "MessageCard",
		Role:      "user",
		Text:      req.Message,
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	a.store.UpsertCard(sessionID, userCard)
	a.broadcastSnapshot(sessionID)

	threadID, err := a.ensureThread(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var result map[string]any
	err = a.codex.Request(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"cwd":            a.cfg.DefaultWorkspace,
		"approvalPolicy": "on-request",
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
		resultCard := model.Card{
			ID:        model.NewID("result"),
			Type:      "ResultCard",
			Title:     "Turn failed",
			Text:      err.Error(),
			Status:    "failed",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		}
		a.store.UpsertCard(sessionID, resultCard)
		a.broadcastSnapshot(sessionID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
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

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := a.codex.Respond(ctx, approval.RequestIDRaw, map[string]any{
		"decision": decision,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	a.store.ResolveApproval(sessionID, approvalID, decision, model.NowString())
	if approval.Type == "command" {
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Status = decision
			card.UpdatedAt = model.NowString()
		})
	} else {
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Status = decision
			card.UpdatedAt = model.NowString()
		})
	}
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
		if _, _, err := conn.ReadMessage(); err != nil {
			return
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
	case "turn/plan/updated":
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
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
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		itemID := getString(payload, "itemId")
		now := model.NowString()
		a.store.UpsertCard(sessionID, model.Card{
			ID:        itemID,
			Type:      "MessageCard",
			Role:      "assistant",
			Status:    "inProgress",
			CreatedAt: now,
			UpdatedAt: now,
		})
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Text += getString(payload, "delta")
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
	case "item/commandExecution/outputDelta":
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		itemID := getString(payload, "itemId")
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Output += getString(payload, "delta")
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
	case "item/fileChange/outputDelta":
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		itemID := getString(payload, "itemId")
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Output += getString(payload, "delta")
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
	case "item/completed":
		a.handleItemCompleted(payload)
	case "serverRequest/resolved":
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		a.broadcastSnapshot(sessionID)
	case "turn/completed":
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		turn := getMap(payload, "turn")
		card := model.Card{
			ID:        "result-" + getString(turn, "id"),
			Type:      "ResultCard",
			Title:     "Turn completed",
			Text:      "status: " + getString(turn, "status"),
			Status:    getString(turn, "status"),
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		}
		a.store.UpsertCard(sessionID, card)
		log.Printf("turn completed session=%s turn=%s status=%s", sessionID, getString(turn, "id"), getString(turn, "status"))
		a.broadcastSnapshot(sessionID)
	case "error":
		sessionID := a.store.SessionIDs()
		text := getString(getMap(payload, "error"), "message")
		for _, id := range sessionID {
			card := model.Card{
				ID:        model.NewID("result"),
				Type:      "ResultCard",
				Title:     "Error",
				Text:      text,
				Status:    "failed",
				CreatedAt: model.NowString(),
				UpdatedAt: model.NowString(),
			}
			a.store.UpsertCard(id, card)
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
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		approval := model.ApprovalRequest{
			ID:           model.NewID("approval"),
			RequestIDRaw: string(rawID),
			Type:         "command",
			Status:       "pending",
			ThreadID:     getString(payload, "threadId"),
			TurnID:       getString(payload, "turnId"),
			ItemID:       getString(payload, "itemId"),
			Command:      getString(payload, "command"),
			Cwd:          getString(payload, "cwd"),
			Reason:       getString(payload, "reason"),
			Decisions:    toStringSlice(payload["availableDecisions"]),
			RequestedAt:  model.NowString(),
		}
		log.Printf("approval requested type=command session=%s item=%s command=%q", sessionID, approval.ItemID, approval.Command)
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
		sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
		if sessionID == "" {
			return
		}
		itemID := getString(payload, "itemId")
		cachedItem := a.store.Item(sessionID, itemID)
		approval := model.ApprovalRequest{
			ID:           model.NewID("approval"),
			RequestIDRaw: string(rawID),
			Type:         "file_change",
			Status:       "pending",
			ThreadID:     getString(payload, "threadId"),
			TurnID:       getString(payload, "turnId"),
			ItemID:       itemID,
			Reason:       getString(payload, "reason"),
			GrantRoot:    getString(payload, "grantRoot"),
			Changes:      toChanges(cachedItem["changes"]),
			Decisions:    []string{"accept", "decline"},
			RequestedAt:  model.NowString(),
		}
		log.Printf("approval requested type=file_change session=%s item=%s", sessionID, itemID)
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
	}
}

func (a *App) handleItemStarted(payload map[string]any) {
	sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
	if sessionID == "" {
		return
	}
	item := getMap(payload, "item")
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	a.store.RememberItem(sessionID, itemID, item)

	now := model.NowString()
	switch itemType {
	case "commandExecution":
		card := model.Card{
			ID:        itemID,
			Type:      "StepCard",
			Title:     "Command execution",
			Command:   getString(item, "command"),
			Cwd:       getString(item, "cwd"),
			Status:    getString(item, "status"),
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
	case "fileChange":
		card := model.Card{
			ID:        itemID,
			Type:      "StepCard",
			Title:     "File change",
			Status:    getString(item, "status"),
			Changes:   toChanges(item["changes"]),
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
	case "agentMessage":
		card := model.Card{
			ID:        itemID,
			Type:      "MessageCard",
			Role:      "assistant",
			Status:    "inProgress",
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) handleItemCompleted(payload map[string]any) {
	sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
	if sessionID == "" {
		return
	}
	item := getMap(payload, "item")
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	a.store.RememberItem(sessionID, itemID, item)

	switch itemType {
	case "agentMessage":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = "completed"
			if card.Text == "" {
				card.Text = getString(item, "text")
			}
			card.UpdatedAt = model.NowString()
		})
	case "commandExecution":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = getString(item, "status")
			if output := getString(item, "aggregatedOutput"); output != "" && card.Output == "" {
				card.Output = output
			}
			card.UpdatedAt = model.NowString()
		})
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("result"),
			Type:      "ResultCard",
			Title:     "Command result",
			Text:      fmt.Sprintf("exit code: %.0f", getFloat(item, "exitCode")),
			Status:    getString(item, "status"),
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
	case "fileChange":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = getString(item, "status")
			card.Changes = toChanges(item["changes"])
			card.UpdatedAt = model.NowString()
		})
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("result"),
			Type:      "ResultCard",
			Title:     "File change result",
			Text:      fmt.Sprintf("%d file changes", len(toChanges(item["changes"]))),
			Status:    getString(item, "status"),
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
	}
	a.broadcastSnapshot(sessionID)
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
		"approvalPolicy": "on-request",
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
	return a.store.Snapshot(sessionID, model.UIConfig{
		OAuthConfigured: a.cfg.OAuthConfigured(),
		CodexAlive:      a.codex.Alive(),
	})
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
		sessionID := a.getOrCreateSessionID(w, r)
		next(w, r, sessionID)
	}
}

func (a *App) getOrCreateSessionID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(a.cfg.SessionCookieName); err == nil && cookie.Value != "" {
		if sessionID, ok := a.verifySessionCookie(cookie.Value); ok {
			a.store.EnsureSession(sessionID)
			return sessionID
		}
	}
	sessionID := model.NewID("sess")
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.SessionCookieName,
		Value:    a.signSessionCookie(sessionID),
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(a.cfg.SessionCookieTTL),
		MaxAge:   int(a.cfg.SessionCookieTTL / time.Second),
		SameSite: http.SameSiteLaxMode,
	})
	a.store.EnsureSession(sessionID)
	return sessionID
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
		if currentTokens.AccessToken != "" && currentAuth.Mode != "apiKey" {
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

func getMap(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return value
}

func getString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func getBool(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func getFloat(payload map[string]any, key string) float64 {
	value, _ := payload[key].(float64)
	return value
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
