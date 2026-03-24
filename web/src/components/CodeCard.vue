<script setup>
import { computed, ref, onMounted, onBeforeUnmount, watch } from "vue";
import { FileCode2Icon, CopyIcon } from "lucide-vue-next";
import * as monaco from "monaco-editor";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const editorContainer = ref(null);
const selectedIndex = ref(0);
let editor = null;

const changes = computed(() => props.card.changes || []);
const selectedChange = computed(() => changes.value[selectedIndex.value] || null);
const isDiff = computed(() => !!selectedChange.value?.diff);
const content = computed(() => {
  if (selectedChange.value?.diff) return selectedChange.value.diff;
  return props.card.output || props.card.text || "";
});
const filename = computed(() => selectedChange.value?.path || "snippet.txt");
const language = computed(() => getLanguageFromFilename(filename.value));
const changeSummary = computed(() => {
  if (changes.value.length > 1) return `${changes.value.length} 个文件变更`;
  if (changes.value.length === 1) return "1 个文件变更";
  return "代码片段";
});
const statusLabel = computed(() => {
  const status = props.card.status || "";
  if (status === "completed") return "已完成";
  if (status === "failed") return "失败";
  return "处理中";
});

function getLanguageFromFilename(name) {
  if (name.endsWith(".go")) return "go";
  if (name.endsWith(".js") || name.endsWith(".jsx")) return "javascript";
  if (name.endsWith(".vue")) return "vue";
  if (name.endsWith(".ts") || name.endsWith(".tsx")) return "typescript";
  if (name.endsWith(".css")) return "css";
  if (name.endsWith(".json")) return "json";
  if (name.endsWith(".md")) return "markdown";
  if (name.endsWith(".sh") || name.endsWith(".bash")) return "shell";
  return "plaintext";
}

function resizeEditor() {
  if (!editor || !editorContainer.value) return;
  const lineCount = content.value.split("\n").length;
  const height = Math.min(Math.max(lineCount * 19 + 28, 92), 420);
  editorContainer.value.style.height = `${height}px`;
  editor.layout();
}

function initMonaco() {
  if (!editorContainer.value) return;

  editor = monaco.editor.create(editorContainer.value, {
    value: content.value,
    language: language.value,
    theme: "vs-light",
    readOnly: true,
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    lineNumbers: "on",
    renderLineHighlight: "none",
    padding: { top: 12, bottom: 12 },
    automaticLayout: true,
    fontSize: 13,
    fontFamily: '"SF Mono", "Fira Code", monospace',
  });

  resizeEditor();
}

async function copyCode() {
  try {
    await navigator.clipboard.writeText(content.value);
  } catch (err) {
    console.error("Failed to copy", err);
  }
}

watch(
  () => props.card.id,
  () => {
    selectedIndex.value = 0;
  }
);

watch([content, language], () => {
  if (!editor) return;
  const model = editor.getModel();
  if (!model) return;
  model.setValue(content.value);
  monaco.editor.setModelLanguage(model, language.value);
  resizeEditor();
});

onMounted(() => {
  setTimeout(initMonaco, 10);
});

onBeforeUnmount(() => {
  if (editor) {
    editor.dispose();
  }
});
</script>

<template>
  <div class="code-card">
    <div class="code-header">
      <div class="header-left">
        <div class="file-info">
          <FileCode2Icon size="16" class="file-icon" />
          <span class="file-name">{{ filename }}</span>
          <span class="file-tag" v-if="selectedChange?.kind">{{ selectedChange.kind }}</span>
        </div>
        <div class="change-summary">{{ changeSummary }}</div>
      </div>

      <div class="header-right">
        <span class="status-pill" :class="card.status || 'inProgress'">{{ statusLabel }}</span>
        <button class="action-btn" @click="copyCode" title="Copy">
          <CopyIcon size="14" />
        </button>
      </div>
    </div>

    <div class="code-body" :class="{ split: changes.length > 1 }">
      <div v-if="changes.length > 1" class="change-list">
        <button
          v-for="(change, index) in changes"
          :key="`${card.id}-${change.path}-${index}`"
          class="change-item"
          :class="{ active: selectedIndex === index }"
          @click="selectedIndex = index"
        >
          <span class="change-path">{{ change.path }}</span>
          <span class="change-kind">{{ change.kind }}</span>
        </button>
      </div>

      <div class="editor-shell">
        <div class="editor-wrapper" ref="editorContainer"></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.code-card {
  border-radius: 16px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  overflow: hidden;
  margin-top: 4px;
  margin-left: 48px;
  max-width: 860px;
  box-shadow: 0 4px 18px rgba(15, 23, 42, 0.04);
  display: flex;
  flex-direction: column;
}

.code-header {
  padding: 12px 16px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  background: #f8fafc;
  border-bottom: 1px solid #e5e7eb;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.file-info {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.file-icon {
  color: #64748b;
  flex-shrink: 0;
}

.file-name {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-tag {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  padding: 2px 6px;
  border-radius: 999px;
  background: #e2e8f0;
  color: #475569;
}

.change-summary {
  font-size: 12px;
  color: #94a3b8;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.status-pill.completed {
  background: #ecfdf5;
  color: #047857;
}

.status-pill.failed {
  background: #fef2f2;
  color: #b91c1c;
}

.status-pill.inProgress {
  background: #eff6ff;
  color: #1d4ed8;
}

.action-btn {
  background: transparent;
  border: none;
  color: #64748b;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.action-btn:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.code-body {
  display: flex;
  min-height: 140px;
}

.code-body.split {
  display: grid;
  grid-template-columns: 220px 1fr;
}

.change-list {
  width: 220px;
  border-right: 1px solid #e5e7eb;
  background: #fcfcfd;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.change-item {
  border: none;
  background: transparent;
  border-radius: 12px;
  padding: 10px 12px;
  text-align: left;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.change-item:hover {
  background: #f8fafc;
}

.change-item.active {
  background: #eff6ff;
}

.change-path {
  font-size: 12px;
  font-weight: 600;
  color: #0f172a;
  word-break: break-all;
}

.change-kind {
  font-size: 11px;
  color: #94a3b8;
  text-transform: uppercase;
}

.editor-shell {
  flex: 1;
  background: #ffffff;
}

.editor-wrapper {
  width: 100%;
  background: #ffffff;
}
</style>
