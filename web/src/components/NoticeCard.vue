<script setup>
import { InfoIcon } from "lucide-vue-next";

defineProps({
  card: {
    type: Object,
    required: true,
  },
});
</script>

<template>
  <div class="notice-wrapper">
    <div class="notice-line">
      <InfoIcon size="14" class="notice-icon" />
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

.notice-text {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 480px;
}
</style>
