<script setup>
import { computed, ref, watch, nextTick } from "vue";
import { useAppStore } from "../store";
import CardItem from "../components/CardItem.vue";
import Omnibar from "../components/Omnibar.vue";
import ThinkingCard from "../components/ThinkingCard.vue";
import PlanCard from "../components/PlanCard.vue";
import { BotIcon, WifiOffIcon, RefreshCwIcon } from "lucide-vue-next";

const store = useAppStore();

const composerMessage = ref("");
const scrollContainer = ref(null);
const showFileDetails = ref(false);
const showSearchDetails = ref(false);
const authCardCollapsed = ref(false);
let isUserScrolling = false;

/* ---- ThinkingCard local state ---- */
const showThinking = ref(false);
const thinkingPhase = ref("thinking");

const thinkingCard = computed(() => ({
  id: "__thinking__",
  type: "ThinkingCard",
  phase: thinkingPhase.value,
}));

watch(
  () => store.runtime.turn.phase,
  (phase) => {
    if (phase === "idle" || phase === "completed" || phase === "failed" || phase === "aborted") {
      showThinking.value = false;
    } else {
      thinkingPhase.value = phase;
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
  if (a.searchCount > 0) parts.push(`${a.searchCount} 个搜索`);
  if (a.listCount > 0) parts.push(`${a.listCount} 个列表`);
  if (a.commandsRun > 0) parts.push(`${a.commandsRun} 个命令`);
  if (parts.length === 0) return "";
  if (a.filesViewed > 0) return `已浏览 ${parts.join("，")}`;
  return `已处理 ${parts.join("，")}`;
});

const currentReadingLine = computed(() => {
  const file = activity.value.currentReadingFile;
  return file ? `现在浏览 ${file}` : "";
});

const currentSearchLine = computed(() => {
  const a = activity.value;
  if (a.currentWebSearchQuery) {
    return `现在搜索网页（${truncateLabel(a.currentWebSearchQuery)}）`;
  }
  if (!a.searchedWebQueries?.length) return "";
  const latest = a.searchedWebQueries[a.searchedWebQueries.length - 1];
  const label = latest?.query || latest?.label || "";
  if (!label) return "";
  return `已搜索网页（${truncateLabel(label)}）`;
});

const viewedFileDetails = computed(() => activity.value.viewedFiles || []);
const searchedQueryDetails = computed(() => activity.value.searchedWebQueries || []);

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
    return true;
  });
});

/* ---- Reconnection ---- */
const showReconnectBanner = computed(() => {
  return store.runtime.codex.status === "reconnecting";
});

const reconnectLabel = computed(() => {
  const c = store.runtime.codex;
  if (c.status === "stopped") return "连接已断开，无法恢复";
  return `Reconnecting... ${c.retryAttempt}/${c.retryMax}`;
});

const isStopped = computed(() => store.runtime.codex.status === "stopped");

const connectionErrorCard = computed(() => {
  if (!isStopped.value) return null;
  const retryMax = store.runtime.codex.retryMax || 5;
  const lastError = store.runtime.codex.lastError;
  return {
    id: "__codex_stopped__",
    type: "ErrorCard",
    title: "Codex app-server 已断开",
    message: lastError
      ? `连接恢复失败，已尝试 ${retryMax} 次。最后错误：${lastError}`
      : `连接恢复失败，已达到 ${retryMax} 次重试上限。`,
    retryable: true,
  };
});

const composerPlaceholder = computed(() => {
  if (!store.snapshot.auth.connected) return "请先登录 GPT 账号后再开始对话";
  if (!store.snapshot.config.codexAlive) return "Codex app-server 当前不可用";
  if (!store.selectedHost.executable) return "当前主机仅展示，不支持执行";
  if (store.selectedHost.status !== "online") return "当前主机离线，暂时不可执行";
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
  if (!store.canSend || !composerMessage.value.trim() || store.runtime.turn.active) return;

  store.sending = true;
  store.errorMessage = "";
  showThinking.value = true;
  thinkingPhase.value = "thinking";
  store.setTurnPhase("thinking");
  store.resetActivity();

  try {
    const response = await fetch("/api/v1/chat/message", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        message: composerMessage.value,
        hostId: store.snapshot.selectedHostId,
      }),
    });
    if (!response.ok) {
      const data = await response.json();
      store.errorMessage = data.error || "message send failed";
      showThinking.value = false;
      store.setTurnPhase("failed");
    } else {
      composerMessage.value = "";
      isUserScrolling = false;
    }
  } catch (e) {
    store.errorMessage = "Network error";
    showThinking.value = false;
    store.setTurnPhase("failed");
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
      isUserScrolling = false;
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
    isUserScrolling = false;
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

function handleScroll(e) {
  const el = e.target;
  if (el.scrollHeight - el.scrollTop - el.clientHeight > 10) {
    isUserScrolling = true;
  } else {
    isUserScrolling = false;
  }
}

watch(
  () => store.snapshot.cards,
  () => {
    if (!isUserScrolling) {
      nextTick(() => {
        if (scrollContainer.value) {
          scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight;
        }
      });
    }
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
    thinkingPhase.value = "waiting_approval";
    showThinking.value = true;
  }
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

  <div class="chat-container" ref="scrollContainer" @scroll="handleScroll">
    <div class="chat-stream-inner">
      <div v-if="store.loading" class="chat-banner loading-banner">
        <span class="spinner"></span> 正在初始化...
      </div>

      <div v-if="!store.snapshot.cards.length && !store.loading && !showThinking" class="empty-state-canvas">
        <BotIcon size="48" class="empty-icon" />
        <h2>What can I help you build?</h2>
        <p>I can help you write code, manage servers, execute commands, and orchestrate complex tasks.</p>
      </div>

      <p v-if="store.errorMessage" class="chat-banner error">{{ store.errorMessage }}</p>

      <div class="chat-stream">
        <div v-if="connectionErrorCard" class="stream-row row-assistant">
          <CardItem
            :card="connectionErrorCard"
            @retry="handleRetry"
            @refresh="handleRefresh"
          />
        </div>

        <div
          v-for="card in visibleCards"
          :key="card.id"
          class="stream-row"
          :class="getRowClass(card)"
        >
          <CardItem
            :card="card"
            @approval="decideApproval"
            @choice="handleChoice"
            @retry="handleRetry"
            @refresh="handleRefresh"
          />
        </div>

        <div v-if="showThinking && (currentReadingLine || activitySummary || currentSearchLine)" class="activity-summary">
          <button
            v-if="currentReadingLine"
            type="button"
            class="activity-line plain"
            :disabled="!viewedFileDetails.length"
            @click="showFileDetails = !showFileDetails"
          >
            {{ currentReadingLine }}
          </button>

          <button
            v-if="activitySummary"
            type="button"
            class="activity-line"
            :disabled="!viewedFileDetails.length"
            @click="showFileDetails = !showFileDetails"
          >
            {{ activitySummary }}
          </button>

          <button
            v-if="currentSearchLine"
            type="button"
            class="activity-line"
            :disabled="!searchedQueryDetails.length"
            @click="showSearchDetails = !showSearchDetails"
          >
            {{ currentSearchLine }}
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

        <div v-if="showThinking" class="stream-row row-assistant">
          <ThinkingCard :card="thinkingCard" />
        </div>
      </div>
    </div>
  </div>

  <footer class="omnibar-dock">
    <div class="omnibar-stack">
      <div v-if="activePlanCard" class="runtime-plan-dock">
        <PlanCard :card="activePlanCard" compact />
      </div>

      <!-- Auth Overlay -->
      <div v-if="activeApprovalCard" class="auth-overlay-dock">
        <div v-if="!authCardCollapsed" class="auth-overlay-container">
          <div class="auth-overlay-header">
             <span class="auth-overlay-title">需要您的确认</span>
             <button class="icon-btn auth-collapse-btn" @click="authCardCollapsed = true">折叠展开输入框</button>
          </div>
          <CardItem :card="activeApprovalCard" :is-overlay="true" @approval="decideApproval" />
        </div>
        
        <button v-else class="auth-restore-btn" @click="authCardCollapsed = false">
           有 1 项待确认任务被折叠，点击展开审核
        </button>
      </div>

      <Omnibar
        v-if="!activeApprovalCard || authCardCollapsed"
        v-model="composerMessage"
        :placeholder="composerPlaceholder"
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
  gap: 8px;
  padding: 8px 16px;
  background: #fef3c7;
  color: #92400e;
  font-size: 13px;
  font-weight: 500;
  border-bottom: 1px solid #fde68a;
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

.activity-summary {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 0;
  margin-left: 48px;
  animation: fadeInUp 0.2s ease-out;
}

.activity-line {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  padding: 0;
  border: none;
  background: transparent;
  font-size: var(--text-meta-size, 12px);
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
  gap: 4px;
  margin-top: 2px;
  padding-left: 12px;
  color: #94a3b8;
  font-size: 12px;
}

.activity-detail-item {
  line-height: 1.5;
}

.omnibar-stack {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: 860px;
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
  border-radius: 14px;
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.1);
  overflow: hidden;
}

.auth-overlay-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 14px;
  border-bottom: 1px solid #f1f5f9;
  background: #f8fafc;
}

.auth-overlay-title {
  font-size: 12px;
  font-weight: 600;
  color: #fb923c;
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
  padding: 12px;
  background: #fff7ed;
  border: 1px solid #fed7aa;
  border-radius: 12px;
  color: #c2410c;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  margin-bottom: 8px;
  box-shadow: 0 4px 12px rgba(234, 88, 12, 0.05);
}

.auth-restore-btn:hover {
  background: #ffedd5;
}

.row-notice {
  justify-content: center;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>
