<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import CardItem from "./components/CardItem.vue";

const snapshot = reactive({
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
});

const authForm = reactive({
  mode: "chatgpt",
  apiKey: "",
  accessToken: "",
  chatgptAccountId: "",
  chatgptPlanType: "",
  email: "",
});

const composer = reactive({
  message: "",
});

const loading = ref(true);
const errorMessage = ref("");
const sending = ref(false);
const wsStatus = ref("disconnected");

let socket;

const selectedHostId = computed({
  get() {
    return snapshot.selectedHostId || "server-local";
  },
  set(value) {
    snapshot.selectedHostId = value;
  },
});

async function fetchState() {
  const response = await fetch("/api/v1/state", {
    credentials: "include",
  });
  const data = await response.json();
  applySnapshot(data);
}

function applySnapshot(data) {
  snapshot.sessionId = data.sessionId;
  snapshot.selectedHostId = data.selectedHostId;
  snapshot.auth = data.auth || snapshot.auth;
  snapshot.hosts = data.hosts || [];
  snapshot.cards = data.cards || [];
  snapshot.approvals = data.approvals || [];
  snapshot.config = data.config || snapshot.config;
  loading.value = false;
}

function connectWs() {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  socket = new WebSocket(`${protocol}://${window.location.host}/ws`);
  wsStatus.value = "connecting";

  socket.onopen = () => {
    wsStatus.value = "connected";
  };

  socket.onmessage = (event) => {
    applySnapshot(JSON.parse(event.data));
  };

  socket.onclose = () => {
    wsStatus.value = "disconnected";
    window.setTimeout(connectWs, 1000);
  };

  socket.onerror = () => {
    wsStatus.value = "error";
  };
}

async function login() {
  errorMessage.value = "";
  const response = await fetch("/api/v1/auth/login", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(authForm),
  });
  const data = await response.json();
  if (!response.ok) {
    errorMessage.value = data.error || "login failed";
    return;
  }
  if (data.authUrl) {
    window.open(data.authUrl, "_blank", "noopener,noreferrer");
  }
}

async function logout() {
  await fetch("/api/v1/auth/logout", {
    method: "POST",
    credentials: "include",
  });
}

function startConfiguredOAuth() {
  window.location.href = "/api/v1/auth/oauth/start";
}

async function sendMessage() {
  if (!canSend.value || !composer.message.trim()) {
    return;
  }
  sending.value = true;
  errorMessage.value = "";
  const response = await fetch("/api/v1/chat/message", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      message: composer.message,
      hostId: selectedHostId.value,
    }),
  });
  const data = await response.json();
  sending.value = false;
  if (!response.ok) {
    errorMessage.value = data.error || "message send failed";
    return;
  }
  composer.message = "";
}

async function decideApproval({ approvalId, decision }) {
  const response = await fetch(`/api/v1/approvals/${approvalId}/decision`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ decision }),
  });
  const data = await response.json();
  if (!response.ok) {
    errorMessage.value = data.error || "approval failed";
  }
}

function selectHost(hostId) {
  selectedHostId.value = hostId;
}

const loginHint = computed(() => {
  const params = new URLSearchParams(window.location.search);
  return params.get("login");
});

const selectedHost = computed(() => {
  return (
    snapshot.hosts.find((host) => host.id === selectedHostId.value) || {
      id: selectedHostId.value,
      name: selectedHostId.value,
      status: "offline",
      executable: false,
    }
  );
});

const authBadgeLabel = computed(() => {
  if (snapshot.auth.connected) {
    return `GPT ${snapshot.auth.planType || "已登录"}`;
  }
  if (snapshot.auth.pending) {
    return "GPT 登录中";
  }
  return "GPT 未登录";
});

const canSend = computed(() => {
  return (
    snapshot.auth.connected &&
    snapshot.config.codexAlive !== false &&
    selectedHost.value.executable &&
    selectedHost.value.status === "online"
  );
});

const composerPlaceholder = computed(() => {
  if (!snapshot.auth.connected) {
    return "请先登录 GPT 账号后再开始对话";
  }
  if (!snapshot.config.codexAlive) {
    return "Codex app-server 当前不可用";
  }
  if (!selectedHost.value.executable) {
    return "当前主机仅展示，不支持执行";
  }
  if (selectedHost.value.status !== "online") {
    return "当前主机离线，暂时不可执行";
  }
  return "要求后续变更";
});

function onComposerKeydown(event) {
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    event.preventDefault();
    sendMessage();
  }
}

onMounted(async () => {
  await fetchState();
  connectWs();
});

onBeforeUnmount(() => {
  if (socket) {
    socket.close();
  }
});
</script>

<template>
  <div class="page">
    <aside class="sidebar">
      <section class="panel">
        <div class="panel-header">
          <h2>GPT 登录</h2>
          <span class="badge" :data-state="snapshot.auth.connected ? 'on' : 'off'">
            {{ snapshot.auth.connected ? "已连接" : "未连接" }}
          </span>
        </div>

        <p v-if="loginHint" class="hint">最近一次登录结果: {{ loginHint }}</p>
        <p v-if="snapshot.auth.email" class="subtle">账号: {{ snapshot.auth.email }}</p>
        <p v-if="snapshot.auth.mode" class="subtle">模式: {{ snapshot.auth.mode }}</p>
        <p v-if="snapshot.auth.planType" class="subtle">计划: {{ snapshot.auth.planType }}</p>
        <p v-if="snapshot.auth.pending" class="hint">登录流程进行中，请在浏览器完成认证。</p>
        <p v-if="snapshot.auth.lastError" class="error">{{ snapshot.auth.lastError }}</p>

        <div class="form-row" v-if="snapshot.config.oauthConfigured">
          <button class="primary" @click="startConfiguredOAuth">使用已配置 OAuth 登录</button>
        </div>

        <div class="divider"></div>

        <div class="field">
          <label>登录模式</label>
          <select v-model="authForm.mode">
            <option value="chatgpt">ChatGPT 登录</option>
            <option value="chatgptAuthTokens">外部 Auth Tokens</option>
            <option value="apiKey">API Key</option>
          </select>
        </div>

        <div v-if="authForm.mode === 'chatgptAuthTokens'" class="stack">
          <div class="field">
            <label>Access Token</label>
            <textarea v-model="authForm.accessToken" rows="3"></textarea>
          </div>
          <div class="field">
            <label>ChatGPT Account ID</label>
            <input v-model="authForm.chatgptAccountId" />
          </div>
          <div class="field">
            <label>Plan Type</label>
            <input v-model="authForm.chatgptPlanType" placeholder="plus / pro / team" />
          </div>
          <div class="field">
            <label>Email</label>
            <input v-model="authForm.email" placeholder="optional" />
          </div>
        </div>

        <div v-if="authForm.mode === 'apiKey'" class="stack">
          <div class="field">
            <label>API Key</label>
            <input v-model="authForm.apiKey" type="password" />
          </div>
          <div class="field">
            <label>Email</label>
            <input v-model="authForm.email" placeholder="optional" />
          </div>
        </div>

        <div class="form-row">
          <button class="primary" @click="login">Connect GPT</button>
          <button @click="logout">Logout</button>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">
          <h2>主机列表</h2>
          <span class="subtle">WS: {{ wsStatus }}</span>
        </div>

        <ul class="host-list">
          <li
            v-for="host in snapshot.hosts"
            :key="host.id"
            class="host-item"
            :class="{ active: selectedHostId === host.id }"
            @click="selectHost(host.id)"
          >
            <div class="host-main">
              <strong>{{ host.name }}</strong>
              <span class="host-kind">{{ host.kind }}</span>
            </div>
            <div class="host-meta">
              <span class="badge" :data-state="host.status === 'online' ? 'on' : 'off'">
                {{ host.status }}
              </span>
              <span v-if="!host.executable" class="subtle">仅展示</span>
            </div>
          </li>
        </ul>
      </section>
    </aside>

    <main class="main">
      <section class="chat-workspace">
        <header class="chat-topbar">
          <div class="chat-topbar-copy">
            <h1>Codex Workspace</h1>
            <p class="subtle">像 Cursor Codex 一样的单列聊天工作区</p>
          </div>
          <div class="chat-topbar-pills">
            <span class="top-pill">{{ authBadgeLabel }}</span>
            <span class="top-pill">主机 {{ selectedHost.name }}</span>
            <span class="top-pill">WS {{ wsStatus }}</span>
          </div>
        </header>

        <div class="chat-scroll">
          <div class="chat-stream-shell">
            <p v-if="loading" class="hint">加载中...</p>

            <div v-if="!snapshot.cards.length && !loading" class="chat-empty">
              <p class="chat-empty-label">AIOps Codex</p>
              <h2>开始一个新任务</h2>
              <p>
                在底部输入框里给 Codex 下发任务，它会像 Cursor 插件里的聊天区一样，持续把消息、
                计划、步骤、审批和结果都堆叠在这里。
              </p>
            </div>

            <p v-if="loginHint" class="chat-banner hint">最近一次登录结果: {{ loginHint }}</p>
            <p v-if="errorMessage" class="chat-banner error">{{ errorMessage }}</p>
            <p v-if="!snapshot.auth.connected" class="chat-banner hint">
              先在左侧完成 GPT 登录，登录成功后才能向 Codex 发送任务。
            </p>
            <p v-if="snapshot.auth.connected && !snapshot.config.codexAlive" class="chat-banner error">
              Codex app-server 当前不可用，请先检查后端服务状态。
            </p>
            <p
              v-if="snapshot.auth.connected && snapshot.config.codexAlive && !selectedHost.executable"
              class="chat-banner hint"
            >
              当前主机仅在线展示，不支持在 MVP 阶段执行任务。
            </p>

            <div class="chat-stream">
              <div
                v-for="card in snapshot.cards"
                :key="card.id"
                class="stream-row"
                :class="{
                  'row-user': card.type === 'MessageCard' && card.role === 'user',
                  'row-assistant': !(card.type === 'MessageCard' && card.role === 'user'),
                }"
              >
                <CardItem :card="card" @approval="decideApproval" />
              </div>
            </div>
          </div>
        </div>

        <footer class="composer-dock">
          <div class="composer-card">
            <textarea
              v-model="composer.message"
              rows="4"
              :placeholder="composerPlaceholder"
              :disabled="!canSend || sending"
              @keydown="onComposerKeydown"
            ></textarea>

            <div class="composer-meta">
              <div class="composer-tools">
                <label class="tool-chip tool-select">
                  <span>主机</span>
                  <select v-model="selectedHostId">
                    <option
                      v-for="host in snapshot.hosts"
                      :key="host.id"
                      :value="host.id"
                      :disabled="!host.executable"
                    >
                      {{ host.name }}{{ host.executable ? "" : "（只读展示）" }}
                    </option>
                  </select>
                </label>
                <span class="tool-chip">{{ snapshot.auth.connected ? "GPT-5.4" : "等待登录" }}</span>
                <span class="tool-chip">默认工作区 ~/.aiops_codex</span>
              </div>

              <div class="composer-send">
                <span class="subtle">Cmd/Ctrl + Enter 发送</span>
                <button class="send-button" :disabled="!canSend || sending" @click="sendMessage">
                  {{ sending ? "…" : "↑" }}
                </button>
              </div>
            </div>
          </div>
        </footer>
      </section>
    </main>
  </div>
</template>
