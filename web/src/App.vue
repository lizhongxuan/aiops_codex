<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAppStore } from "./store";
import { resolveHostDisplay } from "./lib/hostDisplay";
import LoginModal from "./components/LoginModal.vue";
import HostModal from "./components/HostModal.vue";
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
  ArrowLeftIcon,
  EraserIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
} from "lucide-vue-next";

const store = useAppStore();
const router = useRouter();
const route = useRoute();

const isLoginModalOpen = ref(false);
const isHostModalOpen = ref(false);
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

function resolveRequestedHostTarget() {
  const requestedId = normalizeRouteValue(route.query.hostId);
  const requestedName = normalizeRouteValue(route.query.hostName);
  const requestedAddress = normalizeRouteValue(route.query.hostAddress);
  const candidates = [requestedId, requestedName, requestedAddress].filter(Boolean);

  if (!candidates.length) {
    return { hostId: "", hostMeta: null };
  }

  const host = store.snapshot.hosts.find((item) =>
    candidates.some((candidate) => candidate === item.id || candidate === item.name || candidate === item.address),
  );

  return {
    hostId: host?.id || requestedId,
    hostMeta: host || (requestedId ? { id: requestedId, name: requestedName, address: requestedAddress } : null),
  };
}

async function syncChatRouteHostSelection() {
  if (route.name !== "chat" || isRouteHostSyncing.value) return;
  const requestedHostTarget = resolveRequestedHostTarget();
  if (!requestedHostTarget.hostId) return;
  if (store.loading || store.sending || store.runtime.turn.active) return;

  isRouteHostSyncing.value = true;
  try {
    const alreadySingleHost = store.snapshot.kind === "single_host" && requestedHostTarget.hostId === store.snapshot.selectedHostId;
    if (alreadySingleHost) {
      clearRouteHostQuery();
      return;
    }

    const ok = await store.createOrActivateSingleHostSessionForHost(requestedHostTarget.hostId, requestedHostTarget.hostMeta || {});
    if (ok) {
      clearRouteHostQuery();
    }
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
  router.push("/settings");
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

async function createSession(kind = "single_host") {
  if (store.sending) return;
  const ok = await store.createSession(kind);
  if (!ok) return;
  store.errorMessage = "";
  store.noticeMessage = "";
  isHistoryDrawerOpen.value = false;
  if (kind === "workspace") {
    router.push("/protocol");
    return;
  }
  router.push("/");
}

async function startNewThread() {
  await createSession("single_host");
}

async function startWorkspaceSession() {
  await createSession("workspace");
}

async function openWorkspaceEntry() {
  if (store.snapshot.kind === "single_host" && store.workspaceReturnSession?.id) {
    const ok = await store.returnToWorkspaceSession();
    if (!ok) return;
    isHistoryDrawerOpen.value = false;
    router.push("/protocol");
    return;
  }
  router.push("/protocol");
}

async function openHistoryDrawer() {
  await store.fetchSessions();
  isHistoryDrawerOpen.value = true;
}

async function switchSession(sessionId) {
  const target = store.sessionList.find((item) => item.id === sessionId);
  const ok = await store.activateSession(sessionId);
  if (ok) {
    isHistoryDrawerOpen.value = false;
    if (target?.kind === "workspace") {
      router.push("/protocol");
      return;
    }
    router.push("/");
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

function describeTurnPhase(phase) {
  switch (phase) {
    case "thinking":
      return "思考中";
    case "planning":
      return "规划中";
    case "waiting_approval":
      return "等待审批";
    case "waiting_input":
      return "等待补充输入";
    case "executing":
      return "执行中";
    case "finalizing":
      return "收尾中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "aborted":
      return "已停止";
    default:
      return "待命";
  }
}

const workspaceSession = computed(() => {
  if (store.snapshot.kind === "workspace") {
    return store.activeSessionSummary?.kind === "workspace" ? store.activeSessionSummary : null;
  }
  if (store.workspaceReturnSession) {
    return store.workspaceReturnSession;
  }
  return store.activeSessionSummary?.kind === "workspace" ? store.activeSessionSummary : null;
});

const workspaceNavTitle = computed(() => workspaceSession.value?.title || "协作工作台");
const workspaceNavStatus = computed(() => {
  if (store.snapshot.kind === "workspace") {
    const pendingApprovals = (store.snapshot.approvals || []).filter((approval) => approval.status === "pending").length;
    if (store.runtime.turn.active) return describeTurnPhase(store.runtime.turn.phase);
    if (pendingApprovals > 0) return `${pendingApprovals} 项待审批`;
    return workspaceSession.value?.status === "completed"
      ? "已完成"
      : workspaceSession.value?.status === "failed"
        ? "失败"
        : "可用";
  }
  return workspaceSession.value ? "返回工作台" : "规划 / 调度 / 审批";
});

const canReturnToWorkspace = computed(() => store.snapshot.kind === "single_host" && !!store.workspaceReturnSession?.id);

const settingsNavTitle = computed(() => {
  if (route.path.startsWith("/settings/agent")) return "Agent Profile";
  if (route.path.startsWith("/settings/experience-packs")) return "Experience Packs";
  if (route.path.startsWith("/settings/hosts")) return "Hosts";
  return "Hosts / Packs / Agent";
});

const settingsNavStatus = computed(() => {
  if (route.path === "/settings") return "设置中心";
  if (route.path.startsWith("/settings/agent")) return "System Prompt / Skills / MCP";
  if (route.path.startsWith("/settings/experience-packs")) return "Playbooks";
  if (route.path.startsWith("/settings/hosts")) return "Inventory & Scope";
  return "入口";
});

const mainHeaderTitle = computed(() => {
  if (route.name === "chat") return "对话";
  if (route.name === "protocol") return "工作台";
  if (route.path.startsWith("/settings")) return "设置";
  return "Codex Workspace";
});

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
    store.snapshot.kind,
    store.activeSessionId,
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
          <div class="nav-group-title">主导航</div>
          <button class="nav-item" :class="{ active: $route.name === 'chat' }" @click="router.push('/')">
            <AppWindowIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">{{ currentSession.title }}</span>
              <span class="nav-item-time">{{ currentSessionStatus }}</span>
            </div>
          </button>

          <button class="nav-item" :class="{ active: $route.name === 'protocol' }" @click="openWorkspaceEntry">
            <PanelsTopLeftIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">{{ workspaceNavTitle }}</span>
              <span class="nav-item-time">{{ workspaceNavStatus }}</span>
            </div>
          </button>

          <button class="nav-item" :class="{ active: $route.path.startsWith('/settings') }" @click="router.push('/settings')">
            <SettingsIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">{{ settingsNavTitle }}</span>
              <span class="nav-item-time">{{ settingsNavStatus }}</span>
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
              <span class="settings-menu-title">设置总览</span>
              <span class="settings-menu-subtitle">Hosts / Packs / Agent</span>
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
          <h1 class="header-title">{{ mainHeaderTitle }}</h1>
        </div>
        
        <div class="header-right">
          <button class="header-pill subtle-pill" :disabled="!canResetCurrentSession" :title="resetButtonTitle" @click="resetCurrentSession">
            <EraserIcon size="14" />
            <span class="pill-text">清空上下文</span>
          </button>

          <button
            v-if="canReturnToWorkspace"
            class="header-pill subtle-pill"
            :title="`返回到 ${workspaceSession?.title || '工作台'}`"
            @click="openWorkspaceEntry"
          >
            <ArrowLeftIcon size="14" />
            <span class="pill-text">返回工作台</span>
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
      @create-workspace="startWorkspaceSession"
      @select="switchSession"
    />

    <!-- Modals -->
    <LoginModal v-if="isLoginModalOpen" @close="isLoginModalOpen = false" />
    <HostModal v-if="isHostModalOpen" @close="isHostModalOpen = false" />
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
