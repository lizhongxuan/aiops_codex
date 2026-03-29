<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAppStore } from "./store";
import { resolveHostDisplay } from "./lib/hostDisplay";
import LoginModal from "./components/LoginModal.vue";
import HostModal from "./components/HostModal.vue";
import SettingsModal from "./components/SettingsModal.vue";
import SessionHistoryDrawer from "./components/SessionHistoryDrawer.vue";
import {
  MessageSquarePlusIcon,
  AppWindowIcon,
  SettingsIcon,
  UserCircleIcon,
  ServerIcon,
  PanelsTopLeftIcon,
  TerminalIcon,
  HistoryIcon,
  EraserIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
} from "lucide-vue-next";

const store = useAppStore();
const router = useRouter();
const route = useRoute();

const isLoginModalOpen = ref(false);
const isHostModalOpen = ref(false);
const isSettingsModalOpen = ref(false);
const isSettingsMenuOpen = ref(false);
const isMcpDrawerOpen = ref(false);
const isHistoryDrawerOpen = ref(false);
const isSidebarCollapsed = ref(false);
const settingsMenuRef = ref(null);
let noticeTimer = null;
const isRouteHostSyncing = ref(false);

function normalizeRouteValue(value) {
  if (Array.isArray(value)) {
    return (value[0] || "").trim();
  }
  return typeof value === "string" ? value.trim() : "";
}

function clearRouteHostQuery() {
  const nextQuery = { ...route.query };
  delete nextQuery.hostId;
  delete nextQuery.hostName;
  delete nextQuery.hostAddress;
  router.replace({ path: route.path, query: nextQuery });
}

function resolveRequestedHostId() {
  const requestedId = normalizeRouteValue(route.query.hostId);
  const requestedName = normalizeRouteValue(route.query.hostName);
  const requestedAddress = normalizeRouteValue(route.query.hostAddress);
  const candidates = [requestedId, requestedName, requestedAddress].filter(Boolean);

  if (!candidates.length) return "";

  const host = store.snapshot.hosts.find((item) =>
    candidates.some((candidate) => candidate === item.id || candidate === item.name || candidate === item.address),
  );

  return host?.id || requestedId;
}

async function syncChatRouteHostSelection() {
  if (route.name !== "chat" || isRouteHostSyncing.value) return;
  const requestedHostId = resolveRequestedHostId();
  if (!requestedHostId) return;
  if (store.loading || store.sending || store.runtime.turn.active) return;
  if (requestedHostId === store.snapshot.selectedHostId) {
    clearRouteHostQuery();
    return;
  }

  isRouteHostSyncing.value = true;
  try {
    await store.selectHost(requestedHostId);
    clearRouteHostQuery();
  } finally {
    isRouteHostSyncing.value = false;
  }
}

function toggleMcpDrawer() {
  isMcpDrawerOpen.value = !isMcpDrawerOpen.value;
}

function toggleSidebar() {
  isSidebarCollapsed.value = !isSidebarCollapsed.value;
}

function closeSettingsMenu() {
  isSettingsMenuOpen.value = false;
}

function openGeneralSettings() {
  closeSettingsMenu();
  isSettingsModalOpen.value = true;
}

function openAgentProfile() {
  closeSettingsMenu();
  router.push("/settings/agent");
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

function handleDocumentClick(e) {
  if (!settingsMenuRef.value) return;
  if (!settingsMenuRef.value.contains(e.target)) {
    closeSettingsMenu();
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

const selectedHostLabel = computed(() => {
  const host = store.selectedHost;
  return resolveHostDisplay(host) || "server-local";
});

const isSettingsRoute = computed(() => route.path.startsWith("/settings/"));

onMounted(() => {
  store.fetchState();
  store.fetchSessions();
  store.connectWs();
  window.addEventListener("keydown", handleGlobalKeydown);
  document.addEventListener("click", handleDocumentClick);
});

onBeforeUnmount(() => {
  store.disconnectWs();
  window.removeEventListener("keydown", handleGlobalKeydown);
  document.removeEventListener("click", handleDocumentClick);
  if (noticeTimer) {
    window.clearTimeout(noticeTimer);
  }
});

watch(
  () => [
    route.name,
    route.query.hostId,
    route.query.hostName,
    route.query.hostAddress,
    store.loading,
    store.sending,
    store.runtime.turn.active,
    store.snapshot.selectedHostId,
    store.snapshot.hosts.length,
  ],
  () => {
    void syncChatRouteHostSelection();
  },
  { immediate: true },
);
</script>

<template>
  <div class="app-layout">
    <template v-if="isSettingsRoute">
      <router-view />
    </template>
    <template v-else>
      <!-- Left Sidebar: Navigation & Threads -->
      <aside class="app-sidebar" :class="{ collapsed: isSidebarCollapsed }">
        <div class="sidebar-top">
          <div class="sidebar-toolbar">
            <button class="nav-icon-btn sidebar-collapse-btn" :title="isSidebarCollapsed ? '展开侧边栏' : '收起侧边栏'" @click="toggleSidebar">
              <PanelLeftOpenIcon v-if="isSidebarCollapsed" size="18" />
              <PanelLeftCloseIcon v-else size="18" />
            </button>
          </div>
          <div class="sidebar-actions">
            <button class="nav-button new-thread" @click="startNewThread">
              <MessageSquarePlusIcon size="18" />
              <span class="nav-label">新建会话</span>
              <span class="shortcut">⌘ N</span>
            </button>
            <button class="nav-button secondary" @click="openHistoryDrawer">
              <HistoryIcon size="18" />
              <span class="nav-label">历史会话</span>
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
            <div class="nav-group-title">运维工作台</div>
            <button class="nav-item" :class="{ active: $route.name === 'hosts' }" @click="router.push('/hosts')">
              <ServerIcon size="16" />
              <div class="nav-item-content">
                <span class="nav-item-title">主机管理</span>
                <span class="nav-item-time">Inventory & Scope</span>
              </div>
            </button>

            <button class="nav-item" :class="{ active: $route.name === 'experience-packs' }" @click="router.push('/experience-packs')">
              <HistoryIcon size="16" />
              <div class="nav-item-content">
                <span class="nav-item-title">经验包库</span>
                <span class="nav-item-time">Packs & Playbooks</span>
              </div>
            </button>

            <button class="nav-item" :class="{ active: $route.name === 'protocol' }" @click="router.push('/protocol')">
              <PanelsTopLeftIcon size="16" />
              <div class="nav-item-content">
                <span class="nav-item-title">协议工作台</span>
                <span class="nav-item-time">DAG & Sub-agents</span>
              </div>
            </button>
          </div>

          <div class="nav-group">
            <div class="nav-group-title">终端</div>
            <button class="nav-item" :class="{ active: $route.name === 'terminal' }" @click="openTerminal">
              <TerminalIcon size="16" />
              <div class="nav-item-content">
                <span class="nav-item-title">{{ selectedHostLabel }}</span>
                <span class="nav-item-time">
                  <span class="pill-dot-inline" :class="store.selectedHost.status"></span>
                  {{ store.selectedHost.status }}
                </span>
              </div>
            </button>
          </div>
        </div>

        <div class="sidebar-bottom">
          <div ref="settingsMenuRef" class="settings-menu">
            <button class="nav-icon-btn" title="Settings" @click.stop="isSettingsMenuOpen = !isSettingsMenuOpen">
              <SettingsIcon size="20" />
            </button>
            <div v-if="isSettingsMenuOpen" class="settings-menu-popover" @click.stop>
              <button class="settings-menu-item" @click="openGeneralSettings">
                <span class="settings-menu-title">通用设置</span>
                <span class="settings-menu-subtitle">账户、模型与默认参数</span>
              </button>
              <button class="settings-menu-item" @click="openAgentProfile">
                <span class="settings-menu-title">Agent Profile</span>
                <span class="settings-menu-subtitle">System Prompt / Permissions / Skills / MCP</span>
              </button>
            </div>
          </div>
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
              <span class="pill-text">{{ selectedHostLabel }}</span>
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
    </template>
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

.settings-menu {
  position: relative;
}

.settings-menu-popover {
  position: absolute;
  left: 0;
  bottom: calc(100% + 10px);
  width: 260px;
  padding: 8px;
  border-radius: 16px;
  background: rgba(15, 23, 42, 0.98);
  border: 1px solid rgba(148, 163, 184, 0.18);
  box-shadow: 0 18px 60px rgba(2, 6, 23, 0.42);
  z-index: 30;
}

.settings-menu-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
  border: 0;
  border-radius: 12px;
  padding: 12px;
  text-align: left;
  background: transparent;
  color: #e2e8f0;
  cursor: pointer;
  transition: background-color 0.18s ease, transform 0.18s ease;
}

.settings-menu-item + .settings-menu-item {
  margin-top: 4px;
}

.settings-menu-item:hover {
  background: rgba(148, 163, 184, 0.12);
  transform: translateX(1px);
}

.settings-menu-title {
  font-size: 14px;
  font-weight: 600;
  color: #f8fafc;
}

.settings-menu-subtitle {
  font-size: 12px;
  line-height: 1.4;
  color: #94a3b8;
}
</style>
