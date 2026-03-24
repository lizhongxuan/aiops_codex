<script setup>
import { ref, computed } from "vue";
import { ShieldAlertIcon, ChevronDownIcon, MonitorIcon, ShieldIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["approval"]);

const selectedIndex = ref(0);

const isCommand = computed(() => props.card.type === 'CommandApprovalCard' || !!props.card.command);

const options = computed(() => {
  if (isCommand.value) {
    const cmdPrefix = (props.card.command || '').split(' ')[0];
    return [
      { value: "accept", label: "是" },
      { value: "accept_session", label: `是，且对于以后 ${cmdPrefix} 开头的命令不再询问` },
      { value: "decline", label: "否，请告知 Codex 如何调整" },
    ];
  }
  return [
    { value: "accept", label: "是，允许此次修改" },
    { value: "decline", label: "否，请告知 Codex 如何调整" },
  ];
});

function onSelect(idx) {
  selectedIndex.value = idx;
  if (!props.card.approval?.requestId) return;
  emit("approval", {
    approvalId: props.card.approval.requestId,
    decision: options.value[idx].value,
  });
}
</script>

<template>
  <div class="auth-card">
    <!-- Intent header -->
    <div class="auth-intent">
      <ShieldAlertIcon size="18" class="intent-icon" />
      <span class="intent-text">{{ card.text || '要执行以下操作，你要允许吗？' }}</span>
    </div>

    <!-- Command / Code preview -->
    <div class="auth-preview" v-if="card.command || card.changes?.length">
      <div v-if="card.cwd" class="cwd-badge">{{ card.cwd }}</div>
      <pre v-if="card.command" class="command-code">{{ card.command }}</pre>

      <div v-if="card.changes?.length" class="changes-list">
        <div v-for="change in card.changes" :key="change.path" class="change-item">
          <span class="change-kind">{{ change.kind }}</span>
          <span class="change-path">{{ change.path }}</span>
        </div>
      </div>
    </div>

    <!-- Radio options (pending only) -->
    <div v-if="card.status === 'pending' && card.approval" class="auth-options">
      <label
        v-for="(opt, idx) in options"
        :key="opt.value"
        class="option-row"
        :class="{ selected: selectedIndex === idx }"
        @click="onSelect(idx)"
      >
        <span class="option-number">{{ idx + 1 }}。</span>
        <span class="option-label">{{ opt.label }}</span>
        <span class="option-arrows" v-if="selectedIndex === idx">↵</span>
      </label>
    </div>



    <!-- Resolved state -->
    <div v-if="card.status !== 'pending'" class="auth-resolved">
      {{ card.status === 'accepted' || card.status === 'accepted_for_session' ? '已批准执行' : '已拒绝' }}
    </div>
  </div>
</template>

<style scoped>
.auth-card {
  border-radius: var(--radius-card, 16px);
  background: #ffffff;
  border: 1px solid var(--border-card, #e5e7eb);
  overflow: hidden;
  margin-top: 12px;
  margin-left: 48px;
  max-width: 720px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
}

/* Intent header */
.auth-intent {
  padding: 12px 16px;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  font-size: 14px;
  line-height: 1.6;
  color: #374151;
  font-weight: 600;
}

.intent-icon {
  color: #f59e0b;
  flex-shrink: 0;
  margin-top: 1px;
}

/* Command preview */
.auth-preview {
  margin: 0 20px 16px;
  background: #f3f4f6;
  border-radius: 10px;
  padding: 14px 16px;
  overflow-x: auto;
}

.cwd-badge {
  font-size: 11px;
  color: #6b7280;
  margin-bottom: 6px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.command-code {
  margin: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
  color: #1f2937;
  white-space: pre-wrap;
  line-height: 1.5;
}

.changes-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.change-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.change-kind {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  padding: 2px 6px;
  border-radius: 4px;
  background: #e5e7eb;
  color: #6b7280;
}

.change-path {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #374151;
}

/* Radio options */
.auth-options {
  display: flex;
  flex-direction: column;
  padding: 0 20px 12px;
  gap: 2px;
}

.option-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  cursor: pointer;
  font-size: 13px;
  color: #374151;
  line-height: 1.4;
  min-height: 38px;
  transition: background 0.12s;
  user-select: none;
}

.option-row:hover {
  background: #f9fafb;
}

.option-row.selected {
  background: #f3f4f6;
  font-weight: 500;
  border: 1px solid #d1d5db;
}

.option-number {
  color: #6b7280;
  font-weight: 500;
  min-width: 24px;
}

.option-label {
  flex: 1;
}

.option-arrows {
  font-size: 12px;
  color: #9ca3af;
  margin-left: auto;
}



/* Resolved */
.auth-resolved {
  padding: 14px 20px;
  text-align: center;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
  background: #f9fafb;
}
</style>
