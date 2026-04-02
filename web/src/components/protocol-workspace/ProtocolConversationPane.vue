<script setup>
import { computed, ref, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { BotIcon } from "lucide-vue-next";
import MessageCard from "../MessageCard.vue";
import Omnibar from "../Omnibar.vue";
import ThinkingCard from "../ThinkingCard.vue";
import ProtocolInlinePlanWidget from "./ProtocolInlinePlanWidget.vue";
import ProtocolBackgroundAgentsCard from "./ProtocolBackgroundAgentsCard.vue";

defineOptions({
  inheritAttrs: false,
});

const props = defineProps({
  title: {
    type: String,
    default: "Protocol conversation",
  },
  subtitle: {
    type: String,
    default: "对话流、计划映射和后台执行状态",
  },
  messages: {
    type: Array,
    default: () => [],
  },
  conversationCards: {
    type: Array,
    default: () => [],
  },
  planCards: {
    type: Array,
    default: () => [],
  },
  stepItems: {
    type: Array,
    default: () => [],
  },
  backgroundAgents: {
    type: Array,
    default: () => [],
  },
  runningAgents: {
    type: Array,
    default: () => [],
  },
  planSummary: {
    type: [Object, Array, String],
    default: () => [],
  },
  planSummaryLabel: {
    type: String,
    default: "",
  },
  statusCard: {
    type: Object,
    default: null,
  },
  draft: {
    type: String,
    default: "",
  },
  draftPlaceholder: {
    type: String,
    default: "继续输入需求、约束或补充说明",
  },
  sending: {
    type: Boolean,
    default: false,
  },
  busy: {
    type: Boolean,
    default: false,
  },
  primaryActionOverride: {
    type: String,
    default: "",
  },
  showComposer: {
    type: Boolean,
    default: true,
  },
  allowFollowUp: {
    type: Boolean,
    default: false,
  },
  emptyLabel: {
    type: String,
    default: "这里会显示主 Agent 的对话流。",
  },
  starterCard: {
    type: Object,
    default: null,
  },
});

const emit = defineEmits(["update:draft", "send", "stop", "select-message", "plan-action", "agent-select"]);

const draftModel = computed({
  get: () => props.draft,
  set: (value) => emit("update:draft", value),
});

const normalizedMessages = computed(() =>
  (Array.isArray(props.messages) && props.messages.length
    ? props.messages
    : Array.isArray(props.conversationCards)
      ? props.conversationCards
      : [])
    .map((message, index) => {
      const role = String(message?.role || message?.source || "assistant").toLowerCase();
      const isUser = role === "user" || role === "human";
      const text = [message?.text || message?.message || message?.summary || "", message?.detail || message?.note || ""]
        .map((item) => String(item || "").trim())
        .filter(Boolean)
        .join("\n\n");

      if (!text) return null;

      return {
        id: String(message?.id || message?.key || `message-${index}`),
        role,
        isUser,
        card: {
          id: String(message?.id || message?.key || `message-card-${index}`),
          type: isUser ? "UserMessageCard" : "AssistantMessageCard",
          role: isUser ? "user" : "assistant",
          text,
          status: message?.status || "",
        },
      };
    })
    .filter(Boolean),
);

const normalizedPlanCards = computed(() => {
  const source = Array.isArray(props.planCards) && props.planCards.length
    ? props.planCards
    : Array.isArray(props.stepItems)
      ? props.stepItems
      : Array.isArray(props.planSummary)
        ? props.planSummary
        : props.planSummary && typeof props.planSummary === "object"
          ? Object.entries(props.planSummary).map(([key, value], index) => ({
              id: key,
              step: key,
              status: value?.status || value?.state || "pending",
              statusLabel: value?.statusLabel || value?.label || "",
              hostAgent: value?.hostAgent || value?.hostAgents || value?.hosts || [],
              detail: value?.detail || value?.summary || value?.text || (typeof value === "string" ? value : ""),
              note: value?.note || "",
              tags: value?.tags || [],
              actions: value?.actions || value?.buttons || [],
              index: index + 1,
            }))
          : typeof props.planSummary === "string" && props.planSummary.trim()
            ? [
                {
                  id: "plan-summary",
                  step: "摘要",
                  status: "pending",
                  detail: props.planSummary.trim(),
                  index: 1,
                },
              ]
            : [];

  return source;
});

const normalizedBackgroundAgents = computed(() =>
  (Array.isArray(props.backgroundAgents) && props.backgroundAgents.length
    ? props.backgroundAgents
    : Array.isArray(props.runningAgents)
      ? props.runningAgents
      : []),
);

function rowClass(message) {
  return message.isUser ? "row-user" : "row-assistant";
}

function sendDraft() {
  emit("send", String(props.draft || "").trim());
}

function stopDraft() {
  emit("stop");
}

function selectMessage(message, event) {
  if (event?.target?.closest("button")) return;
  emit("select-message", message);
}

function planAction(payload, plan) {
  emit("plan-action", { ...payload, plan });
}

function selectHost(payload, plan) {
  emit("plan-action", {
    action: { key: "host" },
    host: payload,
    plan,
  });
}

function selectAgent(agent) {
  emit("agent-select", agent);
}

/* ---- Auto-scroll ---- */
const scrollContainer = ref(null);
const scrollContent = ref(null);
const autoFollowTail = ref(true);
let contentResizeObserver = null;

function isNearBottom(el, threshold = 100) {
  if (!el) return true;
  return el.scrollHeight - el.scrollTop - el.clientHeight <= threshold;
}

function scrollToBottom(force = false) {
  const el = scrollContainer.value;
  if (!el || (!force && !autoFollowTail.value)) return;
  el.scrollTop = el.scrollHeight;
}

function handleScroll(e) {
  autoFollowTail.value = isNearBottom(e.target);
}

watch(
  () => ({
    msgCount: normalizedMessages.value.length,
    lastText: normalizedMessages.value[normalizedMessages.value.length - 1]?.card?.text?.length || 0,
    hasStatus: !!props.statusCard,
  }),
  async () => {
    await nextTick();
    scrollToBottom();
  },
  { deep: true },
);

onMounted(() => {
  nextTick(() => scrollToBottom(true));
  if (typeof ResizeObserver !== "undefined" && scrollContent.value) {
    contentResizeObserver = new ResizeObserver(() => scrollToBottom());
    contentResizeObserver.observe(scrollContent.value);
  }
});

onBeforeUnmount(() => {
  if (contentResizeObserver) {
    contentResizeObserver.disconnect();
    contentResizeObserver = null;
  }
});
</script>

<template>
  <section class="protocol-conversation-pane">
    <div class="chat-container protocol-chat-container" ref="scrollContainer" @scroll="handleScroll">
      <div class="chat-stream-inner protocol-chat-inner" ref="scrollContent">
        <div v-if="normalizedMessages.length || statusCard" class="chat-stream protocol-chat-stream">
          <div
            v-for="message in normalizedMessages"
            :key="message.id"
            class="stream-row protocol-stream-row"
            :class="rowClass(message)"
            @click="selectMessage(message, $event)"
          >
            <MessageCard :card="message.card" />
          </div>

          <div v-if="statusCard" class="stream-row row-assistant protocol-thinking-row">
            <ThinkingCard :card="statusCard" />
          </div>
        </div>

        <div v-else class="protocol-starter-thread">
          <div class="protocol-starter-kicker">
            <BotIcon size="14" />
            <span>{{ starterCard?.eyebrow || "SYSTEM CONTEXT" }}</span>
          </div>
          <div class="protocol-starter-copy">
            <p class="protocol-starter-title">{{ starterCard?.title || title }}</p>
            <p class="protocol-starter-text">{{ starterCard?.text || emptyLabel }}</p>
            <div v-if="starterCard?.meta" class="protocol-starter-meta">{{ starterCard.meta }}</div>
          </div>
        </div>
      </div>
    </div>

    <footer v-if="showComposer" class="omnibar-dock protocol-omnibar-dock">
      <div v-if="normalizedPlanCards.length || normalizedBackgroundAgents.length" class="protocol-composer-widgets">
        <ProtocolInlinePlanWidget
          v-if="normalizedPlanCards.length"
          docked
          :steps="normalizedPlanCards"
          :summary-label="planSummaryLabel"
          @step-action="({ action, plan }) => planAction({ action }, plan)"
          @host-select="({ host, plan }) => selectHost(host, plan)"
        />

        <ProtocolBackgroundAgentsCard
          v-if="normalizedBackgroundAgents.length"
          docked
          :agents="normalizedBackgroundAgents"
          @select="selectAgent"
        />
      </div>

      <Omnibar
        v-model="draftModel"
        :placeholder="draftPlaceholder"
        :disabled="sending"
        :busy="busy"
        :force-enabled="true"
        :allow-follow-up="allowFollowUp"
        :primary-action-override="primaryActionOverride"
        @send="sendDraft"
        @stop="stopDraft"
      />
    </footer>
  </section>
</template>

<style scoped>
.protocol-conversation-pane {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  background: #ffffff;
}

.protocol-chat-container {
  padding-top: 20px;
  padding-bottom: 380px;
  padding-left: 28px;
  padding-right: 28px;
}

.protocol-chat-inner {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  max-width: 820px;
  margin: 0 auto;
}

.protocol-chat-stream {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.protocol-stream-row {
  cursor: pointer;
  margin-bottom: 0;
}

.protocol-agent-section {
  margin-left: 36px;
  max-width: 720px;
}

.protocol-composer-widgets {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  max-width: 820px;
  margin: 0 auto 8px;
}

.protocol-omnibar-dock {
  padding-bottom: 20px;
  padding-left: 28px;
  padding-right: 28px;
}

.protocol-conversation-pane :deep(.message-wrapper) {
  gap: 10px;
  align-items: flex-start;
}

.protocol-conversation-pane :deep(.message-content) {
  max-width: min(720px, calc(100% - 40px)) !important;
}

.protocol-conversation-pane :deep(.message-text) {
  font-size: 14px !important;
  line-height: 1.6 !important;
  letter-spacing: 0;
  color: #0f172a;
}

.protocol-conversation-pane :deep(.message-line) {
  line-height: 1.6 !important;
}

.protocol-conversation-pane :deep(.rich-message) {
  gap: 6px;
}

.protocol-conversation-pane :deep(.message-spacer) {
  height: 10px;
}

/* Markdown body spacing in protocol pane */
.protocol-conversation-pane :deep(.markdown-body p) {
  margin: 0 0 4px;
}

.protocol-conversation-pane :deep(.markdown-body ul),
.protocol-conversation-pane :deep(.markdown-body ol) {
  margin: 2px 0 4px;
}

.protocol-conversation-pane :deep(.markdown-body li) {
  margin: 1px 0;
}

.protocol-conversation-pane :deep(.avatar),
.protocol-conversation-pane :deep(.user-avatar) {
  display: none;
}

.protocol-conversation-pane :deep(.message-wrapper) {
  gap: 0;
  align-items: flex-start;
}

.protocol-conversation-pane :deep(.message-content) {
  max-width: min(760px, 100%) !important;
}

.protocol-conversation-pane :deep(.relative-block) {
  max-width: min(760px, calc(100vw - 420px)) !important;
}

.protocol-conversation-pane :deep(.copy-btn) {
  bottom: 6px;
  right: -4px;
  border-radius: 6px;
}

.protocol-conversation-pane :deep(.is-user .message-content) {
  max-width: min(480px, 70%) !important;
}

.protocol-conversation-pane :deep(.is-user .message-text) {
  background: #f3f4f6 !important;
  padding: 10px 16px !important;
  border-radius: 14px !important;
  color: #0f172a;
  display: inline-block;
  font-size: 14px !important;
  font-weight: 400;
  line-height: 1.6 !important;
}

.protocol-conversation-pane :deep(.thinking-wrapper) {
  padding: 0;
  margin-left: 0;
}

.protocol-conversation-pane :deep(.thinking-indicator) {
  gap: 4px;
  padding: 7px 12px 8px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  background: linear-gradient(180deg, #ffffff 0%, #f8fafc 100%);
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.03);
  color: #475569;
  max-width: min(640px, calc(100vw - 400px));
}

.protocol-conversation-pane :deep(.thinking-text) {
  color: #0f172a;
  font-size: 13px;
  font-weight: 500;
}

.protocol-conversation-pane :deep(.thinking-detail) {
  color: #64748b;
  font-size: 11.5px;
  line-height: 1.45;
}

.protocol-conversation-pane :deep(.omnibar-wrapper) {
  max-width: 820px !important;
  border-radius: 16px !important;
  border: 1px solid var(--border-color);
  padding: 10px 12px 9px !important;
  background: var(--omnibar-bg);
  box-shadow: 0 4px 18px rgba(15, 23, 42, 0.06);
  gap: 6px;
}

.protocol-conversation-pane :deep(.omnibar-wrapper:focus-within) {
  border-color: #cbd5e1;
  box-shadow: 0 12px 40px rgba(15, 23, 42, 0.12);
  background: #ffffff;
}

.protocol-conversation-pane :deep(.omnibar-input) {
  min-height: 60px !important;
  font-size: 14px !important;
  line-height: 1.5 !important;
  padding: 2px 4px 0 !important;
  color: #0f172a;
}

.protocol-conversation-pane :deep(.omnibar-input::placeholder) {
  color: #94a3b8;
}

.protocol-conversation-pane :deep(.omnibar-tools) {
  padding-top: 0;
  border-top: none;
}

.protocol-conversation-pane :deep(.hint-text) {
  font-size: 11px;
  color: #94a3b8;
  font-weight: 500;
}

.protocol-conversation-pane :deep(.pill-tag) {
  background: rgba(15, 23, 42, 0.06);
  color: #334155;
}

.protocol-conversation-pane :deep(.send-btn) {
  width: 32px;
  height: 32px;
  background: #0f172a;
  box-shadow: none;
}

.protocol-conversation-pane :deep(.send-btn.stop-btn) {
  background: #dc2626;
}

.protocol-starter-thread {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
  max-width: 720px;
  margin: 12px 0 0 36px;
  padding-bottom: 6px;
}

.protocol-starter-kicker {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #94a3b8;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.protocol-starter-copy {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.protocol-starter-title {
  margin: 0;
  color: #0f172a;
  font-size: 15px;
  line-height: 1.45;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.protocol-starter-text {
  margin: 0;
  color: #4b5563;
  font-size: 14px;
  line-height: 1.6;
}

.protocol-starter-meta {
  color: #94a3b8;
  font-size: 12px;
  line-height: 1.5;
}

.protocol-conversation-pane :deep(.stop-link-btn) {
  border-color: rgba(226, 232, 240, 0.95);
  color: #475569;
  background: #ffffff;
}

.protocol-conversation-pane :deep(.stop-link-btn:hover) {
  background: #f8fafc;
}

@media (max-width: 900px) {
  .protocol-agent-section {
    margin-left: 0;
    max-width: 100%;
  }

  .protocol-chat-container,
  .protocol-omnibar-dock {
    padding-left: 18px;
    padding-right: 18px;
  }

  .protocol-conversation-pane :deep(.thinking-wrapper) {
    margin-left: 0;
  }

  .protocol-conversation-pane :deep(.relative-block),
  .protocol-conversation-pane :deep(.thinking-indicator),
  .protocol-conversation-pane :deep(.message-content),
  .protocol-conversation-pane :deep(.is-user .message-content) {
    max-width: 100%;
  }

  .protocol-conversation-pane :deep(.omnibar-wrapper) {
    border-radius: 26px !important;
    padding: 18px 18px 16px !important;
  }

  .protocol-conversation-pane :deep(.omnibar-input) {
    min-height: 120px !important;
  }

  .protocol-starter-thread {
    max-width: 100%;
    margin-left: 0;
  }

  .protocol-starter-title {
    font-size: 20px;
  }

  .protocol-starter-text {
    font-size: 16px;
  }
}
</style>
