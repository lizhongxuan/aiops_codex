<script setup>
import { ref, computed } from "vue";
import { CheckCircle2Icon, CircleIcon, Loader2Icon, ListTodoIcon, ChevronDownIcon, ChevronRightIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const isExpanded = ref(true);

const completedCount = computed(() => {
  return props.card.items?.filter((i) => i.status === "completed").length || 0;
});
const totalCount = computed(() => props.card.items?.length || 0);

const summaryText = computed(() => {
  return `共 ${totalCount.value} 个任务，已完成 ${completedCount.value} 个`;
});

function toggleExpand() {
  isExpanded.value = !isExpanded.value;
}

function getIconForStatus(status) {
  if (status === "completed") return CheckCircle2Icon;
  if (status === "inProgress") return Loader2Icon;
  return CircleIcon;
}
</script>

<template>
  <div class="plan-card">
    <div class="plan-header" @click="toggleExpand">
      <div class="plan-left">
        <ListTodoIcon size="16" class="plan-icon" />
        <span class="plan-summary">{{ summaryText }}</span>
      </div>
      <component :is="isExpanded ? ChevronDownIcon : ChevronRightIcon" size="16" class="plan-toggle" />
    </div>

    <div class="plan-body" v-if="isExpanded">
      <div
        v-for="(item, index) in card.items"
        :key="index"
        class="plan-item"
        :class="item.status"
      >
        <div class="item-icon" :class="item.status">
          <component :is="getIconForStatus(item.status)" size="16" :class="{'spin': item.status === 'inProgress'}" />
        </div>
        <span class="item-number">{{ index + 1 }}.</span>
        <div class="item-text">{{ item.step }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.plan-card {
  border: 1px solid var(--border-card, #e5e7eb);
  border-radius: var(--radius-card, 16px);
  background: #ffffff;
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.02);
  margin-top: 4px;
  margin-left: 48px;
  max-width: 640px;
}

.plan-header {
  padding: 10px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}

.plan-header:hover {
  background: #fafafa;
}

.plan-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.plan-icon {
  color: #6b7280;
}

.plan-summary {
  font-size: 14px;
  font-weight: 600;
  color: #374151;
}

.plan-toggle {
  color: #9ca3af;
}

.plan-body {
  padding: 4px 14px 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.plan-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 0;
  font-size: 14px;
  line-height: 1.6;
}

.item-icon {
  margin-top: 2px;
  color: #d1d5db;
  flex-shrink: 0;
}

.item-icon.inProgress {
  color: #3b82f6;
}

.item-icon.completed {
  color: #22c55e;
}

.item-number {
  color: #9ca3af;
  font-weight: 500;
  min-width: 20px;
  flex-shrink: 0;
}

.item-text {
  color: #374151;
  line-height: 1.6;
}

.plan-item.completed .item-text {
  text-decoration: line-through;
  color: #9ca3af;
}

.plan-item.inProgress .item-text {
  color: #0f172a;
  font-weight: 500;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
