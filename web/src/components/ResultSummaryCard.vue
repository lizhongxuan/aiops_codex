<script setup>
import { computed, ref } from "vue";
import { ClipboardCheckIcon, FileTextIcon, FolderIcon, SearchIcon } from "lucide-vue-next";
import Modal from "./Modal.vue";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const primarySummary = computed(() => props.card.summary || props.card.text || "");
const auxiliaryNote = computed(() => (props.card.summary && props.card.text ? props.card.text : ""));
const fileItems = computed(() => props.card.fileItems || []);

const previewOpen = ref(false);
const previewLoading = ref(false);
const previewError = ref("");
const previewPath = ref("");
const previewContent = ref("");
const previewTruncated = ref(false);

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

function itemIcon(item) {
  if (item.kind === "dir") return FolderIcon;
  if (item.kind === "match") return SearchIcon;
  return FileTextIcon;
}

function canPreview(item) {
  return item.kind !== "dir" && !!tooltipPath(item.path);
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
  <div class="result-card">
    <div class="result-header">
      <ClipboardCheckIcon size="16" class="result-icon" />
      <span class="result-title">{{ card.title || "执行结果" }}</span>
    </div>

    <div class="result-body">
      <p class="result-summary" v-if="primarySummary">{{ primarySummary }}</p>

      <div class="kv-table" v-if="card.kvRows?.length">
        <div class="kv-row" v-for="(row, idx) in card.kvRows" :key="idx">
          <span class="kv-key">{{ row.key }}</span>
          <span class="kv-val">{{ row.value }}</span>
        </div>
      </div>

      <div class="file-items" v-if="fileItems.length">
        <div v-for="(item, idx) in fileItems" :key="`${item.path}-${idx}`" class="file-item">
          <button
            v-if="canPreview(item)"
            type="button"
            class="file-link-text"
            :data-path="tooltipPath(item.path)"
            @click="openFilePreview(item.path)"
          >
            <component :is="itemIcon(item)" size="15" class="file-item-icon" />
            <span class="file-item-label">{{ item.label }}</span>
            <span class="file-item-meta" v-if="item.meta">{{ item.meta }}</span>
          </button>

          <div v-else class="file-link-text static" :data-path="tooltipPath(item.path)">
            <component :is="itemIcon(item)" size="15" class="file-item-icon" />
            <span class="file-item-label">{{ item.label }}</span>
            <span class="file-item-meta" v-if="item.meta">{{ item.meta }}</span>
          </div>

          <pre v-if="item.preview" class="file-item-preview">{{ item.preview }}</pre>
        </div>
      </div>

      <p class="result-note" v-if="auxiliaryNote">{{ auxiliaryNote }}</p>

      <div class="result-highlights" v-if="card.highlights?.length">
        <span class="highlight-pill" v-for="(h, idx) in card.highlights" :key="idx">{{ h }}</span>
      </div>
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
.result-card {
  border-radius: 16px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  overflow: hidden;
  margin-top: 4px;
  margin-left: 48px;
  max-width: 760px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.03);
}

.result-header {
  padding: 14px 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  background: #f0fdf4;
  border-bottom: 1px solid #dcfce7;
}

.result-icon {
  color: #16a34a;
}

.result-title {
  font-size: 14px;
  font-weight: 600;
  color: #166534;
}

.result-body {
  padding: 16px 20px;
}

.result-summary {
  margin: 0 0 14px;
  font-size: 15px;
  line-height: 1.7;
  color: #1f2937;
  word-break: break-word;
}

.kv-table {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-bottom: 14px;
}

.kv-row {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
}

.kv-row:nth-child(odd) {
  background: #f9fafb;
}

.kv-key {
  color: #6b7280;
  font-weight: 500;
}

.kv-val {
  color: #111827;
  font-weight: 600;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  text-align: right;
  word-break: break-all;
}

.file-items {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 14px;
}

.file-item {
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  background: #f8fafc;
  padding: 10px 12px;
}

.file-link-text {
  position: relative;
  width: 100%;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0;
  background: transparent;
  border: none;
  color: #2563eb;
  font-weight: 600;
  cursor: pointer;
  text-decoration: none;
  transition: color 0.2s ease, text-decoration-color 0.2s ease;
  font: inherit;
  text-align: left;
}

.file-link-text.static {
  cursor: default;
}

.file-link-text:hover {
  color: #1d4ed8;
  text-decoration: underline;
}

.file-link-text.static:hover {
  text-decoration: none;
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

.file-item-icon {
  flex-shrink: 0;
}

.file-item-label {
  min-width: 0;
  word-break: break-word;
}

.file-item-meta {
  margin-left: auto;
  flex-shrink: 0;
  color: #94a3b8;
  font-size: 12px;
  font-weight: 500;
}

.file-item-preview {
  margin: 8px 0 0;
  padding: 8px 10px;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  color: #334155;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.result-note {
  margin: 0 0 14px;
  font-size: 13px;
  color: #64748b;
}

.result-highlights {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.highlight-pill {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 9999px;
  background: #eff6ff;
  color: #1e40af;
  font-size: 12px;
  font-weight: 600;
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

.preview-code {
  margin: 0;
  padding: 14px 16px;
  border-radius: 12px;
  background: #0f172a;
  color: #e2e8f0;
  font-size: 13px;
  line-height: 1.6;
  overflow: auto;
  max-height: 60vh;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
