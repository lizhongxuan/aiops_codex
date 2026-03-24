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
const activitySummary = computed(() => {
  const a = store.runtime.activity;
  const parts = [];
  if (a.currentReadingFile) {
    return `现在浏览 ${a.currentReadingFile}`;
  }
  if (a.filesViewed > 0) parts.push(`${a.filesViewed} 个文件`);
  if (a.searchCount > 0) parts.push(`${a.searchCount} 个搜索`);
  if (a.listCount > 0) parts.push(`${a.listCount} 个列表`);
  if (a.commandsRun > 0) parts.push(`${a.commandsRun} 个命令`);
  if (parts.length === 0) return "";
  return `已浏览 ${parts.join("，")}`;
});

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

function retryConnection() {
  store.runtime.codex.retryAttempt = 0;
  store.runtime.codex.status = "reconnecting";
  store.connectWs();
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
          <CardItem :card="card" @approval="decideApproval" />
        </div>

        <div v-if="activitySummary && showThinking" class="activity-summary">
          {{ activitySummary }}
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
  padding: 6px 0;
  margin-left: 48px;
  font-size: var(--text-meta-size, 12px);
  color: var(--text-meta, #9ca3af);
  font-weight: 500;
  animation: fadeInUp 0.2s ease-out;
}

.row-notice {
  justify-content: center;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>
