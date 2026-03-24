<script setup>
import { computed, ref, watch, nextTick } from "vue";
import { useAppStore } from "../store";
import CardItem from "../components/CardItem.vue";
import Omnibar from "../components/Omnibar.vue";
import ThinkingCard from "../components/ThinkingCard.vue";
import { BotIcon, WifiOffIcon, RefreshCwIcon } from "lucide-vue-next";

const store = useAppStore();

const composerMessage = ref("");
const scrollContainer = ref(null);
const showFileDetails = ref(false);
const showSearchDetails = ref(false);
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
    if (phase === "idle" || phase === "completed" || phase === "failed") {
      showThinking.value = false;
    } else {
      thinkingPhase.value = phase;
      showThinking.value = true;
    }
  }
);

let initialCardCount = 0;

watch(
  () => store.snapshot.cards,
  (cards) => {
    if (!showThinking.value) return;
    
    if (cards.length > initialCardCount) {
      const lastCard = cards[cards.length - 1];
      if (lastCard && lastCard.role !== "user" && lastCard.type !== "UserMessageCard") {
        showThinking.value = false;
      }
    }
  },
  { deep: true }
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

/* ---- Reconnection ---- */
const showReconnectBanner = computed(() => {
  return store.runtime.codex.status === "reconnecting" || store.runtime.codex.status === "stopped";
});

const reconnectLabel = computed(() => {
  const c = store.runtime.codex;
  if (c.status === "stopped") return "连接已断开，无法恢复";
  return `Reconnecting... ${c.retryAttempt}/${c.retryMax}`;
});

const isStopped = computed(() => store.runtime.codex.status === "stopped");

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
  if (!store.canSend || !composerMessage.value.trim()) return;

  store.sending = true;
  store.errorMessage = "";
  initialCardCount = store.snapshot.cards.length;
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
        <div
          v-for="card in store.snapshot.cards"
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
    <Omnibar
      v-model="composerMessage"
      :placeholder="composerPlaceholder"
      @send="sendMessage"
      :disabled="isStopped"
    />
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

.row-notice {
  justify-content: center;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>
