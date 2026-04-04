<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import {
  ArrowLeftIcon,
  PlayIcon,
  SquareIcon,
  ZapIcon,
  RotateCcwIcon,
  PlusIcon,
  TrashIcon,
  RefreshCwIcon,
  FlaskConicalIcon,
} from "lucide-vue-next";

const router = useRouter();

const loading = ref(false);
const environments = ref([]);
const stats = ref({ total: 0, running: 0, stopped: 0, draft: 0 });
const activeTab = ref("environments");
const showCreateDialog = ref(false);

const newEnv = ref({
  name: "",
  description: "",
  scenario: "",
  topology: { nodes: [], links: [] },
});

const scenarioTemplates = [
  {
    name: "web-db-2tier",
    label: "Web + DB 双层架构",
    description: "一个 Web 服务器连接一个数据库",
    topology: {
      nodes: [
        { id: "web1", name: "web-server-1", role: "web", os: "linux", services: ["nginx"] },
        { id: "db1", name: "db-server-1", role: "db", os: "linux", services: ["mysql"] },
      ],
      links: [{ from: "web1", to: "db1", protocol: "tcp", port: 3306 }],
    },
  },
  {
    name: "microservice-3tier",
    label: "微服务三层架构",
    description: "负载均衡 + 应用服务 + 数据库",
    topology: {
      nodes: [
        { id: "lb1", name: "load-balancer", role: "lb", os: "linux", services: ["haproxy"] },
        { id: "app1", name: "app-server-1", role: "app", os: "linux", services: ["node"] },
        { id: "app2", name: "app-server-2", role: "app", os: "linux", services: ["node"] },
        { id: "db1", name: "db-primary", role: "db", os: "linux", services: ["postgresql"] },
      ],
      links: [
        { from: "lb1", to: "app1", protocol: "http", port: 8080 },
        { from: "lb1", to: "app2", protocol: "http", port: 8080 },
        { from: "app1", to: "db1", protocol: "tcp", port: 5432 },
        { from: "app2", to: "db1", protocol: "tcp", port: 5432 },
      ],
    },
  },
  {
    name: "cache-layer",
    label: "缓存层架构",
    description: "Web + Redis 缓存 + DB",
    topology: {
      nodes: [
        { id: "web1", name: "web-server", role: "web", os: "linux", services: ["nginx"] },
        { id: "cache1", name: "redis-cache", role: "cache", os: "linux", services: ["redis"] },
        { id: "db1", name: "db-server", role: "db", os: "linux", services: ["mysql"] },
      ],
      links: [
        { from: "web1", to: "cache1", protocol: "tcp", port: 6379 },
        { from: "web1", to: "db1", protocol: "tcp", port: 3306 },
      ],
    },
  },
];

const runningCount = computed(() => environments.value.filter((e) => e.status === "running").length);
const stoppedCount = computed(() => environments.value.filter((e) => e.status === "stopped").length);

async function fetchEnvironments() {
  loading.value = true;
  try {
    const res = await fetch("/api/v1/lab-environments");
    if (res.ok) {
      const data = await res.json();
      environments.value = Array.isArray(data.items) ? data.items : [];
      if (data.stats) stats.value = data.stats;
    }
  } catch {
    environments.value = [];
  } finally {
    loading.value = false;
  }
}

function applyTemplate(tpl) {
  newEnv.value.scenario = tpl.name;
  newEnv.value.topology = JSON.parse(JSON.stringify(tpl.topology));
  if (!newEnv.value.name) {
    newEnv.value.name = tpl.label;
  }
}

async function createEnvironment() {
  if (!newEnv.value.name.trim()) return;
  try {
    const res = await fetch("/api/v1/lab-environments", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(newEnv.value),
    });
    if (res.ok) {
      showCreateDialog.value = false;
      newEnv.value = { name: "", description: "", scenario: "", topology: { nodes: [], links: [] } };
      await fetchEnvironments();
    }
  } catch { /* ignore */ }
}

async function startEnv(id) {
  await fetch(`/api/v1/lab-environments/${id}/start`, { method: "POST" });
  await fetchEnvironments();
}

async function stopEnv(id) {
  await fetch(`/api/v1/lab-environments/${id}/stop`, { method: "POST" });
  await fetchEnvironments();
}

async function resetEnv(id) {
  await fetch(`/api/v1/lab-environments/${id}/reset`, { method: "POST" });
  await fetchEnvironments();
}

async function deleteEnv(id) {
  await fetch(`/api/v1/lab-environments/${id}`, { method: "DELETE" });
  await fetchEnvironments();
}

function statusBadgeClass(status) {
  if (status === "running") return "badge-ok";
  if (status === "stopped") return "badge-warn";
  if (status === "draft") return "badge-unknown";
  if (status === "error") return "badge-crit";
  return "badge-unknown";
}

function goBack() {
  router.push("/");
}

let previousTitle = "";

onMounted(() => {
  previousTitle = typeof document !== "undefined" ? document.title : "";
  document.title = "实验环境";
  void fetchEnvironments();
});

onBeforeUnmount(() => {
  if (previousTitle) document.title = previousTitle;
});
</script>

<template>
  <section class="lab-page">
    <header class="lab-hero">
      <div class="lab-hero-copy">
        <button class="back-link" type="button" @click="goBack">
          <ArrowLeftIcon :size="16" />
          <span>返回首页</span>
        </button>
        <div class="lab-kicker">Lab / 实验环境</div>
        <h1>
          <FlaskConicalIcon :size="28" style="vertical-align: middle; margin-right: 8px;" />
          实验环境管理
        </h1>
        <p>创建沙箱环境，模拟故障注入与混沌工程演练。</p>
      </div>
      <div class="lab-hero-stats">
        <div class="lab-stat stat-ok">
          <PlayIcon :size="18" />
          <span>运行中</span>
          <strong>{{ runningCount }}</strong>
        </div>
        <div class="lab-stat stat-warn">
          <SquareIcon :size="18" />
          <span>已停止</span>
          <strong>{{ stoppedCount }}</strong>
        </div>
        <div class="lab-stat stat-total">
          <FlaskConicalIcon :size="18" />
          <span>总计</span>
          <strong>{{ environments.length }}</strong>
        </div>
      </div>
    </header>

    <nav class="tab-bar">
      <button :class="{ active: activeTab === 'environments' }" @click="activeTab = 'environments'">环境列表</button>
      <button :class="{ active: activeTab === 'templates' }" @click="activeTab = 'templates'">场景模板</button>
      <button class="refresh-btn" type="button" @click="fetchEnvironments" :disabled="loading">
        <RefreshCwIcon :size="14" :class="{ spinning: loading }" />
        刷新
      </button>
      <button class="create-btn" type="button" @click="showCreateDialog = true">
        <PlusIcon :size="14" />
        新建环境
      </button>
    </nav>

    <div v-if="loading" class="loading-hint">加载中…</div>

    <!-- Environments Tab -->
    <section v-if="activeTab === 'environments'" class="tab-content">
      <div class="section-card">
        <h2>环境列表</h2>
        <table class="data-table" v-if="environments.length" role="table">
          <thead>
            <tr>
              <th>名称</th>
              <th>场景</th>
              <th>节点数</th>
              <th>状态</th>
              <th>更新时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="env in environments" :key="env.id">
              <td>{{ env.name }}</td>
              <td>{{ env.scenario || "-" }}</td>
              <td>{{ (env.topology && env.topology.nodes) ? env.topology.nodes.length : 0 }}</td>
              <td><span class="status-badge" :class="statusBadgeClass(env.status)">{{ env.status }}</span></td>
              <td>{{ env.updatedAt || "-" }}</td>
              <td class="action-cell">
                <button v-if="env.status !== 'running'" class="action-btn action-start" type="button" @click="startEnv(env.id)" title="启动">
                  <PlayIcon :size="13" /> 启动
                </button>
                <button v-if="env.status === 'running'" class="action-btn action-stop" type="button" @click="stopEnv(env.id)" title="停止">
                  <SquareIcon :size="13" /> 停止
                </button>
                <button v-if="env.status === 'running'" class="action-btn action-reset" type="button" @click="resetEnv(env.id)" title="重置">
                  <RotateCcwIcon :size="13" /> 重置
                </button>
                <button class="action-btn action-delete" type="button" @click="deleteEnv(env.id)" title="删除">
                  <TrashIcon :size="13" /> 删除
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else-if="!loading" class="empty-hint">暂无实验环境。点击"新建环境"创建。</p>
      </div>
    </section>

    <!-- Templates Tab -->
    <section v-if="activeTab === 'templates'" class="tab-content">
      <div class="template-grid">
        <div v-for="tpl in scenarioTemplates" :key="tpl.name" class="template-card">
          <h3>{{ tpl.label }}</h3>
          <p>{{ tpl.description }}</p>
          <div class="template-meta">
            <span>{{ tpl.topology.nodes.length }} 节点</span>
            <span>{{ tpl.topology.links.length }} 连接</span>
          </div>
          <button class="action-btn" type="button" @click="applyTemplate(tpl); showCreateDialog = true;">
            <ZapIcon :size="13" /> 使用此模板
          </button>
        </div>
      </div>
    </section>

    <!-- Create Dialog -->
    <div v-if="showCreateDialog" class="dialog-overlay" @click.self="showCreateDialog = false">
      <div class="dialog-box">
        <h2>新建实验环境</h2>
        <label>
          名称
          <input v-model="newEnv.name" type="text" placeholder="输入环境名称" />
        </label>
        <label>
          描述
          <input v-model="newEnv.description" type="text" placeholder="可选描述" />
        </label>
        <label>
          场景模板
          <select v-model="newEnv.scenario" @change="scenarioTemplates.find(t => t.name === newEnv.scenario) && applyTemplate(scenarioTemplates.find(t => t.name === newEnv.scenario))">
            <option value="">自定义</option>
            <option v-for="tpl in scenarioTemplates" :key="tpl.name" :value="tpl.name">{{ tpl.label }}</option>
          </select>
        </label>
        <div class="dialog-info" v-if="newEnv.topology.nodes.length">
          拓扑: {{ newEnv.topology.nodes.length }} 节点, {{ newEnv.topology.links.length }} 连接
        </div>
        <div class="dialog-actions">
          <button class="action-btn" type="button" @click="showCreateDialog = false">取消</button>
          <button class="action-btn action-start" type="button" @click="createEnvironment" :disabled="!newEnv.name.trim()">创建</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.lab-page {
  min-height: 100%;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  background:
    radial-gradient(circle at top right, rgba(139, 92, 246, 0.08), transparent 28%),
    linear-gradient(180deg, #f8fbff 0%, #f8fafc 100%);
}

.lab-hero {
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

.lab-kicker {
  display: inline-flex;
  margin-top: 12px;
  padding: 6px 10px;
  border-radius: 999px;
  background: #f3e8ff;
  color: #7c3aed;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.lab-hero h1 { margin: 12px 0 8px; font-size: 30px; }
.lab-hero p { margin: 0; color: #475569; line-height: 1.7; }

.lab-hero-stats {
  display: flex;
  gap: 12px;
  align-self: end;
}

.lab-stat {
  padding: 14px 16px;
  border-radius: 18px;
  background: #f8fafc;
  border: 1px solid rgba(226, 232, 240, 0.9);
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 80px;
}
.lab-stat span { color: #64748b; font-size: 12px; margin-top: 4px; }
.lab-stat strong { font-size: 20px; color: #0f172a; margin-top: 4px; }
.stat-ok strong { color: #16a34a; }
.stat-warn strong { color: #d97706; }
.stat-total strong { color: #7c3aed; }

.tab-bar {
  display: flex;
  gap: 4px;
  padding: 4px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid rgba(226, 232, 240, 0.9);
  width: fit-content;
  align-items: center;
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

.tab-bar button.active {
  background: #0f172a;
  color: #fff;
}

.refresh-btn,
.create-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-left: 8px;
  font-size: 13px;
}

.create-btn {
  background: #7c3aed;
  color: #fff;
  font-weight: 600;
}
.create-btn:hover { background: #6d28d9; }

.spinning { animation: spin 1s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.loading-hint { color: #64748b; font-size: 14px; }

.section-card {
  padding: 18px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid rgba(226, 232, 240, 0.9);
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.04);
}

.section-card h2 { margin: 0 0 14px; font-size: 18px; }

.data-table {
  width: 100%;
  border-collapse: collapse;
}

.data-table th,
.data-table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid rgba(226, 232, 240, 0.9);
  font-size: 13px;
}

.data-table th {
  color: #64748b;
  font-weight: 600;
  font-size: 12px;
}

.data-table tbody tr:hover { background: #f8fafc; }

.status-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
}
.badge-ok { background: #dcfce7; color: #16a34a; }
.badge-warn { background: #fef3c7; color: #d97706; }
.badge-crit { background: #fee2e2; color: #dc2626; }
.badge-unknown { background: #f1f5f9; color: #64748b; }

.action-cell {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.action-btn {
  padding: 6px 14px;
  border: 1px solid rgba(226, 232, 240, 0.9);
  border-radius: 8px;
  background: #fff;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
  color: #475569;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.action-btn:hover { background: #f8fafc; }
.action-start { color: #16a34a; }
.action-stop { color: #d97706; }
.action-reset { color: #2563eb; }
.action-delete { color: #dc2626; }

.template-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}

.template-card {
  padding: 18px;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid rgba(226, 232, 240, 0.9);
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.04);
}

.template-card h3 { margin: 0 0 8px; font-size: 16px; }
.template-card p { margin: 0 0 12px; color: #64748b; font-size: 13px; }

.template-meta {
  display: flex;
  gap: 12px;
  margin-bottom: 12px;
  font-size: 12px;
  color: #94a3b8;
}

.dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.dialog-box {
  background: #fff;
  border-radius: 20px;
  padding: 24px;
  min-width: 400px;
  max-width: 500px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.15);
}

.dialog-box h2 { margin: 0 0 16px; font-size: 20px; }

.dialog-box label {
  display: block;
  margin-bottom: 12px;
  font-size: 13px;
  font-weight: 600;
  color: #475569;
}

.dialog-box input,
.dialog-box select {
  display: block;
  width: 100%;
  margin-top: 4px;
  padding: 8px 12px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font: inherit;
  font-size: 14px;
}

.dialog-info {
  margin-bottom: 12px;
  font-size: 12px;
  color: #7c3aed;
  background: #f3e8ff;
  padding: 8px 12px;
  border-radius: 8px;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}

.empty-hint { color: #64748b; font-size: 13px; margin: 14px 0 0; }

@media (max-width: 760px) {
  .lab-page { padding: 16px; }
  .lab-hero { flex-direction: column; }
  .lab-hero-stats { flex-wrap: wrap; }
  .template-grid { grid-template-columns: 1fr; }
}
</style>
