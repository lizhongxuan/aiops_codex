<script setup>
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { XIcon, MaximizeIcon, MinimizeIcon, ExternalLinkIcon } from "lucide-vue-next";

const props = defineProps({
  title: { type: String, default: "Coroot" },
  url: { type: String, default: "" },
  mode: { type: String, default: "drawer" }, // "drawer" | "modal"
});

const emit = defineEmits(["close"]);

const loading = ref(true);
const content = ref(null);
const expanded = ref(false);

async function fetchContent() {
  if (!props.url) return;
  loading.value = true;
  try {
    const res = await fetch(props.url);
    if (res.ok) {
      content.value = await res.json();
    } else {
      content.value = { error: `请求失败: ${res.status}` };
    }
  } catch (e) {
    content.value = { error: `请求异常: ${e.message}` };
  } finally {
    loading.value = false;
  }
}

function toggleExpand() {
  expanded.value = !expanded.value;
}

function close() {
  emit("close");
}

function handleKeydown(e) {
  if (e.key === "Escape") close();
}

watch(() => props.url, () => { void fetchContent(); });

onMounted(() => {
  document.addEventListener("keydown", handleKeydown);
  void fetchContent();
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", handleKeydown);
});
</script>

<template>
  <Teleport to="body">
    <div class="embed-overlay" :class="{ 'mode-modal': mode === 'modal', 'mode-drawer': mode !== 'modal' }" @click.self="close">
      <div class="embed-panel" :class="{ expanded }" role="dialog" :aria-label="title">
        <header class="embed-header">
          <h3>{{ title }}</h3>
          <div class="embed-actions">
            <button type="button" class="icon-btn" :title="expanded ? '收起' : '展开'" @click="toggleExpand">
              <MaximizeIcon v-if="!expanded" :size="16" />
              <MinimizeIcon v-else :size="16" />
            </button>
            <a v-if="url" :href="url" target="_blank" rel="noopener" class="icon-btn" title="新窗口打开">
              <ExternalLinkIcon :size="16" />
            </a>
            <button type="button" class="icon-btn" title="关闭" @click="close">
              <XIcon :size="16" />
            </button>
          </div>
        </header>
        <div class="embed-body">
          <div v-if="loading" class="embed-loading">加载中…</div>
          <div v-else-if="content && content.error" class="embed-error">{{ content.error }}</div>
          <pre v-else-if="content" class="embed-content">{{ JSON.stringify(content, null, 2) }}</pre>
          <div v-else class="embed-empty">暂无数据</div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.embed-overlay {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
}

.mode-drawer {
  justify-content: flex-end;
  background: rgba(15, 23, 42, 0.3);
}

.mode-modal {
  justify-content: center;
  align-items: center;
  background: rgba(15, 23, 42, 0.5);
}

.embed-panel {
  display: flex;
  flex-direction: column;
  background: #fff;
  box-shadow: -4px 0 24px rgba(15, 23, 42, 0.12);
  overflow: hidden;
  transition: width 0.2s ease, height 0.2s ease;
}

.mode-drawer .embed-panel {
  width: 480px;
  max-width: 90vw;
  height: 100%;
  border-radius: 16px 0 0 16px;
}

.mode-drawer .embed-panel.expanded {
  width: 80vw;
}

.mode-modal .embed-panel {
  width: 640px;
  max-width: 90vw;
  max-height: 80vh;
  border-radius: 16px;
}

.mode-modal .embed-panel.expanded {
  width: 90vw;
  max-height: 90vh;
}

.embed-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid rgba(226, 232, 240, 0.9);
  flex-shrink: 0;
}

.embed-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: #0f172a;
}

.embed-actions {
  display: flex;
  gap: 6px;
}

.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid rgba(226, 232, 240, 0.9);
  border-radius: 8px;
  background: transparent;
  color: #475569;
  cursor: pointer;
  text-decoration: none;
}
.icon-btn:hover { background: #f1f5f9; }

.embed-body {
  flex: 1;
  overflow: auto;
  padding: 18px;
}

.embed-loading {
  color: #64748b;
  font-size: 14px;
  text-align: center;
  padding: 40px 0;
}

.embed-error {
  color: #dc2626;
  font-size: 13px;
  padding: 12px;
  background: #fee2e2;
  border-radius: 8px;
}

.embed-content {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  color: #334155;
  background: #f8fafc;
  padding: 14px;
  border-radius: 10px;
  border: 1px solid rgba(226, 232, 240, 0.9);
}

.embed-empty {
  color: #94a3b8;
  font-size: 13px;
  text-align: center;
  padding: 40px 0;
}
</style>
