import {
  buildWorkspaceHostRows,
  buildWorkspaceLiveTimeline,
  buildWorkspaceStepItems,
  compactText,
  formatShortTime,
  formatTime,
  getWorkspacePlanDetail,
  isApprovalCard,
  isAssistantMessageCard,
  isChoiceCard,
  isDispatchSummaryCard,
  isMissionNoticeCard,
  isPlanCard,
  isProcessCard,
  isSystemNoticeCard,
  isUserMessageCard,
  isWorkspaceResultCard,
  objectRows,
  parseTimestamp,
  pickField,
  statusTone,
} from "./workspaceViewModel";

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function normalizeWorkspaceCopy(value) {
  return compactText(value)
    .replace(/PlannerSession/gi, "主 Agent Session")
    .replace(/Planner/gi, "主 Agent")
    .replace(/planner/gi, "主 Agent");
}

function findLastIndex(list = [], predicate = () => false) {
  for (let index = list.length - 1; index >= 0; index -= 1) {
    if (predicate(list[index], index)) return index;
  }
  return -1;
}

function findLast(list = [], predicate = () => false) {
  const index = findLastIndex(list, predicate);
  return index >= 0 ? list[index] : null;
}

function isStoppedNoticeCard(card) {
  const title = compactText(card?.title).toLowerCase();
  const text = compactText(card?.text || card?.message).toLowerCase();
  return card?.type === "NoticeCard" && (title === "mission stopped" || text.includes("mission 已停止"));
}

function isFailedResultSummaryCard(card) {
  return card?.type === "ResultSummaryCard" && compactText(card?.status).toLowerCase() === "failed";
}

function normalizeEvidenceRows(value, { defaultLabel = "" } = {}) {
  if (!value) return [];
  if (Array.isArray(value)) {
    return value
      .flatMap((item) => {
        if (item == null) return [];
        if (typeof item === "string") {
          const text = compactText(item);
          return text ? [{ label: defaultLabel || "", value: text, text }] : [];
        }
        if (typeof item === "object") {
          const label = compactText(item.label || item.key || item.name || item.title || defaultLabel);
          const valueText = compactText(item.value ?? item.text ?? item.summary ?? item.content ?? item.detail ?? "");
          const description = compactText(item.description || item.note || item.reason || "");
          const rows = [];
          if (label || valueText || description) {
            rows.push({
              label,
              value: valueText || description,
              text: description && description !== valueText ? description : "",
            });
          }
          return rows;
        }
        return [];
      })
      .filter((row) => row.label || row.value || row.text);
  }
  if (typeof value === "object") {
    return objectRows(value).map((row) => ({
      label: row.key,
      value: row.value,
      text: "",
    }));
  }
  const text = compactText(value);
  return text ? [{ label: defaultLabel || "", value: text, text }] : [];
}

function summarizeHostStepItems(planCardModel = null, hostRow = null) {
  const stepItems = asArray(planCardModel?.stepItems);
  if (!stepItems.length || !hostRow) return [];
  const hostId = compactText(hostRow.hostId);
  const hostName = compactText(hostRow.displayName);
  return stepItems.filter((step) => {
    if (asArray(step.hosts).some((host) => compactText(host.id) === hostId)) return true;
    const haystack = `${step.title} ${step.summary} ${step.raw}`.toLowerCase();
    return hostId && haystack.includes(hostId.toLowerCase()) || hostName && haystack.includes(hostName.toLowerCase());
  });
}

function buildDispatchTimelineRows(dispatch = {}) {
  const timeline = pickField(dispatch, "timeline");
  const rows = [];
  for (const item of normalizeEvidenceRows(timeline, { defaultLabel: "事件" })) {
    rows.push({
      label: item.label || "事件",
      value: item.value,
      text: item.text,
    });
  }
  return rows;
}

function buildTaskBindingRows(binding = {}) {
  const rows = [];
  if (!binding || typeof binding !== "object") return rows;
  const orderedKeys = [
    ["taskId", "Task"],
    ["hostId", "Host"],
    ["workerHostId", "Worker Host"],
    ["title", "标题"],
    ["instruction", "指令"],
    ["status", "状态"],
    ["approvalState", "审批状态"],
    ["lastReply", "最后回复"],
    ["lastError", "最后错误"],
    ["externalNodeId", "External Node"],
    ["threadId", "Thread"],
    ["sessionId", "Session"],
  ];
  for (const [key, label] of orderedKeys) {
    const value = compactText(binding[key]);
    if (!value) continue;
    rows.push({ label, value });
  }
  const constraints = asArray(binding.constraints);
  if (constraints.length) {
    rows.push({
      label: "约束",
      value: constraints.map((item) => compactText(item)).filter(Boolean).join(" / "),
    });
  }
  return rows;
}

function buildApprovalAnchorRows(anchor = {}) {
  const rows = [];
  if (!anchor || typeof anchor !== "object") return rows;
  const orderedKeys = [
    ["approvalId", "Approval"],
    ["itemId", "Item"],
    ["sourceCardId", "Source Card"],
    ["hostId", "Host"],
    ["type", "Type"],
    ["title", "Title"],
    ["command", "Command"],
    ["cwd", "Cwd"],
    ["status", "Status"],
    ["summary", "Summary"],
    ["reason", "Reason"],
    ["createdAt", "Created At"],
    ["updatedAt", "Updated At"],
  ];
  for (const [key, label] of orderedKeys) {
    const value = compactText(anchor[key]);
    if (!value) continue;
    rows.push({ label, value });
  }
  return rows;
}

export function normalizeProtocolMissionPhase(value) {
  const normalized = compactText(value).toLowerCase();
  switch (normalized) {
    case "执行中":
    case "running":
    case "inprogress":
    case "in_progress":
      return "executing";
    case "规划中":
    case "planning":
      return "planning";
    case "思考中":
    case "thinking":
      return "thinking";
    case "等待审批":
    case "waitingapproval":
    case "waiting_approval":
      return "waiting_approval";
    case "等待补充输入":
    case "等待输入":
    case "waitinginput":
    case "waiting_input":
      return "waiting_input";
    case "汇总中":
    case "finalizing":
      return "finalizing";
    case "已完成":
    case "completed":
      return "completed";
    case "失败":
    case "failed":
      return "failed";
    case "已停止":
    case "aborted":
      return "aborted";
    case "待命":
    case "idle":
      return "idle";
    default:
      return normalized || "idle";
  }
}

export function resolveProtocolWorkspaceCards(cards = []) {
  const workspaceCards = asArray(cards);
  const latestUserIndex = findLastIndex(workspaceCards, (card) => isUserMessageCard(card));
  const currentMissionCards = latestUserIndex >= 0 ? workspaceCards.slice(latestUserIndex) : workspaceCards;
  const missionScopeCards = currentMissionCards.length ? currentMissionCards : workspaceCards;
  const missionCard = findLast(missionScopeCards, (card) => isMissionNoticeCard(card));
  const planCard = findLast(missionScopeCards, (card) => isPlanCard(card));
  const dispatchSummaryCards = missionScopeCards.filter((card) => isDispatchSummaryCard(card));
  const workspaceResultCard = findLast(missionScopeCards, (card) => isWorkspaceResultCard(card));
  const currentErrorCard = findLast(missionScopeCards, (card) => card?.type === "ErrorCard");
  const currentFailureSummaryCard = findLast(missionScopeCards, (card) => isFailedResultSummaryCard(card));
  const stopNoticeCard = findLast(missionScopeCards, (card) => isStoppedNoticeCard(card));
  const conversationCards = workspaceCards.filter(
    (card) =>
      isUserMessageCard(card) ||
      isAssistantMessageCard(card) ||
      isSystemNoticeCard(card) ||
      card?.type === "ErrorCard" ||
      isFailedResultSummaryCard(card),
  );
  const approvalCards = missionScopeCards.filter((card) => isApprovalCard(card) && card.status === "pending");
  const choiceCards = missionScopeCards.filter((card) => isChoiceCard(card) && card.status === "pending");
  const processCards = missionScopeCards.filter((card) => isProcessCard(card));
  return {
    workspaceCards,
    currentMissionCards,
    latestUserIndex,
    missionCard,
    planCard,
    dispatchSummaryCards,
    workspaceResultCard,
    currentErrorCard,
    currentFailureSummaryCard,
    stopNoticeCard,
    conversationCards,
    approvalCards,
    choiceCards,
    processCards,
  };
}

export function buildProtocolConversationItems(cards = []) {
  return asArray(cards)
    .map((card) => {
      const role = isUserMessageCard(card) ? "user" : "assistant";
      const title = normalizeWorkspaceCopy(card?.title);
      const baseText = normalizeWorkspaceCopy(card?.text || card?.summary || card?.message || card?.title);
      if (!baseText) return null;

      // Filter out system routing / dispatch messages that are not meant for the user
      if (role === "assistant" && isSystemRoutingMessage(baseText)) return null;

      const shouldPrefixTitle = (card?.type === "ErrorCard" || isFailedResultSummaryCard(card)) && title && !baseText.startsWith(title);
      const cleanedText = cleanAssistantMessageText(shouldPrefixTitle ? `${title}\n${baseText}` : baseText, role);
      if (!cleanedText) return null;

      return {
        id: card.id || `${role}-${cleanedText}`,
        role,
        time: formatShortTime(card.updatedAt || card.createdAt),
        title: card?.type === "ErrorCard" ? "系统错误" : role === "user" ? "用户" : "主 Agent",
        text: cleanedText,
      };
    })
    .filter(Boolean);
}

/**
 * Detect system routing / dispatch messages that should be hidden from the user.
 * These are internal Agent routing decisions, not actual replies.
 */
function isSystemRoutingMessage(text) {
  const trimmed = (text || "").trim();
  // Messages that are purely the Agent's internal routing decision
  if (/^主\s*Agent\s*正在判断/.test(trimmed)) return true;
  if (/^这是简单对话/.test(trimmed)) return true;
  if (/^(这是|当前).*(简单|直接).*(对话|回答|回复)/.test(trimmed)) return true;
  // Pure routing notice without any useful content
  if (/^主\s*Agent\s*(会|将)直接回答/.test(trimmed)) return true;
  if (/不会生成计划或派发\s*worker/.test(trimmed)) return true;
  return false;
}

/**
 * Clean assistant message text by stripping embedded JSON routing blocks
 * that are internal Agent metadata, not meant for user display.
 */
function cleanAssistantMessageText(text, role) {
  if (role === "user" || !text) return text;
  // Remove ```json ... ``` fenced blocks containing routing metadata
  let cleaned = text.replace(/`{3}json[\s\S]*?`{3}/g, (match) => {
    if (/"route"\s*:/.test(match)) return "";
    return match;
  }).trim();
  // Fallback: remove unclosed ```json blocks with routing metadata
  cleaned = cleaned.replace(/`{3}json\s*\{[^`]*"route"\s*:[^`]*/g, "").trim();
  // Remove inline { "route": ... } JSON objects
  cleaned = cleaned.replace(/\{[^{}]*"route"\s*:\s*"[^"]*"[^{}]*\}/g, "").trim();
  // Collapse multiple newlines
  cleaned = cleaned.replace(/\n{3,}/g, "\n\n").trim();
  return cleaned;
}

export function buildProtocolBackgroundAgents(hostRows = []) {
  const seen = new Set();
  return asArray(hostRows)
    .filter((row) => {
      if (!row.hostId || row.hostId === "server-local") return false;
      if (seen.has(row.hostId)) return false;
      seen.add(row.hostId);
      return ["running", "waiting_approval", "queued", "idle", "pending", "dispatched"].includes(row.statusKey) || row.worker;
    })
    .map((row) => ({
      id: row.hostId,
      hostId: row.hostId,
      name: row.displayName || row.address || row.hostId || "agent",
      subtitle: compactText(row.taskTitle || row.summary || row.statusLabel || "等待执行"),
      status: row.statusKey,
      statusLabel: row.statusLabel,
      tone: statusTone(row.statusKey),
    }));
}

export function buildProtocolApprovalItems(approvalCards = [], hostRows = []) {
  const hostById = new Map(asArray(hostRows).map((row) => [row.hostId, row]));
  return asArray(approvalCards).map((card) => {
    const host = hostById.get(card.hostId) || null;
    const decisions = asArray(card?.approval?.decisions);
    const dispatchRequest = asObject(host?.dispatch?.request);
    const approvalAnchor = asObject(host?.worker?.approvalAnchor || host?.worker?.approval_anchor);
    return {
      id: card.id,
      approvalId: compactText(card?.approval?.requestId || card?.approvalId || card?.requestId),
      hostId: compactText(card.hostId),
      hostName: host?.displayName || compactText(card.hostId) || "unknown-host",
      command: normalizeWorkspaceCopy(card.command || dispatchRequest.summary || dispatchRequest.title || card.text || card.summary),
      summary: normalizeWorkspaceCopy(card.text || card.summary || host?.taskTitle || host?.summary),
      timeLabel: formatTime(card.updatedAt || card.createdAt),
      supportsAuthorize: decisions.includes("accept_session"),
      detailRows: normalizeEvidenceRows(
        card.detail ||
          card.details ||
          host?.dispatch?.request ||
          host?.dispatch?.taskBinding ||
          host?.dispatch?.task_binding ||
          host?.worker?.approval ||
          host?.worker?.approvalAnchor ||
          host?.worker?.approval_anchor,
      ),
      dispatchRequest,
      approvalAnchor,
      taskBinding: host?.dispatch?.taskBinding || host?.dispatch?.task_binding || null,
      raw: card,
    };
  });
}

export function buildProtocolEventItems({
  planCard = null,
  dispatchSummaryCards = [],
  approvalCards = [],
  choiceCards = [],
  workspaceResultCard = null,
  hostRows = [],
  systemNoticeCards = [],
  dispatchEvents = [],
} = {}) {
  return buildWorkspaceLiveTimeline({
    planCard,
    dispatchEvents,
    dispatchCards: dispatchSummaryCards,
    approvalCards,
    choiceCards,
    resultCard: workspaceResultCard,
    hostRows,
    noticeCards: systemNoticeCards,
  }).map((item) => ({
    id: item.id,
    time: item.time || "",
    title: normalizeWorkspaceCopy(item.title || item.source || "事件"),
    detail: normalizeWorkspaceCopy(item.text || ""),
    tone: item.tone || "neutral",
    targetType: item.targetType || "",
    targetId: item.targetId || "",
    hostId: item.hostId || "",
  }));
}

export function buildProtocolPlanCardModel({
  planCard = null,
  workspaceResultCard = null,
  hostRows = [],
} = {}) {
  const planDetailState = getWorkspacePlanDetail(planCard, workspaceResultCard);
  const planDetail = planDetailState.detail;
  const structuredProcess = planDetailState.structuredProcess;
  const taskHostBindings = asArray(pickField(planDetail, "taskHostBindings", "task_host_bindings"));
  const fallbackStructuredProcess = asArray(planCard?.items)
    .map((item, index) => {
      const stepText = compactText(item?.step || item?.label || item?.title || item?.text);
      const status = compactText(item?.status).toLowerCase() || "pending";
      if (!stepText) return "";
      const match = stepText.match(/^([A-Za-z0-9._:-]+)\s+\[([^\]]+)\]\s+(.+)$/);
      if (match) {
        const [, hostId, taskId, title] = match;
        return `${taskId} [${status}] @${hostId} ${title}`.trim();
      }
      return `step-${index + 1} [${status}] ${stepText}`.trim();
    })
    .filter(Boolean);
  const stepItems = buildWorkspaceStepItems({
    structuredProcess: structuredProcess.length ? structuredProcess : fallbackStructuredProcess,
    taskHostBindings,
    hostRows,
  });

  const completed =
    stepItems.filter((item) => compactText(item.status).includes("complete")).length ||
    asArray(planCard?.items).filter((item) => compactText(item?.status).includes("complete")).length;
  return {
    title: normalizeWorkspaceCopy(planCard?.title || planDetail?.goal || "主 Agent 计划摘要"),
    summary: normalizeWorkspaceCopy(planCard?.text || planDetail?.goal || ""),
    version: compactText(planDetailState.version || "plan-v1"),
    generatedAt: compactText(planDetailState.generatedAt || planCard?.updatedAt || planCard?.createdAt),
    totalSteps: stepItems.length || asArray(planCard?.items).length,
    completedSteps: completed,
    stepItems,
    dispatchEvents: asArray(pickField(planDetail, "dispatchEvents", "dispatch_events")),
  };
}

export function buildProtocolEvidenceTabs({
  planCardModel = null,
  hostRow = null,
  approvalItem = null,
  eventItems = [],
} = {}) {
  const dispatch = asObject(hostRow?.dispatch);
  const worker = asObject(hostRow?.worker);
  const taskBinding = asObject(pickField(dispatch, "task_binding", "taskBinding")) || null;
  const approvalAnchor = asObject(pickField(worker, "approval_anchor", "approvalAnchor"));
  const dispatchRequest = asObject(pickField(dispatch, "request"));

  const mainAgentPlan = [];
  if (compactText(planCardModel?.summary) || compactText(planCardModel?.title)) {
    mainAgentPlan.push({
      id: "plan-summary",
      title: "计划摘要",
      text: normalizeWorkspaceCopy(planCardModel?.summary || planCardModel?.title || "当前还没有可用的计划摘要。"),
      time: formatShortTime(planCardModel?.generatedAt),
    });
  }
  for (const [index, step] of asArray(planCardModel?.stepItems).entries()) {
    const hostLabels = asArray(step.hosts)
      .map((host) => compactText(host.label || host.id))
      .filter(Boolean)
      .join("、");
    const pieces = [step.statusLabel || step.status, hostLabels ? `Host: ${hostLabels}` : "", step.detail || step.note || ""]
      .map((item) => compactText(item))
      .filter(Boolean);
    mainAgentPlan.push({
      id: `plan-step-${step.id || index}`,
      title: `Step ${step.index || index + 1} · ${normalizeWorkspaceCopy(step.title)}`,
      text: normalizeWorkspaceCopy(pieces.join(" · ")),
      time: "",
    });
  }

  const hostConversation = [];
  for (const item of asArray(worker.conversation || worker.conversation_excerpts)) {
    const text = compactText(item.text || item.summary);
    if (!text) continue;
    hostConversation.push({
      id: item.id || `${item.createdAt}-${text}`,
      title: normalizeWorkspaceCopy(item.summary || item.type || item.role || "Host -> AI"),
      text: normalizeWorkspaceCopy(text),
      time: formatShortTime(item.createdAt || hostRow?.updatedAt),
    });
  }

  if (!hostConversation.length) {
    const transcript = asArray(worker.transcript);
    for (let index = 0; index < transcript.length; index += 1) {
      const text = compactText(transcript[index]);
      if (!text) continue;
      hostConversation.push({
        id: `${hostRow?.hostId || "host"}-transcript-${index}`,
        title: `Transcript ${index + 1}`,
        text: normalizeWorkspaceCopy(text),
        time: formatShortTime(worker.updatedAt || hostRow?.updatedAt),
      });
    }
  }

  if (!hostConversation.length) {
    if (compactText(worker.lastReply)) {
      hostConversation.push({
        id: `${hostRow?.hostId || "host"}-last-reply`,
        title: "Last Reply",
        text: normalizeWorkspaceCopy(worker.lastReply),
        time: formatShortTime(worker.updatedAt || hostRow?.updatedAt),
      });
    }
    if (compactText(worker.lastError)) {
      hostConversation.push({
        id: `${hostRow?.hostId || "host"}-last-error`,
        title: "Last Error",
        text: normalizeWorkspaceCopy(worker.lastError),
        time: formatShortTime(worker.updatedAt || hostRow?.updatedAt),
      });
    }
  }

  const approvalContext = [];
  const approvalSource = approvalItem || {
    hostName: hostRow?.displayName,
    hostId: hostRow?.hostId,
    approvalId: compactText(pickField(worker, "approval", "approvalAnchor", "approval_anchor")?.requestId || ""),
    command: compactText(dispatchRequest.title || dispatchRequest.summary),
    summary: compactText(dispatchRequest.summary || dispatchRequest.title || hostRow?.summary || hostRow?.taskTitle),
    detailRows: [...buildTaskBindingRows(taskBinding || dispatch), ...buildDispatchTimelineRows(dispatch), ...buildApprovalAnchorRows(approvalAnchor)],
    approvalAnchor,
    raw: { dispatch, worker },
  };

  if (approvalSource) {
    if (approvalSource.hostName || approvalSource.hostId) {
      approvalContext.push({
        id: `approval-host-${approvalSource.hostId || "host"}`,
        title: "主机",
        text: compactText(approvalSource.hostName || approvalSource.hostId || "unknown-host"),
        time: "",
      });
    }
    if (approvalSource.approvalId) {
      approvalContext.push({
        id: `approval-id-${approvalSource.approvalId}`,
        title: "审批ID",
        text: compactText(approvalSource.approvalId),
        time: "",
      });
    }
    if (approvalSource.command || approvalSource.summary) {
      approvalContext.push({
        id: `approval-command-${approvalSource.hostId || "host"}`,
        title: "命令",
        text: normalizeWorkspaceCopy(approvalSource.command || approvalSource.summary),
        time: "",
      });
    }
    for (const row of asArray(approvalSource.detailRows)) {
      approvalContext.push({
        id: `approval-detail-${approvalSource.hostId || "host"}-${row.label}`,
        title: compactText(row.label || "详情"),
        text: normalizeWorkspaceCopy(row.value || row.text),
        time: "",
      });
    }
    if (approvalSource.approvalAnchor) {
      for (const row of buildApprovalAnchorRows(approvalSource.approvalAnchor)) {
        approvalContext.push({
          id: `approval-anchor-${approvalSource.hostId || "host"}-${row.label}`,
          title: `审批锚点 · ${row.label}`,
          text: normalizeWorkspaceCopy(row.value),
          time: "",
        });
      }
    }
  }

  const hostTerminal = asObject(worker.terminal);
  const hostTerminalRows = [];
  const terminalRows = objectRows(hostTerminal);
  const terminalOutput = pickField(hostTerminal, "output", "stdout", "text", "summary");
  const terminalSummary = [
    hostRow?.displayName ? { label: "Host", value: compactText(hostRow.displayName) } : null,
    hostRow?.statusLabel ? { label: "Status", value: compactText(hostRow.statusLabel) } : null,
    hostRow?.taskTitle ? { label: "Task", value: compactText(hostRow.taskTitle) } : null,
    compactText(worker.mode) ? { label: "Mode", value: compactText(worker.mode) } : null,
    compactText(worker.activeTaskId || worker.active_task_id) ? { label: "Active Task", value: compactText(worker.activeTaskId || worker.active_task_id) } : null,
    compactText(worker.sessionId || worker.session_id) ? { label: "Worker Session", value: compactText(worker.sessionId || worker.session_id) } : null,
    compactText(worker.threadId || worker.thread_id) ? { label: "Worker Thread", value: compactText(worker.threadId || worker.thread_id) } : null,
  ].filter(Boolean);
  hostTerminalRows.push(...terminalSummary, ...terminalRows);
  for (const row of buildApprovalAnchorRows(approvalAnchor)) {
    hostTerminalRows.push({
      label: `Approval ${row.label}`,
      value: row.value,
      text: row.text,
    });
  }
  const hostTerminalOutput = (() => {
    if (Array.isArray(terminalOutput)) return terminalOutput.map((item) => String(item ?? ""));
    if (terminalOutput && typeof terminalOutput === "object") {
      const rows = objectRows(terminalOutput);
      if (rows.length) return rows.map((row) => `${row.key}: ${row.value}`);
      try {
        return JSON.stringify(terminalOutput, null, 2);
      } catch {
        return String(terminalOutput);
      }
    }
    return compactText(terminalOutput || hostTerminal.summary || hostTerminal.status || "");
  })();

  return {
    mainAgentPlan,
    workerConversation: hostConversation,
    hostConversation,
    hostTerminalRows,
    hostTerminalOutput,
    approvalContext,
  };
}

export function buildProtocolWorkspaceModel(snapshot = {}, runtime = {}) {
  const cards = resolveProtocolWorkspaceCards(snapshot.cards || []);
  const hostRows = buildWorkspaceHostRows({
    cards: cards.currentMissionCards,
    hosts: snapshot.hosts || [],
    approvalCards: cards.approvalCards,
  });
  const runtimePhase = normalizeProtocolMissionPhase(pickField(runtime?.turn || {}, "phase") || pickField(cards.missionCard?.detail || {}, "status"));
  const currentFailureCard =
    cards.currentErrorCard ||
    cards.currentFailureSummaryCard ||
    (compactText(cards.workspaceResultCard?.status).toLowerCase() === "failed" ? cards.workspaceResultCard : null);
  let missionPhase = runtimePhase;
  if (cards.stopNoticeCard) {
    missionPhase = "aborted";
  } else if (currentFailureCard) {
    missionPhase = "failed";
  } else if (compactText(cards.workspaceResultCard?.status).toLowerCase() === "completed" && runtime?.turn?.active !== true) {
    missionPhase = "completed";
  }
  const planCardModel = buildProtocolPlanCardModel({
    planCard: cards.planCard,
    workspaceResultCard: cards.workspaceResultCard,
    hostRows,
  });
  const dispatchEvents = asArray(pickField(planCardModel, "dispatchEvents", "dispatch_events"));
  const eventItems = buildProtocolEventItems({
    planCard: cards.planCard,
    dispatchSummaryCards: cards.dispatchSummaryCards,
    approvalCards: cards.approvalCards,
    choiceCards: cards.choiceCards,
    workspaceResultCard: cards.workspaceResultCard,
    hostRows,
    systemNoticeCards: cards.currentMissionCards.filter((card) => isSystemNoticeCard(card)),
    dispatchEvents,
  });
  const canStopCurrentMission =
    Boolean(runtime?.turn?.active) &&
    !["aborted", "failed", "completed"].includes(missionPhase) &&
    !cards.stopNoticeCard &&
    !currentFailureCard;
  const nextSendStartsNewMission = !canStopCurrentMission && Boolean(cards.stopNoticeCard || currentFailureCard || missionPhase === "completed");
  let statusBanner = null;
  if (currentFailureCard) {
    statusBanner = {
      tone: "danger",
      title: compactText(currentFailureCard.title || "当前 mission 执行失败"),
      detail:
        compactText(currentFailureCard.text || currentFailureCard.message || currentFailureCard.summary) ||
        "查看左侧对话和右侧事件，确认失败原因后再发起下一轮。",
      runtimeText: `失败 | ${compactText(currentFailureCard.title || currentFailureCard.summary || "当前 mission 执行失败")}`,
    };
  } else if (cards.stopNoticeCard) {
    statusBanner = {
      tone: "warning",
      title: "当前 mission 已停止",
      detail: "再次发送会在当前工作台里启动一轮新的 mission，不会续跑已停止的那一轮。",
      runtimeText: "已停止 | 再次发送将启动新 mission",
    };
  } else if (missionPhase === "completed" && runtime?.turn?.active !== true) {
    // Don't show the "上一轮任务已完成" banner — the placeholder text in the
    // composer already tells the user that the next send starts a new mission.
    statusBanner = null;
  }

  return {
    missionPhase,
    cards,
    hostRows,
    conversationItems: buildProtocolConversationItems(cards.conversationCards),
    approvalItems: buildProtocolApprovalItems(cards.approvalCards, hostRows),
    backgroundAgents: buildProtocolBackgroundAgents(hostRows),
    planCardModel,
    eventItems,
    processCards: cards.processCards,
    canStopCurrentMission,
    nextSendStartsNewMission,
    statusBanner,
    currentFailureCard,
  };
}
