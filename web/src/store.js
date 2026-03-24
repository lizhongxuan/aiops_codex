import { defineStore } from "pinia";

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
    authForm: {
      mode: "chatgpt",
      apiKey: "",
      accessToken: "",
      chatgptAccountId: "",
      chatgptPlanType: "",
      email: "",
    },
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
        }
      );
    },
    canSend: (state) => {
      const host = (
        state.snapshot.hosts.find((h) => h.id === state.snapshot.selectedHostId) || {
          executable: false,
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
  },
  actions: {
    applySnapshot(data) {
      this.snapshot.sessionId = data.sessionId || this.snapshot.sessionId;
      this.snapshot.selectedHostId = data.selectedHostId || this.snapshot.selectedHostId;
      this.snapshot.auth = data.auth || this.snapshot.auth;
      this.snapshot.hosts = data.hosts || [];
      this.snapshot.cards = data.cards || [];
      this.snapshot.approvals = data.approvals || [];
      this.snapshot.config = data.config || this.snapshot.config;
      this.loading = false;
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
        await this.fetchState();
        return true;
      } catch (e) {
        console.error("Failed to reset thread:", e);
        this.errorMessage = "New thread failed";
        return false;
      }
    },
    connectWs() {
      const protocol = window.location.protocol === "https:" ? "wss" : "ws";
      const socket = new WebSocket(`${protocol}://${window.location.host}/ws`);
      this.wsStatus = "connecting";

      socket.onopen = () => {
        this.wsStatus = "connected";
      };

      socket.onmessage = (event) => {
        try {
          this.applySnapshot(JSON.parse(event.data));
        } catch (e) {
          console.error("Failed to parse websocket message:", e);
        }
      };

      socket.onclose = () => {
        this.wsStatus = "disconnected";
        window.setTimeout(() => this.connectWs(), 1000);
      };

      socket.onerror = () => {
        this.wsStatus = "error";
      };
      
      this._socket = socket;
    },
    selectHost(hostId) {
      this.snapshot.selectedHostId = hostId;
    },
  },
});
