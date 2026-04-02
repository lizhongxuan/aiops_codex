<script setup>
import { computed, defineAsyncComponent, ref, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { useAppStore } from "../store";
import { resolveHostDisplay } from "../lib/hostDisplay";
import CardItem from "../components/CardItem.vue";
import Omnibar from "../components/Omnibar.vue";
import ThinkingCard from "../components/ThinkingCard.vue";
import PlanCard from "../components/PlanCard.vue";
import { BotIcon, WifiOffIcon, RefreshCwIcon, TerminalIcon } from "lucide-vue-next";

const store = useAppStore();
const WorkspaceHostTerminal = defineAsyncComponent(() => import("../components/workspace/WorkspaceHostTerminal.vue"));

const composerMessage = ref("");
const scrollContainer = ref(null);
const scrollContent = ref(null);
const showFileDetails = ref(false);
const showSearchDetails = ref(false);
const authCardCollapsed = ref(false);
const approvalFollowupMode = ref(false);
const autoFollowTail = ref(true);
const terminalDockVisible = ref(false);
const terminalDockHeight = ref(320);
const terminalDockSessionLive = ref(false);
const terminalDockRef = ref(null);
const terminalDockDragging = ref(false);
let contentResizeObserver = null;
let terminalDockDragState = null;
let terminalDockMaxHeight = 560;

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

const allowFollowUpComposer = computed(() => approvalFollowupMode.value && !activeApprovalCard.value);
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
    ? `${terminalDockHeight.value + (activePlanCard.value || activeApprovalCard.value ? 260 : 220)}px`
    : "240px",
}));

const visibleCards = computed(() => {
  return store.snapshot.cards.filter((card) => {
    // Hide active plan card
    if (activePlanCard.value && card.id === activePlanCard.value.id && store.runtime.turn.active) {
      return false;
    }
    // Hide all pending approval cards from the chat stream (rendered in overlay)
    if (card.status === "pending" && (card.type === "CommandApprovalCard" || card.type === "FileChangeApprovalCard")) {
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

function bottomDistance(el) {
  if (!el) return 0;
  return Math.max(el.scrollHeight - el.scrollTop - el.clientHeight, 0);
}

function isNearBottom(el, threshold = 80) {
  return bottomDistance(el) <= threshold;
}

function scrollToBottom(force = false) {
  const el = scrollContainer.value;
  if (!el || (!force && !autoFollowTail.value)) return;
  el.scrollTop = el.scrollHeight;
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
      autoFollowTail.value = true;
      nextTick(() => scrollToBottom(true));
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
      autoFollowTail.value = true;
      nextTick(() => scrollToBottom(true));
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
    autoFollowTail.value = true;
    nextTick(() => scrollToBottom(true));
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

function handleScroll(e) {
  const el = e.target;
  autoFollowTail.value = isNearBottom(el);
}

watch(
  () => ({
    cardCount: visibleCards.value.length,
    lastCardId: visibleCards.value[visibleCards.value.length - 1]?.id || "",
    lastCardUpdatedAt: visibleCards.value[visibleCards.value.length - 1]?.updatedAt || "",
    lastCardTextLength: (visibleCards.value[visibleCards.value.length - 1]?.text || "").length,
    lastCardOutputLength: (visibleCards.value[visibleCards.value.length - 1]?.output || "").length,
    thinking: showThinkingCard.value,
    feedback: hasTopFeedback.value,
  }),
  async () => {
    await nextTick();
    scrollToBottom();
  },
  { deep: true }
);

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
  if (contentResizeObserver) {
    contentResizeObserver.disconnect();
    contentResizeObserver = null;
  }
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
  nextTick(() => scrollToBottom(true));
  if (typeof ResizeObserver === "undefined" || !scrollContent.value) {
    return;
  }
  contentResizeObserver = new ResizeObserver(() => {
    scrollToBottom();
  });
  contentResizeObserver.observe(scrollContent.value);
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

  <div class="chat-container" ref="scrollContainer" :style="chatContainerStyle" @scroll="handleScroll">
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

      <div v-if="!visibleCards.length && !store.loading && !showThinking" class="empty-state-canvas">
        <BotIcon size="48" class="empty-icon" />
        <h2>What can I help you build?</h2>
        <p>I can help you write code, manage servers, execute commands, and orchestrate complex tasks.</p>
      </div>

      <p v-if="store.noticeMessage" class="chat-banner info">{{ store.noticeMessage }}</p>

      <p v-if="selectedHostAlert" class="chat-banner warn">{{ selectedHostAlert }}</p>

      <p v-if="store.errorMessage" class="chat-banner error">{{ store.errorMessage }}</p>

      <div class="chat-stream">
        <div
          v-for="card in visibleCards"
          :key="card.id"
          class="stream-row"
          :class="getRowClass(card)"
        >
          <CardItem
            :card="card"
            :session-kind="store.snapshot.kind"
            @approval="decideApproval"
            @choice="handleChoice"
            @retry="handleRetry"
            @refresh="handleRefresh"
          />
        </div>

        <div v-if="showThinking && hasTopFeedback" class="activity-summary">
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
            <div v-for="entry in viewedFileDetails" :key="entry.label || entry.path" class="activity-detail-item">
              {{ entry.label || entry.path }}
            </div>
          </div>

          <div v-if="showSearchDetails && searchedQueryDetails.length" class="activity-details">
            <div v-for="entry in searchedQueryDetails" :key="entry.label || entry.query" class="activity-detail-item">
              {{ entry.label || entry.query }}
            </div>
          </div>
        </div>

        <div v-if="showThinkingCard" class="stream-row row-assistant">
          <ThinkingCard :card="thinkingCard" />
        </div>
      </div>
    </div>
  </div>

  <footer class="omnibar-dock">
    <div class="omnibar-stack">
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

      <div v-if="activePlanCard" class="runtime-plan-dock">
        <PlanCard :card="activePlanCard" :session-kind="store.snapshot.kind" compact />
      </div>

      <!-- Auth Overlay -->
      <div v-if="activeApprovalCard" class="auth-overlay-dock">
        <div v-if="!authCardCollapsed" class="auth-overlay-container">
          <div class="auth-overlay-header">
             <div class="auth-overlay-title-group">
               <span class="auth-overlay-title">需要您的确认</span>
               <span v-if="activeApprovalQueueLabel" class="auth-overlay-queue-label">{{ activeApprovalQueueLabel }}</span>
             </div>
             <button class="icon-btn auth-collapse-btn" @click="authCardCollapsed = true">折叠审批工作台</button>
          </div>
          <div v-if="activeApprovalQueueNote" class="auth-overlay-queue-note">{{ activeApprovalQueueNote }}</div>
          <CardItem :card="activeApprovalCard" :session-kind="store.snapshot.kind" :is-overlay="true" @approval="decideApproval" />
        </div>
        
        <button v-else class="auth-restore-btn" @click="authCardCollapsed = false">
           <span>当前审批工作台已折叠</span>
           <span v-if="activeApprovalQueueLabel" class="auth-restore-queue">{{ activeApprovalQueueLabel }}</span>
        </button>
      </div>

      <Omnibar
        v-if="!activeApprovalCard || authCardCollapsed"
        v-model="composerMessage"
        :placeholder="composerPlaceholder"
        :allow-follow-up="allowFollowUpComposer"
        @send="sendMessage"
        @stop="stopMessage"
        :disabled="isStopped"
        :is-docked-bottom="!!activePlanCard || !!activeApprovalCard"
      />
    </div>
  </footer>
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
