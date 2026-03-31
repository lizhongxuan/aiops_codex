<script setup>
import { computed } from "vue";
import { InfoIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
  sessionKind: {
    type: String,
    default: "",
  },
});

const contextLabel = computed(() => (props.sessionKind === "workspace" ? "工作台通知" : "系统通知"));
</script>

<template>
  <div class="notice-wrapper">
    <div class="notice-line">
      <InfoIcon size="14" class="notice-icon" />
      <span class="notice-context">{{ contextLabel }}</span>
      <span class="notice-text">{{ card.text || card.title || '系统通知' }}</span>
    </div>
  </div>
</template>

<style scoped>
.notice-wrapper {
  display: flex;
  justify-content: center;
  padding: 6px 0;
}

.notice-line {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 14px;
  border-radius: 9999px;
  background: #f8fafc;
  color: #94a3b8;
  font-size: 12px;
  font-weight: 500;
}

.notice-icon {
  flex-shrink: 0;
  color: #cbd5e1;
}

.notice-context {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: 999px;
  background: #f1f5f9;
  color: #64748b;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.notice-text {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 480px;
}
</style>
