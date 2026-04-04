<script setup>
import { computed, ref, watch } from "vue";
import { ChevronDownIcon, ChevronUpIcon } from "lucide-vue-next";

const props = defineProps({
  turn: {
    type: Object,
    required: true,
  },
});

const expanded = ref(!props.turn?.collapsedByDefault);

watch(
  () => [props.turn?.id, props.turn?.collapsedByDefault],
  () => {
    expanded.value = !props.turn?.collapsedByDefault;
  },
);

const hasItems = computed(() => Array.isArray(props.turn?.processItems) && props.turn.processItems.length > 0);
const hasBody = computed(() => hasItems.value || !!props.turn?.liveHint);

function toggleExpanded() {
  if (!hasBody.value) return;
  expanded.value = !expanded.value;
}

function itemMeta(item = {}) {
  return [item.hostId, item.time].filter(Boolean).join(" · ");
}
</script>

<template>
  <section
    v-if="turn?.processItems?.length || turn?.liveHint || turn?.summary"
    class="chat-process-fold"
    :data-testid="`chat-process-fold-${turn.id}`"
  >
    <div class="chat-process-divider">
      <span class="chat-process-divider-line" />
      <button
        type="button"
        class="chat-process-toggle"
        :aria-expanded="expanded"
        :disabled="!hasBody"
        @click="toggleExpanded"
      >
        <span class="chat-process-label">{{ turn.processLabel || "已处理" }}</span>
        <span v-if="turn.summary" class="chat-process-summary">{{ turn.summary }}</span>
        <component :is="expanded ? ChevronUpIcon : ChevronDownIcon" v-if="hasBody" size="14" class="chat-process-icon" />
      </button>
      <span class="chat-process-divider-line" />
    </div>

    <div v-if="expanded && hasBody" class="chat-process-body">
      <div v-if="turn.liveHint" class="chat-process-live">{{ turn.liveHint }}</div>

      <ul v-if="hasItems" class="chat-process-list">
        <li v-for="item in turn.processItems" :key="item.id" class="chat-process-item">
          <div class="chat-process-item-text">{{ item.text }}</div>
          <div v-if="itemMeta(item)" class="chat-process-item-meta">{{ itemMeta(item) }}</div>
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.chat-process-fold {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin: 6px 0 10px 36px;
  max-width: 720px;
}

.chat-process-divider {
  display: flex;
  align-items: center;
  gap: 12px;
}

.chat-process-divider-line {
  flex: 1;
  height: 1px;
  background: rgba(226, 232, 240, 0.82);
}

.chat-process-toggle {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  max-width: 100%;
  padding: 5px 12px;
  border: 1px solid rgba(226, 232, 240, 0.92);
  border-radius: 999px;
  background: rgba(248, 250, 252, 0.96);
  color: #64748b;
  font-size: 12px;
  line-height: 1.4;
  cursor: pointer;
  transition: background 0.18s ease, border-color 0.18s ease;
}

.chat-process-toggle:hover:not(:disabled) {
  background: rgba(241, 245, 249, 0.98);
  border-color: rgba(203, 213, 225, 0.96);
}

.chat-process-label {
  color: #475569;
  font-weight: 700;
  white-space: nowrap;
}

.chat-process-summary {
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.chat-process-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px 14px;
  border: 1px solid rgba(226, 232, 240, 0.9);
  border-radius: 14px;
  background: rgba(248, 250, 252, 0.74);
}

.chat-process-live {
  color: #6b7280;
  font-size: 12.5px;
  line-height: 1.55;
}

.chat-process-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.chat-process-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-left: 12px;
  border-left: 2px solid rgba(203, 213, 225, 0.92);
}

.chat-process-item-text {
  color: #334155;
  font-size: 12.5px;
  line-height: 1.5;
  white-space: pre-wrap;
}

.chat-process-item-meta {
  color: #94a3b8;
  font-size: 11px;
  line-height: 1.4;
}
</style>
