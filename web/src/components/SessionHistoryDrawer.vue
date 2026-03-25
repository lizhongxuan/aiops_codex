<script setup>
import { computed } from "vue";
import { HistoryIcon, PlusIcon, XIcon, LoaderCircleIcon } from "lucide-vue-next";

const props = defineProps({
  sessions: {
    type: Array,
    default: () => [],
  },
  activeSessionId: {
    type: String,
    default: "",
  },
  loading: {
    type: Boolean,
    default: false,
  },
  switchingDisabled: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(["close", "create", "select"]);

const hasSessions = computed(() => props.sessions.length > 0);

function formatTime(value) {
  if (!value) return "刚刚";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "刚刚";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function statusLabel(status) {
  switch (status) {
    case "running":
      return "执行中";
    case "waiting_approval":
      return "待确认";
    case "failed":
      return "失败";
    case "completed":
      return "已完成";
    default:
      return "空白";
  }
}

function handleSelect(sessionId) {
  emit("select", sessionId);
}
</script>

<template>
  <div class="session-history-overlay" @click="emit('close')">
    <aside class="session-history-drawer" @click.stop>
      <div class="session-history-header">
        <div class="session-history-title">
          <HistoryIcon size="18" />
          <span>历史会话</span>
        </div>
        <button class="history-icon-btn" @click="emit('close')">
          <XIcon size="18" />
        </button>
      </div>

      <button class="history-create-btn" :disabled="switchingDisabled" @click="emit('create')">
        <PlusIcon size="16" />
        <span>新建会话</span>
      </button>

      <div v-if="switchingDisabled" class="history-tip">
        当前任务执行中，完成后再切换或新建会话。
      </div>

      <div class="session-history-body">
        <div v-if="loading" class="history-empty">
          <LoaderCircleIcon size="18" class="spin" />
          <span>正在加载会话...</span>
        </div>

        <div v-else-if="!hasSessions" class="history-empty">
          <span>还没有历史会话，先开始第一段对话。</span>
        </div>

        <div v-else>
          <button
            v-for="session in sessions"
            :key="session.id"
            class="history-session-item"
            :class="{ active: session.id === activeSessionId }"
            :disabled="switchingDisabled && session.id !== activeSessionId"
            @click="handleSelect(session.id)"
          >
            <div class="history-session-top">
              <span class="history-session-title">{{ session.title }}</span>
              <span class="history-session-time">{{ formatTime(session.lastActivityAt) }}</span>
            </div>
            <p class="history-session-preview">{{ session.preview }}</p>
            <div class="history-session-meta">
              <span class="history-status" :class="session.status">{{ statusLabel(session.status) }}</span>
              <span class="history-host">{{ session.selectedHostId || "server-local" }}</span>
              <span class="history-count">{{ session.messageCount || 0 }} 条消息</span>
            </div>
          </button>
        </div>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.session-history-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.18);
  z-index: 40;
}

.session-history-drawer {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 260px;
  width: 360px;
  background: #f8fafc;
  border-right: 1px solid #dbe3ee;
  box-shadow: 12px 0 40px rgba(15, 23, 42, 0.12);
  display: flex;
  flex-direction: column;
}

.session-history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 18px 10px;
}

.session-history-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}

.history-icon-btn {
  border: none;
  background: transparent;
  color: #64748b;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border-radius: 8px;
}

.history-icon-btn:hover {
  background: rgba(15, 23, 42, 0.05);
  color: #0f172a;
}

.history-create-btn {
  margin: 0 18px;
  height: 40px;
  border-radius: 10px;
  border: 1px solid #cfd8e3;
  background: white;
  color: #0f172a;
  font-size: 14px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  cursor: pointer;
}

.history-create-btn:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.history-tip {
  margin: 12px 18px 0;
  padding: 10px 12px;
  border-radius: 10px;
  background: #fff7ed;
  color: #9a3412;
  font-size: 12px;
  line-height: 1.5;
}

.session-history-body {
  flex: 1;
  overflow-y: auto;
  padding: 14px 14px 18px;
}

.history-empty {
  min-height: 160px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #64748b;
  text-align: center;
  font-size: 13px;
}

.history-session-item {
  width: 100%;
  border: 1px solid transparent;
  background: white;
  border-radius: 14px;
  padding: 14px;
  text-align: left;
  cursor: pointer;
  margin-bottom: 10px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.history-session-item:hover {
  border-color: #cbd5e1;
}

.history-session-item.active {
  border-color: #93c5fd;
  background: #eff6ff;
}

.history-session-item:disabled {
  cursor: not-allowed;
  opacity: 0.68;
}

.history-session-top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.history-session-title {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.history-session-time {
  font-size: 11px;
  color: #94a3b8;
  flex-shrink: 0;
}

.history-session-preview {
  margin: 8px 0 12px;
  font-size: 13px;
  line-height: 1.55;
  color: #475569;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.history-session-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 11px;
  color: #64748b;
}

.history-status {
  padding: 3px 8px;
  border-radius: 999px;
  background: #e2e8f0;
  color: #334155;
}

.history-status.running {
  background: #dbeafe;
  color: #1d4ed8;
}

.history-status.waiting_approval {
  background: #ffedd5;
  color: #c2410c;
}

.history-status.failed {
  background: #fee2e2;
  color: #b91c1c;
}

.history-status.completed {
  background: #dcfce7;
  color: #166534;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

@media (max-width: 900px) {
  .session-history-drawer {
    left: 0;
    width: min(100vw, 360px);
  }
}
</style>
