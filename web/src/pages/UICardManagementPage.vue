<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { ArrowLeftIcon } from "lucide-vue-next";

const router = useRouter();

const loading = ref(false);
const cards = ref([]);
const stats = ref({ total: 0, active: 0, draft: 0, disabled: 0, builtIn: 0, custom: 0 });
const activeTab = ref("overview");
const selectedCard = ref(null);
const editDraft = ref(null);
const previewResult = ref(null);
const previewLoading = ref(false);
const debugInput = ref("{}");

const kindLabels = {
  readonly_summary: "只读摘要",
  readonly_chart: "只读图表",
  action_panel: "操作面板",
  form_panel: "表单面板",
  monitor_bundle: "监控聚合",
  remediation_bundle: "修复聚合",
};

const kindGroups = computed(() => {
  const groups = {};
  for (const c of cards.value) {
    const kind = c.kind || "unknown";
    if (!groups[kind]) groups[kind] = { kind, label: kindLabels[kind] || kind, count: 0, items: [] };
    groups[kind].count++;
    groups[kind].items.push(c);
  }
  return Object.values(groups);
});

let previousTitle = "";

async function fetchCards() {
  loading.value = true;
  try {
    const res = await fetch("/api/v1/ui-cards");
    if (res.ok) {
      const data = await res.json();
      cards.value = Array.isArray(data.items) ? data.items : [];
      if (data.stats) stats.value = data.stats;
    }
  } catch {
    cards.value = [];
  } finally {
    loading.value = false;
  }
}

function selectCard(card) {
  selectedCard.value = card;
  editDraft.value = null;
  previewResult.value = null;
  activeTab.value = "detail";
}

function startEdit(card) {
  editDraft.value = JSON.parse(JSON.stringify(card));
  activeTab.value = "editor";
}

async function saveEdit() {
  if (!editDraft.value) return;
  try {
    const res = await fetch(`/api/v1/ui-cards/${editDraft.value.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(editDraft.value),
    });
    if (res.ok) {
      editDraft.value = null;
      activeTab.value = "list";
      await fetchCards();
    }
  } catch { /* ignore */ }
}

async function triggerPreview(card) {
  previewLoading.value = true;
  previewResult.value = null;
  try {
    let inputPayload = {};
    try { inputPayload = JSON.parse(debugInput.value); } catch { /* ignore */ }
    const res = await fetch(`/api/v1/ui-cards/${card.id}/preview`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(inputPayload),
    });
    if (res.ok) {
      previewResult.value = await res.json();
    }
  } catch { /* ignore */ }
  finally { previewLoading.value = false; }
}

function goBack() {
  router.push("/settings");
}

onMounted(() => {
  previousTitle = typeof document !== "undefined" ? document.title : "";
  document.title = "UI 卡片管理 · Settings";
  void fetchCards();
});

onBeforeUnmount(() => {
  if (previousTitle) document.title = previousTitle;
});
</script>

<template>
  <section class="uic-page">
    <header class="uic-hero">
      <div class="uic-hero-copy">
        <button class="back-link" type="button" @click="goBack">
          <ArrowLeftIcon size="16" />
          <span>返回设置</span>
        </button>
        <div class="uic-kicker">Settings / UI Cards</div>
        <h1>UI 卡片管理</h1>
        <p>管理系统内置和自定义 UI 卡片定义，支持预览和触发调试。</p>
      </div>
      <div class="uic-hero-stats">
        <div class="uic-stat"><span>总计</span><strong>{{ stats.total }}</strong></div>
        <div class="uic-stat"><span>启用</span><strong>{{ stats.active }}</strong></div>
        <div class="uic-stat"><span>草稿</span><strong>{{ stats.draft }}</strong></div>
        <div class="uic-stat"><span>内置</span><strong>{{ stats.builtIn }}</strong></div>
      </div>
    </header>

    <nav class="tab-bar">
      <button :class="{ active: activeTab === 'overview' }" @click="activeTab = 'overview'">类型总览</button>
      <button :class="{ active: activeTab === 'list' }" @click="activeTab = 'list'">卡片列表</button>
      <button :class="{ active: activeTab === 'detail' }" @click="activeTab = 'detail'" :disabled="!selectedCard">详情</button>
      <button :class="{ active: activeTab === 'editor' }" @click="activeTab = 'editor'" :disabled="!editDraft">编辑器</button>
      <button :class="{ active: activeTab === 'debugger' }" @click="activeTab = 'debugger'">触发调试器</button>
    </nav>

    <div v-if="loading" class="loading-hint">加载中…</div>

    <!-- Overview Tab -->
    <section v-if="activeTab === 'overview'" class="tab-content">
      <div class="kind-grid">
        <div v-for="g in kindGroups" :key="g.kind" class="kind-card" @click="activeTab = 'list'">
          <div class="kind-label">{{ g.label }}</div>
          <div class="kind-count">{{ g.count }}</div>
          <div class="kind-id">{{ g.kind }}</div>
        </div>
      </div>
      <p v-if="!kindGroups.length && !loading" class="empty-hint">暂无卡片定义。</p>
    </section>

    <!-- List Tab -->
    <section v-if="activeTab === 'list'" class="tab-content">
      <div class="section-card">
        <h2>卡片定义列表</h2>
        <table class="data-table" v-if="cards.length">
          <thead>
            <tr><th>ID</th><th>名称</th><th>类型</th><th>渲染器</th><th>状态</th><th>内置</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="c in cards" :key="c.id">
              <td>{{ c.id }}</td>
              <td>{{ c.name }}</td>
              <td>{{ kindLabels[c.kind] || c.kind }}</td>
              <td><code>{{ c.renderer }}</code></td>
              <td>{{ c.status }}</td>
              <td>{{ c.builtIn ? '是' : '否' }}</td>
              <td class="action-cell">
                <button class="btn-sm" @click="selectCard(c)">详情</button>
                <button class="btn-sm" @click="startEdit(c)">编辑</button>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else class="empty-hint">暂无卡片定义。</p>
      </div>
    </section>

    <!-- Detail Tab -->
    <section v-if="activeTab === 'detail' && selectedCard" class="tab-content">
      <div class="section-card">
        <h2>{{ selectedCard.name }}</h2>
        <div class="detail-grid">
          <div class="detail-row"><span>ID</span><strong>{{ selectedCard.id }}</strong></div>
          <div class="detail-row"><span>类型</span><strong>{{ kindLabels[selectedCard.kind] || selectedCard.kind }}</strong></div>
          <div class="detail-row"><span>渲染器</span><strong>{{ selectedCard.renderer }}</strong></div>
          <div class="detail-row"><span>状态</span><strong>{{ selectedCard.status }}</strong></div>
          <div class="detail-row"><span>内置</span><strong>{{ selectedCard.builtIn ? '是' : '否' }}</strong></div>
          <div class="detail-row"><span>版本</span><strong>{{ selectedCard.version }}</strong></div>
          <div class="detail-row"><span>摘要</span><strong>{{ selectedCard.summary }}</strong></div>
          <div class="detail-row"><span>能力</span><strong>{{ (selectedCard.capabilities || []).join(', ') || '-' }}</strong></div>
          <div class="detail-row"><span>触发类型</span><strong>{{ (selectedCard.triggerTypes || []).join(', ') || '-' }}</strong></div>
          <div class="detail-row"><span>可编辑字段</span><strong>{{ (selectedCard.editableFields || []).join(', ') || '-' }}</strong></div>
        </div>
        <div class="detail-actions">
          <button class="btn-sm" @click="startEdit(selectedCard)">编辑</button>
          <button class="btn-sm" @click="triggerPreview(selectedCard)">预览</button>
        </div>
        <pre v-if="previewResult" class="preview-output">{{ JSON.stringify(previewResult, null, 2) }}</pre>
      </div>
    </section>

    <!-- Editor Tab -->
    <section v-if="activeTab === 'editor' && editDraft" class="tab-content">
      <div class="section-card">
        <h2>编辑卡片定义</h2>
        <div class="form-grid">
          <label>名称 <input v-model="editDraft.name" /></label>
          <label>摘要 <input v-model="editDraft.summary" /></label>
          <label>状态
            <select v-model="editDraft.status">
              <option value="active">active</option>
              <option value="draft">draft</option>
              <option value="disabled">disabled</option>
            </select>
          </label>
        </div>
        <div class="editor-actions">
          <button class="btn-primary" @click="saveEdit">保存</button>
          <button class="btn-sm" @click="editDraft = null; activeTab = 'list'">取消</button>
        </div>
      </div>
    </section>

    <!-- Debugger Tab -->
    <section v-if="activeTab === 'debugger'" class="tab-content">
      <div class="section-card">
        <h2>触发调试器</h2>
        <p class="debug-hint">选择一个卡片并发送模拟输入来预览渲染结果。</p>
        <div class="debug-controls">
          <select @change="e => { const c = cards.find(x => x.id === e.target.value); if (c) selectedCard = c; }">
            <option value="">选择卡片…</option>
            <option v-for="c in cards" :key="c.id" :value="c.id">{{ c.name }} ({{ c.id }})</option>
          </select>
          <label>输入 JSON
            <textarea v-model="debugInput" rows="4" placeholder='{"key": "value"}'></textarea>
          </label>
          <button class="btn-primary" :disabled="!selectedCard || previewLoading" @click="triggerPreview(selectedCard)">
            {{ previewLoading ? '请求中…' : '发送预览' }}
          </button>
        </div>
        <pre v-if="previewResult" class="preview-output">{{ JSON.stringify(previewResult, null, 2) }}</pre>
      </div>
    </section>
  </section>
</template>

<style scoped>
.uic-page {
  min-height: 100%;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  background:
    radial-gradient(circle at top right, rgba(37, 99, 235, 0.08), transparent 28%),
    linear-gradient(180deg, #f8fbff 0%, #f8fafc 100%);
}

.uic-hero {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  padding: 22px;
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.88);
  border: 1px solid rgba(226, 232, 240, 0.9);
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.05);
}

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0;
  border: 0;
  background: transparent;
  color: #475569;
  font: inherit;
  cursor: pointer;
}

.uic-kicker {
  display: inline-flex;
  margin-top: 12px;
  padding: 6px 10px;
  border-radius: 999px;
  background: #eff6ff;
  color: #1d4ed8;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.uic-hero h1 { margin: 12px 0 8px; font-size: 30px; }
.uic-hero p { margin: 0; color: #475569; line-height: 1.7; }

.uic-hero-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(100px, 1fr));
  gap: 12px;
  align-self: end;
}

.uic-stat {
  padding: 14px 16px;
  border-radius: 18px;
  background: #f8fafc;
  border: 1px solid rgba(226, 232, 240, 0.9);
}
.uic-stat span { display: block; color: #64748b; font-size: 12px; }
.uic-stat strong { display: block; margin-top: 8px; font-size: 20px; color: #0f172a; }

.tab-bar {
  display: flex;
  gap: 4px;
  padding: 4px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid rgba(226, 232, 240, 0.9);
  width: fit-content;
}

.tab-bar button {
  padding: 10px 20px;
  border: none;
  border-radius: 10px;
  background: transparent;
  font: inherit;
  cursor: pointer;
  color: #475569;
  font-weight: 500;
}
.tab-bar button.active { background: #0f172a; color: #fff; }
.tab-bar button:disabled { opacity: 0.4; cursor: default; }

.loading-hint { color: #64748b; font-size: 14px; }

.kind-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 14px;
}

.kind-card {
  padding: 18px;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid rgba(226, 232, 240, 0.9);
  cursor: pointer;
  transition: box-shadow 0.15s;
}
.kind-card:hover { box-shadow: 0 8px 24px rgba(15, 23, 42, 0.08); }
.kind-label { font-weight: 600; font-size: 15px; color: #0f172a; }
.kind-count { font-size: 28px; font-weight: 700; margin: 8px 0 4px; color: #1d4ed8; }
.kind-id { font-size: 11px; color: #94a3b8; font-family: monospace; }

.section-card {
  padding: 18px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid rgba(226, 232, 240, 0.9);
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.04);
}
.section-card h2 { margin: 0 0 14px; font-size: 18px; }

.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid rgba(226, 232, 240, 0.9);
  font-size: 13px;
}
.data-table th { color: #64748b; font-weight: 600; font-size: 12px; }
.data-table tbody tr:hover { background: #f8fafc; }
.data-table code { font-size: 12px; background: #f1f5f9; padding: 2px 6px; border-radius: 4px; }

.action-cell { display: flex; gap: 6px; }

.btn-sm {
  padding: 5px 12px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #fff;
  font-size: 12px;
  cursor: pointer;
  color: #334155;
}
.btn-sm:hover { background: #f8fafc; }

.btn-primary {
  padding: 8px 18px;
  border: none;
  border-radius: 10px;
  background: #1d4ed8;
  color: #fff;
  font: inherit;
  font-size: 13px;
  cursor: pointer;
}
.btn-primary:hover { background: #1e40af; }
.btn-primary:disabled { opacity: 0.5; cursor: default; }

.detail-grid { display: grid; gap: 10px; }
.detail-row { display: flex; gap: 12px; font-size: 13px; }
.detail-row span { color: #64748b; min-width: 100px; }
.detail-row strong { color: #0f172a; font-weight: 500; }

.detail-actions { margin-top: 16px; display: flex; gap: 8px; }

.form-grid { display: grid; gap: 12px; }
.form-grid label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; color: #475569; }
.form-grid input, .form-grid select {
  padding: 8px 12px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font: inherit;
  font-size: 13px;
}

.editor-actions { margin-top: 16px; display: flex; gap: 8px; }

.debug-hint { color: #64748b; font-size: 13px; margin: 0 0 12px; }
.debug-controls { display: grid; gap: 12px; }
.debug-controls select, .debug-controls textarea {
  padding: 8px 12px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font: inherit;
  font-size: 13px;
  width: 100%;
}
.debug-controls label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; color: #475569; }

.preview-output {
  margin-top: 14px;
  padding: 14px;
  border-radius: 12px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  font-size: 12px;
  overflow-x: auto;
  white-space: pre-wrap;
}

.empty-hint { color: #64748b; font-size: 13px; margin: 14px 0 0; }

@media (max-width: 760px) {
  .uic-page { padding: 16px; }
  .uic-hero { flex-direction: column; }
  .uic-hero-stats { grid-template-columns: repeat(2, 1fr); }
  .kind-grid { grid-template-columns: 1fr; }
}
</style>
