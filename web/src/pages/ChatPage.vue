<script setup>
import { computed, defineAsyncComponent, ref, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { useAppStore } from "../store";
import { resolveHostDisplay } from "../lib/hostDisplay";
import { formatMainChatTurns, isChatConversationCard } from "../lib/chatTurnFormatter";
import { useChatHistoryPager } from "../composables/useChatHistoryPager";
import { useChatScrollState } from "../composables/useChatScrollState";
import { useAwaySummary } from "../composables/useAwaySummary";
import { useVirtualTurnList } from "../composables/useVirtualTurnList";
import { buildMcpDecisionNotice, buildSyntheticMcpApproval, formatMcpActionLabel, formatMcpActionTarget, isMcpMutationAction } from "../lib/mcpActionRuntime";
import CardItem from "../components/CardItem.vue";
import ChatTurnGroup from "../components/chat/ChatTurnGroup.vue";
import ChatComposerDock from "../components/chat/ChatComposerDock.vue";
import McpBundleHost from "../components/mcp/McpBundleHost.vue";
import McpUiCardHost from "../components/mcp/McpUiCardHost.vue";
import ThinkingCard from "../components/ThinkingCard.vue";
import { BotIcon, WifiOffIcon, RefreshCwIcon, TerminalIcon } from "lucide-vue-next";

const store = useAppStore();
const WorkspaceHostTerminal = defineAsyncComponent(() => import("../components/workspace/WorkspaceHostTerminal.vue"));
const OPEN_SESSION_HISTORY_EVENT = "codex:open-session-history";
const OPEN_MCP_DRAWER_EVENT = "codex:open-mcp-drawer";

const composerMessage = ref("");
const scrollContainer = ref(null);
const scrollContent = ref(null);
const showFileDetails = ref(false);
const showSearchDetails = ref(false);
const authCardCollapsed = ref(false);
const approvalFollowupMode = ref(false);
const localMcpApprovals = ref([]);
const activeMcpSurface = ref(null);
const mcpPinnedSurfaces = ref([]);
const isMcpDrawerOpen = ref(false);
const terminalDockVisible = ref(false);
const terminalDockHeight = ref(320);
const terminalDockSessionLive = ref(false);
const terminalDockRef = ref(null);
const terminalDockDragging = ref(false);
let terminalDockDragState = null;
let terminalDockMaxHeight = 560;
const historyAutoLoadArmed = ref(false);

/* ---- ThinkingCard local state ---- */
const showThinking = ref(false);
const thinkingPhase = ref("thinking");
const thinkingHint = ref("");
const preferredThinkingPhase = ref("");
let thinkingHintTimer = null;

const thinkingCard = computed(() => ({
  id: "__thinking__",
  type: "ThinkingCard",
  phase: thinkingPhase.value,
  hint: thinkingHint.value,
}));
const showThinkingCard = computed(() => showThinking.value && thinkingPhase.value !== "finalizing");

function clearThinkingPrelude() {
  if (thinkingHintTimer) {
    window.clearTimeout(thinkingHintTimer);
    thinkingHintTimer = null;
  }
  thinkingHint.value = "";
  preferredThinkingPhase.value = "";
}

function compactText(value) {
  return typeof value === "string" ? value.trim() : String(value || "").trim();
}

function isMcpBundlePayload(value) {
  const source = value && typeof value === "object" ? value : {};
  const bundleKind = compactText(source.bundleKind || source.bundle_kind).toLowerCase();
  return Boolean(
    bundleKind ||
      source.bundleId ||
      source.bundle_id ||
      Array.isArray(source.sections) ||
      Array.isArray(source.sectionConfig),
  );
}

function normalizeMcpSurfacePayload(payload = {}) {
  const source = payload && typeof payload === "object" ? payload : {};
  const kind = isMcpBundlePayload(source)
    ? "bundle"
    : compactText(source.kind || source.uiKind || source.ui_kind).toLowerCase() === "bundle"
      ? "bundle"
      : "card";
  const bundleId = compactText(source.bundleId || source.bundle_id || source.id || source.key || "");
  const cardId = compactText(source.cardId || source.card_id || source.id || source.key || "");
  const surfaceId = kind === "bundle" ? bundleId || cardId : cardId || bundleId;
  const title =
    compactText(source.title || source.summary || source.label || source.name || source.rootCause || source.root_cause) ||
    (kind === "bundle" ? "MCP 聚合面板" : "MCP 卡片");
  const subtitle = compactText(
    source.subject?.name ||
      source.subject?.service ||
      source.subject?.type ||
      source.scope?.service ||
      source.scope?.hostId ||
      source.scope?.resourceId ||
      source.mcpServer ||
      source.source ||
      "",
  );
  const sourceTag = compactText(source.source || source.mcpServer || source.sourceCardId || source.source_card_id || "");

  return {
    kind,
    id: surfaceId || `${kind}-${title}`,
    title,
    subtitle,
    source: sourceTag,
    bundle: kind === "bundle" ? source : null,
    card: kind === "card" ? source : null,
    raw: source,
  };
}

function mcpSurfaceKey(surface) {
  const record = surface || {};
  return `${record.kind || "card"}:${record.id || record.title || record.subtitle || "surface"}`;
}

function dispatchOpenMcpDrawer(surface, pin = false) {
  if (typeof window === "undefined" || !surface) return;
  window.dispatchEvent(
    new CustomEvent(OPEN_MCP_DRAWER_EVENT, {
      detail: {
        source: "chat-mcp-surface",
        pin,
        surface: {
          kind: surface.kind || "card",
          bundle: surface.kind === "bundle" ? surface.bundle : undefined,
          card: surface.kind === "card" ? surface.card : undefined,
          source: surface.source || "",
          title: surface.title || "",
          subtitle: surface.subtitle || "",
          id: surface.id || "",
        },
      },
    }),
  );
}

function openMcpSurfaceDrawer(payload, { pin = false, silent = false } = {}) {
  const surface = normalizeMcpSurfacePayload(payload);
  activeMcpSurface.value = surface;
  isMcpDrawerOpen.value = true;
  if (pin) {
    const key = mcpSurfaceKey(surface);
    mcpPinnedSurfaces.value = [
      ...mcpPinnedSurfaces.value.filter((item) => mcpSurfaceKey(item) !== key),
      surface,
    ];
  }
  if (!silent) {
    store.noticeMessage = `${surface.title} 已打开完整面板。`;
  }
  dispatchOpenMcpDrawer(surface, pin);
  return surface;
}

async function refreshMcpSurface(payload) {
  const surface = openMcpSurfaceDrawer(payload, { silent: true });
  await store.fetchState();
  store.noticeMessage = `${surface.title} 已刷新。`;
  return surface;
}

function pinMcpSurface(payload) {
  const surface = openMcpSurfaceDrawer(payload, { pin: true });
  store.noticeMessage = `${surface.title} 已固定到 MCP 面板。`;
  return surface;
}

function closeMcpSurfaceDrawer() {
  isMcpDrawerOpen.value = false;
}

function inferThinkingPrelude(message) {
  const text = (message || "").trim();
  const lower = text.toLowerCase();

  const searchLike =
    /a股|股市|行情|指数|港股|美股|财报|新闻|汇率|价格|走势|最新|今天|实时|网页|搜索|查一下|查下|盘面/i.test(text) ||
    /(market|stock|price|latest|news|search|web)/i.test(text);
  if (searchLike) {
    return {
      phase: "searching",
      hint: "我先查一下最新网页信息，再给你一个简洁结论。",
    };
  }

  const browseLike =
    /文件|文档|代码|配置|日志|目录|文件夹|打开|浏览|读取|读一下|看看.*文件|列出/i.test(text) ||
    /(file|folder|directory|read|open|browse|list)/i.test(lower);
  if (browseLike) {
    return {
      phase: "browsing",
      hint: "我先快速浏览相关文件，再给你结论。",
    };
  }

  return {
    phase: "thinking",
    hint: "我先快速理一下思路，再继续处理。",
  };
}

function queueThinkingPrelude(message) {
  clearThinkingPrelude();
  const prelude = inferThinkingPrelude(message);
  preferredThinkingPhase.value = prelude.phase;
  thinkingPhase.value = prelude.phase;
  thinkingHintTimer = window.setTimeout(() => {
    if (!showThinking.value) return;
    if (store.runtime.turn.phase !== "thinking" && store.runtime.turn.phase !== prelude.phase) return;
    if (activeActivityLine.value || summaryLine.value) return;
    thinkingHint.value = prelude.hint;
  }, 900);
}

watch(
  () => store.runtime.turn.phase,
  (phase) => {
    if (phase === "idle" || phase === "completed" || phase === "failed" || phase === "aborted") {
      showThinking.value = false;
      approvalFollowupMode.value = false;
      clearThinkingPrelude();
    } else {
      const shouldPreferLocalPhase =
        phase === "thinking" &&
        !!preferredThinkingPhase.value &&
        !activeActivityLine.value &&
        !summaryLine.value;
      thinkingPhase.value = shouldPreferLocalPhase ? preferredThinkingPhase.value : phase;
      showThinking.value = true;
    }
    
    // Reset collapse state when a new approval arrives
    if (phase === "waiting_approval") {
      authCardCollapsed.value = false;
    }
  }
);

/* ---- Activity summary ---- */
const activity = computed(() => store.runtime.activity);

function truncateLabel(value, max = 88) {
  if (!value || value.length <= max) return value;
  return `${value.slice(0, max - 3)}...`;
}

const activitySummary = computed(() => {
  const a = activity.value;
  const parts = [];
  if (a.filesViewed > 0) parts.push(`${a.filesViewed} 个文件`);
  if (a.searchCount > 0 && a.searchLocationCount > 0) {
    parts.push(`${a.searchCount} 次搜索（命中 ${a.searchLocationCount} 个位置）`);
  } else if (a.searchCount > 0) {
    parts.push(`${a.searchCount} 次搜索`);
  }
  if (a.listCount > 0) parts.push(`${a.listCount} 个列表`);
  if (a.filesChanged > 0) parts.push(`${a.filesChanged} 个文件修改`);
  if (a.commandsRun > 0) parts.push(`${a.commandsRun} 个命令`);
  if (parts.length === 0) return "";
  if (a.filesViewed > 0 && parts.length === 1) return `已浏览 ${parts[0]}`;
  if (a.searchCount > 0 && parts.length === 1) {
    return a.searchLocationCount > 0
      ? `已搜索 ${a.searchCount} 次，命中 ${a.searchLocationCount} 个位置`
      : `已搜索 ${a.searchCount} 次`;
  }
  if (a.filesChanged > 0 && parts.length === 1) return `已修改 ${a.filesChanged} 个文件`;
  return `已处理 ${parts.join("，")}`;
});

const currentReadingLine = computed(() => {
  const file = activity.value.currentReadingFile;
  return file ? `现在浏览 ${file}` : "";
});

const currentChangingLine = computed(() => {
  const file = activity.value.currentChangingFile;
  return file ? `现在修改 ${truncateLabel(file)}` : "";
});

const currentListingLine = computed(() => {
  const path = activity.value.currentListingPath;
  return path ? `现在列出 ${truncateLabel(path)}` : "";
});

const currentSearchLine = computed(() => {
  const a = activity.value;
  const query = a.currentSearchQuery || a.currentWebSearchQuery;
  if (!query) {
    return "";
  }
  if (a.currentSearchKind === "content") {
    return `现在搜索内容（${truncateLabel(query)}）`;
  }
  if (a.currentSearchKind === "web" || a.currentWebSearchQuery) {
    return `现在搜索网页（${truncateLabel(query)}）`;
  }
  return `现在搜索内容（${truncateLabel(query)}）`;
});

const viewedFileDetails = computed(() => activity.value.viewedFiles || []);
const searchedQueryDetails = computed(() => [
  ...(activity.value.searchedWebQueries || []),
  ...(activity.value.searchedContentQueries || []),
]);
const activeActivityLine = computed(() => currentChangingLine.value || currentReadingLine.value || currentListingLine.value || currentSearchLine.value || "");
const activeActivityKind = computed(() => {
  if (currentChangingLine.value) return "change";
  if (currentReadingLine.value) return "files";
  if (currentSearchLine.value) return "search";
  if (currentListingLine.value) return "list";
  return "";
});
const summaryLine = computed(() => {
  if (activeActivityLine.value) return "";
  return activitySummary.value;
});
const hasTopFeedback = computed(() => !!activeActivityLine.value || !!summaryLine.value);
const activeLineExpandable = computed(() => {
  if (activeActivityKind.value === "files") return viewedFileDetails.value.length > 0;
  if (activeActivityKind.value === "search") return searchedQueryDetails.value.length > 0;
  return false;
});
const summaryExpandable = computed(() => viewedFileDetails.value.length > 0 || searchedQueryDetails.value.length > 0);

function toggleActiveLineDetails() {
  if (activeActivityKind.value === "files" && viewedFileDetails.value.length) {
    showFileDetails.value = !showFileDetails.value;
    return;
  }
  if (activeActivityKind.value === "search" && searchedQueryDetails.value.length) {
    showSearchDetails.value = !showSearchDetails.value;
  }
}

function toggleSummaryDetails() {
  if (viewedFileDetails.value.length) {
    showFileDetails.value = !showFileDetails.value;
    return;
  }
  if (searchedQueryDetails.value.length) {
    showSearchDetails.value = !showSearchDetails.value;
  }
}

const activePlanCard = computed(() => {
  if (!store.runtime.turn.active) return null;
  const planCards = store.snapshot.cards.filter((card) => card.type === "PlanCard" && card.items?.length);
  if (!planCards.length) return null;
  return planCards[planCards.length - 1];
});

const pendingApprovalCards = computed(() => {
  return store.snapshot.cards.filter((card) => {
    if (card.status !== "pending") return false;
    return card.type === "CommandApprovalCard" || card.type === "FileChangeApprovalCard";
  });
});

const pendingApprovals = computed(() => {
  return (store.snapshot.approvals || []).filter((approval) => approval.status === "pending");
});

const reconnectErrorPattern = /^Reconnecting\.\.\.\s*\d+\s*\/\s*\d+$/i;

const activeApprovalCard = computed(() => {
  const nextApproval = pendingApprovals.value[0];
  if (!nextApproval) {
    return pendingApprovalCards.value[0] || null;
  }

  const byRequestID = store.snapshot.cards.find((card) => {
    if (card.status !== "pending") return false;
    if (card.type !== "CommandApprovalCard" && card.type !== "FileChangeApprovalCard") return false;
    return card.approval?.requestId === nextApproval.id;
  });
  if (byRequestID) return byRequestID;

  return store.snapshot.cards.find((card) => {
    if (card.status !== "pending") return false;
    if (card.type !== "CommandApprovalCard" && card.type !== "FileChangeApprovalCard") return false;
    return card.id === nextApproval.itemId;
  }) || pendingApprovalCards.value[0] || null;
});

const activeApprovalQueueIndex = computed(() => {
  if (!activeApprovalCard.value?.approval?.requestId) return -1;
  return pendingApprovals.value.findIndex((approval) => approval.id === activeApprovalCard.value.approval.requestId);
});

const activeApprovalQueueCount = computed(() => pendingApprovals.value.length);

const activeApprovalQueueLabel = computed(() => {
  if (!activeApprovalCard.value) return "";
  if (activeApprovalQueueCount.value <= 1) return "当前仅 1 项待确认";
  const position = activeApprovalQueueIndex.value >= 0 ? activeApprovalQueueIndex.value + 1 : 1;
  return `当前 ${position}/${activeApprovalQueueCount.value} 项待确认`;
});

const activeApprovalQueueNote = computed(() => {
  if (!activeApprovalCard.value || activeApprovalQueueCount.value <= 1) return "";
  const position = activeApprovalQueueIndex.value >= 0 ? activeApprovalQueueIndex.value + 1 : 1;
  const remaining = Math.max(activeApprovalQueueCount.value - position, 0);
  return remaining > 0 ? `后面还有 ${remaining} 项排队` : "";
});

const activeMcpApproval = computed(() => localMcpApprovals.value[0] || null);
const hasActiveApprovalOverlay = computed(() => Boolean(activeApprovalCard.value || activeMcpApproval.value));
const allowFollowUpComposer = computed(() => approvalFollowupMode.value && !hasActiveApprovalOverlay.value);
const isWorkspaceSession = computed(() => store.snapshot.kind === "workspace");
const workspaceSessionLabel = computed(() => (isWorkspaceSession.value ? "工作台会话" : ""));
const workspaceDetailLinkLabel = computed(() => (isWorkspaceSession.value ? "查看只读详情" : ""));

const latestTerminalCard = computed(() => {
  const cards = store.snapshot.cards || [];
  for (let i = cards.length - 1; i >= 0; i -= 1) {
    const card = cards[i];
    if (!card || !card.output) {
      continue;
    }
    if (card.hostId && card.hostId !== store.snapshot.selectedHostId) {
      continue;
    }
    return card;
  }
  return null;
});

const terminalDockHost = computed(() => store.selectedHost || { id: store.snapshot.selectedHostId || "server-local" });
const terminalDockHostLabel = computed(() => resolveHostDisplay(terminalDockHost.value) || terminalDockHost.value.id || "server-local");
const terminalDockTitle = computed(() => latestTerminalCard.value?.title || "终端面板");
const terminalDockSubtitle = computed(() => {
  if (latestTerminalCard.value?.summary) return latestTerminalCard.value.summary;
  if (selectedHostAlert.value) return selectedHostAlert.value;
  const status = terminalDockHost.value.status || "unknown";
  return `当前主机 ${terminalDockHostLabel.value} · ${status}`;
});
const terminalDockOutput = computed(() => {
  const card = latestTerminalCard.value;
  if (!card) return "";
  return card.output || card.stdout || card.text || card.summary || "";
});
const terminalDockCanTakeover = computed(() => store.canOpenTerminal);
const terminalDockPanelHeight = computed(() => `${Math.max(140, terminalDockHeight.value - 108)}px`);
const terminalDockToolbarLabel = computed(() => {
  if (!terminalDockVisible.value) return "终端已收起";
  if (terminalDockSessionLive.value) return `终端已连接 · ${terminalDockHostLabel.value}`;
  if (!terminalDockCanTakeover.value) return `终端不可用 · ${terminalDockHostLabel.value}`;
  return `终端准备中 · ${terminalDockHostLabel.value}`;
});
const chatContainerStyle = computed(() => ({
  paddingBottom: terminalDockVisible.value
    ? `${terminalDockHeight.value + (activePlanCard.value || hasActiveApprovalOverlay.value ? 260 : 180)}px`
    : "180px",
}));

function queueLocalMcpApproval(action) {
  const approval = buildSyntheticMcpApproval(action, {
    scope: action?.scope || {},
    summary: action?.confirmText || "等待你确认后继续执行该 MCP 变更操作。",
  });
  localMcpApprovals.value = [
    ...localMcpApprovals.value.filter((item) => item.id !== approval.id),
    approval,
  ];
  store.noticeMessage = `${formatMcpActionLabel(action)} 已进入审批工作台。`;
  authCardCollapsed.value = false;
  approvalFollowupMode.value = false;
}

function completeLocalMcpApproval(approval, decision) {
  localMcpApprovals.value = localMcpApprovals.value.filter((item) => item.id !== approval.id && item.approvalId !== approval.approvalId);
  store.noticeMessage = buildMcpDecisionNotice(approval.action || {}, decision);
  approvalFollowupMode.value = decision === "decline" || decision === "reject";
  nextTick(() => jumpToLatest());
}

function handleTurnMcpAction(action) {
  if (!action || typeof action !== "object") return;
  if (action.bundleKind === "monitor_bundle") {
    void refreshMcpSurface(action);
    return;
  }
  if (action.bundleKind === "remediation_bundle") {
    openMcpSurfaceDrawer(action);
    return;
  }
  const intent = compactText(action.intent || action.key || action.action || "").toLowerCase();
  if (intent === "refresh" || intent === "reload" || intent === "open_detail" || intent === "open-detail") {
    void refreshMcpSurface(action);
    return;
  }
  if (action.uiKind || action.ui_kind || action.bundleId || action.bundle_id) {
    openMcpSurfaceDrawer(action);
    return;
  }
  if (isMcpMutationAction(action)) {
    queueLocalMcpApproval(action);
    return;
  }
  store.noticeMessage = `${formatMcpActionLabel(action)} 已作为只读操作加入当前会话。`;
  approvalFollowupMode.value = false;
  nextTick(() => jumpToLatest());
}

function handleMcpSurfaceDetail(payload) {
  openMcpSurfaceDrawer(payload);
}

function handleMcpSurfacePin(payload) {
  pinMcpSurface(payload);
}

function handleMcpSurfaceRefresh(payload) {
  void refreshMcpSurface(payload);
}

function isApprovalCard(card) {
  return card?.type === "CommandApprovalCard" || card?.type === "FileChangeApprovalCard";
}

function isTerminalOutputCard(card) {
  return card?.type === "CommandCard" || (card?.type === "StepCard" && !!card?.command);
}

const visibleCards = computed(() => {
  return store.snapshot.cards.filter((card) => {
    // Hide active plan card
    if (activePlanCard.value && card.id === activePlanCard.value.id && store.runtime.turn.active) {
      return false;
    }
    // Hide all pending approval cards from the chat stream (rendered in overlay)
    if (card.status === "pending" && isApprovalCard(card)) {
      return false;
    }
    if (card.id === "__codex_reconnect__") {
      return false;
    }
    const reconnectText = (card.message || card.text || "").trim();
    if (card.type === "ErrorCard" && reconnectErrorPattern.test(reconnectText)) {
      return false;
    }
    return true;
  });
});

const streamCards = computed(() =>
  visibleCards.value.filter((card) => {
    if (isApprovalCard(card)) {
      return false;
    }
    if (isTerminalOutputCard(card)) {
      return false;
    }
    return true;
  }),
);

const mainChatActiveProcess = computed(() => {
  if (!showThinkingCard.value && !hasTopFeedback.value) return null;
  const items = [
    ...viewedFileDetails.value.map((entry, index) => ({
      id: `viewed-file-${index}`,
      kind: "file",
      text: entry.label || entry.path || "",
    })),
    ...searchedQueryDetails.value.map((entry, index) => ({
      id: `searched-query-${index}`,
      kind: "search",
      text: entry.label || entry.query || "",
    })),
  ].filter((item) => item.text);

  return {
    phase: thinkingPhase.value,
    liveHint: activeActivityLine.value || thinkingCard.value.hint || "",
    summary: summaryLine.value || (!activeActivityLine.value ? activitySummary.value : ""),
    items,
  };
});

const mainChatTurns = computed(() =>
  formatMainChatTurns({
    conversationCards: streamCards.value.filter((card) => isChatConversationCard(card)),
    turnActive: showThinkingCard.value || store.runtime.turn.active,
    activeProcess: mainChatActiveProcess.value,
  }),
);

const mainChatTurnByAnchorId = computed(() =>
  new Map(mainChatTurns.value.map((turn) => [turn.anchorMessageId, turn])),
);

const baseStreamEntries = computed(() => {
  const entries = [];
  const renderedTurnIds = new Set();

  for (const card of streamCards.value) {
    if (isChatConversationCard(card)) {
      const turn = mainChatTurnByAnchorId.value.get(card.id);
      if (turn && !renderedTurnIds.has(turn.id)) {
        entries.push({
          id: turn.id,
          kind: "turn",
          turn,
        });
        renderedTurnIds.add(turn.id);
      }
      continue;
    }

    entries.push({
      id: `card-${card.id}`,
      kind: "card",
      card,
    });
  }

  if (!mainChatTurns.value.length && showThinking.value && hasTopFeedback.value) {
    entries.push({ id: "__activity__", kind: "activity" });
  }
  if (!mainChatTurns.value.length && showThinkingCard.value) {
    entries.push({ id: "__thinking__", kind: "thinking" });
  }

  return entries;
});

const historySessionKey = computed(() => store.activeSessionId || store.snapshot.sessionId || "");

const historyPager = useChatHistoryPager({
  items: baseStreamEntries,
  scrollContainer,
  resetKey: historySessionKey,
  pageSize: 10,
  initialCount: 10,
  topThreshold: 72,
});

const pagedStreamEntries = computed(() => historyPager.visibleItems.value);

function entrySignature(entry) {
  if (!entry) return "";
  if (entry.kind === "turn") {
    return [
      entry.id,
      entry.turn.processItems?.length || 0,
      entry.turn.finalMessage?.id || "",
      entry.turn.finalMessage?.card?.text?.length || 0,
      entry.turn.liveHint || "",
      entry.turn.summary || "",
    ].join(":");
  }
  if (entry.kind === "card") {
    const card = entry.card || {};
    return [
      entry.id,
      card.updatedAt || "",
      (card.text || card.message || "").length,
      (card.output || "").length,
      card.status || "",
    ].join(":");
  }
  if (entry.kind === "activity") {
    return [
      entry.id,
      activeActivityLine.value,
      summaryLine.value,
      viewedFileDetails.value.length,
      searchedQueryDetails.value.length,
    ].join(":");
  }
  return [entry.id, thinkingPhase.value, thinkingHint.value].join(":");
}

const historyStreamSignature = computed(() => {
  const list = pagedStreamEntries.value;
  const tail = list.slice(-3);
  return [
    tail.map((entry) => entrySignature(entry)).join("|"),
    activeActivityLine.value,
    summaryLine.value,
    showThinkingCard.value ? thinkingPhase.value : "",
    showThinkingCard.value ? thinkingHint.value : "",
  ].join("::");
});

const composerStatusHint = computed(() => {
  if (latestTerminalCard.value && !terminalDockVisible.value) {
    return "最近命令输出已收进终端面板，可随时展开查看。";
  }
  if (allowFollowUpComposer.value) {
    return "当前可以直接继续输入 follow-up。";
  }
  return "";
});

function previewForStreamEntry(entry) {
  if (!entry) return "";
  if (entry.kind === "turn") {
    return entry.turn?.finalMessage?.card?.text || entry.turn?.summary || entry.turn?.liveHint || "";
  }
  if (entry.kind === "card") {
    return entry.card?.summary || entry.card?.text || entry.card?.message || entry.card?.title || "";
  }
  if (entry.kind === "activity") {
    return activeActivityLine.value || summaryLine.value || "";
  }
  return thinkingCard.value.hint || "";
}

const { awaySummary } = useAwaySummary({
  items: baseStreamEntries,
  getItemId: (item) => String(item?.id || ""),
  getPreview: previewForStreamEntry,
});

const {
  isPinnedToBottom,
  unreadCount,
  unreadAnchorId,
  showUnreadPill,
  handleScroll,
  jumpToLatest,
} = useChatScrollState({
  scrollContainer,
  scrollContent,
  items: pagedStreamEntries,
  signature: historyStreamSignature,
  getItemId: (item) => String(item?.id || ""),
  threshold: 80,
});

const renderedStreamEntries = computed(() => {
  const entries = [];
  let awayInserted = false;
  for (const entry of pagedStreamEntries.value) {
    if (awaySummary.value?.anchorId && entry.id === awaySummary.value.anchorId) {
      entries.push({
        id: awaySummary.value.id,
        kind: "away-summary",
        summary: awaySummary.value,
      });
      awayInserted = true;
    }
    if (unreadAnchorId.value && entry.id === unreadAnchorId.value) {
      entries.push({
        id: `unread-divider-${entry.id}`,
        kind: "divider",
      });
    }
    entries.push(entry);
  }
  if (awaySummary.value && !awayInserted) {
    entries.push({
      id: awaySummary.value.id,
      kind: "away-summary",
      summary: awaySummary.value,
    });
  }
  return entries;
});

const virtualStream = useVirtualTurnList({
  items: renderedStreamEntries,
  scrollContainer,
  estimateSize(entry) {
    if (entry?.kind === "turn") return 172;
    if (entry?.kind === "divider") return 72;
    if (entry?.kind === "away-summary") return 110;
    if (entry?.kind === "activity") return 96;
    if (entry?.kind === "thinking") return 96;
    return 120;
  },
  overscan: 4,
  minItemCount: 18,
  getItemKey: (entry, index) => entry?.id || index,
});

const virtualizedStreamEntries = computed(() =>
  virtualStream.virtualItems.value.map((entry) => entry.item),
);
const showVirtualTopSpacer = computed(
  () => virtualStream.enabled.value && virtualStream.topSpacerHeight.value > 0,
);
const showVirtualBottomSpacer = computed(
  () => virtualStream.enabled.value && virtualStream.bottomSpacerHeight.value > 0,
);
const virtualTopSpacerHeight = computed(() => virtualStream.topSpacerHeight.value);
const virtualBottomSpacerHeight = computed(() => virtualStream.bottomSpacerHeight.value);

const historyTopSentinel = computed(() => historyPager.topSentinel.value);
const showHistoryBoundary = computed(() => !!historyTopSentinel.value);
const showSessionHistoryHint = computed(() => {
  return !historyTopSentinel.value && !store.loading && baseStreamEntries.value.length > 0 && (store.sessionList?.length > 1 || mainChatTurns.value.length >= 8);
});

function openHistoryFromSentinel() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent(OPEN_SESSION_HISTORY_EVENT, {
      detail: { source: "chat-history-sentinel" },
    }),
  );
}

async function loadOlderMessages() {
  await historyPager.loadOlder();
  await nextTick();
  virtualStream.syncViewport();
}

function handleChatScroll(event) {
  handleScroll(event);
  virtualStream.handleScroll(event);
  if (!historyAutoLoadArmed.value) {
    historyAutoLoadArmed.value = true;
    return;
  }
  historyPager.handleScroll(event);
}

function jumpToLatestAndSync() {
  jumpToLatest();
  nextTick(() => {
    virtualStream.syncViewport();
  });
}

watch(
  [activeActivityLine, summaryLine, () => store.runtime.turn.phase],
  ([currentLine, currentSummary, phase]) => {
    const hasFeedback = !!currentLine || !!currentSummary;
    const phaseMovedOn =
      phase === "planning" ||
      phase === "waiting_approval" ||
      phase === "waiting_input" ||
      phase === "executing" ||
      phase === "finalizing";

    if (hasFeedback || phaseMovedOn) {
      clearThinkingPrelude();
      if (showThinking.value) {
        thinkingPhase.value = phase;
      }
    } else if (phase === "thinking" && preferredThinkingPhase.value) {
      thinkingPhase.value = preferredThinkingPhase.value;
    }
  }
);

/* ---- Reconnection ---- */
const showReconnectBanner = computed(() => {
  return store.runtime.codex.status === "reconnecting" || isStopped.value;
});

const reconnectHostLabel = computed(() => {
  const host = store.selectedHost;
  if (!host) return "";
  const name = resolveHostDisplay(host);
  const status = host.status === "online" ? "在线" : host.status === "offline" ? "离线" : host.status || "未知";
  return `当前主机 ${name}（${status}）`;
});

const reconnectLabel = computed(() => {
  const c = store.runtime.codex;
  if (c.status === "stopped") return `与本地 ai-server 的实时连接已断开，无法恢复 · ${reconnectHostLabel.value}`;
  return `与本地 ai-server 的实时连接重连中 ${c.retryAttempt}/${c.retryMax} · ${reconnectHostLabel.value}`;
});

const isStopped = computed(() => store.runtime.codex.status === "stopped");

const codexReconnectNotice = computed(() => {
  return (
    store.snapshot.cards.find((card) => card.id === "__codex_reconnect__" && card.status === "inProgress") || null
  );
});

const showCodexReconnectBanner = computed(() => {
  return !!codexReconnectNotice.value && store.runtime.codex.status === "connected";
});

const codexReconnectLabel = computed(() => {
  return codexReconnectNotice.value?.message || codexReconnectNotice.value?.text || "与 GPT 的连接波动，正在自动恢复";
});

const selectedHostAlert = computed(() => {
  const host = store.selectedHost;
  if (!host || host.id === "server-local" || host.status === "online") {
    return "";
  }
  return `当前远程主机 ${resolveHostDisplay(host)} 离线，聊天与终端都不会静默回退到 server-local。`;
});

const composerPlaceholder = computed(() => {
  if (!store.snapshot.auth.connected) return "请先登录 GPT 账号后再开始对话";
  if (!store.snapshot.config.codexAlive) return "Codex app-server 当前不可用";
  if (allowFollowUpComposer.value) return "可以继续输入 follow-up，Cmd+Enter 发送";
  if (store.selectedHost.terminalCapable && !store.selectedHost.executable) {
    return "当前主机已接入远程终端，Codex 自动执行链路还未开启";
  }
  if (!store.selectedHost.executable) return "当前主机仅展示，不支持执行";
  if (store.selectedHost.status !== "online") return "当前主机离线，暂时不可执行";
  if (store.selectedHost.kind === "agent") return "Ask Codex to manage this host";
  return "Ask Codex to build something";
});

function getRowClass(card) {
  if (card.type === "UserMessageCard" || (card.type === "MessageCard" && card.role === "user")) {
    return "row-user";
  }
  if (card.type === "NoticeCard") {
    return "row-notice";
  }
  return "row-assistant";
}

async function sendMessage() {
  if (!store.canSend || !composerMessage.value.trim()) return;
  if (store.runtime.turn.active && !allowFollowUpComposer.value) return;

  const message = composerMessage.value.trim();
  store.sending = true;
  store.errorMessage = "";
  showThinking.value = true;
  queueThinkingPrelude(message);
  store.setTurnPhase(preferredThinkingPhase.value || "thinking");
  store.resetActivity();

  try {
    const response = await fetch("/api/v1/chat/message", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        message,
        hostId: store.snapshot.selectedHostId,
      }),
    });
    if (!response.ok) {
      const data = await response.json();
      store.errorMessage = data.error || "message send failed";
      showThinking.value = false;
      store.setTurnPhase("failed");
      clearThinkingPrelude();
    } else {
      composerMessage.value = "";
      approvalFollowupMode.value = false;
      nextTick(() => jumpToLatest());
    }
  } catch (e) {
    store.errorMessage = "Network error";
    showThinking.value = false;
    store.setTurnPhase("failed");
    clearThinkingPrelude();
  } finally {
    store.sending = false;
  }
}

async function stopMessage() {
  if (!store.runtime.turn.active) return;
  try {
    const response = await fetch("/api/v1/chat/stop", {
      method: "POST",
      credentials: "include",
    });
    const data = await response.json();
    if (!response.ok) {
      store.errorMessage = data.error || "stop failed";
      return;
    }
    store.errorMessage = "";
    showThinking.value = false;
    store.setTurnPhase("aborted");
    approvalFollowupMode.value = false;
    clearThinkingPrelude();
  } catch (e) {
    console.error(e);
    store.errorMessage = "stop failed";
  }
}

async function decideApproval({ approvalId, decision }) {
  const localApproval = localMcpApprovals.value.find((item) => item.id === approvalId || item.approvalId === approvalId);
  if (localApproval) {
    completeLocalMcpApproval(localApproval, decision);
    return;
  }
  try {
    store.setTurnPhase("executing");
    const response = await fetch(`/api/v1/approvals/${approvalId}/decision`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision }),
    });
    if (!response.ok) {
      const data = await response.json();
      store.errorMessage = data.error || "approval failed";
    } else {
      if (decision === "decline" || decision === "reject") {
        approvalFollowupMode.value = true;
      } else {
        approvalFollowupMode.value = false;
      }
      nextTick(() => jumpToLatest());
    }
  } catch (e) {
    console.error(e);
  }
}

async function handleChoice({ requestId, answers }) {
  try {
    store.setTurnPhase("thinking");
    const response = await fetch(`/api/v1/choices/${requestId}/answer`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ answers }),
    });
    if (!response.ok) {
      const data = await response.json();
      store.errorMessage = data.error || "choice submit failed";
      store.setTurnPhase("failed");
      return;
    }
    store.errorMessage = "";
    nextTick(() => jumpToLatest());
  } catch (e) {
    console.error(e);
    store.errorMessage = "choice submit failed";
    store.setTurnPhase("failed");
  }
}

function retryConnection() {
  store.runtime.codex.retryAttempt = 0;
  store.runtime.codex.status = "reconnecting";
  store.connectWs();
}

function handleRetry() {
  if (isStopped.value) {
    retryConnection();
    return;
  }
  store.fetchState();
}

function handleRefresh() {
  window.location.reload();
}

function isEditableTarget(target) {
  if (!target || !(target instanceof HTMLElement)) return false;
  const tagName = target.tagName ? target.tagName.toLowerCase() : "";
  return target.isContentEditable || tagName === "input" || tagName === "textarea" || tagName === "select";
}

async function ensureTerminalDockConnected() {
  await nextTick();
  if (!terminalDockVisible.value) return;
  if (terminalDockRef.value?.takeover) {
    await terminalDockRef.value.takeover();
  }
}

function toggleTerminalDock(forceVisible) {
  terminalDockVisible.value = typeof forceVisible === "boolean" ? forceVisible : !terminalDockVisible.value;
  if (!terminalDockVisible.value) {
    terminalDockSessionLive.value = false;
    return;
  }
  terminalDockSessionLive.value = terminalDockSessionLive.value || false;
  void ensureTerminalDockConnected();
}

function syncTerminalDockToHost() {
  if (!terminalDockRef.value) {
    return;
  }
  if (typeof terminalDockRef.value.reconnect === "function") {
    terminalDockRef.value.reconnect();
    return;
  }
  if (typeof terminalDockRef.value.takeover === "function") {
    terminalDockRef.value.takeover();
  }
}

function handleTerminalDockConnected() {
  terminalDockSessionLive.value = true;
}

function handleTerminalDockDisconnected() {
  terminalDockSessionLive.value = false;
}

function handleTerminalDockError() {
  terminalDockSessionLive.value = false;
}

function handleTerminalDockToggleKeydown(e) {
  const isBackquote = e.key === "`" || e.code === "Backquote";
  if (!isBackquote || !e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) {
    return;
  }
  e.preventDefault();
  toggleTerminalDock();
}

function handleTerminalDockResizeStart(e) {
  if (!terminalDockVisible.value) {
    return;
  }
  e.preventDefault();
  terminalDockDragging.value = true;
  const startY = e.clientY;
  const startHeight = terminalDockHeight.value;
  terminalDockMaxHeight = Math.max(260, Math.floor(window.innerHeight * 0.72));

  const handleMove = (moveEvent) => {
    const delta = startY - moveEvent.clientY;
    const nextHeight = Math.max(220, Math.min(terminalDockMaxHeight, startHeight + delta));
    terminalDockHeight.value = nextHeight;
  };

  const handleUp = () => {
    terminalDockDragging.value = false;
    window.removeEventListener("mousemove", handleMove);
    window.removeEventListener("mouseup", handleUp);
    terminalDockDragState = null;
  };

  terminalDockDragState = { handleMove, handleUp };
  window.addEventListener("mousemove", handleMove);
  window.addEventListener("mouseup", handleUp, { once: true });
}

function openWorkspaceProtocol() {
  if (!isWorkspaceSession.value) return;
  window.location.href = "/protocol";
}

watch(
  () => activity.value.viewedFiles,
  () => {
    showFileDetails.value = false;
  },
  { deep: true }
);

watch(
  () => activity.value.searchedWebQueries,
  () => {
    showSearchDetails.value = false;
  },
  { deep: true }
);

watch(
  () => activeApprovalCard.value?.id,
  (approvalID, previousID) => {
    if (!approvalID || approvalID === previousID) return;
    authCardCollapsed.value = false;
    approvalFollowupMode.value = false;
    thinkingPhase.value = "waiting_approval";
    showThinking.value = true;
    clearThinkingPrelude();
  }
);

watch(
  () => store.snapshot.cards[store.snapshot.cards.length - 1]?.id,
  (cardID, previousID) => {
    if (!cardID || cardID === previousID || !showThinking.value) return;
    const lastCard = store.snapshot.cards[store.snapshot.cards.length - 1];
    if (!lastCard) return;
    const isUserCard =
      lastCard.type === "UserMessageCard" ||
      (lastCard.type === "MessageCard" && lastCard.role === "user");
    if (isUserCard) return;
    clearThinkingPrelude();
  }
);

onBeforeUnmount(() => {
  clearThinkingPrelude();
  window.removeEventListener("keydown", handleTerminalDockToggleKeydown);
  if (terminalDockDragState?.handleMove) {
    window.removeEventListener("mousemove", terminalDockDragState.handleMove);
  }
  if (terminalDockDragState?.handleUp) {
    window.removeEventListener("mouseup", terminalDockDragState.handleUp);
  }
  terminalDockDragState = null;
});

onMounted(() => {
  window.addEventListener("keydown", handleTerminalDockToggleKeydown);
  terminalDockMaxHeight = Math.max(260, Math.floor(window.innerHeight * 0.72));
});

watch(
  () => store.snapshot.selectedHostId,
  () => {
    if (!terminalDockSessionLive.value && !terminalDockVisible.value) {
      return;
    }
    syncTerminalDockToHost();
  },
);

watch(
  () => store.canOpenTerminal,
  (canOpenTerminal, previous) => {
    if (!canOpenTerminal || canOpenTerminal === previous) {
      return;
    }
    if (!terminalDockVisible.value && !terminalDockSessionLive.value) {
      return;
    }
    syncTerminalDockToHost();
  },
);

watch(
  () => terminalDockVisible.value,
  async (visible, previous) => {
    if (visible === previous || !visible) {
      return;
    }
    terminalDockVisible.value = true;
    await ensureTerminalDockConnected();
  },
);
</script>

<template>
  <!-- Reconnection Banner -->
  <div class="reconnect-banner" v-if="showReconnectBanner">
    <WifiOffIcon size="14" />
    <span>{{ reconnectLabel }}</span>
    <button v-if="isStopped" class="reconnect-btn" @click="retryConnection">
      <RefreshCwIcon size="12" /> 重试
    </button>
  </div>

  <div class="reconnect-banner subtle" v-if="showCodexReconnectBanner">
    <WifiOffIcon size="14" />
    <span>{{ codexReconnectLabel }}</span>
  </div>

  <div class="chat-container" ref="scrollContainer" :style="chatContainerStyle" @scroll="handleChatScroll">
    <div class="chat-stream-inner" ref="scrollContent">
      <div v-if="store.loading" class="chat-banner loading-banner">
        <span class="spinner"></span> 正在初始化...
      </div>

      <div v-if="isWorkspaceSession" class="workspace-banner">
        <div class="workspace-banner-copy">
          <strong>{{ workspaceSessionLabel }}</strong>
          <span>下方卡片是后端投影出的只读过程和结果，不会直接改写当前会话。</span>
        </div>
        <button class="workspace-banner-btn" @click="openWorkspaceProtocol">{{ workspaceDetailLinkLabel }}</button>
      </div>

      <div v-if="!streamCards.length && !store.loading && !showThinking" class="empty-state-canvas">
        <BotIcon size="48" class="empty-icon" />
        <h2>What can I help you build?</h2>
        <p>I can help you write code, manage servers, execute commands, and orchestrate complex tasks.</p>
      </div>

      <p v-if="store.noticeMessage" class="chat-banner info">{{ store.noticeMessage }}</p>

      <p v-if="selectedHostAlert" class="chat-banner warn">{{ selectedHostAlert }}</p>

      <p v-if="store.errorMessage" class="chat-banner error">{{ store.errorMessage }}</p>

      <div class="chat-stream">
        <div v-if="showHistoryBoundary && historyTopSentinel" class="chat-history-sentinel" :class="`kind-${historyTopSentinel.kind}`" data-testid="chat-history-sentinel">
          <div class="chat-history-sentinel-copy">
            <span class="chat-history-sentinel-title">{{ historyTopSentinel.text }}</span>
            <span v-if="historyTopSentinel.detail" class="chat-history-sentinel-detail">{{ historyTopSentinel.detail }}</span>
          </div>
          <div class="chat-history-sentinel-actions">
            <button
              v-if="historyTopSentinel.kind === 'compact' || historyTopSentinel.kind === 'error'"
              type="button"
              class="chat-history-sentinel-btn primary"
              :data-testid="historyTopSentinel.kind === 'error' ? 'chat-history-sentinel-retry' : 'chat-history-sentinel-load-older'"
              @click="loadOlderMessages"
            >
              {{ historyTopSentinel.kind === 'error' ? '重试' : '加载更早消息' }}
            </button>
            <button
              v-if="historyTopSentinel.kind === 'compact' || historyTopSentinel.kind === 'error' || historyTopSentinel.kind === 'start'"
              type="button"
              class="chat-history-sentinel-btn"
              data-testid="chat-history-sentinel-open"
              @click="openHistoryFromSentinel"
            >
              查看完整历史
            </button>
          </div>
        </div>

        <div v-else-if="showSessionHistoryHint" class="chat-history-sentinel kind-hint" data-testid="chat-history-sentinel">
          <div class="chat-history-sentinel-copy">
            <span class="chat-history-sentinel-title">更早上下文可能已在历史中，完整会话可从历史列表查看。</span>
          </div>
          <div class="chat-history-sentinel-actions">
            <button
              type="button"
              class="chat-history-sentinel-btn"
              data-testid="chat-history-sentinel-open"
              @click="openHistoryFromSentinel"
            >
              打开历史
            </button>
          </div>
        </div>

        <div
          v-if="showVirtualTopSpacer"
          class="chat-virtual-spacer"
          data-testid="chat-virtual-top-spacer"
          :style="{ height: `${virtualTopSpacerHeight}px` }"
          aria-hidden="true"
        />

        <template v-for="entry in virtualizedStreamEntries" :key="entry.id">
          <div v-if="entry.kind === 'divider'" class="chat-unread-divider" data-testid="chat-unread-divider">
            <span class="chat-unread-divider-line" />
            <span class="chat-unread-divider-label">未读更新</span>
            <span class="chat-unread-divider-count">{{ unreadCount }} 条新结果</span>
            <span class="chat-unread-divider-line" />
          </div>

          <ChatTurnGroup
            v-else-if="entry.kind === 'turn'"
            :turn="entry.turn"
            @action="handleTurnMcpAction"
            @detail="handleMcpSurfaceDetail"
            @pin="handleMcpSurfacePin"
            @refresh="handleMcpSurfaceRefresh"
          />

          <div v-else-if="entry.kind === 'away-summary'" class="chat-away-summary" data-testid="chat-away-summary">
            <div class="chat-away-summary-kicker">你离开期间有新进展</div>
            <div class="chat-away-summary-body">
              离开 {{ entry.summary.durationLabel }}，期间新增 {{ entry.summary.newTurnCount || entry.summary.newEntryCount }} 条更新。
            </div>
            <div v-if="entry.summary.latestPreview" class="chat-away-summary-preview">
              最新结果：{{ entry.summary.latestPreview }}
            </div>
          </div>

          <div
            v-else-if="entry.kind === 'card'"
            class="stream-row"
            :class="getRowClass(entry.card)"
          >
            <CardItem
              :card="entry.card"
              :session-kind="store.snapshot.kind"
              @approval="decideApproval"
              @choice="handleChoice"
              @retry="handleRetry"
              @refresh="handleRefresh"
            />
          </div>

          <div v-else-if="entry.kind === 'activity'" class="activity-summary">
            <button
              v-if="activeActivityLine"
              type="button"
              class="activity-line plain"
              :disabled="!activeLineExpandable"
              @click="toggleActiveLineDetails"
            >
              {{ activeActivityLine }}
            </button>

            <button
              v-else-if="summaryLine"
              type="button"
              class="activity-line"
              :disabled="!summaryExpandable"
              @click="toggleSummaryDetails"
            >
              {{ summaryLine }}
            </button>

            <div v-if="showFileDetails && viewedFileDetails.length" class="activity-details">
              <div v-for="entryItem in viewedFileDetails" :key="entryItem.label || entryItem.path" class="activity-detail-item">
                {{ entryItem.label || entryItem.path }}
              </div>
            </div>

            <div v-if="showSearchDetails && searchedQueryDetails.length" class="activity-details">
              <div v-for="entryItem in searchedQueryDetails" :key="entryItem.label || entryItem.query" class="activity-detail-item">
                {{ entryItem.label || entryItem.query }}
              </div>
            </div>
          </div>

          <div v-else-if="entry.kind === 'thinking'" class="stream-row row-assistant">
            <ThinkingCard :card="thinkingCard" />
          </div>
        </template>

        <div
          v-if="showVirtualBottomSpacer"
          class="chat-virtual-spacer"
          data-testid="chat-virtual-bottom-spacer"
          :style="{ height: `${virtualBottomSpacerHeight}px` }"
          aria-hidden="true"
        />
      </div>
    </div>
  </div>

  <button
    v-if="showUnreadPill"
    type="button"
    class="chat-unread-pill"
    data-testid="chat-unread-pill"
    @click="jumpToLatestAndSync"
  >
    {{ unreadCount }} 条新结果
  </button>

  <ChatComposerDock
    v-model="composerMessage"
    :placeholder="composerPlaceholder"
    :allow-follow-up="allowFollowUpComposer"
    :disabled="isStopped"
    :plan-card="activePlanCard"
    :session-kind="store.snapshot.kind"
    :status-hint="composerStatusHint"
    :show-composer="!hasActiveApprovalOverlay || authCardCollapsed"
    :is-docked-bottom="!!activePlanCard || hasActiveApprovalOverlay"
    @send="sendMessage"
    @stop="stopMessage"
  >
    <template #terminal>
      <div class="chat-terminal-dock-wrap">
        <div class="chat-terminal-toolbar">
          <button
            data-testid="chat-terminal-toggle"
            class="chat-terminal-toggle"
            :class="{ active: terminalDockVisible }"
            :disabled="isWorkspaceSession"
            @click="toggleTerminalDock()"
          >
            <TerminalIcon size="14" />
            <span>{{ terminalDockVisible ? "隐藏终端" : "显示终端" }}</span>
          </button>
          <span class="chat-terminal-toolbar-label">{{ terminalDockToolbarLabel }}</span>
          <span class="chat-terminal-shortcut">Ctrl + `</span>
        </div>

        <div
          v-if="terminalDockVisible"
          data-testid="chat-terminal-dock"
          class="chat-terminal-dock"
          :class="{ dragging: terminalDockDragging }"
          :style="{ height: `${terminalDockHeight}px` }"
        >
          <button
            data-testid="chat-terminal-resizer"
            class="chat-terminal-resizer"
            title="拖拽调整终端高度"
            @mousedown="handleTerminalDockResizeStart"
          >
            拖拽调整高度
          </button>
          <div class="chat-terminal-frame">
            <WorkspaceHostTerminal
              :key="terminalDockHost.id || store.snapshot.selectedHostId"
              ref="terminalDockRef"
              :host-id="terminalDockHost.id || store.snapshot.selectedHostId"
              :host-name="terminalDockHostLabel"
              :title="terminalDockTitle"
              :subtitle="terminalDockSubtitle"
              :output="terminalDockOutput"
              :allow-takeover="terminalDockCanTakeover"
              :auto-takeover="terminalDockCanTakeover"
              :panel-height="terminalDockPanelHeight"
              @connected="handleTerminalDockConnected"
              @disconnected="handleTerminalDockDisconnected"
              @error="handleTerminalDockError"
            />
          </div>
        </div>
      </div>
    </template>

    <template #approval>
      <div v-if="activeApprovalCard || activeMcpApproval" class="auth-overlay-dock">
        <div v-if="!authCardCollapsed" class="auth-overlay-container">
          <div class="auth-overlay-header">
            <div class="auth-overlay-title-group">
              <span class="auth-overlay-title">需要您的确认</span>
              <span v-if="activeApprovalCard && activeApprovalQueueLabel" class="auth-overlay-queue-label">{{ activeApprovalQueueLabel }}</span>
              <span v-else-if="activeMcpApproval" class="auth-overlay-queue-label">MCP 变更待确认</span>
            </div>
            <button class="icon-btn auth-collapse-btn" @click="authCardCollapsed = true">折叠审批工作台</button>
          </div>
          <div v-if="activeApprovalCard && activeApprovalQueueNote" class="auth-overlay-queue-note">{{ activeApprovalQueueNote }}</div>
          <CardItem
            v-if="activeApprovalCard"
            :card="activeApprovalCard"
            :session-kind="store.snapshot.kind"
            :is-overlay="true"
            @approval="decideApproval"
          />
          <div
            v-else-if="activeMcpApproval"
            class="chat-mcp-approval"
            data-testid="chat-mcp-approval-overlay"
          >
            <div class="chat-mcp-approval-copy">
              <strong>{{ activeMcpApproval.title }}</strong>
              <p>{{ activeMcpApproval.summary }}</p>
            </div>
            <dl class="chat-mcp-approval-meta">
              <div class="chat-mcp-approval-row">
                <dt>目标</dt>
                <dd>{{ formatMcpActionTarget(activeMcpApproval.action || {}, activeMcpApproval.action?.scope || {}) }}</dd>
              </div>
              <div class="chat-mcp-approval-row">
                <dt>权限</dt>
                <dd>{{ activeMcpApproval.action?.permissionPath || "未声明" }}</dd>
              </div>
            </dl>
            <div class="chat-mcp-approval-actions">
              <button
                type="button"
                class="option-row secondary"
                data-testid="chat-mcp-approval-reject"
                @click="decideApproval({ approvalId: activeMcpApproval.approvalId, decision: 'reject' })"
              >
                拒绝
              </button>
              <button
                type="button"
                class="option-row primary"
                data-testid="chat-mcp-approval-accept"
                @click="decideApproval({ approvalId: activeMcpApproval.approvalId, decision: 'accept' })"
              >
                同意执行
              </button>
            </div>
          </div>
        </div>

        <button v-else class="auth-restore-btn" @click="authCardCollapsed = false">
           <span>当前审批工作台已折叠</span>
           <span v-if="activeApprovalQueueLabel" class="auth-restore-queue">{{ activeApprovalQueueLabel }}</span>
        </button>
      </div>
    </template>
  </ChatComposerDock>

  <transition name="chat-mcp-drawer-fade">
    <div
      v-if="isMcpDrawerOpen && activeMcpSurface"
      class="chat-mcp-surface-drawer"
      data-testid="chat-mcp-surface-drawer"
      @click.self="closeMcpSurfaceDrawer"
    >
      <aside class="chat-mcp-surface-panel" :data-surface-kind="activeMcpSurface.kind">
        <header class="chat-mcp-surface-head">
          <div class="chat-mcp-surface-copy">
            <span class="chat-mcp-surface-kicker">MCP SURFACE</span>
            <h3>{{ activeMcpSurface.title }}</h3>
            <p v-if="activeMcpSurface.subtitle">{{ activeMcpSurface.subtitle }}</p>
          </div>
          <button type="button" class="chat-mcp-surface-close" data-testid="chat-mcp-surface-close" @click="closeMcpSurfaceDrawer">
            关闭
          </button>
        </header>

        <div class="chat-mcp-surface-toolbar">
          <button
            type="button"
            class="chat-mcp-surface-toolbar-btn"
            data-testid="chat-mcp-surface-pin"
            @click="pinMcpSurface(activeMcpSurface.raw)"
          >
            固定到 MCP 面板
          </button>
          <button
            type="button"
            class="chat-mcp-surface-toolbar-btn"
            data-testid="chat-mcp-surface-refresh"
            @click="handleMcpSurfaceRefresh(activeMcpSurface.raw)"
          >
            刷新
          </button>
          <button
            type="button"
            class="chat-mcp-surface-toolbar-btn"
            data-testid="chat-mcp-surface-open-global"
            @click="dispatchOpenMcpDrawer(activeMcpSurface, true)"
          >
            同步到全局 drawer
          </button>
        </div>

        <div v-if="mcpPinnedSurfaces.length" class="chat-mcp-surface-pins">
          <span class="chat-mcp-surface-pins-label">已固定</span>
          <div class="chat-mcp-surface-pin-list">
            <button
              v-for="surface in mcpPinnedSurfaces"
              :key="mcpSurfaceKey(surface)"
              type="button"
              class="chat-mcp-surface-pin-chip"
              :data-testid="`chat-mcp-surface-pin-${mcpSurfaceKey(surface)}`"
              @click="openMcpSurfaceDrawer(surface.raw || surface, { pin: true })"
            >
              {{ surface.title }}
            </button>
          </div>
        </div>

        <div class="chat-mcp-surface-body">
          <McpBundleHost
            v-if="activeMcpSurface.kind === 'bundle'"
            :bundle="activeMcpSurface.raw"
            @action="handleTurnMcpAction"
            @open-detail="handleMcpSurfaceDetail"
            @pin="handleMcpSurfacePin"
          />
          <McpUiCardHost
            v-else
            :card="activeMcpSurface.raw"
            @action="handleTurnMcpAction"
            @detail="handleMcpSurfaceDetail"
            @refresh="handleMcpSurfaceRefresh"
          />
        </div>
      </aside>
    </div>
  </transition>
</template>

<style scoped>
.reconnect-banner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 6px 14px;
  background: #fef3c7;
  color: #92400e;
  font-size: 12px;
  font-weight: 500;
  border-bottom: 1px solid #fde68a;
}

.reconnect-banner.subtle {
  background: #eff6ff;
  color: #1d4ed8;
  border-bottom-color: #bfdbfe;
}

.reconnect-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  border: 1px solid #d97706;
  background: white;
  color: #92400e;
  cursor: pointer;
  margin-left: 8px;
}

.reconnect-btn:hover {
  background: #fef3c7;
}

.workspace-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin: 8px 0 4px;
  padding: 10px 12px;
  margin-left: 36px;
  max-width: 720px;
  border-radius: 12px;
  border: 1px solid #dbeafe;
  background: linear-gradient(135deg, #eff6ff, #ffffff);
}

.workspace-banner-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: #1e293b;
}

.workspace-banner-copy strong {
  font-size: 12px;
  font-weight: 700;
}

.workspace-banner-copy span {
  font-size: 11px;
  color: #475569;
  line-height: 1.45;
}

.workspace-banner-btn {
  flex-shrink: 0;
  border: 1px solid #bfdbfe;
  background: #ffffff;
  color: #1d4ed8;
  border-radius: 999px;
  padding: 7px 12px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}

.workspace-banner-btn:hover {
  background: #eff6ff;
}

.chat-unread-divider {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 2px 0 6px;
}

.chat-unread-divider-line {
  flex: 1;
  height: 1px;
  background: rgba(59, 130, 246, 0.18);
}

.chat-unread-divider-label {
  color: #1d4ed8;
  font-size: 12px;
  font-weight: 700;
  line-height: 1.4;
}

.chat-unread-divider-count {
  color: #64748b;
  font-size: 12px;
  line-height: 1.4;
}

.chat-virtual-spacer {
  width: 100%;
  flex: none;
  pointer-events: none;
}

.chat-unread-pill {
  position: fixed;
  left: 50%;
  bottom: 118px;
  transform: translateX(-50%);
  z-index: 18;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 34px;
  padding: 0 14px;
  border: 1px solid rgba(59, 130, 246, 0.18);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.96);
  box-shadow: 0 12px 30px rgba(15, 23, 42, 0.12);
  color: #1d4ed8;
  font-size: 12.5px;
  font-weight: 600;
}

.chat-history-sentinel {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin: 0 0 10px 36px;
  padding: 8px 12px;
  border-radius: 12px;
  background: rgba(248, 250, 252, 0.92);
  border: 1px solid #e2e8f0;
  color: #64748b;
  font-size: 12px;
  line-height: 1.5;
}

.chat-history-sentinel-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.chat-history-sentinel-title {
  color: #0f172a;
  font-weight: 600;
}

.chat-history-sentinel-detail {
  color: #64748b;
}

.chat-history-sentinel-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.chat-history-sentinel-btn {
  border: none;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.08);
  color: #0f172a;
  font-size: 12px;
  font-weight: 600;
  padding: 5px 10px;
  cursor: pointer;
}

.chat-history-sentinel-btn.primary {
  background: #0f172a;
  color: white;
}

.chat-history-sentinel-btn:hover {
  background: rgba(15, 23, 42, 0.12);
}

.chat-history-sentinel-btn.primary:hover {
  background: #1e293b;
}

.chat-away-summary {
  margin: 0 0 10px 36px;
  padding: 10px 12px;
  border-radius: 14px;
  background: rgba(239, 246, 255, 0.92);
  border: 1px solid rgba(147, 197, 253, 0.35);
  color: #0f172a;
}

.chat-away-summary-kicker {
  color: #1d4ed8;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.chat-away-summary-body {
  margin-top: 4px;
  font-size: 13px;
  line-height: 1.55;
}

.chat-away-summary-preview {
  margin-top: 6px;
  color: #475569;
  font-size: 12px;
  line-height: 1.5;
}

.activity-summary {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 4px 0;
  margin-left: 36px;
  animation: fadeInUp 0.2s ease-out;
}

.activity-line {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  padding: 0;
  border: none;
  background: transparent;
  font-size: var(--text-meta-size, 11px);
  color: var(--text-meta, #9ca3af);
  font-weight: 500;
  cursor: pointer;
}

.activity-line:disabled {
  cursor: default;
}

.activity-line:not(:disabled):hover {
  color: #6b7280;
}

.activity-line.plain {
  color: #9ca3af;
}

.activity-details {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin-top: 2px;
  padding-left: 10px;
  color: #94a3b8;
  font-size: 11px;
}

.activity-detail-item {
  line-height: 1.45;
}

.chat-terminal-dock-wrap {
  width: 100%;
  margin-bottom: 6px;
}

.chat-terminal-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 4px 6px;
}

.chat-terminal-toggle {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 1px solid rgba(148, 163, 184, 0.28);
  background: rgba(255, 255, 255, 0.86);
  color: #0f172a;
  border-radius: 999px;
  padding: 6px 10px;
  font-size: 11px;
  font-weight: 700;
  cursor: pointer;
}

.chat-terminal-toggle.active {
  background: #eff6ff;
  color: #1d4ed8;
  border-color: rgba(96, 165, 250, 0.45);
}

.chat-terminal-toggle:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.chat-terminal-toolbar-label {
  flex: 1;
  min-width: 0;
  font-size: 11px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.chat-terminal-shortcut {
  flex-shrink: 0;
  font-size: 10px;
  color: #94a3b8;
  font-weight: 700;
}

.chat-terminal-dock {
  display: flex;
  flex-direction: column;
  width: 100%;
  border-radius: 14px;
  overflow: hidden;
  background: rgba(15, 23, 42, 0.98);
  border: 1px solid rgba(148, 163, 184, 0.18);
  box-shadow: 0 14px 36px rgba(15, 23, 42, 0.14);
}

.chat-terminal-dock.dragging {
  box-shadow: 0 22px 56px rgba(15, 23, 42, 0.24);
}

.chat-terminal-resizer {
  border: none;
  width: 100%;
  padding: 7px 12px;
  background: linear-gradient(180deg, rgba(15, 23, 42, 0.94), rgba(15, 23, 42, 0.9));
  color: #94a3b8;
  font-size: 11px;
  font-weight: 700;
  cursor: ns-resize;
  border-bottom: 1px solid rgba(148, 163, 184, 0.18);
}

.chat-terminal-frame {
  flex: 1;
  min-height: 0;
}

.omnibar-stack {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: 820px;
  margin: 0 auto;
}

.runtime-plan-dock {
  width: 100%;
  z-index: 6;
  position: relative;
  /* Shift down slightly to cover top border of omnibar if needed, though we set it to transparent anyway */
  transform: translateY(1px);
}

.auth-overlay-dock {
  width: 100%;
  z-index: 10;
  margin-bottom: 0;
  position: relative;
  transform: translateY(1px);
}

.auth-overlay-container {
  background: white;
  border: 1px solid var(--border-color);
  border-radius: 12px;
  box-shadow: 0 8px 22px rgba(15, 23, 42, 0.08);
  overflow: hidden;
}

.auth-overlay-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 12px;
  border-bottom: 1px solid #f1f5f9;
  background: #f8fafc;
}

.auth-overlay-title-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.auth-overlay-title {
  font-size: 11px;
  font-weight: 600;
  color: #fb923c;
}

.auth-overlay-queue-label {
  font-size: 10px;
  color: #64748b;
  font-weight: 500;
}

.auth-overlay-queue-note {
  padding: 6px 12px 0;
  font-size: 10px;
  color: #94a3b8;
}

.chat-mcp-surface-drawer {
  position: fixed;
  inset: 0;
  z-index: 28;
  display: flex;
  justify-content: flex-end;
  background: rgba(15, 23, 42, 0.26);
  backdrop-filter: blur(8px);
}

.chat-mcp-surface-panel {
  width: min(760px, calc(100vw - 32px));
  height: calc(100vh - 28px);
  margin: 14px 14px 14px 0;
  display: grid;
  gap: 12px;
  padding: 16px;
  border-radius: 24px;
  border: 1px solid rgba(15, 23, 42, 0.08);
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.99), rgba(248, 250, 252, 0.99));
  box-shadow: 0 24px 70px rgba(15, 23, 42, 0.22);
  overflow: auto;
}

.chat-mcp-surface-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.chat-mcp-surface-copy {
  display: grid;
  gap: 4px;
}

.chat-mcp-surface-kicker {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #0ea5e9;
}

.chat-mcp-surface-copy h3 {
  margin: 0;
  font-size: 18px;
  color: #0f172a;
}

.chat-mcp-surface-copy p {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: #475569;
}

.chat-mcp-surface-close,
.chat-mcp-surface-toolbar-btn,
.chat-mcp-surface-pin-chip {
  border: none;
  border-radius: 999px;
  background: rgba(226, 232, 240, 0.9);
  color: #0f172a;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.chat-mcp-surface-close {
  padding: 8px 12px;
}

.chat-mcp-surface-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.chat-mcp-surface-toolbar-btn {
  padding: 8px 12px;
}

.chat-mcp-surface-pins {
  display: grid;
  gap: 8px;
  padding: 12px;
  border-radius: 16px;
  background: rgba(248, 250, 252, 0.96);
  border: 1px solid rgba(148, 163, 184, 0.18);
}

.chat-mcp-surface-pins-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #64748b;
}

.chat-mcp-surface-pin-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.chat-mcp-surface-pin-chip {
  padding: 7px 12px;
}

.chat-mcp-surface-body {
  display: grid;
  gap: 12px;
}

.chat-mcp-drawer-fade-enter-active,
.chat-mcp-drawer-fade-leave-active {
  transition: opacity 0.18s ease;
}

.chat-mcp-drawer-fade-enter-from,
.chat-mcp-drawer-fade-leave-to {
  opacity: 0;
}

.chat-mcp-approval {
  display: grid;
  gap: 12px;
  padding: 12px;
}

.chat-mcp-approval-copy {
  display: grid;
  gap: 4px;
}

.chat-mcp-approval-copy strong {
  font-size: 14px;
  color: #0f172a;
}

.chat-mcp-approval-copy p {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: #475569;
}

.chat-mcp-approval-meta {
  display: grid;
  gap: 8px;
  margin: 0;
}

.chat-mcp-approval-row {
  display: grid;
  grid-template-columns: 52px 1fr;
  gap: 10px;
  font-size: 13px;
}

.chat-mcp-approval-row dt {
  color: #64748b;
}

.chat-mcp-approval-row dd {
  margin: 0;
  color: #0f172a;
}

.chat-mcp-approval-actions {
  display: flex;
  gap: 8px;
}

.auth-collapse-btn {
  font-size: 11px;
  color: #64748b;
  background: none;
  border: none;
  cursor: pointer;
}
.auth-collapse-btn:hover {
  text-decoration: underline;
}

.auth-restore-btn {
  width: 100%;
  padding: 10px;
  background: #fff7ed;
  border: 1px solid #fed7aa;
  border-radius: 10px;
  color: #c2410c;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  margin-bottom: 6px;
  box-shadow: 0 3px 10px rgba(234, 88, 12, 0.04);
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
}

.auth-restore-btn:hover {
  background: #ffedd5;
}

.auth-restore-queue {
  font-size: 11px;
  color: #ea580c;
  font-weight: 500;
}

.row-notice {
  justify-content: center;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>
