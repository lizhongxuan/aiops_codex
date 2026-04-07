<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAppStore } from "./store";
import { resolveHostDisplay } from "./lib/hostDisplay";
import LoginModal from "./components/LoginModal.vue";
import HostModal from "./components/HostModal.vue";
import SessionHistoryDrawer from "./components/SessionHistoryDrawer.vue";
import McpBundleHost from "./components/mcp/McpBundleHost.vue";
import McpUiCardHost from "./components/mcp/McpUiCardHost.vue";
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
  ActivityIcon,
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
const historyDrawerMode = ref("single_host");
const settingsMenuRef = ref(null);
let noticeTimer = null;
const isRouteHostSyncing = ref(false);
const OPEN_SESSION_HISTORY_EVENT = "codex:open-session-history";
const OPEN_MCP_DRAWER_EVENT = "codex:open-mcp-drawer";

function compactText(value) {
  return typeof value === "string" ? value.trim() : String(value || "").trim();
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

const mcpDrawerActiveSurface = computed(() => store.mcpDrawer?.activeSurface || null);
const mcpDrawerPinnedSurfaces = computed(() => store.mcpDrawer?.pinnedSurfaces || []);
const mcpDrawerRecentSurfaces = computed(() => store.mcpDrawer?.recentSurfaces || []);
const enabledMcpEntries = computed(() => {
  if (typeof store.getEnabledMcpEntries === "function") {
    return store.getEnabledMcpEntries();
  }
  return store.enabledMcpEntries || [];
});

function handleOpenMcpDrawerEvent(event) {
  const payload = asObject(event?.detail);
  const surface = store.openMcpDrawerSurface(payload, {
    pin: payload.pin === true,
    reason: payload.pin ? "pin" : "event",
  });
  if (!surface?.model || !surface.id) return;
  const source = compactText(payload.source).toLowerCase();
  const shouldOpen =
    isMcpDrawerOpen.value ||
    payload.pin === true ||
    payload.open === true ||
    source.startsWith("protocol");
  if (shouldOpen) {
    isMcpDrawerOpen.value = true;
  }
}

function pinActiveMcpSurface() {
  if (!mcpDrawerActiveSurface.value) return;
  const surface = store.pinMcpDrawerSurface(mcpDrawerActiveSurface.value);
  if (!surface) return;
  store.noticeMessage = `${surface.title} 已固定到 MCP 抽屉。`;
}

function pinMcpSurface(surface) {
  const pinnedSurface = store.pinMcpDrawerSurface(surface);
  if (!pinnedSurface) return;
  store.noticeMessage = `${pinnedSurface.title} 已固定到 MCP 抽屉。`;
}

function handleDrawerSurfaceAction() {
  store.touchActiveMcpDrawerSurface?.("action");
  store.noticeMessage = "该 MCP 操作已定位到当前对话上下文，请在对应 turn 中继续执行。";
}

function handleDrawerSurfaceDetail(payload) {
  const surface = store.openMcpDrawerSurface(payload, { reason: "detail" });
  if (!surface?.model || !surface.id) return;
  isMcpDrawerOpen.value = true;
}

function handleDrawerSurfaceRefresh() {
  store.touchActiveMcpDrawerSurface?.("refresh");
  store.noticeMessage = "已定位到当前 MCP 面板，可在对话页触发刷新并等待结果回写。";
}

function removePinnedMcpSurface(surfaceId = "") {
  store.removePinnedMcpDrawerSurface(surfaceId);
}

function selectMcpDrawerSurface(surface) {
  const selected = store.selectMcpDrawerSurface(surface);
  if (!selected) return;
  isMcpDrawerOpen.value = true;
}

function openEnabledMcpEntry(entry, pin = false) {
  const surface = store.openEnabledMcpEntry?.(entry, {
    pin,
    reason: pin ? "catalog-pin" : "catalog-open",
  });
  if (!surface) return;
  isMcpDrawerOpen.value = true;
  store.noticeMessage = pin ? `${surface.title} 已加入常驻 MCP 面板。` : `${surface.title} 已打开统一入口。`;
}

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
  if (!isMcpDrawerOpen.value) {
    if (!mcpDrawerActiveSurface.value) {
      const fallbackSurface =
        mcpDrawerPinnedSurfaces.value[0] ||
        mcpDrawerRecentSurfaces.value[0] ||
        null;
      if (fallbackSurface) {
        store.selectMcpDrawerSurface(fallbackSurface);
      } else if (enabledMcpEntries.value[0]) {
        store.openEnabledMcpEntry?.(enabledMcpEntries.value[0], { reason: "catalog-default" });
      }
    }
  }
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

function openSkillCatalog() {
  closeSettingsMenu();
  router.push("/settings/skills");
}

function openMcpCatalog() {
  closeSettingsMenu();
  router.push("/settings/mcp");
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

function resolveHistorySessionKind(source = "") {
  const normalizedSource = compactText(source).toLowerCase();
  if (normalizedSource.includes("workspace") || normalizedSource.includes("protocol")) {
    return "workspace";
  }
  return route.name === "protocol" ? "workspace" : "single_host";
}

async function openHistoryDrawer(mode = resolveHistorySessionKind()) {
  historyDrawerMode.value = mode === "workspace" ? "workspace" : "single_host";
  await store.fetchSessions();
  isHistoryDrawerOpen.value = true;
}

function openHeaderHistoryDrawer() {
  void openHistoryDrawer(resolveHistorySessionKind());
}

function handleOpenSessionHistoryEvent(event) {
  void openHistoryDrawer(resolveHistorySessionKind(event?.detail?.source));
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
  if (route.path.startsWith("/settings/skills")) return "Skills 管理";
  if (route.path.startsWith("/settings/mcp")) return "MCP 管理";
  if (route.path.startsWith("/settings/experience-packs")) return "Experience Packs";
  if (route.path.startsWith("/settings/hosts")) return "Hosts";
  return "Hosts / Skills / MCP / Agent";
});

const settingsNavStatus = computed(() => {
  if (route.path === "/settings") return "设置中心";
  if (route.path.startsWith("/settings/agent")) return "System Prompt / Skills / MCP";
  if (route.path.startsWith("/settings/skills")) return "Catalog / Defaults / Activation";
  if (route.path.startsWith("/settings/mcp")) return "Catalog / Defaults / Permission";
  if (route.path.startsWith("/settings/experience-packs")) return "Playbooks";
  if (route.path.startsWith("/settings/hosts")) return "Inventory & Scope";
  return "入口";
});

const mainHeaderTitle = computed(() => {
  if (route.name === "chat") return "单机会话";
  if (route.name === "protocol") return "协作工作台";
  if (route.path.startsWith("/settings")) return "设置";
  return "Codex Workspace";
});

const isChatRoute = computed(() => route.name === "chat");
const isProtocolRoute = computed(() => route.name === "protocol");

const headerHistoryLabel = computed(() => (isProtocolRoute.value ? "历史工作台" : "历史会话"));
const historyDrawerTitle = computed(() => (historyDrawerMode.value === "workspace" ? "历史工作台" : "历史会话"));
const historyDrawerSessionKind = computed(() => (historyDrawerMode.value === "workspace" ? "workspace" : "single_host"));
const historyDrawerHostScopeId = computed(() =>
  historyDrawerMode.value === "single_host"
    ? compactText(store.snapshot.selectedHostId || store.activeSessionSummary?.selectedHostId || "server-local")
    : "",
);
const historyDrawerScopeLabel = computed(() => {
  if (historyDrawerMode.value === "workspace") {
    return "仅显示多主机协作的工作台历史，主体是主 Agent 会话。";
  }
  return `仅显示当前主机 ${selectedHostLabel.value || historyDrawerHostScopeId.value || "server-local"} 下的单机会话历史。`;
});
const historyDrawerSessions = computed(() => {
  const sessions = Array.isArray(store.sessionList) ? store.sessionList : [];
  if (historyDrawerMode.value === "workspace") {
    return sessions.filter((session) => compactText(session?.kind).toLowerCase() === "workspace");
  }
  const targetHostId = historyDrawerHostScopeId.value;
  return sessions.filter((session) => {
    const sessionKind = compactText(session?.kind || "single_host").toLowerCase();
    if (sessionKind && sessionKind !== "single_host") return false;
    if (!targetHostId) return true;
    return compactText(session?.selectedHostId || "server-local") === targetHostId;
  });
});
const showHeaderHistoryButton = computed(() => isChatRoute.value || isProtocolRoute.value);
const showHeaderHostControls = computed(() => isChatRoute.value);

onMounted(() => {
  store.hydrateMcpDrawerState?.();
  store.fetchState();
  store.fetchSessions();
  store.connectWs();
  window.addEventListener("keydown", handleGlobalKeydown);
  window.addEventListener(OPEN_SESSION_HISTORY_EVENT, handleOpenSessionHistoryEvent);
  window.addEventListener(OPEN_MCP_DRAWER_EVENT, handleOpenMcpDrawerEvent);
  document.addEventListener("click", handleDocumentClick);
});

onBeforeUnmount(() => {
  store.disconnectWs();
  window.removeEventListener("keydown", handleGlobalKeydown);
  window.removeEventListener(OPEN_SESSION_HISTORY_EVENT, handleOpenSessionHistoryEvent);
  window.removeEventListener(OPEN_MCP_DRAWER_EVENT, handleOpenMcpDrawerEvent);
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

watch(
  () => route.name,
  () => {
    if (!isHistoryDrawerOpen.value) {
      historyDrawerMode.value = resolveHistorySessionKind();
    }
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
          <button class="nav-button secondary" @click="startWorkspaceSession">
            <PanelsTopLeftIcon size="18" />
            <span class="nav-label">新建工作台</span>
          </button>
        </div>
      </div>

        <div class="sidebar-scroll">
        <div class="nav-group">
          <div class="nav-group-title">主导航</div>
          <button class="nav-item" :class="{ active: $route.name === 'chat' }" @click="router.push('/')">
            <AppWindowIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">单机会话</span>
              <span class="nav-item-time">{{ currentSessionStatus }}</span>
            </div>
          </button>

          <button class="nav-item" :class="{ active: $route.name === 'protocol' }" @click="openWorkspaceEntry">
            <PanelsTopLeftIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">协作工作台</span>
              <span class="nav-item-time">{{ workspaceNavStatus }}</span>
            </div>
          </button>

          <button class="nav-item" :class="{ active: $route.name === 'coroot' }" @click="router.push('/coroot')">
            <ActivityIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">Coroot 监控</span>
              <span class="nav-item-time">Dashboard</span>
            </div>
          </button>

          <button class="nav-item" :class="{ active: $route.path.startsWith('/settings') }" @click="router.push('/settings/hosts')">
            <ServerIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">主机列表</span>
              <span class="nav-item-time">Hosts</span>
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
            <button class="settings-menu-item" @click="openSkillCatalog">
              <span class="settings-menu-title">Skills 管理</span>
              <span class="settings-menu-subtitle">Skill catalog / 默认值 / 激活方式</span>
            </button>
            <button class="settings-menu-item" @click="openMcpCatalog">
              <span class="settings-menu-title">MCP 管理</span>
              <span class="settings-menu-subtitle">MCP catalog / 默认值 / 权限</span>
            </button>
            <button class="settings-menu-item" @click="closeSettingsMenu(); router.push('/settings/experience-packs')">
              <span class="settings-menu-title">Experience Packs</span>
              <span class="settings-menu-subtitle">Playbooks / 运维经验包</span>
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
          <button
            v-if="showHeaderHistoryButton"
            class="header-pill subtle-pill"
            :title="headerHistoryLabel"
            @click="openHeaderHistoryDrawer"
          >
            <HistoryIcon size="14" />
            <span class="pill-text">{{ headerHistoryLabel }}</span>
          </button>

          <button class="header-pill subtle-pill" :disabled="!canResetCurrentSession" :title="resetButtonTitle" @click="resetCurrentSession">
            <EraserIcon size="14" />
            <span class="pill-text">清空上下文</span>
          </button>

          <button
            v-if="canReturnToWorkspace && isChatRoute"
            class="header-pill subtle-pill"
            :title="`返回到 ${workspaceSession?.title || '工作台'}`"
            @click="openWorkspaceEntry"
          >
            <ArrowLeftIcon size="14" />
            <span class="pill-text">返回工作台</span>
          </button>

          <button v-if="showHeaderHostControls" class="header-pill" @click="isHostModalOpen = true">
            <ServerIcon size="14" />
            <span class="pill-text">{{ selectedHostLabel }}</span>
            <span class="pill-dot" :class="store.selectedHost.status"></span>
          </button>

          <button
            v-if="showHeaderHostControls"
            class="header-pill"
            :disabled="!store.canOpenTerminal"
            @click="openTerminal"
            title="打开终端"
          >
            <TerminalIcon size="14" />
            <span class="pill-text">终端</span>
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
        <div class="mcp-header-copy">
          <h3>MCP Surfaces</h3>
          <p>在这里复用当前对话里打开或固定的监控面板与控制卡片。</p>
        </div>
        <button v-if="mcpDrawerActiveSurface" class="mcp-header-pin" type="button" @click="pinActiveMcpSurface">
          固定当前面板
        </button>
      </div>
      <div class="mcp-body">
        <section v-if="mcpDrawerActiveSurface" class="mcp-drawer-section active" data-testid="app-mcp-active-surface">
          <div class="mcp-section-head">
            <div>
              <span class="mcp-section-kicker">ACTIVE SURFACE</span>
              <h4>{{ mcpDrawerActiveSurface.title }}</h4>
              <p>{{ mcpDrawerActiveSurface.source || "来自当前对话" }}</p>
            </div>
          </div>

          <McpBundleHost
            v-if="mcpDrawerActiveSurface.kind === 'bundle'"
            :bundle="mcpDrawerActiveSurface.model"
            @action="handleDrawerSurfaceAction"
            @open-detail="handleDrawerSurfaceDetail"
            @pin="pinActiveMcpSurface"
          />
          <McpUiCardHost
            v-else
            :card="mcpDrawerActiveSurface.model"
            @action="handleDrawerSurfaceAction"
            @detail="handleDrawerSurfaceDetail"
            @refresh="handleDrawerSurfaceRefresh"
          />
        </section>

        <section class="mcp-drawer-section" data-testid="app-mcp-pinned-list">
          <div class="mcp-section-head">
            <div>
              <span class="mcp-section-kicker">PINNED</span>
              <h4>常驻面板</h4>
              <p>固定后可以在不同 chat 间复用查看。</p>
            </div>
          </div>

          <div v-if="mcpDrawerPinnedSurfaces.length" class="mcp-pinned-list">
            <article
              v-for="surface in mcpDrawerPinnedSurfaces"
              :key="surface.id"
              class="mcp-pinned-item"
              :class="{ active: surface.id === mcpDrawerActiveSurface?.id }"
            >
              <button type="button" class="mcp-pinned-select" @click="selectMcpDrawerSurface(surface)">
                <strong>{{ surface.title }}</strong>
                <span>{{ surface.source || "来自当前对话" }}</span>
              </button>
              <button type="button" class="mcp-pinned-remove" @click="removePinnedMcpSurface(surface.id)">
                移除
              </button>
            </article>
          </div>
          <p v-else class="mcp-empty">当前还没有固定的 MCP 面板。你可以从聊天里的监控 bundle 或控制卡片把它固定到这里。</p>
        </section>

        <section class="mcp-drawer-section" data-testid="app-mcp-recent-list">
          <div class="mcp-section-head">
            <div>
              <span class="mcp-section-kicker">RECENT</span>
              <h4>最近操作</h4>
              <p>保留最近打开、刷新或执行过的 MCP 面板，方便跨 chat 继续处理。</p>
            </div>
          </div>

          <div v-if="mcpDrawerRecentSurfaces.length" class="mcp-pinned-list recent">
            <article
              v-for="surface in mcpDrawerRecentSurfaces"
              :key="`recent-${surface.id}`"
              class="mcp-pinned-item recent"
              :class="{ 'is-current': surface.id === mcpDrawerActiveSurface?.id }"
            >
              <button type="button" class="mcp-pinned-select" @click="selectMcpDrawerSurface(surface)">
                <strong>{{ surface.title }}</strong>
                <span>{{ surface.subtitle || surface.source || "最近打开" }}</span>
              </button>
              <button
                type="button"
                class="mcp-pinned-remove secondary"
                @click="pinMcpSurface(surface)"
              >
                固定
              </button>
            </article>
          </div>
          <p v-else class="mcp-empty">最近还没有 MCP 操作记录。打开监控 bundle、刷新卡片或执行操作后会自动出现在这里。</p>
        </section>

        <section class="mcp-drawer-section" data-testid="app-mcp-enabled-list">
          <div class="mcp-section-head">
            <div>
              <span class="mcp-section-kicker">ENABLED MCPS</span>
              <h4>启用中的 MCP</h4>
              <p>统一列出当前 Agent Profile 已启用的 MCP，作为常驻入口而不是分散在各页面里。</p>
            </div>
          </div>

          <div v-if="enabledMcpEntries.length" class="mcp-pinned-list enabled">
            <article
              v-for="entry in enabledMcpEntries"
              :key="`enabled-${entry.id}`"
              class="mcp-pinned-item enabled"
            >
              <button type="button" class="mcp-pinned-select" @click="openEnabledMcpEntry(entry)">
                <strong>{{ entry.name }}</strong>
                <span>{{ entry.permission === "readwrite" ? "读写" : "只读" }} · {{ entry.source || "local" }}</span>
              </button>
              <button type="button" class="mcp-pinned-remove secondary" @click="openEnabledMcpEntry(entry, true)">
                固定入口
              </button>
            </article>
          </div>
          <p v-else class="mcp-empty">当前 Agent Profile 还没有启用中的 MCP。</p>
        </section>
      </div>
    </aside>

    <SessionHistoryDrawer
      v-if="isHistoryDrawerOpen"
      :sessions="historyDrawerSessions"
      :active-session-id="store.activeSessionId"
      :loading="store.historyLoading"
      :switching-disabled="store.runtime.turn.active"
      :title="historyDrawerTitle"
      :session-kind="historyDrawerSessionKind"
      :scope-label="historyDrawerScopeLabel"
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

.app-mcp-drawer {
  width: 0;
  overflow: hidden;
  border-left: 1px solid transparent;
  background: linear-gradient(180deg, rgba(248, 250, 252, 0.98), rgba(255, 255, 255, 0.98));
  transition: width 0.22s ease, border-color 0.22s ease;
}

.app-mcp-drawer.is-open {
  width: min(420px, 36vw);
  border-left-color: rgba(226, 232, 240, 0.92);
}

.mcp-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  padding: 18px 18px 14px;
  border-bottom: 1px solid rgba(226, 232, 240, 0.9);
}

.mcp-header-copy h3,
.mcp-section-head h4 {
  margin: 0;
  color: #0f172a;
}

.mcp-header-copy p,
.mcp-section-head p,
.mcp-empty,
.mcp-pinned-select span {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: #64748b;
}

.mcp-header-pin,
.mcp-pinned-remove {
  border: 1px solid rgba(148, 163, 184, 0.3);
  border-radius: 999px;
  background: #fff;
  color: #0f172a;
  font-size: 12px;
  padding: 6px 10px;
  cursor: pointer;
}

.mcp-pinned-remove.secondary {
  color: #0369a1;
  border-color: rgba(14, 165, 233, 0.24);
  background: rgba(240, 249, 255, 0.98);
}

.mcp-body {
  display: grid;
  gap: 16px;
  padding: 16px 18px 20px;
  overflow-y: auto;
  max-height: calc(100vh - 78px);
}

.mcp-drawer-section {
  display: grid;
  gap: 12px;
}

.mcp-section-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
}

.mcp-section-kicker {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #0ea5e9;
}

.mcp-pinned-list {
  display: grid;
  gap: 10px;
}

.mcp-pinned-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px;
  border: 1px solid rgba(226, 232, 240, 0.95);
  border-radius: 16px;
  background: #fff;
}

.mcp-pinned-item.active {
  border-color: rgba(14, 165, 233, 0.36);
  box-shadow: 0 10px 30px rgba(14, 165, 233, 0.08);
}

.mcp-pinned-item.recent,
.mcp-pinned-item.enabled {
  align-items: stretch;
}

.mcp-pinned-item.is-current {
  border-color: rgba(14, 165, 233, 0.24);
  box-shadow: 0 8px 24px rgba(14, 165, 233, 0.06);
}

.mcp-pinned-select {
  display: grid;
  gap: 2px;
  border: none;
  background: transparent;
  padding: 0;
  text-align: left;
  cursor: pointer;
}

.mcp-pinned-select strong {
  color: #0f172a;
  font-size: 13px;
  font-weight: 600;
}
</style>
