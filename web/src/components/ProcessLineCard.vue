<script setup>
import { computed } from "vue";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const lineClass = computed(() => {
  if (props.card.status === "failed") return "failed";
  if (props.card.status === "completed") return "completed";
  return "running";
});
</script>

<template>
  <div class="process-line-card" :class="lineClass">
    <span class="process-dot">•</span>
    <span class="process-text">{{ card.text || card.title }}</span>
  </div>
</template>

<style scoped>
.process-line-card {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-left: 48px;
  padding: 2px 0;
  color: #94a3b8;
  font-size: 13px;
  line-height: 1.6;
}

.process-line-card.running {
  color: #94a3b8;
}

.process-line-card.completed {
  color: #64748b;
}

.process-line-card.failed {
  color: #b45309;
}

.process-dot {
  font-size: 16px;
  line-height: 1;
}

.process-text {
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
