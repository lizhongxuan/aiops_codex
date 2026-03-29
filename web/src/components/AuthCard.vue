<script setup>
import { computed } from "vue";
import { ShieldAlertIcon } from "lucide-vue-next";
import { resolveHostBadge } from "../lib/hostDisplay";
import { useAppStore } from "../store";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
  isOverlay: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(["approval"]);
const store = useAppStore();

const isCommand = computed(() => props.card.type === "CommandApprovalCard" || !!props.card.command);
const isFileChange = computed(() => props.card.type === "FileChangeApprovalCard" || !!props.card.changes?.length);
const decisions = computed(() => props.card.approval?.decisions || []);
const hostCaption = computed(() => {
  const host = store.snapshot.hosts.find((item) => item.id === props.card.hostId) || {};
  return resolveHostBadge({
    ...host,
    hostId: props.card.hostId,
    hostName: props.card.hostName,
  });
});
const fileChanges = computed(() => (props.card.changes || []).map((change, index) => normalizeFileChange(change, index)));
const fileChangeCount = computed(() => fileChanges.value.length);
const fileChangePrimaryPath = computed(() => fileChanges.value[0]?.path || "");
const fileChangeModeSummary = computed(() => {
  if (!fileChanges.value.length) return "";
  const kinds = new Set(fileChanges.value.map((item) => item.kindLabel));
  if (kinds.size === 1) {
    return fileChanges.value[0].writeModeLabel;
  }
  return "混合写入";
});
const fileChangeSummary = computed(() => {
  if (!fileChanges.value.length) return "";
  if (fileChanges.value.length === 1) {
    const item = fileChanges.value[0];
    return item.diffPreview || "没有可展示的 diff 片段。";
  }
  const first = fileChanges.value[0];
  return first.diffPreview || `共 ${fileChanges.value.length} 个文件变更。`;
});
const fileChangeReason = computed(() => (props.card.text || "要执行以下文件修改，你要允许吗？").trim());

const options = computed(() => {
  if (isCommand.value) {
    const cmdPrefix = (props.card.command || "").split(" ")[0] || "当前命令";
    return [
      { value: "accept", label: "同意一次", description: "仅批准当前这次执行。" },
      { value: "accept_session", label: `同意并记住 ${cmdPrefix}`, description: "本会话内相同前缀命令不再重复询问。" },
      { value: "decline", label: "拒绝并让 Codex 调整", description: "阻止当前执行，并让 Codex 换一种方式处理。" },
    ];
  }
  const rows = [
    { value: "accept", label: "允许此次修改", description: "仅批准当前这次文件变更。" },
  ];
  if (decisions.value.includes("accept_session")) {
    rows.push({ value: "accept_session", label: "允许并记住本目录修改", description: "本会话内同目录下的同类文件修改不再重复询问。" });
  }
  rows.push({ value: "decline", label: "拒绝并让 Codex 调整", description: "阻止当前修改，并提示 Codex 改方案。" });
  return rows;
});

const resolvedText = computed(() => {
  if (props.card.status === "accept" || props.card.status === "accepted" || props.card.status === "accepted_for_session") {
    return "已批准执行";
  }
  if (props.card.status === "accepted_for_session_auto") {
    return "已按本会话规则自动批准";
  }
  if (props.card.status === "decline" || props.card.status === "declined") {
    return "已拒绝";
  }
  return "已处理";
});

function submitDecision(decision) {
  if (!props.card.approval?.requestId) return;
  emit("approval", {
    approvalId: props.card.approval.requestId,
    decision,
  });
}

function normalizeFileChange(change, index) {
  const path = (change?.path || "").trim();
  const kind = (change?.kind || "update").trim() || "update";
  return {
    index,
    path,
    kind,
    kindLabel: fileChangeKindLabel(kind),
    writeModeLabel: describeWriteMode(kind),
    diff: (change?.diff || "").trim(),
    diffPreview: summarizeDiff(change?.diff || ""),
  };
}

function fileChangeKindLabel(kind) {
  switch ((kind || "").toLowerCase()) {
    case "create":
      return "新建";
    case "append":
      return "追加";
    case "delete":
      return "删除";
    default:
      return "更新";
  }
}

function describeWriteMode(kind) {
  switch ((kind || "").toLowerCase()) {
    case "create":
      return "新建并写入";
    case "append":
      return "追加写入";
    case "delete":
      return "删除文件";
    default:
      return "覆盖写入";
  }
}

function summarizeDiff(diff) {
  const text = (diff || "").trim();
  if (!text) return "";
  const lines = text.split("\n");
  const preview = lines.slice(0, 6).join("\n").trim();
  if (preview.length >= 260 || lines.length <= 6) {
    return preview;
  }
  return `${preview}\n...`;
}
</script>

<template>
  <div class="auth-card" :class="{ 'is-overlay': isOverlay }">
    <div class="auth-intent">
      <ShieldAlertIcon size="18" class="intent-icon" />
      <span class="intent-text">{{ card.text || "要执行以下操作，你要允许吗？" }}</span>
    </div>

    <div class="auth-preview" v-if="card.command || card.changes?.length">
      <div v-if="hostCaption" class="host-badge">{{ hostCaption }}</div>
      <template v-if="isCommand">
        <div v-if="card.cwd" class="cwd-badge">{{ card.cwd }}</div>
        <pre v-if="card.command" class="command-code">{{ card.command }}</pre>
      </template>

      <template v-else-if="isFileChange">
        <div class="file-change-overview">
          <div class="file-change-kpis">
            <div class="kpi-chip">
              <span class="kpi-label">目标路径</span>
              <span class="kpi-value">{{ fileChangePrimaryPath || "未指定" }}</span>
            </div>
            <div class="kpi-chip">
              <span class="kpi-label">变更数量</span>
              <span class="kpi-value">{{ fileChangeCount }} 个文件</span>
            </div>
            <div class="kpi-chip">
              <span class="kpi-label">写入方式</span>
              <span class="kpi-value">{{ fileChangeModeSummary }}</span>
            </div>
          </div>

          <div v-if="fileChangeReason" class="file-change-reason">
            <span class="reason-label">变更原因</span>
            <span class="reason-text">{{ fileChangeReason }}</span>
          </div>

          <div class="file-change-summary">
            <span class="summary-label">摘要 diff</span>
            <pre class="summary-diff">{{ fileChangeSummary }}</pre>
          </div>
        </div>

        <div class="changes-list">
          <article v-for="change in fileChanges" :key="`${change.path}-${change.index}`" class="change-item">
            <div class="change-row">
              <span class="change-kind">{{ change.kindLabel }}</span>
              <span class="change-path">{{ change.path }}</span>
            </div>
            <div class="change-meta">
              <span class="change-meta-chip">{{ change.writeModeLabel }}</span>
              <span class="change-meta-chip">文件 {{ change.index + 1 }}</span>
            </div>
            <pre v-if="change.diffPreview" class="change-diff">{{ change.diffPreview }}</pre>
          </article>
        </div>
      </template>
    </div>

    <div v-if="card.status === 'pending' && card.approval" class="auth-options">
      <button
        v-for="(opt, idx) in options"
        :key="opt.value"
        type="button"
        class="option-row"
        @click="submitDecision(opt.value)"
      >
        <span class="option-radio">
          <span class="option-dot"></span>
        </span>
        <span class="option-copy">
          <span class="option-label">{{ opt.label }}</span>
          <span class="option-description">{{ opt.description }}</span>
        </span>
      </button>
    </div>

    <div v-if="card.status !== 'pending'" class="auth-resolved">
      {{ resolvedText }}
    </div>
  </div>
</template>

<style scoped>
.auth-card {
  border-radius: var(--radius-card, 16px);
  background: #ffffff;
  border: 1px solid var(--border-card, #e5e7eb);
  overflow: hidden;
  margin-top: 10px;
  margin-left: 48px;
  max-width: 700px;
  box-shadow: 0 6px 20px rgba(15, 23, 42, 0.05);
}

.auth-card.is-overlay {
  margin: 0;
  max-width: none;
  border: none;
  box-shadow: none;
  border-radius: 0;
}

.auth-intent {
  padding: 12px 14px 6px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 13px;
  line-height: 1.5;
  color: #374151;
  font-weight: 600;
}

.intent-icon {
  color: #f59e0b;
  flex-shrink: 0;
  margin-top: 1px;
}

.auth-preview {
  margin: 0 14px 12px;
  background: #f3f4f6;
  border-radius: 10px;
  padding: 10px 12px;
  overflow-x: auto;
}

.host-badge {
  display: inline-flex;
  align-items: center;
  margin-bottom: 8px;
  padding: 3px 8px;
  border-radius: 999px;
  background: #ffffff;
  border: 1px solid #dbe3ee;
  color: #475569;
  font-size: 11px;
  font-weight: 700;
}

.cwd-badge {
  font-size: 10px;
  color: #6b7280;
  margin-bottom: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.command-code {
  margin: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  color: #1f2937;
  white-space: pre-wrap;
  line-height: 1.45;
}

.changes-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.change-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 6px 0;
}

.file-change-overview {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 10px;
}

.file-change-kpis {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.kpi-chip {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 120px;
  padding: 8px 10px;
  border-radius: 10px;
  background: #ffffff;
  border: 1px solid #dbe3ee;
}

.kpi-label,
.summary-label,
.reason-label {
  font-size: 10px;
  line-height: 1.2;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.kpi-value {
  font-size: 12px;
  line-height: 1.35;
  color: #0f172a;
  font-weight: 600;
  word-break: break-word;
}

.file-change-reason {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  border-radius: 10px;
  background: #ffffff;
  border: 1px solid #dbe3ee;
}

.reason-text {
  font-size: 12px;
  line-height: 1.5;
  color: #1f2937;
  word-break: break-word;
}

.file-change-summary {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.summary-diff {
  margin: 0;
  padding: 10px 12px;
  border-radius: 10px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  color: #1f2937;
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.change-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.change-kind {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  padding: 2px 6px;
  border-radius: 999px;
  background: #e5e7eb;
  color: #6b7280;
}

.change-path {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #374151;
  word-break: break-word;
}

.change-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.change-meta-chip {
  display: inline-flex;
  align-items: center;
  padding: 2px 6px;
  border-radius: 999px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  color: #64748b;
  font-size: 10px;
  font-weight: 600;
}

.change-diff {
  margin: 0;
  padding: 8px 10px;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  color: #1f2937;
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.auth-options {
  display: flex;
  flex-direction: column;
  padding: 0 14px 8px;
  gap: 6px;
}

.option-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  cursor: pointer;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  background: #ffffff;
  transition: background 0.12s, border-color 0.12s;
  text-align: left;
}

.option-row:hover {
  background: #f8fafc;
  border-color: #d1d5db;
}

.option-radio {
  width: 17px;
  height: 17px;
  margin-top: 1px;
  border: 2px solid #0f172a;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.option-dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
  background: #0f172a;
}

.option-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.option-label {
  font-size: 12px;
  color: #1f2937;
  font-weight: 600;
  line-height: 1.4;
}

.option-description {
  font-size: 11px;
  color: #94a3b8;
  line-height: 1.45;
}

.auth-resolved {
  padding: 12px 16px;
  text-align: center;
  color: #6b7280;
  font-size: 12px;
  font-weight: 500;
  background: #f9fafb;
}

@media (max-width: 640px) {
  .auth-card {
    margin-left: 0;
    max-width: none;
  }

  .auth-intent {
    padding: 12px 12px 6px;
  }

  .auth-preview {
    margin: 0 10px 10px;
    padding: 10px;
  }

  .kpi-chip {
    min-width: 0;
    flex: 1 1 140px;
  }

  .change-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .change-kind {
    width: fit-content;
  }
}
</style>
