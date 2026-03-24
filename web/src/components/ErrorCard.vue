<script setup>
import { AlertTriangleIcon, RefreshCwIcon, RotateCcwIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["retry", "refresh"]);

function onRetry() {
  emit("retry", { cardId: props.card.id });
}

function onRefresh() {
  emit("refresh");
}
</script>

<template>
  <div class="error-card">
    <div class="error-header">
      <AlertTriangleIcon size="20" class="error-icon" />
      <span class="error-title">{{ card.title || '出现错误' }}</span>
    </div>

    <div class="error-body">
      <p class="error-message">{{ card.message || card.text || '发生了未知错误，请重试。' }}</p>

      <div class="error-actions" v-if="card.retryable !== false">
        <button class="action-btn retry" @click="onRetry">
          <RotateCcwIcon size="14" />
          <span>重试</span>
        </button>
        <button class="action-btn refresh" @click="onRefresh">
          <RefreshCwIcon size="14" />
          <span>刷新</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.error-card {
  border-radius: 16px;
  background: #fef2f2;
  border: 1px solid #fecaca;
  overflow: hidden;
  margin-top: 4px;
  margin-left: 48px;
  max-width: 680px;
}

.error-header {
  padding: 14px 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid #fee2e2;
}

.error-icon {
  color: #ef4444;
  flex-shrink: 0;
}

.error-title {
  font-size: 15px;
  font-weight: 600;
  color: #991b1b;
}

.error-body {
  padding: 16px 20px;
}

.error-message {
  margin: 0 0 16px;
  font-size: 14px;
  line-height: 1.6;
  color: #7f1d1d;
}

.error-actions {
  display: flex;
  gap: 10px;
}

.action-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: background 0.2s, transform 0.1s;
}

.action-btn:active {
  transform: translateY(1px);
}

.action-btn.retry {
  background: #0f172a;
  color: white;
}

.action-btn.retry:hover {
  background: #1e293b;
}

.action-btn.refresh {
  background: white;
  color: #374151;
  border: 1px solid #d1d5db;
}

.action-btn.refresh:hover {
  background: #f9fafb;
}
</style>
