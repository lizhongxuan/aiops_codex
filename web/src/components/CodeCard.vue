<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { FileCode2Icon, CopyIcon } from "lucide-vue-next";
import * as monaco from "monaco-editor";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
  // The card object could contain changes array. 
  // We'll just render the first diff or the whole snippet if it's text
});

const editorContainer = ref(null);
let editor = null;

// Determine if we have a diff or just code
const fileChange = props.card.changes && props.card.changes.length > 0 ? props.card.changes[0] : null;
const isDiff = fileChange && fileChange.diff;
const content = isDiff ? fileChange.diff : (props.card.output || props.card.text || "");
const filename = fileChange ? fileChange.path : "snippet.txt";
const language = getLanguageFromFilename(filename);

function getLanguageFromFilename(name) {
  if (name.endsWith('.go')) return 'go';
  if (name.endsWith('.js') || name.endsWith('.jsx')) return 'javascript';
  if (name.endsWith('.vue')) return 'vue';
  if (name.endsWith('.ts') || name.endsWith('.tsx')) return 'typescript';
  if (name.endsWith('.css')) return 'css';
  if (name.endsWith('.json')) return 'json';
  if (name.endsWith('.md')) return 'markdown';
  if (name.endsWith('.sh') || name.endsWith('.bash')) return 'shell';
  return 'plaintext';
}

function initMonaco() {
  if (!editorContainer.value) return;

  // Simplified syntax highlighter setup. We use standard editor for both.
  // In a real diff editor we'd use monaco.editor.createDiffEditor, 
  // but for simple snippet + inline git patches, regular editor with syntax is often better 
  // unless we have original & modified texts separate.
  editor = monaco.editor.create(editorContainer.value, {
    value: content,
    language: language,
    theme: "vs-light",
    readOnly: true,
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    lineNumbers: "on",
    renderLineHighlight: "none",
    padding: { top: 12, bottom: 12 },
    automaticLayout: true, // auto resize
    fontSize: 13,
    fontFamily: '"SF Mono", "Fira Code", monospace',
  });

  // Calculate height based on line count, max 400px
  const lineCount = content.split('\n').length;
  const height = Math.min(Math.max(lineCount * 19 + 24, 60), 400);
  editorContainer.value.style.height = `${height}px`;
}

async function copyCode() {
  try {
    await navigator.clipboard.writeText(content);
  } catch (err) {
    console.error('Failed to copy', err);
  }
}

onMounted(() => {
  // Wait a tick for DOM to size
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
      <div class="file-info">
        <FileCode2Icon size="16" class="file-icon" />
        <span class="file-name">{{ filename }}</span>
        <span class="file-tag" v-if="fileChange && fileChange.kind">{{ fileChange.kind }}</span>
      </div>
      
      <div class="code-actions">
        <button class="action-btn" @click="copyCode" title="Copy">
          <CopyIcon size="14" />
        </button>
      </div>
    </div>
    
    <div class="editor-wrapper" ref="editorContainer"></div>
  </div>
</template>

<style scoped>
.code-card {
  border-radius: 12px;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  overflow: hidden;
  margin-top: 8px;
  margin-left: 48px; /* align with message bubble */
  max-width: 800px;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.03);
  display: flex;
  flex-direction: column;
}

.code-header {
  padding: 8px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #f8fafc;
  border-bottom: 1px solid #e2e8f0;
}

.file-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.file-icon {
  color: #64748b;
}

.file-name {
  font-size: 13px;
  font-weight: 500;
  color: #0f172a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.file-tag {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  padding: 2px 6px;
  border-radius: 4px;
  background: #e2e8f0;
  color: #475569;
}

.code-actions {
  display: flex;
  align-items: center;
}

.action-btn {
  background: transparent;
  border: none;
  color: #64748b;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.action-btn:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.editor-wrapper {
  width: 100%;
  background: #ffffff;
}
</style>
