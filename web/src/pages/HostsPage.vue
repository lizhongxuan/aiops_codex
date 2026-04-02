<script setup>
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useAppStore } from "../store";
import HostEditorModal from "../components/HostEditorModal.vue";
import HostBatchTagModal from "../components/HostBatchTagModal.vue";
import { hostCapabilityLabel, labelList, normalizeHostRecord } from "../data/opsWorkspace";

const store = useAppStore();
const router = useRouter();

const searchQuery = ref("");
const selectedHostId = ref(store.snapshot.selectedHostId || "");
const selectedRows = ref([]);
const hostSessions = ref([]);
const hostSessionsLoading = ref(false);
const editorHost = ref(null);
const showHostEditor = ref(false);
const showTagModal = ref(false);
const pageError = ref("");
const pageNotice = ref("");
const busyAction = ref("");

function clearMessage(kind = "all") {
  if (kind === "all" || kind === "error") pageError.value = "";
  if (kind === "all" || kind === "notice") pageNotice.value = "";
}

function pushError(message) {
  pageNotice.value = "";
  pageError.value = message;
}

function pushNotice(message) {
  pageError.value = "";
  pageNotice.value = message;
}

function isHostSelected(hostId) {
  return selectedRows.value.includes(hostId);
}

function toggleRowSelection(hostId) {
  if (isHostSelected(hostId)) {
    selectedRows.value = selectedRows.value.filter((id) => id !== hostId);
    return;
  }
  selectedRows.value = [...selectedRows.value, hostId];
}

function toggleSelectAll() {
  if (selectedRows.value.length === filteredHosts.value.length) {
    selectedRows.value = [];
    return;
  }
  selectedRows.value = filteredHosts.value.map((host) => host.id);
}

function parseIsoTime(value) {
  if (!value || value === "offline") return null;
  const timestamp = Date.parse(value);
  if (Number.isNaN(timestamp)) return null;
  return timestamp;
}

function formatLastSeen(value, status) {
  if (!value) return status === "online" ? "刚注册" : "未建立控制通道";
  if (value === "offline") return "未建立控制通道";
  const timestamp = parseIsoTime(value);
  if (!timestamp) return value;
  const diffSeconds = Math.max(1, Math.floor((Date.now() - timestamp) / 1000));
  if (diffSeconds < 60) return `${diffSeconds}s 前`;
  if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m 前`;
  if (diffSeconds < 86400) return `${Math.floor(diffSeconds / 3600)}h 前`;
  return `${Math.floor(diffSeconds / 86400)}d 前`;
}

function statusLabel(host) {
  switch (host.status) {
    case "online":
      return "控制通道活跃";
    case "connecting":
      return "安装完成，等待回连";
    case "installing":
      return "SSH 安装中";
    case "pending_install":
      return "待安装 agent";
    default:
      return host.status || "未接入";
  }
}

function transportLabel(host) {
  switch (host.transport) {
    case "local":
      return "本机";
    case "grpc_reverse":
      return "反向 gRPC";
    case "ssh_bootstrap":
      return "SSH 引导";
    default:
      return host.transport || host.kind || "inventory";
  }
}

function installTone(host) {
  switch (host.installState) {
    case "installed":
      return "is-success";
    case "installing":
      return "is-info";
    case "install_failed":
      return "is-danger";
    default:
      return "is-warning";
  }
}

const hostRecords = computed(() => {
  return (store.snapshot.hosts || []).map((host, index) => {
    const normalized = normalizeHostRecord(host, index);
    return {
      ...normalized,
      lastSeenText: formatLastSeen(normalized.lastHeartbeat, normalized.status),
    };
  });
});

const filteredHosts = computed(() => {
  const query = searchQuery.value.trim().toLowerCase();
  if (!query) return hostRecords.value;
  return hostRecords.value.filter((host) => {
    const labelText = labelList(host.labels).join(" ").toLowerCase();
    return [
      host.id,
      host.name,
      host.address,
      host.kind,
      host.status,
      host.transport,
      labelText,
    ]
      .filter(Boolean)
      .some((item) => String(item).toLowerCase().includes(query));
  });
});

watch(
  filteredHosts,
  (hosts) => {
    if (!hosts.length) {
      selectedHostId.value = "";
      return;
    }
    if (!hosts.some((host) => host.id === selectedHostId.value)) {
      selectedHostId.value = store.snapshot.selectedHostId || hosts[0].id;
    }
    selectedRows.value = selectedRows.value.filter((id) => hosts.some((host) => host.id === id));
  },
  { immediate: true }
);

watch(
  () => store.snapshot.selectedHostId,
  (nextHostId) => {
    if (nextHostId) {
      selectedHostId.value = nextHostId;
    }
  }
);

const selectedHost = computed(() => {
  return filteredHosts.value.find((host) => host.id === selectedHostId.value) || filteredHosts.value[0] || null;
});

const metrics = computed(() => {
  const hosts = hostRecords.value.filter((host) => host.id !== "server-local");
  const online = hosts.filter((host) => host.status === "online").length;
  const executable = hosts.filter((host) => host.executable).length;
  const waitingInstall = hosts.filter((host) => host.installState !== "installed").length;
  const sessions = store.sessionList.filter((session) => session.selectedHostId && session.selectedHostId !== "server-local").length;

  return [
    { label: "控制通道活跃", value: online, meta: `${Math.max(hosts.length - online, 0)} 台不活跃`, tone: "is-success" },
    { label: "待安装 / 待回连", value: waitingInstall, meta: "SSH 安装或等待 agent 回连", tone: "is-warning" },
    { label: "可执行主机", value: executable, meta: `${Math.max(hosts.length - executable, 0)} 台仅在册`, tone: "is-info" },
    { label: "远程会话数", value: sessions, meta: "每个子 agent 对应一个会话", tone: "is-purple" },
  ];
});

const allRowsSelected = computed(() => {
  return filteredHosts.value.length > 0 && selectedRows.value.length === filteredHosts.value.length;
});

const selectedSessionsCount = computed(() => hostSessions.value.length);

async function refreshInventory() {
  await Promise.all([store.fetchState(), store.fetchSessions()]);
}

async function loadHostSessions(hostId) {
  if (!hostId) {
    hostSessions.value = [];
    return;
  }
  hostSessionsLoading.value = true;
  try {
    const response = await fetch(`/api/v1/hosts/${encodeURIComponent(hostId)}/sessions?limit=8`, {
      credentials: "include",
    });
    const data = await response.json();
    if (!response.ok) {
      pushError(data.error || "加载主机会话失败");
      return;
    }
    hostSessions.value = data.items || [];
  } catch (_err) {
    pushError("加载主机会话失败");
  } finally {
    hostSessionsLoading.value = false;
  }
}

watch(
  selectedHostId,
  async (hostId) => {
    await loadHostSessions(hostId);
  },
  { immediate: true }
);

async function selectHost(host) {
  selectedHostId.value = host.id;
  await store.selectHost(host.id);
}

async function openTerminal(host) {
  await selectHost(host);
  router.push(`/terminal/${host.id}`);
}

function statusTone(host) {
  if (host.status === "online") return "is-success";
  if (host.status === "installing" || host.status === "connecting") return "is-info";
  if (host.installState === "install_failed") return "is-danger";
  return "is-warning";
}

function canInstall(host) {
  return host && host.id !== "server-local" && host.address && host.status !== "online" && busyAction.value !== `install:${host.id}`;
}

function canOpenTerminal(host) {
  return host && host.status === "online" && (host.terminalCapable || host.executable);
}

function openCreateModal() {
  editorHost.value = null;
  showHostEditor.value = true;
}

function openEditModal(host) {
  editorHost.value = host;
  showHostEditor.value = true;
}

async function saveHost(payload) {
  clearMessage();
  const isEditing = !!editorHost.value?.id;
  busyAction.value = isEditing ? `update:${editorHost.value.id}` : "create";
  try {
    const url = isEditing
      ? `/api/v1/hosts/${encodeURIComponent(editorHost.value.id)}`
      : "/api/v1/hosts";
    const method = isEditing ? "PUT" : "POST";
    const response = await fetch(url, {
      method,
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await response.json();
    if (!response.ok) {
      pushError(data.error || "保存主机失败");
      return;
    }
    showHostEditor.value = false;
    await refreshInventory();
    selectedHostId.value = data.host?.id || payload.id;
    await loadHostSessions(selectedHostId.value);
    pushNotice(payload.installViaSsh ? "主机已创建，并已触发 SSH 安装。" : "主机信息已保存。");
  } catch (_err) {
    pushError("保存主机失败");
  } finally {
    busyAction.value = "";
  }
}

async function applyBatchTags(payload) {
  clearMessage();
  busyAction.value = "tags";
  try {
    const response = await fetch("/api/v1/hosts/tags", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        hostIds: selectedRows.value,
        add: payload.add,
        remove: payload.remove,
      }),
    });
    const data = await response.json();
    if (!response.ok) {
      pushError(data.error || "批量标签失败");
      return;
    }
    showTagModal.value = false;
    await refreshInventory();
    await loadHostSessions(selectedHostId.value);
    pushNotice(`已更新 ${selectedRows.value.length} 台主机标签。`);
  } catch (_err) {
    pushError("批量标签失败");
  } finally {
    busyAction.value = "";
  }
}

async function installHost(host) {
  if (!host?.id) return;
  clearMessage();
  busyAction.value = `install:${host.id}`;
  try {
    const response = await fetch(`/api/v1/hosts/${encodeURIComponent(host.id)}/install`, {
      method: "POST",
      credentials: "include",
    });
    const data = await response.json();
    if (!response.ok) {
      pushError(data.error || "SSH 安装失败");
      return;
    }
    await refreshInventory();
    await loadHostSessions(host.id);
    pushNotice("SSH 安装命令已执行，等待 host-agent 回连。");
  } catch (_err) {
    pushError("SSH 安装失败");
  } finally {
    busyAction.value = "";
  }
}

async function removeHost(host) {
  if (!host?.id || host.id === "server-local") return;
  const confirmed = window.confirm(`确认删除主机 ${host.name || host.id} 吗？`);
  if (!confirmed) return;
  clearMessage();
  busyAction.value = `delete:${host.id}`;
  try {
    const response = await fetch(`/api/v1/hosts/${encodeURIComponent(host.id)}`, {
      method: "DELETE",
      credentials: "include",
    });
    const data = await response.json();
    if (!response.ok) {
      pushError(data.error || "删除主机失败");
      return;
    }
    await refreshInventory();
    selectedRows.value = selectedRows.value.filter((id) => id !== host.id);
    pushNotice("主机已删除。");
  } catch (_err) {
    pushError("删除主机失败");
  } finally {
    busyAction.value = "";
  }
}

async function jumpToSession(sessionId) {
  const ok = await store.activateSession(sessionId);
  if (!ok) return;
  router.push("/");
}

onMounted(async () => {
  await refreshInventory();
});
</script>

<template>
  <section class="ops-page">
    <div class="ops-page-inner">
      <header class="ops-page-header">
        <div>
          <h2 class="ops-page-title">主机管理</h2>
          <p class="ops-page-subtitle">
            主机清单、SSH 引导安装、批量标签，以及每台子 agent 对应会话都收敛在一个 inventory 视图里。
          </p>
        </div>
      </header>

      <div class="ops-scope-bar">
        <div class="ops-scope-left">
          <span class="ops-scope-label">Connection Semantics</span>
          <span class="ops-pill is-info">状态 = 控制通道活跃度</span>
          <span class="ops-pill">接入 = reverse gRPC</span>
          <span class="ops-pill">安装 = SSH 引导</span>
        </div>
        <div class="ops-actions">
          <button class="ops-button ghost" @click="showTagModal = true" :disabled="!selectedRows.length">
            批量标签
          </button>
          <button class="ops-button primary" @click="openCreateModal">新增主机</button>
        </div>
      </div>

      <div class="ops-banner success" v-if="pageNotice">{{ pageNotice }}</div>
      <div class="ops-banner error" v-if="pageError">{{ pageError }}</div>

      <div class="ops-metric-grid">
        <article v-for="metric in metrics" :key="metric.label" class="ops-metric-card">
          <span class="ops-metric-label">{{ metric.label }}</span>
          <strong class="ops-metric-value">{{ metric.value }}</strong>
          <span class="ops-metric-meta">
            <span class="ops-inline-dot" :class="metric.tone"></span>
            {{ metric.meta }}
          </span>
        </article>
      </div>

      <div class="ops-grid ops-grid-hosts">
        <article class="ops-card">
          <div class="ops-card-header hosts-toolbar">
            <div>
              <h3 class="ops-card-title">主机清单</h3>
              <p class="ops-card-subtitle">支持新增主机、SSH 安装、批量打标签，以及查看子会话。</p>
            </div>

            <div class="hosts-toolbar-actions">
              <input v-model="searchQuery" class="hosts-search" placeholder="搜索主机 / 标签 / 地址" />
            </div>
          </div>

          <div class="ops-selection-bar" v-if="selectedRows.length">
            <span>已选 {{ selectedRows.length }} 台主机</span>
            <button class="ops-button ghost small" @click="showTagModal = true">批量加 / 删标签</button>
            <button class="ops-button ghost small" @click="selectedRows = []">清空选择</button>
          </div>

          <div class="ops-table-wrap">
            <table class="ops-table">
              <thead>
                <tr>
                  <th class="hosts-check-col">
                    <input type="checkbox" :checked="allRowsSelected" @change="toggleSelectAll" />
                  </th>
                  <th>名称</th>
                  <th>HOST ID</th>
                  <th>目标机器</th>
                  <th>接入方式</th>
                  <th>控制能力</th>
                  <th>标签</th>
                  <th>最近握手</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="host in filteredHosts"
                  :key="host.id"
                  :class="{ 'is-active': selectedHost && selectedHost.id === host.id }"
                  @click="selectHost(host)"
                >
                  <td class="hosts-check-col" @click.stop>
                    <input type="checkbox" :checked="isHostSelected(host.id)" @change="toggleRowSelection(host.id)" />
                  </td>
                  <td>
                    <div class="ops-host-name">
                      <span class="ops-inline-dot" :class="statusTone(host)"></span>
                      <div class="host-title-stack">
                        <strong>{{ host.name }}</strong>
                        <span class="host-status-copy">{{ statusLabel(host) }}</span>
                      </div>
                    </div>
                  </td>
                  <td><span class="ops-mono ops-faint">{{ host.id }}</span></td>
                  <td>
                    <div class="host-target">
                      <span class="ops-mono">{{ host.address || "server-local" }}</span>
                      <span v-if="host.sshUser || host.sshPort" class="host-target-meta">
                        {{ host.sshUser || "默认用户" }} · :{{ host.sshPort || 22 }}
                      </span>
                    </div>
                  </td>
                  <td>
                    <div class="host-target">
                      <span class="ops-mini-pill">{{ transportLabel(host) }}</span>
                      <span class="ops-mini-pill" :class="installTone(host)">{{ host.installState || "inventory" }}</span>
                    </div>
                  </td>
                  <td>{{ hostCapabilityLabel(host) }}</td>
                  <td>
                    <div class="host-label-cell">
                      <span
                        v-for="label in labelList(host.labels).slice(0, 3)"
                        :key="`${host.id}-${label}`"
                        class="ops-mini-pill"
                      >
                        {{ label }}
                      </span>
                      <span v-if="labelList(host.labels).length > 3" class="ops-faint">
                        +{{ labelList(host.labels).length - 3 }}
                      </span>
                    </div>
                  </td>
                  <td class="ops-align-right">{{ host.lastSeenText }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>

        <article class="ops-card ops-sidebar-card" v-if="selectedHost">
          <div class="ops-card-header">
            <div>
              <h3 class="ops-card-title">主机详情</h3>
              <p class="ops-card-subtitle">{{ selectedHost.name }} · {{ selectedHost.id }}</p>
            </div>
          </div>

          <div class="ops-badge-row">
            <span class="ops-pill" :class="statusTone(selectedHost)">{{ statusLabel(selectedHost) }}</span>
            <span class="ops-pill">{{ transportLabel(selectedHost) }}</span>
            <span class="ops-pill" :class="installTone(selectedHost)">{{ selectedHost.installState || "inventory" }}</span>
          </div>

          <div class="ops-detail-block">
            <h4 class="ops-detail-title">{{ selectedHost.os }} · {{ selectedHost.arch }}</h4>
            <p class="ops-detail-copy">
              target {{ selectedHost.address || "server-local" }} · agent {{ selectedHost.agentVersion || "未注册" }}
            </p>
            <p class="ops-detail-copy">
              最近握手 {{ selectedHost.lastSeenText }} · 模式 {{ selectedHost.controlMode || "inventory" }}
            </p>
          </div>

          <div class="ops-subcard">
            <span class="ops-subcard-label">标签</span>
            <div class="ops-chip-row">
              <span v-for="label in labelList(selectedHost.labels)" :key="label" class="ops-mini-pill">{{ label }}</span>
              <span v-if="!labelList(selectedHost.labels).length" class="ops-faint">暂无标签</span>
            </div>
          </div>

          <div class="ops-subcard">
            <span class="ops-subcard-label">控制通道说明</span>
            <p class="ops-detail-copy">
              在线状态只表示控制通道最近活跃，不代表业务健康。大规模场景下建议把它当“最近可直接拉起执行”的信号。
            </p>
            <p class="ops-detail-copy" v-if="selectedHost.lastError">
              最近错误: {{ selectedHost.lastError }}
            </p>
          </div>

          <div class="ops-subcard">
            <div class="ops-inline-actions host-session-header">
              <span class="ops-subcard-label">主 Agent / 子会话</span>
              <span class="ops-faint">{{ selectedSessionsCount }} 个会话</span>
            </div>

            <div v-if="hostSessionsLoading" class="ops-faint">正在加载会话...</div>
            <div v-else-if="!hostSessions.length" class="ops-faint">该主机还没有独立子会话。</div>
            <div v-else class="host-session-list">
              <article v-for="session in hostSessions" :key="session.sessionId" class="host-session-card">
                <div class="host-session-top">
                  <strong>{{ session.title || session.sessionId }}</strong>
                  <span class="ops-mini-pill">{{ session.status }}</span>
                </div>
                <p class="host-session-copy">
                  <span class="host-session-label">主 Agent 任务</span>
                  {{ session.taskSummary || "暂无任务摘要" }}
                </p>
                <p class="host-session-copy">
                  <span class="host-session-label">子会话回复</span>
                  {{ session.replySummary || "暂无回复摘要" }}
                </p>
                <div class="host-session-thread" v-if="session.messages?.length">
                  <div v-for="(message, idx) in session.messages" :key="`${session.sessionId}-${idx}`" class="host-session-message">
                    <span class="host-session-role" :class="`is-${message.role}`">{{ message.role }}</span>
                    <span>{{ message.text }}</span>
                  </div>
                </div>
                <div class="host-session-actions">
                  <span class="ops-faint">{{ session.messageCount }} 条消息 · {{ formatLastSeen(session.lastActivityAt, session.status) }}</span>
                  <button class="ops-button ghost small" @click="jumpToSession(session.sessionId)">切到该会话</button>
                </div>
              </article>
            </div>
          </div>

          <div class="ops-card-actions">
            <button class="ops-button primary" @click="openTerminal(selectedHost)" :disabled="!canOpenTerminal(selectedHost)">
              进入终端
            </button>
            <button class="ops-button ghost" @click="selectHost(selectedHost)">设为当前上下文</button>
            <button class="ops-button ghost" @click="openEditModal(selectedHost)" :disabled="selectedHost.id === 'server-local'">
              编辑主机
            </button>
            <button class="ops-button ghost" @click="installHost(selectedHost)" :disabled="!canInstall(selectedHost)">
              通过 SSH 安装
            </button>
            <button class="ops-button ghost danger" @click="removeHost(selectedHost)" :disabled="selectedHost.id === 'server-local'">
              删除主机
            </button>
          </div>
        </article>
      </div>
    </div>
  </section>

  <HostEditorModal
    v-if="showHostEditor"
    :host="editorHost"
    @close="showHostEditor = false"
    @save="saveHost"
  />

  <HostBatchTagModal
    v-if="showTagModal"
    :count="selectedRows.length"
    @close="showTagModal = false"
    @save="applyBatchTags"
  />
</template>

<style scoped>
.hosts-toolbar {
  gap: 16px;
}

.hosts-toolbar-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.hosts-search {
  width: 280px;
  max-width: 100%;
  border: 1px solid #dbe3ee;
  border-radius: 999px;
  padding: 10px 14px;
  font-size: 13px;
  background: #fff;
}

.hosts-search:focus {
  outline: none;
  border-color: #93c5fd;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.12);
}

.ops-selection-bar {
  margin-bottom: 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 14px;
  background: #eff6ff;
  color: #1d4ed8;
  font-size: 13px;
  font-weight: 600;
}

.hosts-check-col {
  width: 42px;
}

.host-title-stack,
.host-target {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.host-status-copy,
.host-target-meta {
  font-size: 12px;
  color: #64748b;
}

.host-label-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.host-session-header {
  justify-content: space-between;
  margin-bottom: 12px;
}

.host-session-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.host-session-card {
  border: 1px solid #e2e8f0;
  border-radius: 14px;
  padding: 12px;
  background: #fff;
}

.host-session-top,
.host-session-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.host-session-copy {
  margin: 10px 0 0;
  font-size: 13px;
  line-height: 1.6;
  color: #1e293b;
}

.host-session-label {
  display: inline-block;
  min-width: 88px;
  color: #64748b;
  font-weight: 600;
}

.host-session-thread {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.host-session-message {
  display: flex;
  gap: 8px;
  font-size: 12px;
  line-height: 1.5;
  color: #334155;
}

.host-session-role {
  min-width: 58px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #64748b;
  font-weight: 700;
  font-size: 11px;
}

.host-session-role.is-user {
  color: #1d4ed8;
}

.host-session-role.is-assistant {
  color: #166534;
}

.host-session-role.is-system {
  color: #b45309;
}

.ops-banner {
  margin-bottom: 16px;
  padding: 12px 14px;
  border-radius: 14px;
  font-size: 13px;
  font-weight: 600;
}

.ops-banner.success {
  background: #ecfdf5;
  color: #166534;
  border: 1px solid #bbf7d0;
}

.ops-banner.error {
  background: #fef2f2;
  color: #b91c1c;
  border: 1px solid #fecaca;
}

.danger {
  color: #b91c1c;
  border-color: #fecaca;
}

@media (max-width: 960px) {
  .hosts-toolbar-actions {
    width: 100%;
  }

  .hosts-search {
    width: 100%;
  }

  .ops-selection-bar {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
