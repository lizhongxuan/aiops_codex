<script setup>
import { computed, ref, watch } from "vue";
import { AlertTriangleIcon, Loader2Icon, PanelsTopLeftIcon, RefreshCwIcon } from "lucide-vue-next";
import ProtocolApprovalRail from "../components/protocol-workspace/ProtocolApprovalRail.vue";
import ProtocolConversationPane from "../components/protocol-workspace/ProtocolConversationPane.vue";
import ProtocolEventTimeline from "../components/protocol-workspace/ProtocolEventTimeline.vue";
import ProtocolEvidenceModal from "../components/protocol-workspace/ProtocolEvidenceModal.vue";
import { buildProtocolEvidenceTabs, buildProtocolWorkspaceModel } from "../lib/protocolWorkspaceVm";
import { compactText } from "../lib/workspaceViewModel";
import { useAppStore } from "../store";

const store = useAppStore();

const refreshBusy = ref(false);
const decisionBusy = ref(false);
const composerDraft = ref("");
const actionNotice = ref("");
const actionTone = ref("info");
const evidenceOpen = ref(false);
const evidenceTab = ref("planner-ai");
const selectedHostId = ref("");
const selectedStepId = ref("");
const selectedApprovalId = ref("");
const selectedMessageId = ref("");
const evidenceSource = ref("mission");

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function normalizePhaseLabel(value) {
  const phase = compactText(value).toLowerCase();
  switch (phase) {
    case "executing":
    case "running":
      return "执行中";
    case "planning":
      return "规划中";
    case "thinking":
      return "思考中";
    case "waiting_approval":
      return "等待审批";
    case "waiting_input":
      return "等待补充输入";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "aborted":
      return "已停止";
    default:
      return phase || "待命";
  }
}

function stepStatusLabel(value) {
  const status = compactText(value).toLowerCase();
  if (status.includes("complete") || status.includes("done")) return "已完成";
  if (status.includes("run") || status.includes("progress") || status.includes("active")) return "执行中";
  if (status.includes("wait")) return "等待审批";
  if (status.includes("fail") || status.includes("error")) return "失败";
  return "待执行";
}

function stringifyRaw(value) {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map((item) => String(item ?? "")).join("\n");
  if (value && typeof value === "object") {
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return String(value);
    }
  }
  return "";
}

function pushActionNotice(message, tone = "info") {
  actionNotice.value = compactText(message);
  actionTone.value = tone;
}

const isWorkspaceSession = computed(() => store.snapshot.kind === "workspace");
const recentWorkspaceSession = computed(
  () => store.sessionList.find((session) => session.kind === "workspace" && session.id !== store.activeSessionId) || null,
);
const workspaceModel = computed(() => buildProtocolWorkspaceModel(store.snapshot, store.runtime));
const hostRows = computed(() => workspaceModel.value.hostRows || []);
const planCardModel = computed(() => workspaceModel.value.planCardModel || { stepItems: [] });
const conversationItems = computed(() => workspaceModel.value.conversationItems || []);
const approvalItems = computed(() => workspaceModel.value.approvalItems || []);
const eventItems = computed(() => workspaceModel.value.eventItems || []);
const timelineItems = computed(() => [...eventItems.value].reverse());
const backgroundAgents = computed(() => workspaceModel.value.backgroundAgents || []);

const selectedApprovalItem = computed(() => {
  if (selectedApprovalId.value) {
    return approvalItems.value.find((item) => item.id === selectedApprovalId.value) || approvalItems.value[0] || null;
  }
  return approvalItems.value[0] || null;
});

const selectedStep = computed(() => planCardModel.value.stepItems?.find((item) => item.id === selectedStepId.value) || null);
const selectedHostRow = computed(() => {
  if (selectedHostId.value) {
    const direct = hostRows.value.find((row) => row.hostId === selectedHostId.value);
    if (direct) return direct;
  }

  if (selectedApprovalItem.value?.hostId) {
    const approvalHost = hostRows.value.find((row) => row.hostId === selectedApprovalItem.value.hostId);
    if (approvalHost) return approvalHost;
  }

  const stepHostId = selectedStep.value?.hosts?.[0]?.id;
  if (stepHostId) {
    const stepHost = hostRows.value.find((row) => row.hostId === stepHostId);
    if (stepHost) return stepHost;
  }

  return (
    hostRows.value.find((row) => ["running", "waiting_approval", "queued"].includes(row.statusKey)) ||
    hostRows.value[0] ||
    null
  );
});

const canSendWorkspaceMessage = computed(() => {
  return (
    isWorkspaceSession.value &&
    store.snapshot.auth?.connected !== false &&
    store.snapshot.config?.codexAlive !== false &&
    !store.sending
  );
});

const conversationSubtitle = computed(() => {
  const summary = compactText(planCardModel.value.summary);
  if (summary) return summary;
  if (workspaceModel.value.missionPhase === "waiting_approval") return "主 Agent 已产出计划，当前正在等待审批继续推进。";
  return "通过主 Agent 对话直接看 plan、step 分配和 host-agent 执行状态。";
});

const planSummaryLabel = computed(() => {
  const total = Number(planCardModel.value.totalSteps || 0);
  const completed = Number(planCardModel.value.completedSteps || 0);
  if (!total) return "计划生成后，会在这里直接展示 step -> host-agent 映射。";
  return `共 ${total} 个任务，已完成 ${completed} 个`;
});

const planCards = computed(() =>
  asArray(planCardModel.value.stepItems).map((item) => ({
    id: item.id,
    step: {
      id: item.id,
      title: item.title,
      description: item.summary,
    },
    status: item.status,
    statusLabel: stepStatusLabel(item.status),
    hostAgent: item.hosts || [],
    detail: item.summary,
    note: item.constraints?.length ? `约束：${item.constraints.join(" / ")}` : "",
    tags: [
      ...(item.approvalCount ? [{ id: `${item.id}-approval`, label: `待审批 ${item.approvalCount}` }] : []),
      ...asArray(item.constraints).slice(0, 2).map((constraint, index) => ({
        id: `${item.id}-constraint-${index}`,
        label: constraint,
      })),
    ],
    actions: [
      {
        id: `${item.id}-evidence`,
        key: "evidence",
        label: "查看证据",
      },
    ],
    index: item.index,
  })),
);

const filteredEventItems = computed(() => {
  const selectedHost = selectedHostRow.value?.hostId;
  if (!selectedHost) return eventItems.value;
  return eventItems.value.filter((item) => !item.hostId || item.hostId === selectedHost || item.targetType === "dispatch");
});

const evidenceBase = computed(() =>
  buildProtocolEvidenceTabs({
    planCardModel: planCardModel.value,
    hostRow: selectedHostRow.value,
    eventItems: filteredEventItems.value,
  }),
);

const plannerAiPanel = computed(() => {
  const items = evidenceBase.value.plannerAi.length
    ? evidenceBase.value.plannerAi.map((item) => ({
        label: item.time || item.title || "Planner",
        value: item.text,
      }))
    : [
        {
          label: "Plan",
          value: compactText(planCardModel.value.summary) || "当前还没有可用的 Planner 对话摘录。",
        },
      ];

  return {
    title: "Planner -> AI 对话",
    summary: compactText(planCardModel.value.title || planCardModel.value.version || "查看主 Agent 如何理解并生成 plan。"),
    items,
    raw: planCardModel.value.rawPlannerTraceRef,
  };
});

const plannerHostPanel = computed(() => {
  const rows = [];
  if (selectedStep.value) {
    rows.push(
      { label: "Step", value: selectedStep.value.title },
      { label: "状态", value: stepStatusLabel(selectedStep.value.status) },
      { label: "Host-agent", value: asArray(selectedStep.value.hosts).map((host) => host.label).join("、") || "未分配" },
    );
  }
  if (selectedApprovalItem.value) {
    rows.push(
      { label: "审批主机", value: selectedApprovalItem.value.hostName || selectedApprovalItem.value.hostId || "未指定" },
      { label: "命令", value: selectedApprovalItem.value.command || selectedApprovalItem.value.summary || "未提供命令" },
    );
    rows.push(
      ...asArray(selectedApprovalItem.value.detailRows).map((item) => ({
        label: compactText(item.label || "详情"),
        value: compactText(item.value || item.text),
      })),
    );
  }

  const eventRows = evidenceBase.value.plannerHost.map((item) => ({
    label: item.time || item.title || "Dispatcher",
    value: item.text || item.title,
  }));

  return {
    title: "Planner -> Host / Dispatcher",
    summary: rows.length ? "当前 step / 审批对应的派发与执行上下文。" : "查看 Planner 如何把任务分发给 host-agent。",
    items: rows.concat(eventRows),
    raw: selectedApprovalItem.value?.raw || null,
  };
});

const hostAiPanel = computed(() => {
  const transcript = evidenceBase.value.hostConversation.length
    ? evidenceBase.value.hostConversation.map((item) => ({
        label: item.time || item.title || "Host",
        value: item.text,
      }))
    : asArray(selectedHostRow.value?.worker?.transcript).map((item, index) => ({
        label: `Transcript ${index + 1}`,
        value: String(item ?? ""),
      }));

  return {
    title: `${selectedHostRow.value?.displayName || "Host"} -> AI`,
    summary: selectedHostRow.value?.taskTitle || "当前 host-agent 与 AI 的对话摘录。",
    items: transcript.length ? transcript : [{ label: "状态", value: "当前 host-agent 还没有可展示的对话摘录。" }],
    raw: selectedHostRow.value?.worker || null,
  };
});

const terminalPanel = computed(() => ({
  title: `${selectedHostRow.value?.displayName || "Host"} terminal`,
  summary: selectedHostRow.value?.summary || "查看当前 host-agent 对应主机的终端输出。",
  items: asArray(evidenceBase.value.hostTerminalRows).map((row) => ({
    label: row.label || row.key,
    value: row.value || row.text,
  })),
  raw: evidenceBase.value.hostTerminalOutput || selectedHostRow.value?.worker?.terminal || "",
}));

const evidencePanels = computed(() => ({
  "planner-ai": plannerAiPanel.value,
  "planner-host": plannerHostPanel.value,
  "host-ai": hostAiPanel.value,
  terminal: terminalPanel.value,
}));

const evidenceTabs = computed(() => [
  { value: "planner-ai", label: "Planner -> AI", badge: plannerAiPanel.value.items?.length || 0 },
  { value: "planner-host", label: "Planner -> Host", badge: plannerHostPanel.value.items?.length || 0 },
  { value: "host-ai", label: "Host -> AI", badge: hostAiPanel.value.items?.length || 0 },
  { value: "terminal", label: "Host Terminal", badge: terminalPanel.value.items?.length || 0 },
]);

const evidenceTitle = computed(() => {
  if (evidenceSource.value === "approval" && selectedApprovalItem.value) {
    return `审批证据 · ${selectedApprovalItem.value.hostName || selectedApprovalItem.value.hostId || "Host"}`;
  }
  if (evidenceSource.value === "step" && selectedStep.value) {
    return `步骤证据 · ${selectedStep.value.title}`;
  }
  if (evidenceSource.value === "host" && selectedHostRow.value) {
    return `Host 证据 · ${selectedHostRow.value.displayName}`;
  }
  return "执行证据";
});

const evidenceSubtitle = computed(() => {
  if (selectedApprovalItem.value) {
    return "审批详情通过弹框查看，不占用主页面空间。";
  }
  if (selectedStep.value) {
    return "这里汇总当前 step 的 Planner、Dispatcher、Host 与 terminal 上下文。";
  }
  return "按 tab 切换 Planner、Host 与终端视角。";
});

const runtimeStatus = computed(() => {
  const phase = normalizePhaseLabel(workspaceModel.value.missionPhase);
  const total = Number(planCardModel.value.totalSteps || 0);
  const completed = Number(planCardModel.value.completedSteps || 0);
  if (!total) return `${phase} | 等待主 Agent 生成计划`;
  return `${phase} | 共 ${total} 个任务，已完成 ${completed} 个`;
});

const toolbarTone = computed(() => {
  if (store.errorMessage) return "danger";
  if (actionTone.value) return actionTone.value;
  return "info";
});

watch(
  approvalItems,
  (items) => {
    if (selectedApprovalId.value && items.some((item) => item.id === selectedApprovalId.value)) return;
    selectedApprovalId.value = items[0]?.id || "";
  },
  { immediate: true, deep: true },
);

watch(
  hostRows,
  (items) => {
    if (selectedHostId.value && items.some((item) => item.hostId === selectedHostId.value)) return;
    selectedHostId.value = items[0]?.hostId || "";
  },
  { immediate: true, deep: true },
);

function openEvidence({ source = "mission", hostId = "", stepId = "", approvalId = "", tab = "planner-ai" } = {}) {
  if (hostId) selectedHostId.value = hostId;
  if (stepId) selectedStepId.value = stepId;
  if (approvalId) selectedApprovalId.value = approvalId;
  evidenceSource.value = source;
  evidenceTab.value = tab;
  evidenceOpen.value = true;
}

async function refreshProtocolState() {
  refreshBusy.value = true;
  try {
    await Promise.all([store.fetchState(), store.fetchSessions()]);
    pushActionNotice("工作台状态已刷新。", "info");
  } finally {
    refreshBusy.value = false;
  }
}

async function createWorkspaceSession() {
  const ok = await store.createSession("workspace");
  if (ok) pushActionNotice("已创建新的协作工作台。", "info");
}

async function activateRecentWorkspaceSession() {
  if (!recentWorkspaceSession.value?.id) return;
  const ok = await store.activateSession(recentWorkspaceSession.value.id);
  if (ok) pushActionNotice("已切换到最近的工作台。", "info");
}

async function sendWorkspaceMessage(payload = composerDraft.value) {
  if (!canSendWorkspaceMessage.value || !compactText(payload)) return;
  store.sending = true;
  store.errorMessage = "";
  actionNotice.value = "";
  store.setTurnPhase("thinking");
  store.resetActivity();

  try {
    const response = await fetch("/api/v1/chat/message", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        message: compactText(payload),
        hostId: selectedHostRow.value?.hostId || store.snapshot.selectedHostId || "server-local",
      }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      store.errorMessage = data.error || "message send failed";
      store.setTurnPhase("failed");
      return;
    }
    composerDraft.value = "";
    pushActionNotice("消息已发送给主 Agent。", "info");
    await Promise.all([store.fetchState(), store.fetchSessions()]);
  } catch (_error) {
    store.errorMessage = "Network error";
    store.setTurnPhase("failed");
  } finally {
    store.sending = false;
  }
}

async function stopWorkspaceMessage() {
  if (!store.runtime.turn.active || decisionBusy.value) return;
  try {
    const response = await fetch("/api/v1/chat/stop", {
      method: "POST",
      credentials: "include",
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      store.errorMessage = data.error || "stop failed";
      store.setTurnPhase("failed");
      return;
    }
    pushActionNotice("已停止当前工作台任务。", "info");
    store.errorMessage = "";
    store.setTurnPhase("aborted");
    await Promise.all([store.fetchState(), store.fetchSessions()]);
  } catch (_error) {
    store.errorMessage = "Network error";
    store.setTurnPhase("failed");
  }
}

async function postApprovalDecision(approvalId, decision) {
  const response = await fetch(`/api/v1/approvals/${approvalId}/decision`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision }),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || "approval failed");
  }
}

async function submitApprovalDecision(approval, decision) {
  const approvalId = compactText(approval?.approvalId || approval?.requestId || approval?.raw?.approval?.requestId);
  if (!approvalId || decisionBusy.value) return;

  selectedApprovalId.value = compactText(approval?.id);
  decisionBusy.value = true;
  try {
    store.errorMessage = "";
    await postApprovalDecision(approvalId, decision);
    pushActionNotice(decision === "decline" ? "已提交拒绝，等待主 Agent 调整方案。" : "审批结果已提交。", decision === "decline" ? "warning" : "info");
    await Promise.all([store.fetchState(), store.fetchSessions()]);
  } catch (error) {
    store.errorMessage = error?.message || "approval failed";
    store.setTurnPhase("failed");
  } finally {
    decisionBusy.value = false;
  }
}

function handlePlanAction(payload) {
  const plan = payload?.plan || {};
  const hostId = compactText(payload?.host?.id || asArray(plan.hostAgent || plan.hosts || [])[0]?.id);
  if (compactText(payload?.action?.key) === "host" && hostId) {
    openEvidence({
      source: "host",
      stepId: compactText(plan.id || plan.step?.id || plan.stepId),
      hostId,
      tab: "host-ai",
    });
    return;
  }
  openEvidence({
    source: "step",
    stepId: compactText(plan.id || plan.step?.id || plan.stepId),
    hostId,
    tab: "planner-host",
  });
}

function handleAgentSelect(agent) {
  openEvidence({
    source: "host",
    hostId: compactText(agent?.hostId || agent?.id),
    tab: "host-ai",
  });
}

function handleMessageSelect(message) {
  selectedMessageId.value = compactText(message?.id);
  if (message?.role === "user") return;
  openEvidence({ source: "message", tab: "planner-ai" });
}

function handleEventSelect(item) {
  const targetType = compactText(item?.targetType).toLowerCase();
  if (targetType === "approval") {
    openEvidence({ source: "approval", approvalId: compactText(item?.targetId), hostId: compactText(item?.hostId), tab: "planner-host" });
    return;
  }
  if (targetType === "host") {
    openEvidence({ source: "host", hostId: compactText(item?.hostId || item?.targetId), tab: "host-ai" });
    return;
  }
  if (targetType === "dispatch") {
    openEvidence({ source: "dispatch", hostId: compactText(item?.hostId), tab: "planner-host" });
    return;
  }
  openEvidence({ source: "event", tab: "planner-ai" });
}

function handleApprovalDetail(approval) {
  selectedApprovalId.value = compactText(approval?.id);
  openEvidence({
    source: "approval",
    approvalId: compactText(approval?.id),
    hostId: compactText(approval?.hostId),
    tab: "planner-host",
  });
}

function handleApprovalAuthorize(approval) {
  selectedApprovalId.value = compactText(approval?.id);
  void submitApprovalDecision(approval, "accept_session");
}

function handleApprovalReject(approval) {
  selectedApprovalId.value = compactText(approval?.id);
  void submitApprovalDecision(approval, "decline");
}

function handleApprovalAccept(approval) {
  selectedApprovalId.value = compactText(approval?.id);
  void submitApprovalDecision(approval, "accept");
}
</script>

<template>
  <div class="protocol-workspace-page" data-testid="protocol-workspace-page">
    <div v-if="!isWorkspaceSession" class="protocol-workspace-empty">
      <PanelsTopLeftIcon size="30" class="empty-icon" />
      <h2>当前不是协作工作台会话</h2>
      <p>新页面只服务主 Agent 编排工作台。你可以直接新建一个 workspace，或者回到最近的工作台继续处理审批和 plan。</p>
      <div class="empty-actions">
        <button class="ops-button primary" type="button" @click="createWorkspaceSession">新建工作台</button>
        <button v-if="recentWorkspaceSession" class="ops-button ghost" type="button" @click="activateRecentWorkspaceSession">
          切到最近工作台
        </button>
      </div>
    </div>

    <template v-else>
      <div class="protocol-workspace-toolbar">
        <div v-if="actionNotice || store.errorMessage || store.noticeMessage" class="toolbar-notice" :class="toolbarTone">
          <AlertTriangleIcon v-if="store.errorMessage" size="14" />
          <span>{{ store.errorMessage || actionNotice || store.noticeMessage }}</span>
        </div>
        <button class="toolbar-refresh" type="button" :disabled="refreshBusy" @click="refreshProtocolState">
          <RefreshCwIcon size="14" :class="{ spin: refreshBusy }" />
          <span>{{ refreshBusy ? "刷新中..." : "刷新" }}</span>
        </button>
      </div>

      <div class="protocol-workspace-shell">
        <section class="workspace-stage-card">
          <div v-if="store.loading" class="stage-empty">
            <Loader2Icon size="18" class="spin" />
            <span>正在载入工作台...</span>
          </div>

          <ProtocolConversationPane
            v-else
            title="Main Agent"
            :subtitle="conversationSubtitle"
            :messages="conversationItems"
            :plan-cards="planCards"
            :plan-summary-label="planSummaryLabel"
            :background-agents="backgroundAgents"
            :draft="composerDraft"
            :sending="store.sending"
            empty-label="这里会显示主 Agent 的对话、plan 解释和风险提示。"
            @update:draft="composerDraft = $event"
            @send="sendWorkspaceMessage"
            @stop="stopWorkspaceMessage"
            @select-message="handleMessageSelect"
            @plan-action="handlePlanAction"
            @agent-select="handleAgentSelect"
          />
        </section>

        <aside class="workspace-side-rail">
          <ProtocolApprovalRail
            title="待审批决策"
            subtitle="右侧固定审批区，直接快速完成授权、拒绝或同意执行。"
            :queue-items="approvalItems"
            :active-approval-id="selectedApprovalId"
            :busy="decisionBusy"
            empty-label="当前没有待审批操作。"
            @detail="handleApprovalDetail"
            @authorize="handleApprovalAuthorize"
            @reject="handleApprovalReject"
            @accept="handleApprovalAccept"
          />

          <ProtocolEventTimeline
            title="实时事件"
            subtitle="轻量时间线只保留关键变化，帮助你快速判断当前执行推进到哪里。"
            :items="timelineItems"
            :max-items="8"
            @select="handleEventSelect"
          />

          <div class="runtime-pill">
            <span class="runtime-dot"></span>
            <span>{{ runtimeStatus }}</span>
          </div>
        </aside>
      </div>
    </template>

    <ProtocolEvidenceModal
      v-model:open="evidenceOpen"
      v-model:active-tab="evidenceTab"
      :title="evidenceTitle"
      :subtitle="evidenceSubtitle"
      :tabs="evidenceTabs"
      :panels="evidencePanels"
    >
      <template #terminal="{ panel }">
        <section class="terminal-evidence-panel">
          <div class="terminal-summary">
            <h4>{{ panel.title || "Host terminal" }}</h4>
            <p>{{ panel.summary || "当前没有额外说明。" }}</p>
          </div>

          <div v-if="panel.items?.length" class="terminal-meta-grid">
            <article v-for="(item, index) in panel.items" :key="`${item.label || 'meta'}-${index}`" class="terminal-meta-card">
              <span>{{ item.label }}</span>
              <strong>{{ item.value }}</strong>
            </article>
          </div>

          <pre class="terminal-output">{{ stringifyRaw(panel.raw) || "暂无终端输出" }}</pre>
        </section>
      </template>
    </ProtocolEvidenceModal>
  </div>
</template>

<style scoped>
.protocol-workspace-page {
  display: flex;
  flex-direction: column;
  gap: 0;
  min-height: 0;
  height: calc(100vh - 48px);
  overflow: hidden;
}

.protocol-workspace-toolbar {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  align-items: center;
  padding: 6px 16px;
  flex-shrink: 0;
  border-bottom: 1px solid #e8ecf1;
  background: #ffffff;
}

.toolbar-notice {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  height: 32px;
  border-radius: 6px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  color: #334155;
  font-size: 12px;
  font-weight: 500;
}

.toolbar-notice.danger {
  border-color: #fca5a5;
  color: #991b1b;
  background: #fef2f2;
}

.toolbar-refresh {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0 10px;
  height: 32px;
  border-radius: 6px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  color: #475569;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.toolbar-refresh:hover {
  background: #f8fafc;
}

.protocol-workspace-shell {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 0;
  flex: 1;
  overflow: hidden;
}

.workspace-stage-card {
  min-height: 0;
  padding: 0;
  border-radius: 0;
  border: none;
  border-right: 1px solid #e8ecf1;
  background: #ffffff;
  box-shadow: none;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.workspace-side-rail {
  display: flex;
  flex-direction: column;
  gap: 0;
  min-height: 0;
  overflow: hidden;
  border-left: 1px solid #e8ecf1;
  background: #f8fafc;
}

.runtime-pill {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border-top: 1px solid #e8ecf1;
  background: #f8fafc;
  color: #475569;
  font-size: 12px;
  font-weight: 600;
  flex-shrink: 0;
  border-radius: 0;
  box-shadow: none;
}

.runtime-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #22c55e;
}

.protocol-workspace-empty,
.stage-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 40px 24px;
  min-height: 360px;
  border: 1px dashed #e2e8f0;
  background: #ffffff;
  text-align: center;
  border-radius: 0;
}

.protocol-workspace-empty h2 {
  margin: 0;
  color: #0f172a;
}

.protocol-workspace-empty p,
.stage-empty span {
  margin: 0;
  max-width: 680px;
  color: #64748b;
  line-height: 1.7;
}

.empty-icon {
  color: #2563eb;
}

.empty-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
}

.terminal-evidence-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.terminal-summary h4 {
  margin: 0 0 6px;
  color: #0f172a;
  font-size: 16px;
}

.terminal-summary p {
  margin: 0;
  color: #64748b;
  line-height: 1.6;
}

.terminal-meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.terminal-meta-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px;
  border-radius: 18px;
  border: 1px solid rgba(226, 232, 240, 0.95);
  background: rgba(248, 250, 252, 0.92);
}

.terminal-meta-card span {
  color: #64748b;
  font-size: 12px;
  font-weight: 700;
}

.terminal-meta-card strong {
  color: #0f172a;
  font-size: 13px;
  line-height: 1.5;
  word-break: break-word;
}

.terminal-output {
  margin: 0;
  min-height: 300px;
  padding: 16px;
  border-radius: 20px;
  background: #0f172a;
  color: #e2e8f0;
  font-size: 13px;
  line-height: 1.6;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}

.spin {
  animation: protocol-spin 1s linear infinite;
}

@keyframes protocol-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1200px) {
  .protocol-workspace-shell {
    grid-template-columns: minmax(0, 1fr);
  }

  .workspace-stage-card {
    min-height: 0;
  }
}

@media (max-width: 720px) {
  .protocol-workspace-toolbar {
    flex-direction: column;
    align-items: stretch;
  }

  .toolbar-refresh {
    justify-content: center;
  }

  .workspace-stage-card {
    padding: 18px 16px;
    border-radius: 24px;
  }

  .runtime-pill {
    border-radius: 18px;
  }
}
</style>
