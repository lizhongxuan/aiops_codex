<script setup>
import { computed, ref, watch } from "vue";
import { ChevronDownIcon, ChevronRightIcon } from "lucide-vue-next";
import MessageCard from "../MessageCard.vue";
import ChatTerminalPreview from "./ChatTerminalPreview.vue";
import ToolDisplayRenderer from "./tool-display/ToolDisplayRenderer.vue";

const props = defineProps({
  turn: {
    type: Object,
    required: true,
  },
});

const expanded = ref(!props.turn?.collapsedByDefault);
const expandedCommandIds = ref(new Set());

watch(
  () => props.turn?.id,
  () => {
    expanded.value = !props.turn?.collapsedByDefault;
    expandedCommandIds.value = new Set();
  },
);

const hasContent = computed(() => {
  return (
    (props.turn?.processItems?.length > 0) ||
    props.turn?.liveHint ||
    props.turn?.summary ||
    intermediateMessages.value.length > 0 ||
    historyItems.value.length > 0
  );
});

// Intermediate assistant messages (model's thinking text, not the final answer)
const intermediateMessages = computed(() => {
  const items = props.turn?.processItems || [];
  return items.filter(item => item.kind === "assistant" || item.kind === "assistant_message" || item.kind === "message");
});

const historyItems = computed(() => {
  const items = props.turn?.processItems || [];
  const msgSet = new Set(intermediateMessages.value.map(i => i.id));
  return items.filter(item => !msgSet.has(item.id));
});

const foldLabel = computed(() => {
  return props.turn?.processLabel || "已处理";
});

function toggleExpanded() {
  if (!hasContent.value) return;
  expanded.value = !expanded.value;
}

function isCommandItem(item) {
  return item?.kind === "command" && (item?.command || item?.commandCard?.command);
}

function commandOutput(item) {
  return String(
    item?.output ||
    item?.commandCard?.output ||
    item?.commandCard?.stdout ||
    item?.commandCard?.stderr ||
    item?.detail ||
    "",
  ).trim();
}

function itemDisplay(item) {
  return item?.display || null;
}

function isCommandExpanded(item) {
  return expandedCommandIds.value.has(item.id);
}

function toggleCommandItem(item) {
  if (!isCommandItem(item)) return;
  const next = new Set(expandedCommandIds.value);
  if (next.has(item.id)) next.delete(item.id);
  else next.add(item.id);
  expandedCommandIds.value = next;
}
</script>

<template>
  <section
    v-if="hasContent"
    class="chat-process-fold"
    :data-testid="`chat-process-fold-${turn.id}`"
  >
    <!-- Fold header: "已处理 1m 8s >" -->
    <div class="chat-process-header">
      <button
        type="button"
        class="chat-process-toggle"
        :aria-expanded="expanded"
        @click="toggleExpanded"
      >
        <span class="chat-process-label">{{ foldLabel }}</span>
        <component :is="expanded ? ChevronDownIcon : ChevronRightIcon" size="14" class="chat-process-icon" />
      </button>
      <span class="chat-process-divider-line" />
    </div>

    <!-- Expanded content -->
    <div v-if="expanded" class="chat-process-surface">
      <div class="chat-process-body">
        <div v-for="msg in intermediateMessages" :key="msg.id" class="chat-process-message">
          <MessageCard v-if="msg.card" :card="msg.card" />
          <div v-else class="chat-process-text">{{ msg.text }}</div>
          <ToolDisplayRenderer v-if="itemDisplay(msg)" class="chat-process-item-display" :display="itemDisplay(msg)" />
        </div>

        <template v-for="item in historyItems" :key="item.id">
          <template v-if="isCommandItem(item)">
            <button
              type="button"
              class="chat-process-command-row"
              :data-testid="`chat-process-command-row-${item.id}`"
              @click="toggleCommandItem(item)"
            >
              <component :is="isCommandExpanded(item) ? ChevronDownIcon : ChevronRightIcon" size="14" class="chat-process-command-icon" />
              <span class="chat-process-command-text">{{ item.text }}</span>
            </button>

            <ToolDisplayRenderer v-if="itemDisplay(item)" class="chat-process-item-display" :display="itemDisplay(item)" />

            <ChatTerminalPreview
              v-if="isCommandExpanded(item)"
              :test-id="`chat-process-terminal-${item.id}`"
              :command="item.command || item.commandCard?.command || item.commandCard?.title || ''"
              :output="commandOutput(item)"
            />
          </template>

          <div v-else class="chat-process-item">
            <div v-if="item.text" class="chat-process-item-line">
              <span class="chat-process-item-bullet">•</span>
              <span>{{ item.text }}</span>
            </div>
            <ToolDisplayRenderer v-if="itemDisplay(item)" class="chat-process-item-display" :display="itemDisplay(item)" />
          </div>
        </template>

        <div v-if="turn.active && turn.liveHint" class="chat-process-live">{{ turn.liveHint }}</div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.chat-process-fold {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: min(980px, 100%);
  margin: 2px auto;
}

.chat-process-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.chat-process-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  border: none;
  background: transparent;
  color: #6b7280;
  font-size: 12.5px;
  font-weight: 500;
  line-height: 1.3;
  cursor: pointer;
  white-space: nowrap;
}

.chat-process-toggle:hover {
  color: #374151;
}

.chat-process-label {
  color: inherit;
}

.chat-process-icon {
  color: #9ca3af;
  flex-shrink: 0;
}

.chat-process-divider-line {
  flex: 1;
  height: 1px;
  background: #e5e7eb;
}

.chat-process-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.chat-process-surface {
  border: 1px solid rgba(226, 232, 240, 0.98);
  background: linear-gradient(180deg, rgba(248, 250, 252, 0.96), rgba(241, 245, 249, 0.9));
  border-radius: 14px;
  padding: 12px 14px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.chat-process-message {
  /* Intermediate messages render inline */
}

.chat-process-surface :deep(.message-wrapper) {
  padding-left: 0;
}

.chat-process-surface :deep(.assistant-thread-block) {
  background: transparent;
  border: none;
  box-shadow: none;
}

.chat-process-surface :deep(.message-text),
.chat-process-surface :deep(.markdown-body) {
  color: #475569;
}

.chat-process-item-line {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12.5px;
  color: #64748b;
  line-height: 1.45;
}

.chat-process-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.chat-process-item-bullet {
  flex-shrink: 0;
  color: #94a3b8;
}

.chat-process-text {
  font-size: 13px;
  color: #475569;
  line-height: 1.5;
}

.chat-process-item-display {
  margin-top: 4px;
}

.chat-process-command-row {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  width: fit-content;
  max-width: 100%;
  padding: 0;
  border: none;
  background: transparent;
  color: #475569;
  font-size: 12.5px;
  line-height: 1.45;
  text-align: left;
  cursor: pointer;
}

.chat-process-command-row:hover {
  color: #0f172a;
}

.chat-process-command-icon {
  flex-shrink: 0;
  color: #94a3b8;
}

.chat-process-command-text {
  min-width: 0;
}

.chat-process-live {
  color: #64748b;
  font-size: 12.5px;
  line-height: 1.42;
}
</style>
