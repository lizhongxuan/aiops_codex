import { defineStore } from "pinia";

function normalizeCardText(card) {
  const candidates = [card?.text, card?.message, card?.summary, card?.title];
  for (const candidate of candidates) {
    const text = (candidate || "").trim().replace(/\s+/g, " ");
    if (text) return text;
  }
  return "";
}

function isUserCard(card) {
  return card?.type === "UserMessageCard" || (card?.type === "MessageCard" && card?.role === "user");
}

function isAssistantCard(card) {
  return card?.type === "AssistantMessageCard" || (card?.type === "MessageCard" && card?.role === "assistant");
}

function deriveSessionStatus(cards, runtime) {
  if (runtime?.turn?.active) {
    return runtime.turn.phase === "waiting_approval" ? "waiting_approval" : "running";
  }
  if (!cards?.length) return "empty";
  for (let i = cards.length - 1; i >= 0; i -= 1) {
    const card = cards[i];
    if (card?.type === "ErrorCard" || card?.status === "failed") return "failed";
    if (isUserCard(card) || isAssistantCard(card) || card?.type === "NoticeCard") break;
  }
  return "completed";
}

function deriveSessionSummary(snapshot, runtime) {
  const cards = snapshot?.cards || [];
  let title = "New Thread";
  let preview = "暂无消息";
  let messageCount = 0;

  for (const card of cards) {
    if (isUserCard(card) || isAssistantCard(card)) {
      messageCount += 1;
    }
    if (title === "New Thread" && isUserCard(card)) {
      const text = normalizeCardText(card);
      if (text) title = text.slice(0, 24);
    }
  }

  for (let i = cards.length - 1; i >= 0; i -= 1) {
    const text = normalizeCardText(cards[i]);
    if (text) {
      preview = text.slice(0, 60);
      break;
    }
  }

  return {
    id: snapshot?.sessionId || "",
    title,
    preview,
    selectedHostId: snapshot?.selectedHostId || "server-local",
    status: deriveSessionStatus(cards, runtime),
    messageCount,
    lastActivityAt: snapshot?.lastActivityAt || "",
  };
}

export const useAppStore = defineStore("app", {
  state: () => ({
    snapshot: {
      sessionId: "",
      selectedHostId: "server-local",
      auth: {
        connected: false,
        pending: false,
        mode: "",
        planType: "",
        email: "",
        lastError: "",
      },
      hosts: [],
      cards: [],
      approvals: [],
      config: {
        oauthConfigured: false,
        codexAlive: false,
      },
    },
    /* Turn-level and connection-level runtime state */
    runtime: {
      turn: {
        active: false,
        phase: "idle", // idle | thinking | planning | waiting_approval | waiting_input | executing | finalizing | completed | failed | aborted
        hostId: "",
        startedAt: null,
      },
      codex: {
        status: "connected", // connected | reconnecting | disconnected | stopped
        retryAttempt: 0,
        retryMax: 5,
        lastError: "",
      },
      activity: {
        filesViewed: 0,
        searchCount: 0,
        searchLocationCount: 0,
        listCount: 0,
        commandsRun: 0,
        filesChanged: 0,
        currentReadingFile: "",
        currentChangingFile: "",
        currentListingPath: "",
        currentSearchKind: "",
        currentSearchQuery: "",
        viewedFiles: [],
        currentWebSearchQuery: "",
        searchedWebQueries: [],
        searchedContentQueries: [],
      },
    },
    authForm: {
      mode: "chatgpt",
      apiKey: "",
      accessToken: "",
      chatgptAccountId: "",
      chatgptPlanType: "",
      email: "",
    },
    settings: {
      quota: "",
      model: "gpt-4-turbo",
      reasoningEffort: "medium",
      models: [],
    },
    sessionList: [],
    activeSessionId: "",
    historyLoading: false,
    loading: true,
    errorMessage: "",
    sending: false,
    wsStatus: "disconnected",
  }),
  getters: {
    selectedHost: (state) => {
      return (
        state.snapshot.hosts.find((host) => host.id === state.snapshot.selectedHostId) || {
          id: state.snapshot.selectedHostId,
          name: state.snapshot.selectedHostId,
          status: "offline",
          executable: false,
          terminalCapable: false,
        }
      );
    },
    canSend: (state) => {
      const host = (
        state.snapshot.hosts.find((h) => h.id === state.snapshot.selectedHostId) || {
          executable: false,
          terminalCapable: false,
          status: "offline",
        }
      );
      return (
        state.snapshot.auth.connected &&
        state.snapshot.config.codexAlive !== false &&
        host.executable &&
        host.status === "online"
      );
    },
    canOpenTerminal: (state) => {
      const host = (
        state.snapshot.hosts.find((h) => h.id === state.snapshot.selectedHostId) || {
          executable: false,
          terminalCapable: false,
          status: "offline",
        }
      );
      return host.status === "online" && (host.terminalCapable || host.executable);
    },
    activeSessionSummary: (state) => {
      return state.sessionList.find((session) => session.id === state.activeSessionId) || null;
    },
  },
  actions: {
    applySnapshot(data) {
      this.snapshot.sessionId = data.sessionId || this.snapshot.sessionId;
      this.activeSessionId = this.snapshot.sessionId;
      this.snapshot.selectedHostId = data.selectedHostId || this.snapshot.selectedHostId;
      this.snapshot.auth = data.auth || this.snapshot.auth;
      this.snapshot.hosts = data.hosts || [];
      this.snapshot.cards = data.cards || [];
      this.snapshot.approvals = data.approvals || [];
      this.snapshot.config = data.config || this.snapshot.config;
      /* Merge runtime if server sends it */
      if (data.runtime) {
        this.runtime.turn = {
          active: false,
          phase: "idle",
          hostId: this.snapshot.selectedHostId || "server-local",
          startedAt: null,
          ...(data.runtime.turn || {}),
        };
        this.runtime.codex = {
          status: "connected",
          retryAttempt: this.runtime.codex.retryAttempt,
          retryMax: 5,
          lastError: "",
          ...(data.runtime.codex || {}),
        };
        this.runtime.activity = {
          filesViewed: 0,
          searchCount: 0,
          searchLocationCount: 0,
          listCount: 0,
          commandsRun: 0,
          filesChanged: 0,
          currentReadingFile: "",
          currentChangingFile: "",
          currentListingPath: "",
          currentSearchKind: "",
          currentSearchQuery: "",
          viewedFiles: [],
          currentWebSearchQuery: "",
          searchedWebQueries: [],
          searchedContentQueries: [],
          ...(data.runtime.activity || {}),
        };
      }
      const summary = deriveSessionSummary(this.snapshot, this.runtime);
      const index = this.sessionList.findIndex((session) => session.id === summary.id);
      if (index >= 0) {
        this.sessionList[index] = { ...this.sessionList[index], ...summary };
      }
      this.loading = false;
    },
    applySessions(data) {
      this.activeSessionId = data.activeSessionId || this.activeSessionId || this.snapshot.sessionId;
      this.sessionList = data.sessions || [];
    },
    setTurnPhase(phase) {
      this.runtime.turn.active = phase !== "idle" && phase !== "completed" && phase !== "failed" && phase !== "aborted";
      this.runtime.turn.phase = phase;
    },
    resetActivity() {
      this.runtime.activity = {
        filesViewed: 0,
        searchCount: 0,
        searchLocationCount: 0,
        listCount: 0,
        commandsRun: 0,
        filesChanged: 0,
        currentReadingFile: "",
        currentChangingFile: "",
        currentListingPath: "",
        currentSearchKind: "",
        currentSearchQuery: "",
        viewedFiles: [],
        currentWebSearchQuery: "",
        searchedWebQueries: [],
        searchedContentQueries: [],
      };
    },
    async fetchState() {
      try {
        const response = await fetch("/api/v1/state", { credentials: "include" });
        const data = await response.json();
        this.applySnapshot(data);
      } catch (e) {
        console.error("Failed to fetch state:", e);
      }
    },
    async fetchSessions() {
      this.historyLoading = true;
      try {
        const response = await fetch("/api/v1/sessions", { credentials: "include" });
        const data = await response.json();
        if (!response.ok) {
          this.errorMessage = data.error || "load sessions failed";
          return false;
        }
        this.applySessions(data);
        return true;
      } catch (e) {
        console.error("Failed to fetch sessions:", e);
        this.errorMessage = "Load sessions failed";
        return false;
      } finally {
        this.historyLoading = false;
      }
    },
    async fetchSettings() {
      try {
        const response = await fetch("/api/v1/settings", { credentials: "include" });
        if (response.ok) {
          const data = await response.json();
          this.settings = { ...this.settings, ...data };
        }
      } catch (e) {
        console.error("Failed to fetch settings:", e);
      }
    },
    async updateSettings(newSettings) {
      try {
        const response = await fetch("/api/v1/settings", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "include",
          body: JSON.stringify(newSettings),
        });
        if (response.ok) {
          const data = await response.json();
          this.settings = { ...this.settings, ...data };
        } else {
          // Fallback update in case API is completely mocked
          this.settings = { ...this.settings, ...newSettings };
        }
      } catch (e) {
        console.error("Failed to update settings:", e);
        this.settings = { ...this.settings, ...newSettings }; // Mock fallback
      }
    },
    async resetThread() {
      try {
        const response = await fetch("/api/v1/thread/reset", {
          method: "POST",
          credentials: "include",
        });
        const data = await response.json();
        if (!response.ok) {
          this.errorMessage = data.error || "new thread failed";
          return false;
        }
        this.errorMessage = "";
        this.resetActivity();
        this.setTurnPhase("idle");
        await this.fetchState();
        await this.fetchSessions();
        return true;
      } catch (e) {
        console.error("Failed to reset thread:", e);
        this.errorMessage = "New thread failed";
        return false;
      }
    },
    clearSocketTimers() {
      if (this._pingInterval) {
        window.clearInterval(this._pingInterval);
        this._pingInterval = null;
      }
      if (this._heartbeatTimer) {
        window.clearTimeout(this._heartbeatTimer);
        this._heartbeatTimer = null;
      }
    },
    disconnectWs() {
      const socket = this._socket;
      this._socket = null;
      this.clearSocketTimers();
      if (socket) {
        try {
          socket.close();
        } catch (e) {
          console.error("Failed to close websocket:", e);
        }
      }
    },
    reconnectWs() {
      this.disconnectWs();
      this.runtime.codex.retryAttempt = 0;
      this.connectWs();
    },
    async createSession() {
      if (this.runtime.turn.active) {
        this.errorMessage = "当前任务执行中，完成后再新建会话";
        return false;
      }
      try {
        const response = await fetch("/api/v1/sessions", {
          method: "POST",
          credentials: "include",
        });
        const data = await response.json();
        if (!response.ok) {
          this.errorMessage = data.error || "create session failed";
          return false;
        }
        this.errorMessage = "";
        this.applySessions(data);
        if (data.snapshot) {
          this.applySnapshot(data.snapshot);
        } else {
          await this.fetchState();
        }
        this.resetActivity();
        this.setTurnPhase("idle");
        this.reconnectWs();
        return true;
      } catch (e) {
        console.error("Failed to create session:", e);
        this.errorMessage = "Create session failed";
        return false;
      }
    },
    async activateSession(sessionId) {
      if (!sessionId || sessionId === this.activeSessionId) {
        return true;
      }
      if (this.runtime.turn.active) {
        this.errorMessage = "当前任务执行中，完成后再切换会话";
        return false;
      }
      try {
        const response = await fetch(`/api/v1/sessions/${sessionId}/activate`, {
          method: "POST",
          credentials: "include",
        });
        const data = await response.json();
        if (!response.ok) {
          this.errorMessage = data.error || "switch session failed";
          return false;
        }
        this.errorMessage = "";
        this.applySessions(data);
        if (data.snapshot) {
          this.applySnapshot(data.snapshot);
        } else {
          await this.fetchState();
        }
        this.reconnectWs();
        return true;
      } catch (e) {
        console.error("Failed to activate session:", e);
        this.errorMessage = "Switch session failed";
        return false;
      }
    },
    connectWs() {
      this.disconnectWs();
      const protocol = window.location.protocol === "https:" ? "wss" : "ws";
      const socket = new WebSocket(`${protocol}://${window.location.host}/ws`);
      const touchHeartbeat = () => {
        if (this._heartbeatTimer) {
          window.clearTimeout(this._heartbeatTimer);
        }
        this._heartbeatTimer = window.setTimeout(() => {
          if (this._socket === socket && socket.readyState === WebSocket.OPEN) {
            this.runtime.codex.lastError = "heartbeat timeout";
            socket.close();
          }
        }, 45000);
      };
      this.wsStatus = "connecting";
      this.runtime.codex.status = "reconnecting";
      this._socket = socket;

      socket.onopen = () => {
        if (this._socket !== socket) return;
        this.wsStatus = "connected";
        this.runtime.codex.status = "connected";
        this.runtime.codex.retryAttempt = 0;
        this.runtime.codex.lastError = "";
        touchHeartbeat();
        this._pingInterval = window.setInterval(() => {
          if (this._socket !== socket || socket.readyState !== WebSocket.OPEN) return;
          socket.send(JSON.stringify({ type: "ping" }));
        }, 10000);
      };

      socket.onmessage = (event) => {
        if (this._socket !== socket) return;
        touchHeartbeat();
        try {
          const data = JSON.parse(event.data);
          if (data?.type === "heartbeat") {
            return;
          }
          this.applySnapshot(data);
        } catch (e) {
          console.error("Failed to parse websocket message:", e);
        }
      };

      socket.onclose = () => {
        if (this._socket !== socket) return;
        this.clearSocketTimers();
        this._socket = null;
        this.wsStatus = "disconnected";
        this.runtime.codex.retryAttempt += 1;

        if (this.runtime.codex.retryAttempt > this.runtime.codex.retryMax) {
          this.runtime.codex.status = "stopped";
          this.wsStatus = "error";
          if (!this.runtime.codex.lastError) {
            this.runtime.codex.lastError = "connection closed";
          }
          return;
        }
        this.runtime.codex.status = "reconnecting";
        window.setTimeout(() => this.connectWs(), 1000);
      };

      socket.onerror = () => {
        if (this._socket !== socket) return;
        this.wsStatus = "error";
        this.runtime.codex.lastError = "connection error";
      };
    },
    selectHost(hostId) {
      this.snapshot.selectedHostId = hostId;
    },
  },
});
