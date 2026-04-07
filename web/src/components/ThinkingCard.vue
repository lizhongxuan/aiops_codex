<script setup>
import { computed } from "vue";
import { Loader2Icon, DotIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const normalizedPhase = computed(() => {
  const phase = (props.card.phase || "").trim();
  const map = {
    thinking: "thinking",
    planning: "planning",
    waiting_approval: "waiting_approval",
    waiting_confirmation: "waiting_confirmation",
    waiting_input: "waiting_input",
    executing: "executing",
    browsing: "browsing",
    searching: "searching",
    editing: "editing",
    testing: "testing",
    finalizing: "finalizing",
  };
  return map[phase] || "thinking";
});

const phaseLabel = computed(() => {
  const map = {
    thinking: "正在思考",
    planning: "正在规划步骤",
    waiting_approval: "等待审批中",
    waiting_confirmation: "等待确认中",
    waiting_input: "等待输入中",
    executing: "执行中",
    browsing: "正在浏览文件",
    searching: "正在搜索内容",
    editing: "正在修改代码",
    testing: "正在验证与测试",
    finalizing: "正在整理结果",
  };
  return map[normalizedPhase.value];
});

const phaseTone = computed(() => normalizedPhase.value);
const phaseDetail = computed(() => (props.card.hint || props.card.text || "").trim());
</script>

<template>
  <div class="thinking-wrapper" :class="phaseTone">
    <div class="thinking-indicator">
      <span class="thinking-state">
        <Loader2Icon v-if="normalizedPhase !== 'waiting_approval'" size="15" class="thinking-spinner" />
        <DotIcon v-else size="15" class="thinking-dot" />
        <span class="thinking-text">{{ phaseLabel }}</span>
      </span>
      <span v-if="phaseDetail" class="thinking-detail">{{ phaseDetail }}</span>
    </div>
  </div>
</template>

<style scoped>
.thinking-wrapper {
  padding: 2px 0;
  margin-left: 36px;
  animation: fadeInUp 0.2s ease-out;
}

.thinking-indicator {
  display: inline-flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  padding: 7px 12px 8px;
  border-radius: 10px;
  background: linear-gradient(180deg, #ffffff 0%, #f8fafc 100%);
  border: 1px solid #e2e8f0;
  color: #475569;
  font-size: 12.5px;
  line-height: 1.4;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.03);
  max-width: min(640px, calc(100vw - 80px));
}

.thinking-spinner {
  animation: spin 1s linear infinite;
  color: #3b82f6;
  flex-shrink: 0;
}

.thinking-dot {
  color: #64748b;
  flex-shrink: 0;
}

.thinking-text {
  font-weight: 500;
  color: #0f172a;
  font-size: 13px;
}

.thinking-state {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.thinking-detail {
  color: #64748b;
  font-size: 11.5px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(6px); }
  to   { opacity: 1; transform: translateY(0); }
}

@media (max-width: 640px) {
  .thinking-wrapper {
    margin-left: 0;
  }

  .thinking-indicator {
    max-width: none;
  }
}
</style>
