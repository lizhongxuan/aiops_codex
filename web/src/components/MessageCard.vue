<script setup>
import { computed, ref } from "vue";
import { UserIcon, BotIcon, CopyIcon, CheckIcon } from "lucide-vue-next";
import { marked } from "marked";
import Modal from "./Modal.vue";

// Configure marked for safe rendering
marked.setOptions({
  breaks: true,
  gfm: true,
});

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const isUser = computed(() => props.card.role === "user");
const rawText = computed(() => props.card.text || props.card.title || "");
const messageText = computed(() => isUser.value ? rawText.value : cleanDisplayText(rawText.value));

const avatarIcon = computed(() => {
  return isUser.value ? UserIcon : BotIcon;
});

const renderAsCode = computed(() => {
  if (isUser.value) return false;
  if (containsMarkdownLinks(messageText.value)) return false;
  return looksStructuredText(messageText.value);
});

// Always render assistant messages as Markdown — marked handles plain text fine
// and properly formats lists, paragraphs, bold, code blocks, etc.
const renderAsMarkdown = computed(() => {
  if (isUser.value) return false;
  if (renderAsCode.value) return false;
  return true;
});

const renderedMarkdown = computed(() => {
  if (!renderAsMarkdown.value) return "";
  try {
    const preprocessed = preprocessForMarkdown(messageText.value);
    return marked.parse(preprocessed, { breaks: true, gfm: true });
  } catch {
    return "";
  }
});

/**
 * Preprocess text for better Markdown rendering:
 * - If text already has proper line breaks / markdown, leave it alone
 * - If text is a dense Chinese paragraph with no line breaks, add breaks
 *   at logical boundaries (after 。followed by a topic shift)
 */
function preprocessForMarkdown(text) {
  if (!text) return text;
  // If text already has multiple lines, it's already formatted
  if (text.split("\n").filter((l) => l.trim()).length > 2) return text;
  // If text has markdown formatting, don't touch it
  if (/^#{1,6}\s/m.test(text) || /^\s*[-*+]\s/m.test(text) || /^\s*\d+\.\s/m.test(text) || /^```/m.test(text)) return text;

  // For dense single-paragraph Chinese text, add paragraph breaks
  // at sentence boundaries where a new topic/section starts
  let result = text;
  // Break before "- " dash lists that are inline
  result = result.replace(/([。！？])\s*-\s+/g, "$1\n\n- ");
  // Break before Chinese dash lists "－"
  result = result.replace(/([。！？])\s*[－—]\s*/g, "$1\n\n— ");
  // Break at major topic transitions (after period + space + new sentence starter)
  result = result.replace(/。\s*(?=[A-Z\u4e00-\u9fff])/g, "。\n\n");

  return result;
}

function hasMarkdownFormatting(value) {
  if (!value) return false;
  // Detect common Markdown patterns
  return /^#{1,6}\s/m.test(value) ||       // headings
    /^\s*[-*+]\s/m.test(value) ||           // unordered lists
    /^\s*\d+\.\s/m.test(value) ||           // ordered lists
    /\*\*[^*]+\*\*/m.test(value) ||         // bold
    /`[^`]+`/.test(value) ||                // inline code
    /^```/m.test(value) ||                  // code blocks
    /^>\s/m.test(value) ||                  // blockquotes
    /\|.*\|.*\|/m.test(value);             // tables
}

function containsMarkdownLinks(value) {
  return /\[([^\]]+)\]\(([^)]+)\)/.test(value || "");
}

function looksStructuredText(value) {
  const trimmed = value.trim();
  if (!trimmed.includes("\n")) return false;
  const lines = trimmed
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  if (lines.length < 2) return false;

  let structuredCount = 0;
  for (const line of lines) {
    if (
      /^[./~\w-][./~\w\s-]*$/.test(line) ||
      /[\\/]/.test(line) ||
      /\.[A-Za-z0-9_-]+$/.test(line) ||
      /^[A-Za-z0-9_.-]+$/.test(line)
    ) {
      structuredCount += 1;
    }
  }
  return structuredCount / lines.length >= 0.6;
}

/**
 * Clean assistant message text for display:
 * - Remove embedded JSON routing blocks (```json {"route":...} ```)
 * - Remove inline JSON routing objects
 * - Filter out system routing preamble lines
 */
function cleanDisplayText(text) {
  if (!text) return text;
  let cleaned = text;
  // Remove ```json ... ``` fenced blocks containing routing metadata (multiline)
  cleaned = cleaned.replace(/`{3}json[\s\S]*?`{3}/g, (match) => {
    if (/"route"\s*:/.test(match)) return "";
    return match; // keep non-routing code blocks
  });
  // Fallback: remove unclosed ```json blocks that contain routing metadata
  cleaned = cleaned.replace(/`{3}json\s*\{[^`]*"route"\s*:[^`]*/g, "");
  // Remove inline JSON objects containing "route" key
  cleaned = cleaned.replace(/\{[^{}]*"route"\s*:\s*"[^"]*"[^{}]*\}/g, "");
  // Remove system routing preamble lines
  cleaned = cleaned.replace(/^主\s*Agent\s*正在判断[^\n]*\n?/gm, "");
  cleaned = cleaned.replace(/^这是简单对话[^\n]*\n?/gm, "");
  cleaned = cleaned.replace(/^(这是|当前).*(简单|直接).*(对话|回答|回复)[^\n]*\n?/gm, "");
  cleaned = cleaned.replace(/不会生成计划或派发\s*worker[^\n]*\n?/gm, "");
  // Collapse excessive newlines
  cleaned = cleaned.replace(/\n{3,}/g, "\n\n").trim();
  return cleaned || text;
}

function parseInlineChunks(text) {
  const regex = /\[([^\]]+)\]\(([^)]+)\)/g;
  let match;
  let lastIndex = 0;
  const chunks = [];

  while ((match = regex.exec(text)) !== null) {
    if (match.index > lastIndex) {
      chunks.push({ type: "text", content: text.substring(lastIndex, match.index) });
    }
    chunks.push({ type: "link", label: match[1], path: match[2] });
    lastIndex = regex.lastIndex;
  }

  if (lastIndex < text.length) {
    chunks.push({ type: "text", content: text.substring(lastIndex) });
  }

  return chunks.length > 0 ? chunks : [{ type: "text", content: text }];
}

const parsedMessageChunks = computed(() => {
  const text = messageText.value;
  if (!text) return [];
  return parseInlineChunks(text);
});

const messageBlocks = computed(() => {
  const text = messageText.value;
  if (!text || renderAsCode.value) return [];

  const blocks = [];
  const lines = text.split("\n");
  let fileItems = [];

  const flushFileItems = () => {
    if (!fileItems.length) return;
    blocks.push({ type: "file-list", items: fileItems });
    fileItems = [];
  };

  const pushSpacer = () => {
    if (!blocks.length || blocks[blocks.length - 1].type === "spacer") return;
    blocks.push({ type: "spacer" });
  };

  for (const line of lines) {
    const fileMatch = line.match(/^\s*[-*]\s+\[([^\]]+)\]\(([^)]+)\)\s*$/);
    if (fileMatch) {
      fileItems.push({ label: fileMatch[1], path: fileMatch[2] });
      continue;
    }

    flushFileItems();

    if (!line.trim()) {
      pushSpacer();
      continue;
    }

    blocks.push({
      type: "text",
      chunks: parseInlineChunks(line),
    });
  }

  flushFileItems();

  return blocks.length
    ? blocks
    : [
        {
          type: "text",
          chunks: parseInlineChunks(text),
        },
      ];
});

const isCopied = ref(false);
const previewOpen = ref(false);
const previewLoading = ref(false);
const previewError = ref("");
const previewPath = ref("");
const previewContent = ref("");
const previewTruncated = ref(false);

async function handleCopy() {
  if (!messageText.value || isCopied.value) return;
  try {
    await navigator.clipboard.writeText(messageText.value);
    isCopied.value = true;
    setTimeout(() => {
      isCopied.value = false;
    }, 2000);
  } catch (err) {
    console.error("Failed to copy:", err);
  }
}

function parseFileLinkTarget(raw) {
  const value = (raw || "").trim();
  if (!value) {
    return { hostId: "server-local", path: "", line: 0 };
  }

  if (value.startsWith("remote://")) {
    try {
      const parsed = new URL(value);
      const path = decodeURIComponent(parsed.pathname || "");
      const lineMatch = parsed.hash.match(/^#L(\d+)$/i);
      return {
        hostId: parsed.host || "server-local",
        path,
        line: lineMatch ? Number(lineMatch[1]) : 0,
      };
    } catch (_err) {
      return { hostId: "server-local", path: value.replace(/^remote:\/\//, ""), line: 0 };
    }
  }

  const [pathPart, hashPart] = value.split("#", 2);
  const lineMatch = (hashPart || "").match(/^L(\d+)$/i);
  return {
    hostId: "server-local",
    path: pathPart,
    line: lineMatch ? Number(lineMatch[1]) : 0,
  };
}

function tooltipPath(raw) {
  return parseFileLinkTarget(raw).path;
}

async function openFilePreview(raw) {
  const target = parseFileLinkTarget(raw);
  if (!target.path) return;

  previewOpen.value = true;
  previewLoading.value = true;
  previewError.value = "";
  previewPath.value = target.path;
  previewContent.value = "";
  previewTruncated.value = false;

  try {
    const response = await fetch(
      `/api/v1/files/preview?hostId=${encodeURIComponent(target.hostId)}&path=${encodeURIComponent(target.path)}`,
      { credentials: "include" }
    );
    const data = await response.json();
    if (!response.ok) {
      previewError.value = data.error || "文件预览失败";
      return;
    }
    previewPath.value = data.path || target.path;
    previewContent.value = data.content || "";
    previewTruncated.value = !!data.truncated;
  } catch (_err) {
    previewError.value = "文件预览失败";
  } finally {
    previewLoading.value = false;
  }
}

function closePreview() {
  previewOpen.value = false;
}
</script>

<template>
  <div class="message-wrapper" :class="{ 'is-user': isUser }">
    <div class="avatar assistant-avatar" v-if="!isUser">
      <BotIcon size="16" />
    </div>
    
    <div class="message-content">
      <div class="content-block relative-block assistant-thread-block" v-if="!isUser">
        <pre v-if="renderAsCode" class="message-code">{{ messageText }}</pre>
        <div v-else-if="renderAsMarkdown" class="message-text markdown-body" v-html="renderedMarkdown"></div>
        <div v-else class="message-text rich-message">
          <template v-for="(block, blockIdx) in messageBlocks" :key="blockIdx">
            <div v-if="block.type === 'text'" class="message-line">
              <template v-for="(chunk, idx) in block.chunks" :key="idx">
                <span v-if="chunk.type === 'text'">{{ chunk.content }}</span>
                <button
                  v-else-if="chunk.type === 'link'"
                  type="button"
                  class="file-link-text"
                  :data-path="tooltipPath(chunk.path)"
                  @click="openFilePreview(chunk.path)"
                >
                  {{ chunk.label }}
                </button>
              </template>
            </div>

            <div v-else-if="block.type === 'file-list'" class="file-list-block">
              <div v-for="item in block.items" :key="item.path" class="file-list-item">
                <button
                  type="button"
                  class="file-link-text"
                  :data-path="tooltipPath(item.path)"
                  @click="openFilePreview(item.path)"
                >
                  {{ item.label }}
                </button>
              </div>
            </div>

            <div v-else-if="block.type === 'spacer'" class="message-spacer"></div>
          </template>
        </div>
        <button class="copy-btn" @click="handleCopy" :class="{ copied: isCopied }" title="Copy">
          <CheckIcon v-if="isCopied" size="14" class="text-green-500" />
          <CopyIcon v-else size="14" />
          <span v-if="isCopied" class="copy-tooltip">✓ 复制成功</span>
        </button>
      </div>
      <template v-else>
        <pre v-if="renderAsCode" class="message-code">{{ messageText }}</pre>
        <div v-else class="message-text user-message-bubble">
          <template v-for="(chunk, idx) in parsedMessageChunks" :key="idx">
            <span v-if="chunk.type === 'text'">{{ chunk.content }}</span>
            <button
              v-else-if="chunk.type === 'link'"
              type="button"
              class="file-link-text"
              :data-path="tooltipPath(chunk.path)"
              @click="openFilePreview(chunk.path)"
            >
              {{ chunk.label }}
            </button>
          </template>
        </div>
      </template>
      <div class="ghost-loader" v-if="card.status === 'inProgress'">
        <span class="spinner-small"></span> 
        <span class="ghost-text">Thinking...</span>
      </div>
    </div>
    
    <div class="avatar user-avatar" v-if="isUser">
      <UserIcon size="16" />
    </div>
  </div>

  <Modal v-if="previewOpen" :title="previewPath || '文件预览'" @close="closePreview">
    <div class="preview-modal">
      <div v-if="previewLoading" class="preview-state">正在读取文件...</div>
      <div v-else-if="previewError" class="preview-error">{{ previewError }}</div>
      <template v-else>
        <pre class="preview-code">{{ previewContent }}</pre>
        <div v-if="previewTruncated" class="preview-note">文件内容过长，当前仅展示前一部分。</div>
      </template>
    </div>
  </Modal>
</template>

<style scoped>
.message-wrapper {
  display: flex;
  gap: 8px;
  max-width: 100%;
  width: 100%;
}

.message-wrapper.is-user {
  justify-content: flex-end;
}

.avatar {
  width: 22px;
  height: 22px;
  border-radius: 999px;
  background: rgba(248, 250, 252, 0.98);
  border: 1px solid rgba(226, 232, 240, 0.9);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  flex-shrink: 0;
  margin-top: 2px;
}

.assistant-avatar {
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.9);
}

.user-avatar {
  background: #eef2f7;
  color: #475569;
}

.message-content {
  flex: 1;
  max-width: calc(100% - 34px);
}

.is-user .message-content {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  max-width: min(520px, 68%);
}

.message-text {
  font-size: var(--text-body, 13.5px);
  line-height: var(--line-height-body, 1.58);
  color: #0f172a;
  white-space: pre-wrap;
  letter-spacing: 0;
}

.rich-message {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.message-line {
  white-space: pre-wrap;
  line-height: 1.58;
}

.message-spacer {
  height: 5px;
}

.file-list-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 1px;
}

.file-list-item {
  line-height: 1.55;
}

.message-code {
  margin: 0;
  padding: 9px 12px;
  border-radius: 12px;
  border: 1px solid rgba(226, 232, 240, 0.92);
  background: rgba(248, 250, 252, 0.96);
  color: #0f172a;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.52;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.user-message-bubble {
  background: #f3f4f6;
  border: 1px solid rgba(226, 232, 240, 0.95);
  padding: 9px 14px;
  border-radius: 16px;
  color: #0f172a;
  display: inline-block;
  font-size: 13.5px;
  line-height: 1.55;
  box-shadow: 0 1px 1px rgba(15, 23, 42, 0.02);
}

.is-user .message-code {
  background: #f3f4f6;
  border-color: transparent;
}

.ghost-loader {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  color: #94a3b8;
}

.ghost-text {
  font-size: 12px;
  font-style: italic;
}


.spinner-small {
  display: inline-block;
  width: 12px;
  height: 12px;
  border: 2px solid rgba(0,0,0,0.1);
  border-radius: 50%;
  border-top-color: currentColor;
  animation: spin 1s linear infinite;
}

@keyframes spin { 
  to { transform: rotate(360deg); }
}

.relative-block {
  position: relative;
  display: block;
  width: min(720px, 100%);
  max-width: 100%;
}

.copy-btn {
  position: absolute;
  bottom: 4px;
  right: 0;
  background: rgba(255, 255, 255, 0.98);
  border: 1px solid rgba(226, 232, 240, 0.95);
  border-radius: 999px;
  padding: 5px;
  color: #6b7280;
  cursor: pointer;
  opacity: 0;
  transition: all 0.2s ease;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 8px 20px rgba(15, 23, 42, 0.08);
}

.relative-block:hover .copy-btn,
.relative-block:focus-within .copy-btn {
  opacity: 1;
}

.copy-btn:hover {
  background: #f9fafb;
  color: #111827;
}

.copy-btn.copied {
  opacity: 1;
  border-color: #22c55e;
  background: #f0fdf4;
  color: #15803d;
}

.text-green-500 {
  color: #22c55e;
}

.copy-tooltip {
  position: absolute;
  bottom: 110%;
  right: 0;
  background: #111827;
  color: white;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 11px;
  white-space: nowrap;
  pointer-events: none;
  animation: fadeIn 0.2s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}

.file-link-text {
  position: relative;
  display: inline-flex;
  align-items: center;
  padding: 0;
  background: transparent;
  border: none;
  color: #2563eb;
  font-weight: 500;
  cursor: pointer;
  text-decoration: none;
  transition: color 0.2s ease, text-decoration-color 0.2s ease;
  font: inherit;
}

.file-link-text:hover {
  color: #1d4ed8;
  text-decoration: underline;
}

.file-link-text::after {
  content: attr(data-path);
  position: absolute;
  left: 0;
  bottom: calc(100% + 8px);
  min-width: 240px;
  max-width: min(560px, 80vw);
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.96);
  color: #f8fafc;
  font-size: 12px;
  line-height: 1.5;
  white-space: normal;
  word-break: break-word;
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.18);
  opacity: 0;
  pointer-events: none;
  transform: translateY(4px);
  transition: opacity 0.16s ease, transform 0.16s ease;
  z-index: 20;
}

.file-link-text:hover::after {
  opacity: 1;
  transform: translateY(0);
}

.preview-modal {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.preview-state,
.preview-error,
.preview-note {
  font-size: 13px;
  color: #64748b;
}

.preview-error {
  color: #b91c1c;
}

.preview-code {
  margin: 0;
  padding: 14px 16px;
  border-radius: 12px;
  background: #f8fafc;
  border: 1px solid #dbe3ee;
  color: #0f172a;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  max-height: 60vh;
  overflow: auto;
}

/* Markdown rendered content */
.markdown-body {
  font-size: var(--text-body, 13.5px);
  line-height: 1.58;
  color: #0f172a;
  word-break: break-word;
}

.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3),
.markdown-body :deep(h4),
.markdown-body :deep(h5),
.markdown-body :deep(h6) {
  margin: 6px 0 3px;
  font-weight: 600;
  line-height: 1.28;
  color: #0f172a;
}

.markdown-body :deep(h1) { font-size: 1.22em; }
.markdown-body :deep(h2) { font-size: 1.1em; }
.markdown-body :deep(h3) { font-size: 1.02em; }

.markdown-body :deep(p) {
  margin: 0 0 2px;
  line-height: 1.58;
}

.markdown-body :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  margin: 1px 0 3px;
  padding-left: 18px;
}

.markdown-body :deep(li) {
  margin: 0;
  line-height: 1.56;
}

.markdown-body :deep(li p) {
  margin: 0;
}

.markdown-body :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.88em;
  background: #f1f5f9;
  padding: 1px 4px;
  border-radius: 4px;
  color: #334155;
}

.markdown-body :deep(pre) {
  margin: 6px 0;
  padding: 9px 12px;
  border-radius: 10px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  overflow-x: auto;
}

.markdown-body :deep(pre code) {
  background: transparent;
  padding: 0;
  font-size: 12px;
  line-height: 1.48;
}

.markdown-body :deep(blockquote) {
  margin: 6px 0;
  padding: 3px 10px;
  border-left: 3px solid #cbd5e1;
  color: #475569;
}

.markdown-body :deep(strong) {
  font-weight: 600;
}

.markdown-body :deep(table) {
  border-collapse: collapse;
  margin: 6px 0;
  font-size: 12.5px;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid #e2e8f0;
  padding: 5px 8px;
  text-align: left;
}

.markdown-body :deep(th) {
  background: #f8fafc;
  font-weight: 600;
}

.markdown-body :deep(hr) {
  border: none;
  border-top: 1px solid #e2e8f0;
  margin: 8px 0;
}

.markdown-body :deep(a) {
  color: #2563eb;
  text-decoration: none;
}

.markdown-body :deep(a:hover) {
  text-decoration: underline;
}
</style>
