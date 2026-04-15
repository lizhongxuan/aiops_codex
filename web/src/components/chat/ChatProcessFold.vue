<script setup>
import { computed, ref, watch } from "vue";
import { ChevronDownIcon, ChevronRightIcon } from "lucide-vue-next";
import MessageCard from "../MessageCard.vue";

const props = defineProps({
  turn: {
    type: Object,
    required: true,
  },
});

const expanded = ref(!props.turn?.collapsedByDefault);
const searchExpanded = ref(false);

watch(
  () => [props.turn?.id, props.turn?.collapsedByDefault],
  () => {
    expanded.value = !props.turn?.collapsedByDefault;
    searchExpanded.value = false;
  },
);

const hasContent = computed(() => {
  return (
    (props.turn?.processItems?.length > 0) ||
    props.turn?.liveHint ||
    props.turn?.summary ||
    intermediateMessages.value.length > 0 ||
    searchItems.value.length > 0
  );
});

// Intermediate assistant messages (model's thinking text, not the final answer)
const intermediateMessages = computed(() => {
  const items = props.turn?.processItems || [];
  return items.filter(item => item.kind === "assistant" || item.kind === "assistant_message" || item.kind === "message");
});

// Search activity items
const searchItems = computed(() => {
  const items = props.turn?.processItems || [];
  return items.filter(item =>
    item.kind === "search" ||
    item.kind === "web_search" ||
    item.processKind === "search" ||
    (item.text && (item.text.startsWith("已搜索") || item.text.startsWith("Searched")))
  );
});

// Other process items (commands, file reads, etc.)
const otherItems = computed(() => {
  const items = props.turn?.processItems || [];
  const searchSet = new Set(searchItems.value.map(i => i.id));
  const msgSet = new Set(intermediateMessages.value.map(i => i.id));
  return items.filter(item => !searchSet.has(item.id) && !msgSet.has(item.id));
});

const searchCountLabel = computed(() => {
  const count = searchItems.value.length;
  if (!count) return "";
  return `Searched web ${count} time${count > 1 ? "s" : ""}`;
});

const foldLabel = computed(() => {
  return props.turn?.processLabel || "已处理";
});

function toggleExpanded() {
  if (!hasContent.value) return;
  expanded.value = !expanded.value;
}

function toggleSearchExpanded() {
  searchExpanded.value = !searchExpanded.value;
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
    <div v-if="expanded" class="chat-process-body">
      <!-- Intermediate assistant messages (model's thinking text) -->
      <div v-for="msg in intermediateMessages" :key="msg.id" class="chat-process-message">
        <MessageCard v-if="msg.card" :card="msg.card" />
        <div v-else class="chat-process-text">{{ msg.text }}</div>
      </div>

      <!-- Search sub-fold: "Searched web 9 times >" -->
      <div v-if="searchItems.length" class="chat-search-fold">
        <button
          type="button"
          class="chat-search-toggle"
          @click="toggleSearchExpanded"
        >
          <span>{{ searchCountLabel }}</span>
          <component :is="searchExpanded ? ChevronDownIcon : ChevronRightIcon" size="14" />
        </button>
        <div v-if="searchExpanded" class="chat-search-list">
          <div v-for="item in searchItems" :key="item.id" class="chat-search-item">
            {{ item.text }}
          </div>
        </div>
      </div>

      <!-- Other process items (commands, file reads) -->
      <div v-for="item in otherItems" :key="item.id" class="chat-process-item-line">
        {{ item.text }}
      </div>

      <!-- Live hint (during active turn) -->
      <div v-if="turn.liveHint" class="chat-process-live">{{ turn.liveHint }}</div>
    </div>
  </section>
</template>

<style scoped>
.chat-process-fold {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 4px 0 4px 30px;
  max-width: 720px;
}

.chat-process-header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.chat-process-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  border: none;
  background: transparent;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
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
  gap: 8px;
}

.chat-process-message {
  /* Intermediate messages render inline */
}

.chat-process-text {
  font-size: 14px;
  color: #374151;
  line-height: 1.6;
}

/* Search sub-fold */
.chat-search-fold {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.chat-search-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  border: none;
  background: transparent;
  color: #9ca3af;
  font-size: 13px;
  font-weight: 400;
  cursor: pointer;
  white-space: nowrap;
}

.chat-search-toggle:hover {
  color: #6b7280;
}

.chat-search-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-left: 4px;
}

.chat-search-item {
  font-size: 13px;
  color: #9ca3af;
  line-height: 1.6;
}

.chat-process-item-line {
  font-size: 13px;
  color: #9ca3af;
  line-height: 1.6;
}

.chat-process-live {
  color: #6b7280;
  font-size: 13px;
  line-height: 1.5;
}
</style>
