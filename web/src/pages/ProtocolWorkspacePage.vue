<script setup>
import { computed, onMounted, ref, watch } from "vue";
import { AlertTriangleIcon, Loader2Icon, PanelsTopLeftIcon, RefreshCwIcon } from "lucide-vue-next";
import ProtocolApprovalRail from "../components/protocol-workspace/ProtocolApprovalRail.vue";
import ProtocolConversationPane from "../components/protocol-workspace/ProtocolConversationPane.vue";
import ProtocolEventTimeline from "../components/protocol-workspace/ProtocolEventTimeline.vue";
import ProtocolEvidenceModal from "../components/protocol-workspace/ProtocolEvidenceModal.vue";
import { buildMcpDecisionNotice, buildSyntheticMcpApproval, buildSyntheticMcpEvent, formatMcpActionLabel, formatMcpActionTarget, isMcpMutationAction } from "../lib/mcpActionRuntime";
import { buildProtocolEvidenceTabs, buildProtocolWorkspaceModel } from "../lib/protocolWorkspaceVm";
import { compactText } from "../lib/workspaceViewModel";
import { useAppStore } from "../store";

const store = useAppStore();
const OPEN_SESSION_HISTORY_EVENT = "codex:open-session-history";
const OPEN_MCP_DRAWER_EVENT = "codex:open-mcp-drawer";
const MCP_SURFACE_TAB = "mcp-surface";

const refreshBusy = ref(false);
const decisionBusy = ref(false);
const stopBusy = ref(false);
const composerDraft = ref("");
const actionNotice = ref("");
const actionTone = ref("info");
const evidenceOpen = ref(false);
const evidenceTab = ref("main-agent-plan");
const selectedHostId = ref("");
const selectedStepId = ref("");
const selectedApprovalId = ref("");
const selectedMessageId = ref("");
const selectedMcpSurface = ref(null);
const evidenceSource = ref("mission");
const workspaceBootstrapBusy = ref(false);
const workspaceBootstrapAttempted = ref(false);
const localMcpApprovals = ref([]);
const localMcpEvents = ref([]);

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
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

function pushMcpEvent(action, options = {}) {
  localMcpEvents.value = [
    ...localMcpEvents.value,
    buildSyntheticMcpEvent(action, options),
  ];
}

function buildMcpDrawerSurface(surface) {
  const normalized = normalizeMcpSurface(surface);
  const base = {
    kind: normalized.kind,
    source: normalized.source,
  };
  if (normalized.kind === "bundle") {
    return {
      ...base,
      bundle: normalized.bundle || normalized,
    };
  }
  return {
    ...base,
    card: normalized.card || normalized,
  };
}

function dispatchMcpDrawer(surface, pin = false) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent(OPEN_MCP_DRAWER_EVENT, {
      detail: {
        source: "protocol-mcp-surface",
        pin: Boolean(pin),
        surface: buildMcpDrawerSurface(surface),
      },
    }),
  );
}

function normalizeMcpSurface(surface) {
  const source = asObject(surface);
  const bundle = asObject(source.bundle || source);
  const card = asObject(source.card || source);
  const kind = compactText(source.kind || source.surfaceKind || (bundle.bundleKind ? "bundle" : card.uiKind ? "card" : "card")).toLowerCase();
  return {
    ...source,
    kind: kind === "bundle" ? "bundle" : "card",
    bundle: Object.keys(bundle).length ? bundle : null,
    card: Object.keys(card).length ? card : null,
    source: compactText(source.source || bundle.source || card.source || "protocol-workspace"),
  };
}

function mcpSurfaceTitle(surface) {
  const normalized = normalizeMcpSurface(surface);
  if (normalized.kind === "bundle") {
    const bundle = normalized.bundle || {};
    const subject = bundle.subject || {};
    return [subject.type || "service", subject.name || subject.service || bundle.title || "MCP 聚合面板", subject.env || ""]
      .filter(Boolean)
      .join(" / ");
  }
  const card = normalized.card || {};
  const action = card.action || card.actions?.[0] || {};
  return formatMcpActionLabel(action) || card.title || "MCP 操作面板";
}

function mcpSurfaceSummary(surface) {
  const normalized = normalizeMcpSurface(surface);
  if (normalized.kind === "bundle") {
    const bundle = normalized.bundle || {};
    return compactText(bundle.summary || bundle.rootCause || bundle.root_cause || "MCP 聚合面板详情");
  }
  const card = normalized.card || {};
  const action = card.action || card.actions?.[0] || {};
  return compactText(card.summary || action.confirmText || action.description || "MCP 操作详情");
}

function mcpSurfaceItems(surface) {
  const normalized = normalizeMcpSurface(surface);
  if (normalized.kind === "bundle") {
    const bundle = normalized.bundle || {};
    const sections = asArray(bundle.sections);
    const subject = bundle.subject || {};
    const headRows = [
      { label: "类型", value: bundle.bundleKind || "mcp_bundle" },
      { label: "主题", value: [subject.type || "", subject.name || subject.service || ""].filter(Boolean).join(" / ") || "未指定" },
      { label: "范围", value: [bundle.scope?.service, bundle.scope?.hostId, bundle.scope?.env, bundle.scope?.cluster].filter(Boolean).join(" / ") || "未指定" },
      { label: "时效", value: bundle.freshness?.label || bundle.freshness?.capturedAt || "未声明" },
      { label: "分区", value: sections.length ? `${sections.length} 个分区` : "无分区" },
    ];
    const sectionRows = sections.map((section, index) => ({
      label: `${section?.title || section?.kind || `Section ${index + 1}`}`,
      value: [section?.summary || "", `${asArray(section?.cards).length || 0} 个卡片`].filter(Boolean).join(" · ") || "已聚合",
    }));
    return [...headRows, ...sectionRows];
  }

  const card = normalized.card || {};
  const action = card.action || card.actions?.[0] || {};
  const scope = asObject(card.scope || action.scope);
  return [
    { label: "类型", value: card.uiKind || "action_panel" },
    { label: "操作", value: formatMcpActionLabel(action) || card.title || "未命名操作" },
    { label: "目标", value: formatMcpActionTarget(action, scope) },
    { label: "权限路径", value: action.permissionPath || action.permission_path || "未声明" },
    { label: "作用范围", value: [scope.service, scope.hostId, scope.env, scope.cluster].filter(Boolean).join(" / ") || "未指定" },
    { label: "时效", value: card.freshness?.label || card.freshness?.capturedAt || "未声明" },
  ];
}

function openMcpSurfaceEvidence(surface) {
  const normalized = normalizeMcpSurface(surface);
  selectedMcpSurface.value = normalized;
  const hostId = normalized.kind === "bundle"
    ? compactText(normalized.bundle?.scope?.hostId || normalized.bundle?.hostId || "")
    : compactText(normalized.card?.scope?.hostId || normalized.card?.hostId || "");
  if (hostId) {
    selectedHostId.value = hostId;
  }
  evidenceSource.value = "mcp-surface";
  evidenceTab.value = MCP_SURFACE_TAB;
  evidenceOpen.value = true;
}

function handleMcpSurfaceDetail(surface) {
  openMcpSurfaceEvidence(surface);
  pushActionNotice(`${mcpSurfaceTitle(surface)} 已打开详情。`, "info");
}

function handleMcpSurfacePin(surface) {
  dispatchMcpDrawer(surface, true);
  pushActionNotice(`${mcpSurfaceTitle(surface)} 已固定到全局 MCP 抽屉。`, "info");
}

async function handleMcpSurfaceRefresh(surface) {
  pushActionNotice(`${mcpSurfaceTitle(surface)} 已请求刷新。`, "info");
  await refreshProtocolState();
}

function queueLocalMcpApproval(action) {
  const approval = buildSyntheticMcpApproval(action, {
    scope: action?.scope || {},
    summary: action?.confirmText || "等待你确认后继续执行该 MCP 变更操作。",
  });
  localMcpApprovals.value = [
    ...localMcpApprovals.value.filter((item) => item.id !== approval.id),
    approval,
  ];
  selectedApprovalId.value = approval.id;
  pushMcpEvent(action, {
    approvalId: approval.approvalId,
    hostId: approval.hostId,
    text: `${formatMcpActionLabel(action)} 已进入审批队列`,
    tone: "warning",
  });
  pushActionNotice(`${formatMcpActionLabel(action)} 已进入右侧审批栏。`, "warning");
}

function settleLocalMcpApproval(approval, decision) {
  localMcpApprovals.value = localMcpApprovals.value.filter((item) => item.id !== approval.id && item.approvalId !== approval.approvalId);
  const message = buildMcpDecisionNotice(approval.action || {}, decision);
  pushMcpEvent(approval.action || {}, {
    approvalId: approval.approvalId,
    hostId: approval.hostId,
    text: message,
    tone: decision === "decline" || decision === "reject" ? "warning" : "success",
  });
  pushActionNotice(message, decision === "decline" || decision === "reject" ? "warning" : "success");
  if (selectedApprovalId.value === approval.id) {
    selectedApprovalId.value = localMcpApprovals.value[0]?.id || "";
  }
}

function handleMcpAction(action) {
  if (!action || typeof action !== "object") return;
  if (isMcpMutationAction(action)) {
    queueLocalMcpApproval(action);
    return;
  }
  const message = `${formatMcpActionLabel(action)} 已作为只读操作加入当前工作台。`;
  pushMcpEvent(action, {
    text: message,
    tone: "info",
  });
  pushActionNotice(message, "info");
}

function openConversationHistory() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent(OPEN_SESSION_HISTORY_EVENT, {
      detail: { source: "protocol-history-sentinel" },
    }),
  );
}

const isWorkspaceSession = computed(() => store.snapshot.kind === "workspace");
const recentWorkspaceSession = computed(
  () => store.sessionList.find((session) => session.kind === "workspace" && session.id !== store.activeSessionId) || null,
);
const workspaceModel = computed(() => buildProtocolWorkspaceModel(store.snapshot, store.runtime));
const hostRows = computed(() => workspaceModel.value.hostRows || []);
const planCardModel = computed(() => workspaceModel.value.planCardModel || { stepItems: [] });
const choiceCards = computed(() => workspaceModel.value.choiceCards || []);
const workspaceApprovalItems = computed(() => workspaceModel.value.approvalItems || []);
const workspaceEventItems = computed(() => workspaceModel.value.eventItems || []);
const approvalItems = computed(() => [...workspaceApprovalItems.value, ...localMcpApprovals.value]);
const eventItems = computed(() => [...workspaceEventItems.value, ...localMcpEvents.value]);
const timelineItems = computed(() => [...eventItems.value].reverse());
const backgroundAgents = computed(() => workspaceModel.value.backgroundAgents || []);
const canRestartMission = computed(() => workspaceModel.value.nextSendStartsNewMission);
const statusBanner = computed(() => {
  const banner = workspaceModel.value.statusBanner;
  if (!banner || workspaceModel.value.canStopCurrentMission) return null;
  return {
    tone: banner.tone,
    title: banner.title,
    text: banner.detail,
    hint: canRestartMission.value ? "继续发送会在当前 workspace 会话里启动一轮新的 mission。" : "",
  };
});

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

const starterCard = computed(() => {
  const connectedHost = compactText(store.snapshot.selectedHostId || selectedHostRow.value?.displayName || "server-local");
  const pendingApprovals = approvalItems.value.length;
  return {
    eyebrow: "SYSTEM CONTEXT",
    title: `${connectedHost} 已连接，工作台已就绪。`,
    text: "可以直接问我当前状态，或者描述你想处理的问题。",
    meta: pendingApprovals
      ? `当前有 ${pendingApprovals} 条待审批操作，处理后我会继续推进。`
      : "当前没有待审批操作。",
  };
});

const composerPrimaryActionOverride = computed(() => (workspaceModel.value.canStopCurrentMission ? "" : "send"));

const composerPlaceholder = computed(() =>
  workspaceModel.value.nextSendStartsNewMission
    ? "上一轮任务已结束，继续输入会在当前工作台启动新 mission"
    : "继续输入需求、约束或补充说明",
);

const planSummaryLabel = computed(() => {
  const total = Number(planCardModel.value.totalSteps || 0);
  const completed = Number(planCardModel.value.completedSteps || 0);
  if (!total) return "计划生成后，会在这里直接展示 step -> host-agent 映射。";
  return `共 ${total} 个任务，已完成 ${completed} 个`;
});

const planCards = computed(() => {
  const stepCards = asArray(planCardModel.value.stepItems).map((item) => ({
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
  }));
  if (stepCards.length) return stepCards;

  const hasPlanProjection = Boolean(
    workspaceModel.value.cards?.planCard ||
      compactText(planCardModel.value.summary) ||
      compactText(planCardModel.value.generatedAt),
  );
  if (!hasPlanProjection) return [];

  return [
    {
      id: "plan-projection",
      step: {
        id: "plan-projection",
        title: compactText(planCardModel.value.title || "主 Agent 已生成计划摘要"),
        description: compactText(planCardModel.value.summary || conversationSubtitle.value),
      },
      status: workspaceModel.value.missionPhase || "planning",
      statusLabel: normalizePhaseLabel(workspaceModel.value.missionPhase),
      hostAgent: [],
      detail: compactText(planCardModel.value.summary || "已收到计划投影，正在同步具体步骤。"),
      note: "当前还在整理 step -> host-agent 映射，稍后会直接显示在这里。",
      tags: [{ id: "plan-projection-tag", label: "已收到计划投影" }],
      actions: [{ id: "plan-projection-evidence", key: "evidence", label: "查看证据" }],
      index: 1,
    },
  ];
});

const conversationStatusCard = computed(() => workspaceModel.value.conversationStatusCard || null);
const formattedTurns = computed(() => workspaceModel.value.formattedTurns || []);

const filteredEventItems = computed(() => {
  const selectedHost = selectedHostRow.value?.hostId;
  if (!selectedHost) return eventItems.value;
  return eventItems.value.filter((item) => !item.hostId || item.hostId === selectedHost || item.targetType === "dispatch");
});

const evidenceBase = computed(() =>
  buildProtocolEvidenceTabs({
    planCardModel: planCardModel.value,
    hostRow: selectedHostRow.value,
    approvalItem: selectedApprovalItem.value,
    eventItems: filteredEventItems.value,
  }),
);

const mainAgentPlanPanel = computed(() => {
  const items = [];
  const hasPlanSummary = Boolean(
    compactText(planCardModel.value.summary) || asArray(planCardModel.value.stepItems).length || compactText(planCardModel.value.generatedAt),
  );
  if (hasPlanSummary) {
    items.push({
      label: "计划摘要",
      value: compactText(planCardModel.value.summary || "当前还没有可用的计划摘要。"),
    });
  }
  for (const [index, step] of asArray(planCardModel.value.stepItems).entries()) {
    const hostNames = asArray(step.hosts).map((host) => compactText(host.label || host.id)).filter(Boolean).join("、");
    items.push({
      label: `Step ${step.index || index + 1}`,
      value: [step.title, step.statusLabel || step.status, hostNames ? `Host: ${hostNames}` : ""]
        .map((part) => compactText(part))
        .filter(Boolean)
        .join(" · "),
    });
  }
  return {
    title: "主 Agent 计划摘要",
    summary: compactText(planCardModel.value.summary || "查看主 Agent 如何生成并拆分 plan。"),
    items: items.length ? items : [{ label: "状态", value: "当前还没有可用的计划摘要。" }],
    raw: {
      version: planCardModel.value.version,
      generatedAt: planCardModel.value.generatedAt,
    },
  };
});

const workerConversationPanel = computed(() => {
  const transcript = evidenceBase.value.workerConversation.length
    ? evidenceBase.value.workerConversation.map((item) => ({
        label: item.time || item.title || "Worker",
        value: item.text,
      }))
    : asArray(selectedHostRow.value?.worker?.transcript).map((item, index) => ({
        label: `Transcript ${index + 1}`,
        value: String(item ?? ""),
      }));

  return {
    title: `${selectedHostRow.value?.displayName || "Worker"} 对话`,
    summary: selectedHostRow.value?.taskTitle || "当前 host-agent 与 AI 的对话摘录。",
    items: transcript.length ? transcript : [{ label: "状态", value: "当前 host-agent 还没有可展示的对话摘录。" }],
    raw: selectedHostRow.value?.worker || null,
  };
});

const hostTerminalPanel = computed(() => ({
  title: `${selectedHostRow.value?.displayName || "Host"} terminal`,
  summary: selectedHostRow.value?.summary || "查看当前 host-agent 对应主机的终端输出。",
  items: asArray(evidenceBase.value.hostTerminalRows).map((row) => ({
    label: row.label || row.key,
    value: row.value || row.text,
  })),
  raw: evidenceBase.value.hostTerminalOutput || selectedHostRow.value?.worker?.terminal || "",
}));

const approvalContextPanel = computed(() => {
  const rows = [];
  if (selectedApprovalItem.value) {
    rows.push(
      { label: "主机", value: selectedApprovalItem.value.hostName || selectedApprovalItem.value.hostId || "未指定" },
      { label: "审批ID", value: selectedApprovalItem.value.approvalId || selectedApprovalItem.value.id || "未提供" },
      { label: "命令", value: selectedApprovalItem.value.command || selectedApprovalItem.value.summary || "未提供命令" },
    );
    rows.push(
      ...asArray(selectedApprovalItem.value.detailRows).map((item) => ({
        label: compactText(item.label || "详情"),
        value: compactText(item.value || item.text),
      })),
    );
  } else if (evidenceBase.value.approvalContext.length) {
    rows.push(
      ...evidenceBase.value.approvalContext.map((item) => ({
        label: item.label || item.title || "审批上下文",
        value: item.value || item.text,
      })),
    );
  }

  return {
    title: "审批上下文",
    summary: selectedApprovalItem.value
      ? "通过弹框查看当前审批所关联的命令、主机和证据。"
      : "当前没有待处理的审批上下文。",
    items: rows.length
      ? rows
      : [
          {
            label: "状态",
            value: "当前没有待处理的审批上下文。",
          },
        ],
    raw: selectedApprovalItem.value?.raw || null,
  };
});

const mcpSurfacePanel = computed(() => {
  if (!selectedMcpSurface.value) {
    return {
      title: "MCP 面板",
      summary: "当前没有选中的 MCP 面板。",
      items: [{ label: "状态", value: "尚未选择任何 MCP surface。" }],
      raw: "",
      sections: [],
    };
  }

  const surface = selectedMcpSurface.value;
  return {
    title: mcpSurfaceTitle(surface),
    summary: mcpSurfaceSummary(surface),
    items: mcpSurfaceItems(surface),
    raw: surface,
    sections: surface.kind === "bundle"
      ? asArray(surface.bundle?.sections).map((section) => ({
          ...section,
          cards: asArray(section?.cards),
        }))
      : [],
  };
});

const evidencePanels = computed(() => ({
  "main-agent-plan": mainAgentPlanPanel.value,
  "worker-conversation": workerConversationPanel.value,
  "host-terminal": hostTerminalPanel.value,
  [MCP_SURFACE_TAB]: mcpSurfacePanel.value,
  "approval-context": approvalContextPanel.value,
}));

const evidenceTabs = computed(() => [
  { value: "main-agent-plan", label: "主 Agent 计划摘要", badge: mainAgentPlanPanel.value.items?.length || 0 },
  { value: "worker-conversation", label: "Worker 对话", badge: workerConversationPanel.value.items?.length || 0 },
  { value: "host-terminal", label: "Host Terminal", badge: hostTerminalPanel.value.items?.length || 0 },
  { value: MCP_SURFACE_TAB, label: "MCP 面板", badge: mcpSurfacePanel.value.items?.length || 0 },
  { value: "approval-context", label: "审批上下文", badge: approvalContextPanel.value.items?.length || 0 },
]);

const evidenceTitle = computed(() => {
  if (evidenceSource.value === "approval" && selectedApprovalItem.value) {
    return `审批上下文 · ${selectedApprovalItem.value.hostName || selectedApprovalItem.value.hostId || "Host"}`;
  }
  if (evidenceSource.value === "step" && selectedStep.value) {
    return `主 Agent 计划摘要 · ${selectedStep.value.title}`;
  }
  if (evidenceSource.value === "host" && selectedHostRow.value) {
    const hostLabel = selectedHostRow.value.displayName || selectedHostRow.value.hostId || "Host";
    if (evidenceTab.value === "host-terminal") {
      return `命令执行详情 · ${hostLabel}`;
    }
    return `执行详情 · ${hostLabel}`;
  }
  if (evidenceSource.value === "mcp-surface" && selectedMcpSurface.value) {
    return `MCP 面板 · ${mcpSurfaceTitle(selectedMcpSurface.value)}`;
  }
  if (evidenceSource.value === "message" || evidenceSource.value === "dispatch" || evidenceSource.value === "event") {
    return "主 Agent 计划摘要";
  }
  return "执行证据";
});

const evidenceSubtitle = computed(() => {
  if (evidenceSource.value === "approval") {
    return "审批详情通过弹框查看，不占用主页面空间。";
  }
  if (evidenceSource.value === "host") {
    return "这里汇总当前 worker 对话和 Host Terminal 上下文。";
  }
  if (evidenceSource.value === "mcp-surface") {
    return "这里展示当前 MCP 面板的完整详情，不会把长图表和长表格重新灌回正文。";
  }
  if (evidenceSource.value === "step" || evidenceSource.value === "message" || evidenceSource.value === "dispatch" || evidenceSource.value === "event") {
    return "这里汇总主 Agent 计划摘要、Worker 对话、Host Terminal 与审批上下文。";
  }
  return "按 tab 切换主 Agent 计划摘要、Worker 对话、Host Terminal 与审批上下文。";
});

const runtimeStatus = computed(() => {
  if (workspaceModel.value.statusBanner?.runtimeText) {
    return workspaceModel.value.statusBanner.runtimeText;
  }
  const phase = normalizePhaseLabel(workspaceModel.value.missionPhase);
  const total = Number(planCardModel.value.totalSteps || 0);
  const completed = Number(planCardModel.value.completedSteps || 0);
  if (workspaceModel.value.missionPhase === "aborted") {
    return `已停止 | ${canRestartMission.value ? "可直接发送启动新一轮 mission" : "当前任务已结束"}`;
  }
  if (workspaceModel.value.missionPhase === "failed") {
    return `失败 | ${compactText(workspaceModel.value.currentFailureCard?.title || "查看最近一次失败原因")}`;
  }
  if (workspaceModel.value.missionPhase === "completed") {
    return "已完成 | 可继续补充下一轮需求";
  }
  if (!total) {
    if (workspaceModel.value.missionPhase === "thinking") {
      return "思考中";
    }
    if (workspaceModel.value.cards?.planCard || compactText(planCardModel.value.generatedAt || planCardModel.value.summary)) {
      return `${phase} | 已收到计划投影，等待步骤同步`;
    }
    return `${phase} | 等待主 Agent 生成计划`;
  }
  return `${phase} | 共 ${total} 个任务，已完成 ${completed} 个`;
});

const toolbarTone = computed(() => {
  if (store.errorMessage) return "danger";
  if (!actionNotice.value && workspaceModel.value.statusBanner?.tone) return workspaceModel.value.statusBanner.tone;
  if (actionTone.value) return actionTone.value;
  return "info";
});

const toolbarMessage = computed(() => {
  const raw = store.errorMessage || actionNotice.value || workspaceModel.value.statusBanner?.detail || store.noticeMessage || "";
  // Replace raw "approval not found" with a user-friendly message
  if (/approval.*not\s*found|not\s*found.*approval/i.test(raw)) {
    return "该审批已过期或已被处理，请刷新页面查看最新状态。";
  }
  return raw;
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

watch(
  () => ({
    isWorkspace: isWorkspaceSession.value,
    turnActive: store.runtime.turn.active,
    phase: workspaceModel.value.missionPhase,
    stepCount: Number(planCardModel.value.totalSteps || 0),
  }),
  (state, _previous, onCleanup) => {
    if (
      !state.isWorkspace ||
      !state.turnActive ||
      !["planning", "thinking", "executing", "waiting_approval", "waiting_input"].includes(state.phase)
    ) {
      return;
    }

    const timer = window.setInterval(() => {
      if (refreshBusy.value || decisionBusy.value) return;
      void store.fetchState();
    }, state.stepCount > 0 ? 5000 : 3000);

    onCleanup(() => {
      window.clearInterval(timer);
    });
  },
  { immediate: true },
);

watch(
  () => ({
    kind: store.snapshot.kind,
    loading: store.loading,
    turnActive: store.runtime.turn.active,
  }),
  (state) => {
    if (state.kind === "workspace" || state.loading || state.turnActive || workspaceBootstrapAttempted.value) {
      return;
    }
    void bootstrapWorkspaceSession();
  },
  { immediate: false },
);

onMounted(() => {
  if (!isWorkspaceSession.value && !store.loading && !store.runtime.turn.active && !workspaceBootstrapAttempted.value) {
    void bootstrapWorkspaceSession();
  }
});

function openEvidence({ source = "mission", hostId = "", stepId = "", approvalId = "", tab = "main-agent-plan" } = {}) {
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
  if (workspaceBootstrapBusy.value) return false;
  workspaceBootstrapBusy.value = true;
  try {
    const ok = await store.createSession("workspace");
    if (ok) {
      pushActionNotice("已创建新的协作工作台。", "info");
      return true;
    }
    pushActionNotice(store.errorMessage || "创建协作工作台失败。", "danger");
    return false;
  } finally {
    workspaceBootstrapBusy.value = false;
    workspaceBootstrapAttempted.value = true;
  }
}

async function activateRecentWorkspaceSession() {
  if (!recentWorkspaceSession.value?.id || workspaceBootstrapBusy.value) return false;
  workspaceBootstrapBusy.value = true;
  try {
    const ok = await store.activateSession(recentWorkspaceSession.value.id);
    if (ok) {
      pushActionNotice("已切换到最近的工作台。", "info");
      return true;
    }
    pushActionNotice(store.errorMessage || "切换最近工作台失败。", "danger");
    return false;
  } finally {
    workspaceBootstrapBusy.value = false;
    workspaceBootstrapAttempted.value = true;
  }
}

async function bootstrapWorkspaceSession() {
  if (isWorkspaceSession.value || workspaceBootstrapBusy.value || store.runtime.turn.active) return false;
  workspaceBootstrapBusy.value = true;
  try {
    await store.fetchSessions();
    const recent = store.sessionList.find((session) => session.kind === "workspace" && session.id !== store.activeSessionId) || null;
    if (recent?.id) {
      const ok = await store.activateSession(recent.id);
      if (ok) {
        pushActionNotice("已切换到最近的协作工作台。", "info");
        return true;
      }
    }
    const ok = await store.createSession("workspace");
    if (ok) {
      pushActionNotice("已自动创建新的协作工作台。", "info");
      return true;
    }
    pushActionNotice(store.errorMessage || "进入协作工作台失败。", "danger");
    return false;
  } finally {
    workspaceBootstrapBusy.value = false;
    workspaceBootstrapAttempted.value = true;
  }
}

async function sendWorkspaceMessage(payload = composerDraft.value) {
  if (!canSendWorkspaceMessage.value || !compactText(payload)) return;
  const restartingMission = canRestartMission.value;
  store.sending = true;
  store.errorMessage = "";
  actionNotice.value = "";
  if (restartingMission) {
    selectedApprovalId.value = "";
    selectedStepId.value = "";
    selectedHostId.value = "";
    evidenceOpen.value = false;
    pushActionNotice("上一轮 mission 已结束，本次发送会在当前工作台启动新的 mission。", "info");
  }
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
    pushActionNotice(restartingMission ? "已在当前会话启动新一轮 mission。" : "消息已发送给主 Agent。", "info");
    await Promise.all([store.fetchState(), store.fetchSessions()]);
  } catch (_error) {
    store.errorMessage = "Network error";
    store.setTurnPhase("failed");
  } finally {
    store.sending = false;
  }
}

async function stopWorkspaceMessage() {
  if (!store.runtime.turn.active || decisionBusy.value || stopBusy.value) return;
  stopBusy.value = true;
  pushActionNotice("正在中断当前任务...", "info");
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
  } finally {
    stopBusy.value = false;
  }
}

async function handleChoice({ requestId, answers }) {
  if (!requestId || !Array.isArray(answers) || !answers.length) return;
  try {
    store.errorMessage = "";
    store.setTurnPhase("thinking");
    const response = await fetch(`/api/v1/choices/${requestId}/answer`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ answers }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      store.errorMessage = data.error || "choice submit failed";
      store.setTurnPhase("failed");
      return;
    }
    pushActionNotice("已提交补充输入，主 Agent 会基于你的选择继续推进。", "info");
    await Promise.all([store.fetchState(), store.fetchSessions()]);
  } catch (_error) {
    store.errorMessage = "choice submit failed";
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
  if (approval?.mcpSynthetic) {
    settleLocalMcpApproval(approval, decision);
    return;
  }
  const approvalId = compactText(approval?.approvalId || approval?.requestId || approval?.raw?.approval?.requestId);
  if (!approvalId || decisionBusy.value) return;

  const cardId = compactText(approval?.id);
  selectedApprovalId.value = cardId;
  decisionBusy.value = true;
  try {
    store.errorMessage = "";
    await postApprovalDecision(approvalId, decision);
    pushActionNotice(decision === "decline" ? "已提交拒绝，等待主 Agent 调整方案。" : "审批结果已提交。", decision === "decline" ? "warning" : "info");
    await Promise.all([store.fetchState(), store.fetchSessions()]);
  } catch (error) {
    const msg = error?.message || "approval failed";
    // Friendly message for stale / expired approvals
    if (/not found|expired|过期|不存在/i.test(msg)) {
      // Locally dismiss the stale approval card so it disappears from the UI
      dismissStaleApprovalCard(cardId, approvalId);
      pushActionNotice("该审批已过期或已被处理，已自动清除。", "warning");
      store.errorMessage = "";
      selectedApprovalId.value = "";
    } else {
      store.errorMessage = msg;
      store.setTurnPhase("failed");
    }
  } finally {
    decisionBusy.value = false;
  }
}

/** Locally mark a stale approval card as dismissed so it's filtered out of the pending list */
function dismissStaleApprovalCard(cardId, approvalId) {
  const cards = store.snapshot.cards;
  if (!Array.isArray(cards)) return;
  for (const card of cards) {
    if (!card) continue;
    const isMatch =
      card.id === cardId ||
      card.approval?.requestId === approvalId ||
      card.approvalId === approvalId ||
      card.requestId === approvalId;
    if (isMatch && card.status === "pending") {
      card.status = "dismissed";
    }
  }
  // Also remove from snapshot.approvals
  if (Array.isArray(store.snapshot.approvals)) {
    store.snapshot.approvals = store.snapshot.approvals.filter(
      (a) => a.id !== approvalId && a.requestId !== approvalId,
    );
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
      tab: "worker-conversation",
    });
    return;
  }
  openEvidence({
    source: "step",
    stepId: compactText(plan.id || plan.step?.id || plan.stepId),
    hostId,
    tab: "main-agent-plan",
  });
}

function handleAgentSelect(agent) {
  openEvidence({
    source: "host",
    hostId: compactText(agent?.hostId || agent?.id),
    tab: "worker-conversation",
  });
}

function handleMessageSelect(message) {
  // Don't open evidence modal when clicking messages — it's confusing
  selectedMessageId.value = compactText(message?.id);
}

function handleProcessItemSelect(payload) {
  const item = payload?.item || {};
  const hostId = compactText(item.hostId);
  if (item.kind === "assistant_message") {
    openEvidence({ source: "message", tab: "main-agent-plan" });
    return;
  }
  if (hostId) {
    const approval = approvalItems.value.find((entry) => compactText(entry.hostId) === hostId) || null;
    const looksLikeApprovalBlock = /审批|批准|授权|approval/i.test(`${item.text || ""} ${item.detail || ""} ${item.status || ""}`);
    if (approval && looksLikeApprovalBlock) {
      openEvidence({
        source: "approval",
        approvalId: compactText(approval.id),
        hostId,
        tab: "approval-context",
      });
      return;
    }
    openEvidence({
      source: "host",
      hostId,
      tab: "worker-conversation",
    });
    return;
  }
  openEvidence({ source: "message", tab: "main-agent-plan" });
}

function handleEventSelect(item) {
  const targetType = compactText(item?.targetType).toLowerCase();
  if (targetType === "mcp_approval") {
    selectedApprovalId.value = compactText(item?.targetId);
    pushActionNotice("已定位到 MCP 审批上下文。", "info");
    return;
  }
  if (targetType === "mcp_action") {
    pushActionNotice(compactText(item?.text || "已定位到 MCP 动作事件。"), "info");
    return;
  }
  if (targetType === "approval") {
    openEvidence({ source: "approval", approvalId: compactText(item?.targetId), hostId: compactText(item?.hostId), tab: "approval-context" });
    return;
  }
  if (targetType === "host") {
    // Show terminal output (commands & errors) instead of worker conversation
    openEvidence({ source: "host", hostId: compactText(item?.hostId || item?.targetId), tab: "host-terminal" });
    return;
  }
  if (targetType === "dispatch") {
    openEvidence({ source: "dispatch", hostId: compactText(item?.hostId), tab: "main-agent-plan" });
    return;
  }
  // Default: show terminal for any event with a hostId, otherwise show plan
  if (item?.hostId) {
    openEvidence({ source: "host", hostId: compactText(item.hostId), tab: "host-terminal" });
    return;
  }
  openEvidence({ source: "event", tab: "main-agent-plan" });
}

function handleApprovalDetail(approval) {
  if (approval?.mcpSynthetic) {
    selectedApprovalId.value = compactText(approval?.id);
    pushActionNotice(`${approval.title || "MCP 操作"} 已定位到右侧审批栏。`, "info");
    return;
  }
  selectedApprovalId.value = compactText(approval?.id);
  openEvidence({
    source: "approval",
    approvalId: compactText(approval?.id),
    hostId: compactText(approval?.hostId),
    tab: "approval-context",
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

function handleMcpSurfaceEventDetail(surface) {
  handleMcpSurfaceDetail(surface);
}

function handleMcpSurfaceEventPin(surface) {
  handleMcpSurfacePin(surface);
}

function handleMcpSurfaceEventRefresh(surface) {
  void handleMcpSurfaceRefresh(surface);
}
</script>

<template>
  <div class="protocol-workspace-page" data-testid="protocol-workspace-page">
    <div v-if="!isWorkspaceSession" class="protocol-workspace-empty">
      <PanelsTopLeftIcon size="30" class="empty-icon" />
      <h2>当前不是协作工作台会话</h2>
      <p>新页面只服务主 Agent 编排工作台。你可以直接新建一个 workspace，或者回到最近的工作台继续处理审批和 plan。</p>
      <p v-if="workspaceBootstrapBusy" class="empty-hint">正在进入协作工作台...</p>
      <p v-else-if="store.errorMessage || actionNotice" class="empty-hint" :class="toolbarTone">
        {{ store.errorMessage || actionNotice }}
      </p>
      <div class="empty-actions">
        <button class="ops-button primary" type="button" :disabled="workspaceBootstrapBusy" @click="createWorkspaceSession">
          {{ workspaceBootstrapBusy ? "处理中..." : "新建工作台" }}
        </button>
        <button
          v-if="recentWorkspaceSession"
          class="ops-button ghost"
          type="button"
          :disabled="workspaceBootstrapBusy"
          @click="activateRecentWorkspaceSession"
        >
          切到最近工作台
        </button>
      </div>
    </div>

    <template v-else>
      <div class="protocol-workspace-toolbar">
        <div v-if="toolbarMessage" class="toolbar-notice" :class="toolbarTone">
          <AlertTriangleIcon v-if="store.errorMessage || toolbarTone === 'danger'" size="14" />
          <span>{{ toolbarMessage }}</span>
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

          <article v-else-if="statusBanner" class="workspace-status-banner" :class="statusBanner.tone">
            <div class="workspace-status-banner-head">
              <strong>{{ statusBanner.title }}</strong>
            </div>
            <p>{{ statusBanner.text }}</p>
            <span v-if="statusBanner.hint" class="workspace-status-banner-hint">{{ statusBanner.hint }}</span>
          </article>

          <ProtocolConversationPane
            v-if="!store.loading"
            title="Main Agent"
            :subtitle="conversationSubtitle"
            :formatted-turns="formattedTurns"
            :history-reset-key="store.activeSessionId || store.snapshot.sessionId || ''"
            :status-card="conversationStatusCard"
            :plan-cards="planCards"
            :plan-summary-label="planSummaryLabel"
            :background-agents="backgroundAgents"
            :choice-cards="choiceCards"
            :starter-card="starterCard"
            :draft="composerDraft"
            :draft-placeholder="composerPlaceholder"
            :sending="store.sending"
            :busy="stopBusy"
            :primary-action-override="composerPrimaryActionOverride"
            empty-label="工作台已连接，可以直接开始对话。"
            @update:draft="composerDraft = $event"
            @send="sendWorkspaceMessage"
            @stop="stopWorkspaceMessage"
            @choice="handleChoice"
            @select-message="handleMessageSelect"
            @process-item-select="handleProcessItemSelect"
            @plan-action="handlePlanAction"
            @agent-select="handleAgentSelect"
            @action="handleMcpAction"
            @detail="handleMcpSurfaceEventDetail"
            @pin="handleMcpSurfaceEventPin"
            @refresh="handleMcpSurfaceEventRefresh"
            @open-history="openConversationHistory"
          />
        </section>

        <aside class="workspace-side-rail">
          <section class="workspace-side-panel approval-panel" data-testid="protocol-side-panel-approval">
            <ProtocolApprovalRail
              title="待审批决策"
              subtitle="右侧固定审批区，直接快速完成授权、拒绝或同意执行。"
              :queue-items="approvalItems"
              :active-approval-id="selectedApprovalId"
              :busy="decisionBusy"
              empty-label="当前没有待处理的审批。"
              @detail="handleApprovalDetail"
              @authorize="handleApprovalAuthorize"
              @reject="handleApprovalReject"
              @accept="handleApprovalAccept"
            />
          </section>

          <section class="workspace-side-panel timeline-panel" data-testid="protocol-side-panel-timeline">
            <ProtocolEventTimeline
              title="实时事件"
              subtitle="轻量时间线只保留关键变化，帮助你快速判断当前执行推进到哪里。"
              :items="timelineItems"
              empty-label="当前还没有可展示的实时事件。"
              :max-items="8"
              @select="handleEventSelect"
            />
          </section>

          <div class="runtime-pill" data-testid="protocol-runtime-pill">
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
      <template #mcp-surface="{ panel }">
        <section class="mcp-evidence-panel">
          <div class="mcp-evidence-hero">
            <div>
              <h4>{{ panel.title || "MCP 面板" }}</h4>
              <p>{{ panel.summary || "当前没有额外说明。" }}</p>
            </div>
            <button
              type="button"
              class="mcp-evidence-pin"
              @click="dispatchMcpDrawer(panel.raw || selectedMcpSurface, true)"
            >
              固定到 MCP 抽屉
            </button>
          </div>

          <div v-if="panel.items?.length" class="mcp-evidence-grid">
            <article v-for="(item, index) in panel.items" :key="`${item.label || 'mcp'}-${index}`" class="mcp-evidence-card">
              <span>{{ item.label }}</span>
              <strong>{{ item.value }}</strong>
            </article>
          </div>

          <div v-if="panel.sections?.length" class="mcp-evidence-sections">
            <article v-for="section in panel.sections" :key="section.id || section.kind" class="mcp-evidence-section">
              <header>
                <strong>{{ section.title || section.kind }}</strong>
                <span>{{ section.summary || `${section.cards?.length || 0} 个卡片` }}</span>
              </header>
              <div v-if="section.cards?.length" class="mcp-evidence-cards">
                <div v-for="card in section.cards" :key="card.id" class="mcp-evidence-card-row">
                  <strong>{{ card.title || card.uiKind || card.id }}</strong>
                  <span>{{ card.summary || card.detail || card.text || "" }}</span>
                </div>
              </div>
            </article>
          </div>

          <pre class="mcp-evidence-raw">{{ stringifyRaw(panel.raw) || "暂无 MCP 面板详情" }}</pre>
        </section>
      </template>
      <template #host-terminal="{ panel }">
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

.workspace-status-banner {
  margin: 16px 20px 0;
  padding: 14px 16px;
  border-radius: 14px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  color: #334155;
  flex-shrink: 0;
}

.workspace-status-banner.danger {
  border-color: #fecaca;
  background: #fef2f2;
  color: #991b1b;
}

.workspace-status-banner.warning {
  border-color: #fde68a;
  background: #fffbeb;
  color: #92400e;
}

.workspace-status-banner-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.workspace-status-banner p {
  margin: 0;
  font-size: 14px;
  line-height: 1.55;
}

.workspace-status-banner-hint {
  display: block;
  margin-top: 8px;
  font-size: 12px;
  font-weight: 600;
  opacity: 0.82;
}

.workspace-side-rail {
  display: grid;
  grid-template-rows: minmax(320px, 340px) minmax(0, 1fr) auto;
  gap: 0;
  min-height: 0;
  height: 100%;
  overflow: hidden;
  border-left: 1px solid #e8ecf1;
  background: #f8fafc;
}

.workspace-side-panel {
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.approval-panel {
  background: #f8fafc;
  border-bottom: 1px solid #e8ecf1;
}

.timeline-panel {
  background: #fff;
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

.empty-hint {
  font-size: 13px;
  font-weight: 600;
}

.empty-hint.danger {
  color: #b91c1c;
}

.empty-hint.warning {
  color: #b45309;
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

.mcp-evidence-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mcp-evidence-hero {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 18px;
  background: linear-gradient(180deg, rgba(239, 246, 255, 0.95), rgba(255, 255, 255, 0.98));
  border: 1px solid rgba(191, 219, 254, 0.95);
}

.mcp-evidence-hero h4 {
  margin: 0 0 6px;
  color: #0f172a;
  font-size: 16px;
}

.mcp-evidence-hero p {
  margin: 0;
  color: #475569;
  line-height: 1.6;
}

.mcp-evidence-pin {
  flex-shrink: 0;
  border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 999px;
  padding: 8px 12px;
  background: #ffffff;
  color: #0f172a;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.mcp-evidence-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.mcp-evidence-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px;
  border-radius: 18px;
  border: 1px solid rgba(226, 232, 240, 0.95);
  background: rgba(248, 250, 252, 0.94);
}

.mcp-evidence-card span,
.mcp-evidence-section span {
  color: #64748b;
  font-size: 12px;
  font-weight: 700;
}

.mcp-evidence-card strong,
.mcp-evidence-card-row strong {
  color: #0f172a;
  font-size: 13px;
  line-height: 1.5;
  word-break: break-word;
}

.mcp-evidence-sections {
  display: grid;
  gap: 12px;
}

.mcp-evidence-section {
  display: grid;
  gap: 10px;
  padding: 14px 16px;
  border-radius: 18px;
  border: 1px solid rgba(226, 232, 240, 0.95);
  background: rgba(255, 255, 255, 0.98);
}

.mcp-evidence-section header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
}

.mcp-evidence-section strong {
  color: #0f172a;
  font-size: 14px;
}

.mcp-evidence-cards {
  display: grid;
  gap: 8px;
}

.mcp-evidence-card-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border-radius: 14px;
  background: rgba(248, 250, 252, 0.94);
  border: 1px solid rgba(226, 232, 240, 0.85);
}

.mcp-evidence-raw {
  margin: 0;
  padding: 14px 16px;
  border-radius: 18px;
  background: #0f172a;
  color: #e2e8f0;
  font-size: 13px;
  line-height: 1.6;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
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

  .workspace-side-rail {
    grid-template-rows: auto auto auto;
    overflow: visible;
  }

  .approval-panel {
    min-height: 320px;
  }

  .timeline-panel {
    min-height: 360px;
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

  .workspace-side-panel {
    min-height: 280px;
  }

  .runtime-pill {
    border-radius: 18px;
  }
}
</style>
