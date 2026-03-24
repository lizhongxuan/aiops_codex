<script setup>
import { computed } from "vue";
import { CheckCircle2Icon, CircleIcon, Loader2Icon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const completedCount = computed(() => {
  return props.card.items?.filter((i) => i.status === "completed").length || 0;
});
const totalCount = computed(() => props.card.items?.length || 0);

function getIconForStatus(status) {
  if (status === "completed") return CheckCircle2Icon;
  if (status === "inProgress") return Loader2Icon;
  return CircleIcon;
}
</script>

<template>
  <div class="plan-card">
    <div class="plan-header">
      <span class="plan-title">Task Plan</span>
      <span class="plan-stats">Total {{ totalCount }} tasks, {{ completedCount }} completed</span>
    </div>
    
    <div class="plan-body">
      <div 
        v-for="(item, index) in card.items" 
        :key="index"
        class="plan-item"
        :class="item.status"
      >
        <div class="item-icon" :class="item.status">
          <component :is="getIconForStatus(item.status)" size="18" :class="{'spin': item.status === 'inProgress'}" />
        </div>
        <div class="item-text">{{ item.step }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.plan-card {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  background: #ffffff;
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.02);
  margin-top: 8px;
  margin-left: 48px; /* align with message body */
  max-width: 600px;
}

.plan-header {
  padding: 12px 16px;
  background: #f8fafc;
  border-bottom: 1px solid #f1f5f9;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
}

.plan-title {
  font-weight: 600;
  color: #334155;
}

.plan-stats {
  color: #64748b;
}

.plan-body {
  padding: 8px 16px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.plan-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 8px 0;
  font-size: 14px;
}

.item-icon {
  margin-top: 2px;
  color: #cbd5e1;
}

.item-icon.inProgress {
  color: #3b82f6;
}

.item-icon.completed {
  color: #22c55e;
}

.item-text {
  color: #334155;
  line-height: 1.5;
}

.plan-item.completed .item-text {
  text-decoration: line-through;
  color: #94a3b8;
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
