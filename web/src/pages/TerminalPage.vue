<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import { useRouter } from "vue-router";
import { useAppStore } from "../store";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import {
  ArrowLeftIcon,
  TerminalIcon,
  WifiIcon,
  WifiOffIcon,
  RotateCcwIcon,
  Trash2Icon,
  XIcon,
  InfoIcon,
} from "lucide-vue-next";

const props = defineProps({
  hostId: {
    type: String,
    required: true,
  },
});

const router = useRouter();
const store = useAppStore();

const termContainer = ref(null);
const isDrawerOpen = ref(false);
let term = null;
let fitAddon = null;
let ws = null;
const sessionId = ref("");
const connectionStatus = ref("connecting"); // connecting | connected | reconnecting | disconnected | error
const currentCwd = ref("~");
const currentShell = ref("/bin/zsh");
const startedAt = ref(null);

const hostInfo = computed(() => {
  return (
    store.snapshot.hosts.find((h) => h.id === props.hostId) || {
      id: props.hostId,
      name: props.hostId,
      status: "offline",
    }
  );
});

const isOnline = computed(() => hostInfo.value.status === "online");

function goBack() {
  router.push("/");
}

function toggleDrawer() {
  isDrawerOpen.value = !isDrawerOpen.value;
}

async function createSession() {
  connectionStatus.value = "connecting";
  try {
    const response = await fetch("/api/v1/terminal/sessions", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        hostId: props.hostId,
        cwd: "~",
        shell: "/bin/zsh",
        cols: term ? term.cols : 120,
        rows: term ? term.rows : 36,
      }),
    });
    if (!response.ok) {
      connectionStatus.value = "error";
      term?.write("\r\n\x1b[31m无法创建终端会话\x1b[0m\r\n");
      return;
    }
    const data = await response.json();
    sessionId.value = data.sessionId;
    currentCwd.value = data.cwd || currentCwd.value;
    currentShell.value = data.shell || currentShell.value;
    startedAt.value = data.startedAt || new Date().toISOString();
    connectTerminalWs(data.sessionId);
  } catch (e) {
    connectionStatus.value = "error";
    term?.write("\r\n\x1b[31m网络错误\x1b[0m\r\n");
  }
}

function connectTerminalWs(sid) {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${protocol}://${window.location.host}/api/v1/terminal/ws?sessionId=${sid}`);

  ws.onopen = () => {
    connectionStatus.value = "connected";
    term?.focus();
  };

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      switch (msg.type) {
        case "ready":
          connectionStatus.value = "connected";
          currentCwd.value = msg.cwd || currentCwd.value;
          currentShell.value = msg.shell || currentShell.value;
          startedAt.value = msg.startedAt || startedAt.value;
          break;
        case "output":
          term?.write(msg.data);
          break;
        case "exit":
          term?.write(`\r\n\x1b[90m[进程已退出，退出码: ${msg.code}]\x1b[0m\r\n`);
          connectionStatus.value = "disconnected";
          break;
        case "status":
          connectionStatus.value = msg.status;
          break;
        case "error":
          term?.write(`\r\n\x1b[31m${msg.message}\x1b[0m\r\n`);
          connectionStatus.value = "error";
          break;
      }
    } catch (e) {
      // Plain text fallback
      term?.write(event.data);
    }
  };

  ws.onclose = () => {
    connectionStatus.value = "disconnected";
  };

  ws.onerror = () => {
    connectionStatus.value = "error";
  };
}

function sendInput(data) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "input", data }));
  }
}

function sendSignal(signal) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "signal", signal }));
  }
}

function sendResize() {
  if (ws && ws.readyState === WebSocket.OPEN && term) {
    ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
  }
}

function clearScreen() {
  term?.clear();
}

function reconnect() {
  if (ws) {
    ws.close();
  }
  createSession();
}

function initTerminal() {
  if (!termContainer.value) return;

  term = new Terminal({
    theme: {
      background: "#0f172a",
      foreground: "#f8fafc",
      cursor: "#f8fafc",
      cursorAccent: "#0f172a",
      selection: "rgba(255, 255, 255, 0.2)",
    },
    fontFamily: '"SF Mono", "Fira Code", "Menlo", "Monaco", "Consolas", monospace',
    fontSize: 14,
    lineHeight: 1.4,
    cursorBlink: true,
    padding: 16,
  });

  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(termContainer.value);
  fitAddon.fit();

  // Forward user input to WS
  term.onData((data) => {
    sendInput(data);
  });

  // Handle window resize
  const resizeObserver = new ResizeObserver(() => {
    fitAddon?.fit();
    sendResize();
  });
  resizeObserver.observe(termContainer.value);

  term.write("正在连接到 " + props.hostId + " ...\r\n");
}

onMounted(() => {
  nextTick(() => {
    initTerminal();
    if (isOnline.value) {
      createSession();
    } else {
      term?.write("\r\n\x1b[33m主机当前离线，无法建立终端连接\x1b[0m\r\n");
      connectionStatus.value = "error";
    }
  });
});

onBeforeUnmount(() => {
  if (ws) {
    ws.close();
  }
  if (term) {
    term.dispose();
  }
});
</script>

<template>
  <div class="terminal-page">
    <!-- Top bar -->
    <header class="term-topbar">
      <div class="topbar-left">
        <button class="topbar-btn" @click="goBack" title="返回聊天">
          <ArrowLeftIcon size="16" />
        </button>
        <TerminalIcon size="16" class="topbar-icon" />
        <span class="topbar-host">{{ hostInfo.name }}</span>
        <span
          class="topbar-status-dot"
          :class="{
            online: connectionStatus === 'connected',
            connecting: connectionStatus === 'connecting' || connectionStatus === 'reconnecting',
            offline: connectionStatus === 'disconnected' || connectionStatus === 'error',
          }"
        ></span>
        <span class="topbar-status-text">{{ connectionStatus }}</span>
      </div>

      <div class="topbar-center">
        <span class="topbar-shell" v-if="currentShell">{{ currentShell }}</span>
      </div>

      <div class="topbar-right">
        <button class="topbar-btn" @click="sendSignal('SIGINT')" title="Ctrl+C">
          <XIcon size="14" />
          <span>Ctrl+C</span>
        </button>
        <button class="topbar-btn" @click="clearScreen" title="清屏">
          <Trash2Icon size="14" />
          <span>清屏</span>
        </button>
        <button class="topbar-btn" @click="reconnect" title="重新连接">
          <RotateCcwIcon size="14" />
          <span>重连</span>
        </button>
        <button class="topbar-btn" @click="toggleDrawer" title="信息">
          <InfoIcon size="14" />
        </button>
      </div>
    </header>

    <div class="term-main-area">
      <!-- Terminal body -->
      <div class="term-canvas" ref="termContainer"></div>

      <!-- Right drawer -->
      <aside class="term-drawer" v-if="isDrawerOpen">
        <div class="drawer-header">
          <h4>会话信息</h4>
        </div>
        <div class="drawer-body">
          <div class="info-row">
            <span class="info-label">会话 ID</span>
            <span class="info-value mono">{{ sessionId || '—' }}</span>
          </div>
          <div class="info-row">
            <span class="info-label">主机</span>
            <span class="info-value">{{ hostInfo.name }}</span>
          </div>
          <div class="info-row">
            <span class="info-label">状态</span>
            <span class="info-value">{{ connectionStatus }}</span>
          </div>
          <div class="info-row">
            <span class="info-label">启动时间</span>
            <span class="info-value mono">{{ startedAt || '—' }}</span>
          </div>
          <div class="info-row">
            <span class="info-label">Shell</span>
            <span class="info-value mono">{{ currentShell }}</span>
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.terminal-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #0f172a;
  color: #f8fafc;
}

/* Top bar */
.term-topbar {
  height: 48px;
  min-height: 48px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  background: #1e293b;
  border-bottom: 1px solid #334155;
  flex-shrink: 0;
}

.topbar-left,
.topbar-center,
.topbar-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.topbar-icon {
  color: #94a3b8;
}

.topbar-host {
  font-size: 14px;
  font-weight: 600;
  color: #e2e8f0;
}

.topbar-status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #94a3b8;
}

.topbar-status-dot.online {
  background: #22c55e;
  box-shadow: 0 0 6px rgba(34, 197, 94, 0.5);
}

.topbar-status-dot.connecting {
  background: #eab308;
  animation: pulse 1.5s ease-in-out infinite;
}

.topbar-status-dot.offline {
  background: #ef4444;
}

.topbar-status-text {
  font-size: 12px;
  color: #94a3b8;
}

.topbar-shell {
  font-size: 12px;
  color: #64748b;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.topbar-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 10px;
  border-radius: 6px;
  background: transparent;
  border: 1px solid #334155;
  color: #94a3b8;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
}

.topbar-btn:hover {
  background: #334155;
  color: #e2e8f0;
}

/* Main area */
.term-main-area {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.term-canvas {
  flex: 1;
  padding: 8px;
  overflow: hidden;
}

/* Drawer */
.term-drawer {
  width: 280px;
  min-width: 280px;
  background: #1e293b;
  border-left: 1px solid #334155;
  display: flex;
  flex-direction: column;
}

.drawer-header {
  padding: 16px;
  border-bottom: 1px solid #334155;
}

.drawer-header h4 {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: #e2e8f0;
}

.drawer-body {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.info-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.info-label {
  font-size: 11px;
  font-weight: 600;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.info-value {
  font-size: 13px;
  color: #e2e8f0;
  word-break: break-all;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
</style>
