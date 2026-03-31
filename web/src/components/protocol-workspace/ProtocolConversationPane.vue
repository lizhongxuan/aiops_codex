<script setup>
import { computed } from "vue";
import { BotIcon } from "lucide-vue-next";
import MessageCard from "../MessageCard.vue";
import Omnibar from "../Omnibar.vue";
import ProtocolPlanSummaryCard from "./ProtocolPlanSummaryCard.vue";
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
</script>

<template>
  <section class="protocol-conversation-pane">
    <div class="chat-container protocol-chat-container">
      <div class="chat-stream-inner protocol-chat-inner">
        <div v-if="normalizedMessages.length" class="chat-stream protocol-chat-stream">
          <div
            v-for="message in normalizedMessages"
            :key="message.id"
            class="stream-row protocol-stream-row"
            :class="rowClass(message)"
            @click="selectMessage(message, $event)"
          >
            <MessageCard :card="message.card" />
          </div>
        </div>

        <div v-else class="empty-state-canvas protocol-empty-state">
          <BotIcon size="42" class="empty-icon" />
          <h2>{{ title }}</h2>
          <p>{{ emptyLabel }}</p>
        </div>

        <section v-if="normalizedPlanCards.length" class="protocol-plan-section">
          <div class="protocol-plan-section-head">
            <div class="protocol-plan-section-copy">
              <span>工作台计划投影</span>
              <strong>{{ planSummaryLabel || "主 Agent 生成的 step 和 host-agent 映射会显示在这里。" }}</strong>
            </div>
          </div>

          <div class="protocol-plan-section-body">
            <ProtocolPlanSummaryCard
              v-for="(plan, index) in normalizedPlanCards"
              :key="plan.id || plan.key || plan.step?.id || plan.step?.key || index"
              :step="plan.step || plan.title || plan"
              :status="plan.status || plan.state || 'pending'"
              :status-label="plan.statusLabel || plan.statusText || ''"
              :host-agent="plan.hostAgent || plan.hostAgents || plan.hosts || []"
              :detail="plan.detail || plan.summary || plan.text || ''"
              :note="plan.note || ''"
              :tags="plan.tags || []"
              :actions="plan.actions || plan.buttons || []"
              :index="plan.index || index + 1"
              @action="(payload) => planAction(payload, plan)"
              @host-select="(host) => selectHost(host, plan)"
            />
          </div>
        </section>

        <ProtocolBackgroundAgentsCard
          v-if="normalizedBackgroundAgents.length"
          class="protocol-agent-section"
          :agents="normalizedBackgroundAgents"
          @select="selectAgent"
        />
      </div>
    </div>

    <footer v-if="showComposer" class="omnibar-dock protocol-omnibar-dock">
      <Omnibar
        v-model="draftModel"
        :placeholder="draftPlaceholder"
        :disabled="sending"
        :force-enabled="true"
        :allow-follow-up="allowFollowUp"
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
  padding-bottom: 260px;
}

.protocol-chat-inner {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.protocol-chat-stream {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.protocol-stream-row {
  cursor: pointer;
}

.protocol-empty-state h2 {
  margin: 0;
  font-size: 22px;
  color: #0f172a;
}

.protocol-empty-state p {
  margin: 0;
  max-width: 480px;
}

.protocol-plan-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px;
  margin-left: 48px;
  max-width: 760px;
  border-radius: 18px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  box-shadow: 0 8px 30px rgba(15, 23, 42, 0.06);
}

.protocol-plan-section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.protocol-plan-section-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.protocol-plan-section-copy span {
  color: #64748b;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.protocol-plan-section-copy strong {
  color: #0f172a;
  font-size: 15px;
  line-height: 1.5;
}

.protocol-plan-section-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.protocol-agent-section {
  margin-left: 48px;
  max-width: 760px;
}

.protocol-omnibar-dock {
  padding-bottom: 16px;
}

@media (max-width: 900px) {
  .protocol-plan-section,
  .protocol-agent-section {
    margin-left: 0;
    max-width: 100%;
  }
}
</style>
