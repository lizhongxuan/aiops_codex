<script setup>
import { computed } from "vue";
import { Loader2Icon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const phaseLabel = computed(() => {
  const map = {
    thinking: "正在思考",
    planning: "正在规划步骤",
    waiting_approval: "正在等待审批",
    waiting_input: "正在等待你的选择",
    executing: "正在执行命令",
    finalizing: "正在整理结果",
  };
  return map[props.card.phase] || "正在思考";
});
</script>

<template>
  <div class="thinking-wrapper">
    <div class="thinking-indicator">
      <Loader2Icon size="16" class="thinking-spinner" />
      <span class="thinking-text">{{ phaseLabel }}</span>
      <span class="thinking-dots"></span>
    </div>
  </div>
</template>

<style scoped>
.thinking-wrapper {
  padding: 4px 0;
  margin-left: 48px;
  animation: fadeInUp 0.2s ease-out;
}

.thinking-indicator {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border-radius: 12px;
  background: #f8fafc;
  color: #64748b;
  font-size: 14px;
}

.thinking-spinner {
  animation: spin 1s linear infinite;
  color: #3b82f6;
}

.thinking-text {
  font-weight: 500;
}

.thinking-dots::after {
  content: "";
  animation: dots 1.5s steps(4, end) infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@keyframes dots {
  0%   { content: ""; }
  25%  { content: "."; }
  50%  { content: ".."; }
  75%  { content: "..."; }
  100% { content: ""; }
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(6px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>
