<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useAppStore } from "./store";
import LoginModal from "./components/LoginModal.vue";
import HostModal from "./components/HostModal.vue";
import SettingsModal from "./components/SettingsModal.vue";
import SessionHistoryDrawer from "./components/SessionHistoryDrawer.vue";
import { MessageSquarePlusIcon, AppWindowIcon, SettingsIcon, UserCircleIcon, ServerIcon, PanelsTopLeftIcon, TerminalIcon, HistoryIcon, EraserIcon } from "lucide-vue-next";

const store = useAppStore();
const router = useRouter();

const isLoginModalOpen = ref(false);
const isHostModalOpen = ref(false);
const isSettingsModalOpen = ref(false);
const isMcpDrawerOpen = ref(false);
const isHistoryDrawerOpen = ref(false);
let noticeTimer = null;

function toggleMcpDrawer() {
  isMcpDrawerOpen.value = !isMcpDrawerOpen.value;
}

const authBadgeLabel = computed(() => {
  if (store.snapshot.auth.connected) {
    return `GPT ${store.snapshot.auth.planType || "接入"}`;
  }
  if (store.snapshot.auth.pending) {
    return "登录中";
  }
  return "未登录";
});

async function startNewThread() {
  if (store.sending) return;
  const ok = await store.createSession();
  if (!ok) return;
  store.errorMessage = "";
  store.noticeMessage = "";
  isHistoryDrawerOpen.value = false;
}

async function openHistoryDrawer() {
  await store.fetchSessions();
  isHistoryDrawerOpen.value = true;
}

async function switchSession(sessionId) {
  const ok = await store.activateSession(sessionId);
  if (ok) {
    isHistoryDrawerOpen.value = false;
  }
}

function handleGlobalKeydown(e) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "n") {
    e.preventDefault();
    startNewThread();
  }
}

function openTerminal() {
  if (!store.canOpenTerminal) return;
  router.push(`/terminal/${store.snapshot.selectedHostId}`);
}

const currentSession = computed(() => {
  return store.activeSessionSummary || {
    title: "新建会话",
    status: "empty",
  };
});

const canResetCurrentSession = computed(() => {
  if (store.loading || store.sending || store.runtime.turn.active) return false;
  return store.snapshot.cards.length > 0 || (store.snapshot.approvals || []).length > 0;
});

const resetButtonTitle = computed(() => {
  if (store.loading) return "正在加载当前会话";
  if (store.runtime.turn.active) return "当前任务执行中，暂不能清空上下文";
  if (!canResetCurrentSession.value) return "当前会话已经是空白";
  return "清空当前会话的消息、审批和运行态";
});

function pushNotice(message) {
  store.noticeMessage = message;
  if (noticeTimer) {
    window.clearTimeout(noticeTimer);
  }
  noticeTimer = window.setTimeout(() => {
    store.noticeMessage = "";
    noticeTimer = null;
  }, 3000);
}

async function resetCurrentSession() {
  if (!canResetCurrentSession.value) return;
  const confirmed = window.confirm("确认清空当前会话上下文吗？这会移除当前会话的消息、审批和运行态，其他历史会话不会受影响。");
  if (!confirmed) return;
  const ok = await store.resetThread();
  if (!ok) return;
  pushNotice("已清空当前会话上下文");
}

const currentSessionStatus = computed(() => {
  if (store.runtime.turn.active) return "执行中";
  if (currentSession.value.status === "failed") return "失败";
  if (currentSession.value.status === "completed") return "已保存";
  return "空白";
});

onMounted(() => {
  store.fetchState();
  store.fetchSessions();
  store.connectWs();
  window.addEventListener("keydown", handleGlobalKeydown);
});

onBeforeUnmount(() => {
  store.disconnectWs();
  window.removeEventListener("keydown", handleGlobalKeydown);
  if (noticeTimer) {
    window.clearTimeout(noticeTimer);
  }
});
</script>

<template>
  <div class="app-layout">
    <!-- Left Sidebar: Navigation & Threads -->
    <aside class="app-sidebar">
      <div class="sidebar-top">
        <div class="sidebar-actions">
          <button class="nav-button new-thread" @click="startNewThread">
            <MessageSquarePlusIcon size="18" />
            <span>新建会话</span>
            <span class="shortcut">⌘ N</span>
          </button>
          <button class="nav-button secondary" @click="openHistoryDrawer">
            <HistoryIcon size="18" />
            <span>历史会话</span>
          </button>
        </div>
      </div>

      <div class="sidebar-scroll">
        <div class="nav-group">
          <div class="nav-group-title">会话</div>
          <button class="nav-item" :class="{ active: $route.name === 'chat' }" @click="router.push('/')">
            <AppWindowIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">{{ currentSession.title }}</span>
              <span class="nav-item-time">{{ currentSessionStatus }}</span>
            </div>
          </button>
        </div>

        <div class="nav-group">
          <div class="nav-group-title">终端</div>
          <button class="nav-item" :class="{ active: $route.name === 'terminal' }" @click="openTerminal">
            <TerminalIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">{{ store.selectedHost.name }}</span>
              <span class="nav-item-time">
                <span class="pill-dot-inline" :class="store.selectedHost.status"></span>
                {{ store.selectedHost.status }}
              </span>
            </div>
          </button>
        </div>
      </div>

      <div class="sidebar-bottom">
        <button class="nav-icon-btn" title="Settings" @click="isSettingsModalOpen = true">
          <SettingsIcon size="20" />
        </button>
        <div class="flex-spacer"></div>
        <div class="ws-badge" :class="store.wsStatus" :title="'WS: ' + store.wsStatus"></div>
      </div>
    </aside>

    <!-- Main Canvas -->
    <main class="app-main">
      <header class="main-header">
        <div class="header-left">
          <h1 class="header-title">Codex Workspace</h1>
        </div>
        
        <div class="header-right">
          <button class="header-pill subtle-pill" :disabled="!canResetCurrentSession" :title="resetButtonTitle" @click="resetCurrentSession">
            <EraserIcon size="14" />
            <span class="pill-text">清空上下文</span>
          </button>

          <button class="header-pill" :disabled="!store.canOpenTerminal" @click="openTerminal" title="打开终端">
            <TerminalIcon size="14" />
            <span class="pill-text">终端</span>
          </button>

          <button class="header-pill" @click="isHostModalOpen = true">
            <ServerIcon size="14" />
            <span class="pill-text">{{ store.selectedHost.name }}</span>
            <span class="pill-dot" :class="store.selectedHost.status"></span>
          </button>
          
          <button class="header-pill auth-pill" @click="isLoginModalOpen = true" :class="{'connected': store.snapshot.auth.connected}">
            <UserCircleIcon size="16" />
            <span class="pill-text">{{ authBadgeLabel }}</span>
          </button>
          
          <button class="header-icon-btn" @click="toggleMcpDrawer" :class="{ 'is-active': isMcpDrawerOpen }" title="Skills & MCP">
            <PanelsTopLeftIcon size="20" />
          </button>
        </div>
      </header>

      <!-- Router View: ChatPage or TerminalPage -->
      <router-view />
    </main>

    <!-- Right Drawer: MCP & Core Panel -->
    <aside class="app-mcp-drawer" :class="{ 'is-open': isMcpDrawerOpen }">
      <div class="mcp-header">
        <h3>Skills & MCP</h3>
      </div>
      <div class="mcp-body">
         <p class="subtle" style="font-size:13px">No skills configured yet.</p>
      </div>
    </aside>

    <SessionHistoryDrawer
      v-if="isHistoryDrawerOpen"
      :sessions="store.sessionList"
      :active-session-id="store.activeSessionId"
      :loading="store.historyLoading"
      :switching-disabled="store.runtime.turn.active"
      @close="isHistoryDrawerOpen = false"
      @create="startNewThread"
      @select="switchSession"
    />

    <!-- Modals -->
    <LoginModal v-if="isLoginModalOpen" @close="isLoginModalOpen = false" />
    <HostModal v-if="isHostModalOpen" @close="isHostModalOpen = false" />
    <SettingsModal v-if="isSettingsModalOpen" @close="isSettingsModalOpen = false" />
  </div>
</template>

<style scoped>
.pill-dot-inline {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-right: 4px;
  vertical-align: middle;
}
.pill-dot-inline.online { background: #22c55e; }
.pill-dot-inline.offline { background: #94a3b8; }
</style>
