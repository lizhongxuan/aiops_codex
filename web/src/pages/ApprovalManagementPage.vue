<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useAppStore } from "../store";
import {
  ArrowLeftIcon,
  CheckCircleIcon,
  ClockIcon,
  FilterIcon,
  RefreshCcwIcon,
  ShieldCheckIcon,
  TerminalIcon,
  XCircleIcon,
} from "lucide-vue-next";

const router = useRouter();
const store = useAppStore();

/* ---- Tab state ---- */
const activeTab = ref("audits");

/* ---- Stats ---- */
const stats = reactive({
  todayTotal: 0,
  pending: 0,
  autoAccepted: 0,
  grantedCommands: 0,
});

/* ---- Filter state ---- */
const filters = reactive({
  timeRange: "",
  sessionKind: "",
  host: "",
  operator: "",
  decision: "",
  toolName: "",
});

/* ---- Audit trail table ---- */
const audits = ref([]);
const auditPage = ref(1);
const auditPageSize = ref(20);
const auditTotal = ref(0);
const auditLoading = ref(false);

const auditTotalPages = computed(() => Math.max(1, Math.ceil(auditTotal.value / auditPageSize.value)));

/* ---- Detail drawer ---- */
const drawerVisible = ref(false);
const drawerRecord = ref(null);

/* ---- Grants tab ---- */
const grants = ref([]);
const grantsLoading = ref(false);
const grantsHostId = ref("");

/* ---- API helpers ---- */
function buildAuditQuery() {
  const params = new URLSearchParams();
  params.set("page", String(auditPage.value));
  params.set("pageSize", String(auditPageSize.value));
  if (filters.timeRange) params.set("timeRange", filters.timeRange);
  if (filters.sessionKind) params.set("sessionKind", filters.sessionKind);
  if (filters.host) params.set("host", filters.host);
  if (filters.operator) params.set("operator", filters.operator);
  if (filters.decision) params.set("decision", filters.decision);
  if (filters.toolName) params.set("toolName", filters.toolName);
  return params.toString();
}

async function fetchAudits() {
  auditLoading.value = true;
  try {
    const query = buildAuditQuery();
    const res = await fetch(`/api/v1/approval-audits?${query}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    audits.value = data.items || [];
    auditTotal.value = data.total || 0;
    stats.todayTotal = data.stats?.todayTotal ?? stats.todayTotal;
    stats.pending = data.stats?.pending ?? stats.pending;
    stats.autoAccepted = data.stats?.autoAccepted ?? stats.autoAccepted;
    stats.grantedCommands = data.stats?.grantedCommands ?? stats.grantedCommands;
  } catch (err) {
    console.error("[ApprovalManagement] fetchAudits failed:", err);
  } finally {
    auditLoading.value = false;
  }
}

async function fetchGrants(hostId) {
  grantsLoading.value = true;
  try {
    const url = hostId
      ? `/api/v1/approval-grants?hostId=${encodeURIComponent(hostId)}`
      : "/api/v1/approval-grants";
    const res = await fetch(url);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    grants.value = data.items || [];
  } catch (err) {
    console.error("[ApprovalManagement] fetchGrants failed:", err);
  } finally {
    grantsLoading.value = false;
  }
}

async function grantAction(grantId, action) {
  try {
    const res = await fetch(`/api/v1/approval-grants/${encodeURIComponent(grantId)}/${action}`, {
      method: "POST",
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    await fetchGrants(grantsHostId.value);
  } catch (err) {
    console.error(`[ApprovalManagement] grantAction(${action}) failed:`, err);
  }
}

function revokeGrant(grantId) {
  return grantAction(grantId, "revoke");
}

function disableGrant(grantId) {
  return grantAction(grantId, "disable");
}

function enableGrant(grantId) {
  return grantAction(grantId, "enable");
}

/* ---- Filter / pagination handlers ---- */
function applyFilters() {
  auditPage.value = 1;
  fetchAudits();
}

function resetFilters() {
  Object.assign(filters, {
    timeRange: "",
    sessionKind: "",
    host: "",
    operator: "",
    decision: "",
    toolName: "",
  });
  applyFilters();
}

function goToPage(page) {
  if (page < 1 || page > auditTotalPages.value) return;
  auditPage.value = page;
  fetchAudits();
}

/* ---- Drawer ---- */
function openDrawer(record) {
  drawerRecord.value = record;
  drawerVisible.value = true;
}

function closeDrawer() {
  drawerVisible.value = false;
  drawerRecord.value = null;
}

/* ---- Decision badge ---- */
function decisionClass(decision) {
  switch (decision) {
    case "approved":
      return "badge-approved";
    case "rejected":
      return "badge-rejected";
    case "auto_accepted":
      return "badge-auto";
    case "pending":
      return "badge-pending";
    default:
      return "badge-default";
  }
}

function decisionLabel(decision) {
  switch (decision) {
    case "approved":
      return "已批准";
    case "rejected":
      return "已拒绝";
    case "auto_accepted":
      return "自动放行";
    case "pending":
      return "待审核";
    default:
      return decision || "未知";
  }
}

function grantStatusClass(status) {
  switch (status) {
    case "active":
      return "badge-approved";
    case "disabled":
      return "badge-pending";
    case "revoked":
      return "badge-rejected";
    default:
      return "badge-default";
  }
}

function grantStatusLabel(status) {
  switch (status) {
    case "active":
      return "生效中";
    case "disabled":
      return "已禁用";
    case "revoked":
      return "已撤销";
    default:
      return status || "未知";
  }
}

function formatTime(value) {
  if (!value) return "-";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString("zh-CN", { hour12: false });
}

/* ---- Tab switch ---- */
watch(activeTab, (tab) => {
  if (tab === "grants") {
    fetchGrants(grantsHostId.value);
  }
});

watch(grantsHostId, (hostId) => {
  if (activeTab.value === "grants") {
    fetchGrants(hostId);
  }
});

/* ---- Lifecycle ---- */
onMounted(() => {
  fetchAudits();
});
</script>

<template>
  <div class="approval-page" data-testid="approval-management-page">
    <!-- Header -->
    <header class="approval-header">
      <button class="back-link" @click="router.push('/')">
        <ArrowLeftIcon :size="16" />
        <span>返回工作区</span>
      </button>
      <div class="header-copy">
        <h1>审批管理</h1>
        <p>集中查看审批流水、授权记录，管理命令授权的生命周期。</p>
      </div>
    </header>

    <!-- Stats row -->
    <section class="stats-row" data-testid="stats-row">
      <div class="stat-card">
        <ClockIcon :size="18" class="stat-icon" />
        <div class="stat-body">
          <span class="stat-label">今日审批</span>
          <strong class="stat-value" data-testid="stat-today-total">{{ stats.todayTotal }}</strong>
        </div>
      </div>
      <div class="stat-card">
        <FilterIcon :size="18" class="stat-icon pending" />
        <div class="stat-body">
          <span class="stat-label">待处理</span>
          <strong class="stat-value" data-testid="stat-pending">{{ stats.pending }}</strong>
        </div>
      </div>
      <div class="stat-card">
        <CheckCircleIcon :size="18" class="stat-icon auto" />
        <div class="stat-body">
          <span class="stat-label">自动放行</span>
          <strong class="stat-value" data-testid="stat-auto-accepted">{{ stats.autoAccepted }}</strong>
        </div>
      </div>
      <div class="stat-card">
        <TerminalIcon :size="18" class="stat-icon granted" />
        <div class="stat-body">
          <span class="stat-label">已授权命令</span>
          <strong class="stat-value" data-testid="stat-granted-commands">{{ stats.grantedCommands }}</strong>
        </div>
      </div>
    </section>

    <!-- Tabs -->
    <nav class="tab-bar" data-testid="tab-bar">
      <button
        class="tab-btn"
        :class="{ active: activeTab === 'audits' }"
        data-testid="tab-audits"
        @click="activeTab = 'audits'"
      >
        审核流水
      </button>
      <button
        class="tab-btn"
        :class="{ active: activeTab === 'grants' }"
        data-testid="tab-grants"
        @click="activeTab = 'grants'"
      >
        授权列表
      </button>
    </nav>

    <!-- Audits Tab -->
    <template v-if="activeTab === 'audits'">
      <!-- Filter area -->
      <section class="filter-bar" data-testid="filter-bar">
        <label class="filter-field">
          <span>时间范围</span>
          <select v-model="filters.timeRange" data-testid="filter-time-range">
            <option value="">全部</option>
            <option value="1h">最近 1 小时</option>
            <option value="24h">最近 24 小时</option>
            <option value="7d">最近 7 天</option>
            <option value="30d">最近 30 天</option>
          </select>
        </label>
        <label class="filter-field">
          <span>会话类型</span>
          <select v-model="filters.sessionKind" data-testid="filter-session-kind">
            <option value="">全部</option>
            <option value="chat">chat</option>
            <option value="workspace">workspace</option>
          </select>
        </label>
        <label class="filter-field">
          <span>主机</span>
          <input v-model="filters.host" type="text" placeholder="主机 ID" data-testid="filter-host" />
        </label>
        <label class="filter-field">
          <span>操作人</span>
          <input v-model="filters.operator" type="text" placeholder="操作人" data-testid="filter-operator" />
        </label>
        <label class="filter-field">
          <span>决策</span>
          <select v-model="filters.decision" data-testid="filter-decision">
            <option value="">全部</option>
            <option value="approved">已批准</option>
            <option value="rejected">已拒绝</option>
            <option value="auto_accepted">自动放行</option>
            <option value="pending">待审核</option>
          </select>
        </label>
        <label class="filter-field">
          <span>工具名称</span>
          <input v-model="filters.toolName" type="text" placeholder="工具名称" data-testid="filter-tool-name" />
        </label>
        <div class="filter-actions">
          <button class="filter-btn primary" data-testid="apply-filters-btn" @click="applyFilters">筛选</button>
          <button class="filter-btn secondary" data-testid="reset-filters-btn" @click="resetFilters">重置</button>
        </div>
      </section>

      <!-- Audit table -->
      <section class="table-section" data-testid="audit-table-section">
        <div v-if="auditLoading" class="table-loading">加载中...</div>
        <table v-else class="audit-table" data-testid="audit-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>会话类型</th>
              <th>主机</th>
              <th>操作人</th>
              <th>工具</th>
              <th>决策</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in audits"
              :key="row.id"
              class="audit-row"
              :data-testid="`audit-row-${row.id}`"
              @click="openDrawer(row)"
            >
              <td>{{ formatTime(row.createdAt) }}</td>
              <td>{{ row.sessionKind || "-" }}</td>
              <td>{{ row.host || "-" }}</td>
              <td>{{ row.operator || "-" }}</td>
              <td>{{ row.toolName || "-" }}</td>
              <td>
                <span class="badge" :class="decisionClass(row.decision)">{{ decisionLabel(row.decision) }}</span>
              </td>
              <td>
                <button class="detail-btn" @click.stop="openDrawer(row)">详情</button>
              </td>
            </tr>
            <tr v-if="!audits.length">
              <td colspan="7" class="empty-row">暂无审批记录</td>
            </tr>
          </tbody>
        </table>

        <!-- Pagination -->
        <div class="pagination" data-testid="pagination">
          <button :disabled="auditPage <= 1" data-testid="page-prev" @click="goToPage(auditPage - 1)">上一页</button>
          <span class="page-info" data-testid="page-info">{{ auditPage }} / {{ auditTotalPages }}</span>
          <button :disabled="auditPage >= auditTotalPages" data-testid="page-next" @click="goToPage(auditPage + 1)">下一页</button>
        </div>
      </section>
    </template>

    <!-- Grants Tab -->
    <template v-if="activeTab === 'grants'">
      <section class="grants-section" data-testid="grants-section">
        <div class="grants-toolbar">
          <label class="filter-field">
            <span>主机 ID</span>
            <input v-model="grantsHostId" type="text" placeholder="按主机筛选" data-testid="grants-host-filter" />
          </label>
          <button class="filter-btn secondary" data-testid="grants-refresh-btn" @click="fetchGrants(grantsHostId)">
            <RefreshCcwIcon :size="14" />
            <span>刷新</span>
          </button>
        </div>

        <div v-if="grantsLoading" class="table-loading">加载中...</div>
        <table v-else class="audit-table" data-testid="grants-table">
          <thead>
            <tr>
              <th>授权 ID</th>
              <th>主机</th>
              <th>命令/工具</th>
              <th>授权时间</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="grant in grants" :key="grant.id" :data-testid="`grant-row-${grant.id}`">
              <td>{{ grant.id }}</td>
              <td>{{ grant.hostId || "-" }}</td>
              <td>{{ grant.command || grant.toolName || "-" }}</td>
              <td>{{ formatTime(grant.grantedAt) }}</td>
              <td>
                <span class="badge" :class="grantStatusClass(grant.status)">{{ grantStatusLabel(grant.status) }}</span>
              </td>
              <td class="grant-actions">
                <button
                  v-if="grant.status === 'active'"
                  class="action-btn warn"
                  :data-testid="`revoke-${grant.id}`"
                  @click="revokeGrant(grant.id)"
                >
                  撤销
                </button>
                <button
                  v-if="grant.status === 'active'"
                  class="action-btn muted"
                  :data-testid="`disable-${grant.id}`"
                  @click="disableGrant(grant.id)"
                >
                  禁用
                </button>
                <button
                  v-if="grant.status === 'disabled'"
                  class="action-btn primary"
                  :data-testid="`enable-${grant.id}`"
                  @click="enableGrant(grant.id)"
                >
                  启用
                </button>
              </td>
            </tr>
            <tr v-if="!grants.length">
              <td colspan="6" class="empty-row">暂无授权记录</td>
            </tr>
          </tbody>
        </table>
      </section>
    </template>

    <!-- Detail Drawer -->
    <Teleport to="body">
      <div v-if="drawerVisible" class="drawer-overlay" data-testid="drawer-overlay" @click.self="closeDrawer">
        <aside class="drawer-panel" data-testid="detail-drawer">
          <header class="drawer-header">
            <h2>审批详情</h2>
            <button class="drawer-close" data-testid="drawer-close" @click="closeDrawer">✕</button>
          </header>
          <div v-if="drawerRecord" class="drawer-body">
            <dl class="detail-list">
              <dt>审批 ID</dt>
              <dd>{{ drawerRecord.id }}</dd>
              <dt>时间</dt>
              <dd>{{ formatTime(drawerRecord.createdAt) }}</dd>
              <dt>会话类型</dt>
              <dd>{{ drawerRecord.sessionKind || "-" }}</dd>
              <dt>主机</dt>
              <dd>{{ drawerRecord.host || "-" }}</dd>
              <dt>操作人</dt>
              <dd>{{ drawerRecord.operator || "-" }}</dd>
              <dt>工具</dt>
              <dd>{{ drawerRecord.toolName || "-" }}</dd>
              <dt>决策</dt>
              <dd>
                <span class="badge" :class="decisionClass(drawerRecord.decision)">{{ decisionLabel(drawerRecord.decision) }}</span>
              </dd>
              <dt>命令</dt>
              <dd><code>{{ drawerRecord.command || "-" }}</code></dd>
              <dt>备注</dt>
              <dd>{{ drawerRecord.notes || "-" }}</dd>
            </dl>
          </div>
        </aside>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.approval-page {
  min-height: 100%;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  background: linear-gradient(180deg, #f8fbff 0%, #f8fafc 100%);
}

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: #2563eb;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  padding: 0;
}

.approval-header {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.header-copy h1 {
  margin: 8px 0 4px;
  font-size: 28px;
  color: #0f172a;
}

.header-copy p {
  margin: 0;
  color: #475569;
  line-height: 1.7;
}

/* Stats */
.stats-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}

.stat-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  border-radius: 16px;
  background: white;
  border: 1px solid rgba(226, 232, 240, 0.9);
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.04);
}

.stat-icon { color: #2563eb; }
.stat-icon.pending { color: #f59e0b; }
.stat-icon.auto { color: #10b981; }
.stat-icon.granted { color: #6366f1; }

.stat-label {
  display: block;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #64748b;
}

.stat-value {
  display: block;
  margin-top: 2px;
  font-size: 22px;
  color: #0f172a;
}

/* Tabs */
.tab-bar {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid #e2e8f0;
  padding-bottom: 0;
}

.tab-btn {
  padding: 10px 18px;
  border: none;
  background: none;
  font-size: 14px;
  font-weight: 600;
  color: #64748b;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  transition: color 0.15s, border-color 0.15s;
}

.tab-btn.active {
  color: #2563eb;
  border-bottom-color: #2563eb;
}

/* Filters */
.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: flex-end;
  padding: 14px 16px;
  border-radius: 14px;
  background: white;
  border: 1px solid rgba(226, 232, 240, 0.9);
}

.filter-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 120px;
}

.filter-field span {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #64748b;
}

.filter-field input,
.filter-field select {
  padding: 6px 10px;
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  font-size: 13px;
  background: #f8fafc;
}

.filter-actions {
  display: flex;
  gap: 6px;
  align-self: flex-end;
}

.filter-btn {
  padding: 7px 14px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  border: 1px solid transparent;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.filter-btn.primary {
  background: #2563eb;
  color: white;
}

.filter-btn.secondary {
  background: white;
  color: #0f172a;
  border-color: #cbd5e1;
}

/* Table */
.table-section,
.grants-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.table-loading {
  padding: 32px;
  text-align: center;
  color: #64748b;
}

.audit-table {
  width: 100%;
  border-collapse: collapse;
  background: white;
  border-radius: 14px;
  overflow: hidden;
  border: 1px solid rgba(226, 232, 240, 0.9);
}

.audit-table th {
  padding: 10px 14px;
  text-align: left;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #64748b;
  background: #f8fafc;
  border-bottom: 1px solid #e2e8f0;
}

.audit-table td {
  padding: 10px 14px;
  font-size: 13px;
  color: #334155;
  border-bottom: 1px solid #f1f5f9;
}

.audit-row {
  cursor: pointer;
  transition: background 0.12s;
}

.audit-row:hover {
  background: #f0f7ff;
}

.empty-row {
  text-align: center;
  color: #94a3b8;
  padding: 32px 14px !important;
}

/* Badges */
.badge {
  display: inline-block;
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
}

.badge-approved { background: #dcfce7; color: #166534; }
.badge-rejected { background: #fee2e2; color: #991b1b; }
.badge-auto { background: #dbeafe; color: #1e40af; }
.badge-pending { background: #fef3c7; color: #92400e; }
.badge-default { background: #f1f5f9; color: #475569; }

/* Buttons */
.detail-btn {
  padding: 4px 10px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  background: white;
  font-size: 12px;
  cursor: pointer;
  color: #2563eb;
  font-weight: 600;
}

.action-btn {
  padding: 4px 10px;
  border: 1px solid transparent;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.action-btn.primary { background: #2563eb; color: white; }
.action-btn.warn { background: #fef3c7; color: #92400e; border-color: #fcd34d; }
.action-btn.muted { background: #f1f5f9; color: #475569; border-color: #cbd5e1; }

.grant-actions {
  display: flex;
  gap: 6px;
}

/* Pagination */
.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 8px 0;
}

.pagination button {
  padding: 6px 14px;
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  background: white;
  font-size: 13px;
  cursor: pointer;
  color: #0f172a;
}

.pagination button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.page-info {
  font-size: 13px;
  color: #64748b;
}

/* Grants toolbar */
.grants-toolbar {
  display: flex;
  gap: 10px;
  align-items: flex-end;
}

/* Drawer */
.drawer-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.35);
  z-index: 1000;
  display: flex;
  justify-content: flex-end;
}

.drawer-panel {
  width: 420px;
  max-width: 90vw;
  background: white;
  height: 100%;
  display: flex;
  flex-direction: column;
  box-shadow: -8px 0 32px rgba(15, 23, 42, 0.12);
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 20px;
  border-bottom: 1px solid #e2e8f0;
}

.drawer-header h2 {
  margin: 0;
  font-size: 18px;
  color: #0f172a;
}

.drawer-close {
  background: none;
  border: none;
  font-size: 18px;
  cursor: pointer;
  color: #64748b;
  padding: 4px;
}

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}

.detail-list {
  display: grid;
  grid-template-columns: 100px 1fr;
  gap: 10px 14px;
}

.detail-list dt {
  font-size: 12px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.detail-list dd {
  margin: 0;
  font-size: 13px;
  color: #334155;
  word-break: break-all;
}

.detail-list code {
  background: #f1f5f9;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 12px;
}

@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: repeat(2, 1fr);
  }

  .filter-bar {
    flex-direction: column;
  }

  .drawer-panel {
    width: 100%;
  }
}
</style>
