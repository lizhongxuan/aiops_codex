<script setup>
import { computed } from "vue";
import { AlertTriangleIcon, RefreshCwIcon, RotateCcwIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["retry", "refresh"]);

const errorText = computed(() => {
  return [props.card.title, props.card.message, props.card.text, props.card.error]
    .filter(Boolean)
    .join("\n")
    .toLowerCase();
});

const errorTypeLabel = computed(() => {
  const text = errorText.value;
  if (text.includes("declined") || text.includes("拒绝") || text.includes("decline")) return "审批已拒绝";
  if (text.includes("回退到本地执行") || (text.includes("server-local") && text.includes("请改用"))) return "执行路径不允许";
  if (text.includes("permission denied") || text.includes("operation not permitted") || text.includes("权限")) return "OS 权限不足";
  if (text.includes("heartbeat timed out") || text.includes("心跳超时") || text.includes("network timeout")) return "网络 / 心跳超时";
  if (text.includes("remote host disconnected") || text.includes("host-agent") || text.includes("主机断连") || text.includes("离线")) {
    return "Agent 断连";
  }
  if (text.includes("terminal") && (text.includes("disconnected") || text.includes("closed") || text.includes("断开"))) {
    return "终端断开";
  }
  if (text.includes("exit code") || text.includes("执行失败") || text.includes("command failed")) return "命令退出失败";
  return "执行异常";
});

const needsAlternative = computed(() => {
  const text = errorText.value;
  return (
    text.includes("declined") ||
    text.includes("拒绝") ||
    text.includes("decline") ||
    text.includes("回退到本地执行") ||
    text.includes("permission denied") ||
    text.includes("operation not permitted") ||
    text.includes("执行失败") ||
    text.includes("command failed")
  );
});

const isRetryable = computed(() => {
  if (props.card.retryable === false) return false;
  return !needsAlternative.value;
});

const retryabilityLabel = computed(() => (isRetryable.value ? "可直接重试" : "需要调整后再试"));
const strategyLabel = computed(() => (needsAlternative.value ? "建议换方案" : "建议等待恢复后重试"));

const errorHint = computed(() => {
  const text = errorText.value;
  if (text.includes("declined") || text.includes("拒绝") || text.includes("decline")) {
    return "这次请求已经停止。可以调整方案后重新发起，或改成更小范围的只读检查。";
  }
  if (text.includes("回退到本地执行") || (text.includes("server-local") && text.includes("请改用"))) {
    return "当前会话已绑定远程主机。继续处理前，需要改用对应的远程 execute_* 工具，或先切换回正确主机。";
  }
  if (text.includes("permission denied") || text.includes("operation not permitted") || text.includes("权限")) {
    return "建议切换更高权限用户，或先确认目标路径 / 服务是否允许当前账号访问。";
  }
  if (text.includes("heartbeat timed out") || text.includes("心跳超时") || text.includes("network timeout")) {
    return "建议先确认 ai-server 与目标主机网络恢复，再重试当前操作。";
  }
  if (text.includes("remote host disconnected") || text.includes("host-agent") || text.includes("主机断连") || text.includes("离线")) {
    return "建议刷新主机状态，确认 host-agent 恢复在线后再继续。";
  }
  if (text.includes("terminal") && (text.includes("disconnected") || text.includes("closed") || text.includes("断开"))) {
    return "可以重新进入终端；如果频繁断开，优先检查目标主机连通性和 agent 状态。";
  }
  if (text.includes("exit code") || text.includes("执行失败") || text.includes("command failed")) {
    return "建议查看命令输出和退出码，修正命令参数后重试。";
  }
  return "建议刷新状态后重试；如果持续失败，需要换一种执行方案。";
});

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
      <div class="error-title-group">
        <span class="error-title">{{ card.title || '出现错误' }}</span>
        <span v-if="card.hostId || card.hostName" class="error-host">
          {{ card.hostName || card.hostId }}<span v-if="card.hostId"> · {{ card.hostId }}</span>
        </span>
      </div>
    </div>

    <div class="error-body">
      <div class="error-meta">
        <span class="error-chip type">{{ errorTypeLabel }}</span>
        <span class="error-chip" :class="{ positive: isRetryable, muted: !isRetryable }">{{ retryabilityLabel }}</span>
        <span class="error-chip strategy">{{ strategyLabel }}</span>
      </div>
      <p class="error-message">{{ card.message || card.text || '发生了未知错误，请重试。' }}</p>
      <p class="error-hint">{{ errorHint }}</p>

      <div class="error-actions">
        <button v-if="isRetryable" class="action-btn retry" @click="onRetry">
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

.error-title-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.error-host {
  font-size: 11px;
  color: #b91c1c;
  opacity: 0.8;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.error-body {
  padding: 16px 20px;
}

.error-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}

.error-chip {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  background: #ffffff;
  border: 1px solid #fecaca;
  color: #991b1b;
  font-size: 11px;
  font-weight: 700;
}

.error-chip.positive {
  border-color: #bbf7d0;
  color: #166534;
}

.error-chip.muted {
  border-color: #fed7aa;
  color: #9a3412;
}

.error-chip.strategy {
  color: #92400e;
}

.error-message {
  margin: 0 0 16px;
  font-size: 14px;
  line-height: 1.6;
  color: #7f1d1d;
}

.error-hint {
  margin: -8px 0 16px;
  font-size: 12px;
  line-height: 1.6;
  color: #92400e;
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
