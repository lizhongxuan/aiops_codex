import { buildProtocolConversationItems } from "./protocolWorkspaceVm";
import {
  cleanAssistantDisplayText,
  compactText,
  formatShortTime,
  getWorkspaceCardType,
  isApprovalCard,
  parseTimestamp,
  phaseLabel,
  isAssistantMessageCard,
  isChoiceCard,
  isProcessCard,
  isSystemNoticeCard,
  isUserMessageCard,
  sortProcessDisplayItems,
} from "./workspaceViewModel";
import { adaptMcpUiPayloadFromCard } from "./mcpUiPayloadAdapter";
import { normalizeMcpBundle, normalizeMcpPayloadSource, normalizeMcpUiCard } from "./mcpUiCardModel";
import { resolveMcpBundlePreset } from "./mcpBundleResolver";

const ACTIVE_PHASES = new Set(["planning", "thinking", "executing", "waiting_approval", "waiting_input", "finalizing"]);
const WAITING_PHASES = new Set(["waiting_approval", "waiting_input"]);
const PROTOCOL_SURFACE_OWNED_PATTERN = /审批|批准|授权|approval|派发|dispatch|host-agent|worker|时间线|timeline|step\s*->\s*host|host-agent 映射|编排执行计划|接管任务|执行位/i;

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function messageCardFromConversationItem(item = {}, rawCard = null) {
  const role = compactText(item.role).toLowerCase() === "user" ? "user" : "assistant";
  const id = compactText(item.id || rawCard?.id || `${role}-message`);
  const text = cleanAssistantDisplayText(String(item.text || "").trim(), role);
  if (!text) return null;
  return {
    id,
    role,
    time: compactText(item.time || formatShortTime(rawCard?.updatedAt || rawCard?.createdAt)),
    createdAt: rawCard?.createdAt || "",
    updatedAt: rawCard?.updatedAt || rawCard?.createdAt || "",
    sourceCard: rawCard || null,
    card: {
      id,
      type: role === "user" ? "UserMessageCard" : "AssistantMessageCard",
      role,
      text,
      status: compactText(rawCard?.status || ""),
    },
  };
}

function messageCardFromRawCard(card = {}) {
  const role = isUserMessageCard(card) ? "user" : "assistant";
  const id = compactText(card?.id || `${role}-message`);
  const text = cleanAssistantDisplayText(String(card?.text || card?.message || card?.summary || card?.title || "").trim(), role);
  if (!text) return null;
  return {
    id,
    role,
    time: formatShortTime(card?.updatedAt || card?.createdAt),
    createdAt: card?.createdAt || "",
    updatedAt: card?.updatedAt || card?.createdAt || "",
    sourceCard: card || null,
    card: {
      id,
      type: role === "user" ? "UserMessageCard" : "AssistantMessageCard",
      role,
      text,
      status: compactText(card?.status || ""),
    },
  };
}

function processToneFromStatus(status) {
  const normalized = compactText(status).toLowerCase();
  if (normalized.includes("fail") || normalized.includes("error") || normalized.includes("permission") || normalized.includes("denied")) return "danger";
  if (normalized.includes("complete") || normalized.includes("done")) return "success";
  if (normalized.includes("wait")) return "warning";
  return "neutral";
}

function stripMatchingQuotes(value = "") {
  const text = String(value || "").trim();
  if (text.length >= 2 && ((text.startsWith("'") && text.endsWith("'")) || (text.startsWith('"') && text.endsWith('"')))) {
    return text.slice(1, -1);
  }
  return text;
}

function displayCommand(value = "") {
  const raw = String(value || "").trim();
  if (!raw) return "";
  const shellMatch = raw.match(/^(?:\/[\w./-]+\/)?(?:zsh|bash|sh)\s+-lc\s+([\s\S]+)$/);
  if (shellMatch) return stripMatchingQuotes(shellMatch[1]);
  return raw;
}

function commandStatusLabel(value = "") {
  const normalized = compactText(value).toLowerCase();
  if (normalized.includes("permission") || normalized.includes("denied")) return "权限不足";
  if (normalized.includes("fail") || normalized.includes("error")) return "失败";
  if (normalized.includes("complete") || normalized.includes("done")) return "已完成";
  if (normalized.includes("run") || normalized.includes("progress")) return "执行中";
  return compactText(value) || "已处理";
}

function formatDurationLabel(ms) {
  const totalSeconds = Math.max(0, Math.round(ms / 1000));
  if (!totalSeconds) return "";
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const parts = [];
  if (hours) parts.push(`${hours}h`);
  if (minutes) parts.push(`${minutes}m`);
  if (seconds && !hours) parts.push(`${seconds}s`);
  return parts.join(" ");
}

function activeProcessLabel(phase) {
  switch (compactText(phase).toLowerCase()) {
    case "planning":
      return "正在规划步骤";
    case "thinking":
      return "正在思考";
    case "waiting_approval":
      return "等待审批";
    case "waiting_input":
      return "等待补充输入";
    case "finalizing":
      return "正在汇总结果";
    default:
      return "处理中";
  }
}

function latestTimestamp(values = []) {
  let latest = 0;
  for (const value of values) {
    const stamp = parseTimestamp(value);
    if (stamp > latest) latest = stamp;
  }
  return latest;
}

function deriveLiveHint({ statusCard = null, missionPhase = "", approvalItems = [] } = {}) {
  const explicitHint = compactText(statusCard?.hint);
  if (explicitHint) return explicitHint;

  const normalizedPhase = compactText(statusCard?.phase || missionPhase).toLowerCase();
  if (normalizedPhase === "waiting_approval" && approvalItems.length) {
    return "等待审批后继续推进。";
  }
  return compactText(phaseLabel(normalizedPhase))
    .replace("主 Agent ", "")
    .replace("生成计划", "正在规划步骤");
}

function buildAssistantProcessItems(messages = [], options = {}) {
  const exclude = typeof options.exclude === "function" ? options.exclude : () => false;
  return asArray(messages).map((message, index) => ({
    id: `${message.id || "assistant"}-process-${index}`,
    kind: "assistant_message",
    processKind: inferProcessKind(message.card?.text || ""),
    text: String(message.card?.text || "").trim(),
    detail: "",
    time: compactText(message.time),
    hostId: "",
    tone: "neutral",
    status: "",
    sortTimestamp: message.updatedAt || message.createdAt || "",
  })).filter((item) => item.text && !exclude(item));
}

function buildProcessLineItems(processCards = [], options = {}) {
  const exclude = typeof options.exclude === "function" ? options.exclude : () => false;
  return asArray(processCards).map((card, index) => {
    const hostId = compactText(card?.hostId || card?.title);
    const primary = compactText(card?.text || card?.summary);
    const secondary = compactText(card?.summary && card?.summary !== card?.text ? card.summary : "");
    const item = {
      id: compactText(card?.id || `process-${index}`),
      kind: "process_line",
      processKind: inferProcessKind(`${card?.title || ""} ${primary} ${secondary}`),
      text: primary,
      detail: secondary,
      time: formatShortTime(card?.updatedAt || card?.createdAt),
      hostId,
      tone: processToneFromStatus(card?.status),
      status: compactText(card?.status),
      sortTimestamp: card?.updatedAt || card?.createdAt || "",
    };
    if (!item.text || exclude(item, card)) {
      return null;
    }
    return item;
  }).filter(Boolean);
}

function buildCommandProcessItems(commandCards = []) {
  return asArray(commandCards)
    .filter((card) => compactText(card?.command))
    .map((card, index) => {
      const command = displayCommand(card.command);
      return {
        id: `command-${compactText(card?.id || index)}`,
        kind: "command",
        processKind: "command",
        text: `${commandStatusLabel(card?.status)} · ${command}`,
        detail: compactText(card?.output || card?.stdout || card?.stderr || card?.text || card?.summary),
        time: formatShortTime(card?.updatedAt || card?.createdAt),
        hostId: compactText(card?.hostId),
        tone: processToneFromStatus(card?.status),
        status: compactText(card?.status),
        sortTimestamp: card?.updatedAt || card?.createdAt || "",
        command,
        commandCard: card,
      };
    });
}

function inferProcessKind(value = "") {
  const normalized = compactText(value).toLowerCase();
  if (!normalized) return "notice";
  if (/读取|浏览|打开|read|viewed file|file/.test(normalized)) return "read";
  if (/搜索|search|grep|query/.test(normalized)) return "search";
  if (/\bls\b|list|列出|枚举/.test(normalized)) return "list";
  if (/command|terminal|bash|systemctl|journalctl|npm |go run|执行/.test(normalized)) return "command";
  if (/thinking|思考|分析|整理/.test(normalized)) return "thinking";
  if (/agent|worker|host-agent|background/.test(normalized)) return "agent_status";
  return "notice";
}

export function classifyChatCardSemantic(card = {}) {
  const type = getWorkspaceCardType(card);
  const text = compactText(card?.text || card?.message || card?.summary || card?.title);
  if (isUserMessageCard(card)) {
    return { layer: "user", semantic: "user", processKind: "" };
  }
  if (isApprovalCard(card)) {
    return { layer: "blocking", semantic: "approval", processKind: "" };
  }
  if (isChoiceCard(card)) {
    return { layer: "blocking", semantic: "waiting_input", processKind: "" };
  }
  if (type === "ErrorCard") {
    return { layer: "blocking", semantic: "error", processKind: "" };
  }
  if (isProcessCard(card)) {
    return { layer: "process", semantic: "process", processKind: inferProcessKind(text) };
  }
  if (isSystemNoticeCard(card)) {
    return { layer: "process", semantic: "notice", processKind: "notice" };
  }
  if (isAssistantMessageCard(card)) {
    const cleaned = cleanAssistantDisplayText(text, "assistant");
    if (!cleaned) {
      return { layer: "hidden", semantic: "internal_routing", processKind: "notice", shouldHide: true };
    }
    return { layer: "final", semantic: "assistant_final", processKind: "" };
  }
  return { layer: "other", semantic: type || "unknown", processKind: "" };
}

function looksLikeProtocolSurfaceOwnedCopy(value = "") {
  return PROTOCOL_SURFACE_OWNED_PATTERN.test(compactText(value));
}

function isProtocolAssistantProcessRedundant(item = {}) {
  const text = [item.text, item.detail].filter(Boolean).join(" ");
  return looksLikeProtocolSurfaceOwnedCopy(text);
}

function isProtocolProcessCardRedundant(item = {}, card = {}) {
  const text = [item.text, item.detail, item.status, card?.summary, card?.title].filter(Boolean).join(" ");
  if (/^已处理\s*\d+\s*个命令$/.test(compactText(item.text))) {
    return true;
  }
  if (looksLikeProtocolSurfaceOwnedCopy(text)) {
    return true;
  }
  const status = compactText(card?.status || item.status).toLowerCase();
  const hostId = compactText(card?.hostId || item.hostId);
  if (!hostId) {
    return false;
  }
  return ["wait", "approval", "progress", "running", "queued", "dispatch", "complete"].some((keyword) => status.includes(keyword));
}

function summarizeTurnProcess({ processItems = [], activeTurn = false, liveHint = "" } = {}) {
  const assistantUpdates = asArray(processItems).filter((item) => item.kind === "assistant_message").length;
  if (assistantUpdates > 1) {
    return `已记录 ${assistantUpdates} 条过程消息`;
  }
  if (!activeTurn && liveHint) return liveHint;
  return "";
}

function summarizeProtocolTurnProcess({ processItems = [], missionPhase = "", activeTurn = false, liveHint = "" } = {}) {
  if (!activeTurn) {
    const itemCount = asArray(processItems).length;
    if (!itemCount) return liveHint || "";
    if (itemCount === 1) return "已记录 1 条过程细项";
    return `已记录 ${itemCount} 条过程细项`;
  }

  const phase = compactText(missionPhase).toLowerCase();
  if (phase === "waiting_approval") {
    return "审批详情已收进右侧审批面板";
  }
  if (phase === "planning") {
    return "计划细节已收进计划与证据面板";
  }
  if (phase === "executing" || phase === "thinking" || phase === "finalizing") {
    return processItems.length ? "执行细节已收进右侧执行面板" : "";
  }
  return liveHint ? "" : summarizeTurnProcess({ processItems, activeTurn, liveHint });
}

function summarizeMainChatProcess({ processItems = [], activeProcess = null, liveHint = "" } = {}) {
  const explicitSummary = compactText(activeProcess?.summary);
  if (explicitSummary) return explicitSummary;
  if (liveHint) return "";
  const itemCount = asArray(processItems).length;
  if (!itemCount) return "";
  if (itemCount === 1) return "已记录 1 条过程细项";
  return `已记录 ${itemCount} 条过程细项`;
}

function createTurnBucket(seedMessage = null) {
  const id = compactText(seedMessage?.id || `turn-${Date.now()}`);
  return {
    id: `turn-${id}`,
    userMessage: seedMessage?.role === "user" ? seedMessage : null,
    assistantMessages: seedMessage?.role === "assistant" ? [seedMessage] : [],
  };
}

function pushTurn(turns, bucket) {
  if (!bucket) return;
  if (!bucket.userMessage && !bucket.assistantMessages.length) return;
  turns.push(bucket);
}

export function collectMcpUiSurfaceEntries(cards = []) {
  return asArray(cards).flatMap((card, index) => adaptMcpUiPayloadFromCard(card, index).items);
}

function createTurnSurfaceDefaults(card = {}, sourceCardId = "") {
  return {
    sourceCardId: compactText(sourceCardId || card?.id),
    placement: compactText(card?.placement || "inline_final"),
    source: card?.source,
    mcpServer: card?.mcpServer || card?.mcp_server,
    scope: card?.scope,
    freshness: card?.freshness,
    errors: [
      card?.error,
      ...(Array.isArray(card?.errors) ? card.errors : card?.errors ? [card.errors] : []),
    ],
  };
}

function createTurnCardEntry(payload = {}, defaults = {}, index = 0) {
  const model = normalizeMcpUiCard(
    {
      ...payload,
      id: compactText(payload?.id || `${compactText(defaults.sourceCardId || "mcp-ui-card")}-${index + 1}`),
    },
    defaults,
  );
  return {
    id: model.id,
    kind: "mcp_ui_card",
    placement: model.placement,
    source: normalizeMcpPayloadSource(model.source),
    mcpServer: compactText(model.mcpServer),
    freshness: model.freshness,
    scope: model.scope,
    errors: model.errors,
    sourceCardId: compactText(defaults.sourceCardId),
    model,
  };
}

function createTurnBundleEntry(payload = {}, defaults = {}, index = 0) {
  const resolvedPreset = resolveMcpBundlePreset(payload, {
    scope: payload?.scope || defaults.scope,
    bundleKind: payload?.bundleKind || payload?.bundle_kind,
  });
  const model = normalizeMcpBundle(
    {
      ...payload,
      bundleId: compactText(payload?.bundleId || payload?.bundle_id || payload?.id || `${compactText(defaults.sourceCardId || "mcp-bundle")}-${index + 1}`),
      bundleKind: compactText(payload?.bundleKind || payload?.bundle_kind || resolvedPreset.bundleKind),
      sections: asArray(payload?.sections).length ? payload.sections : resolvedPreset.sections,
      scope: payload?.scope || resolvedPreset.scope,
    },
    defaults,
  );
  return {
    id: model.bundleId,
    kind: "mcp_bundle",
    placement: compactText(payload?.placement || defaults.placement || "inline_final") || "inline_final",
    source: normalizeMcpPayloadSource(model.source),
    mcpServer: compactText(model.mcpServer),
    freshness: model.freshness,
    scope: model.scope,
    errors: model.errors,
    sourceCardId: compactText(defaults.sourceCardId),
    model,
  };
}

function pushTurnSurface(target, entry, keys) {
  if (!entry) return;
  const key = `${entry.kind}:${entry.id}:${entry.placement}:${entry.sourceCardId || ""}`;
  if (keys.has(key)) return;
  keys.add(key);
  target.push(entry);
}

function readTurnSurfacePayload(card = {}) {
  const payload = card?.payload && typeof card.payload === "object" ? card.payload : {};
  return {
    resultAttachments: asArray(payload.resultAttachments || payload.result_attachments),
    actionSurfaces: asArray(payload.actionSurfaces || payload.action_surfaces),
    workspaceSurfaces: asArray(payload.workspaceSurfaces || payload.workspace_surfaces),
    resultBundles: asArray(payload.resultBundles || payload.result_bundles),
    actionBundles: asArray(payload.actionBundles || payload.action_bundles),
  };
}

function classifyTurnSurfaceEntry(groups, entry, keys) {
  if (!entry) return;
  if (entry.kind === "mcp_bundle") {
    if (entry.placement === "inline_action") {
      pushTurnSurface(groups.actionBundles, entry, keys);
      return;
    }
    pushTurnSurface(groups.resultBundles, entry, keys);
    return;
  }
  if (entry.placement === "inline_action") {
    pushTurnSurface(groups.actionSurfaces, entry, keys);
    return;
  }
  if (["side_panel", "drawer", "modal"].includes(entry.placement)) {
    pushTurnSurface(groups.workspaceSurfaces, entry, keys);
    return;
  }
  pushTurnSurface(groups.resultAttachments, entry, keys);
}

function collectTurnMcpSurfaceGroups(sourceCards = []) {
  const groups = {
    resultAttachments: [],
    actionSurfaces: [],
    workspaceSurfaces: [],
    resultBundles: [],
    actionBundles: [],
  };
  const keys = new Set();

  asArray(sourceCards).forEach((card, cardIndex) => {
    if (!card || typeof card !== "object") return;
    const defaults = createTurnSurfaceDefaults(card, card?.id || `turn-source-${cardIndex + 1}`);
    const explicit = readTurnSurfacePayload(card);

    explicit.resultAttachments.forEach((item, index) => {
      classifyTurnSurfaceEntry(groups, createTurnCardEntry(item, defaults, index), keys);
    });
    explicit.actionSurfaces.forEach((item, index) => {
      classifyTurnSurfaceEntry(groups, createTurnCardEntry({ ...item, placement: item?.placement || "inline_action" }, defaults, index), keys);
    });
    explicit.workspaceSurfaces.forEach((item, index) => {
      classifyTurnSurfaceEntry(groups, createTurnCardEntry({ ...item, placement: item?.placement || "drawer" }, defaults, index), keys);
    });
    explicit.resultBundles.forEach((item, index) => {
      classifyTurnSurfaceEntry(groups, createTurnBundleEntry(item, defaults, index), keys);
    });
    explicit.actionBundles.forEach((item, index) => {
      classifyTurnSurfaceEntry(groups, createTurnBundleEntry({ ...item, placement: item?.placement || "inline_action" }, defaults, index), keys);
    });

    adaptMcpUiPayloadFromCard(card, cardIndex).items.forEach((entry) => {
      classifyTurnSurfaceEntry(groups, entry, keys);
    });
  });

  return groups;
}

export function formatProtocolChatTurns({
  conversationCards = [],
  processCards = [],
  commandCards = [],
  missionPhase = "idle",
  turnActive = false,
  statusCard = null,
  approvalItems = [],
} = {}) {
  const conversationItems = buildProtocolConversationItems(conversationCards);
  const rawCardById = new Map(asArray(conversationCards).map((card) => [compactText(card?.id), card]));
  const normalizedMessages = conversationItems
    .map((item) => messageCardFromConversationItem(item, rawCardById.get(compactText(item.id))))
    .filter((item) => compactText(item.card?.text));

  const buckets = [];
  let currentBucket = null;

  for (const message of normalizedMessages) {
    if (message.role === "user") {
      pushTurn(buckets, currentBucket);
      currentBucket = createTurnBucket(message);
      continue;
    }
    if (!currentBucket) {
      currentBucket = createTurnBucket(message);
    } else {
      currentBucket.assistantMessages.push(message);
    }
  }
  pushTurn(buckets, currentBucket);

  return buckets.map((bucket, index) => {
    const isCurrentTurn = index === buckets.length - 1;
    const isActiveTurn = isCurrentTurn && Boolean(turnActive) && ACTIVE_PHASES.has(compactText(missionPhase).toLowerCase());
    const assistantMessages = asArray(bucket.assistantMessages);
    const finalMessage = isActiveTurn ? null : assistantMessages[assistantMessages.length - 1] || null;
    const assistantProcessMessages = isActiveTurn ? assistantMessages : assistantMessages.slice(0, -1);
    const activeProcessCards = isCurrentTurn ? asArray(processCards) : [];
    const activeCommandCards = isCurrentTurn ? asArray(commandCards) : [];
    const processItems = sortProcessDisplayItems([
      ...buildAssistantProcessItems(assistantProcessMessages, {
        exclude: isProtocolAssistantProcessRedundant,
      }),
      ...buildCommandProcessItems(activeCommandCards),
      ...buildProcessLineItems(activeProcessCards, {
        exclude: isProtocolProcessCardRedundant,
      }),
    ]);
    const liveHint = isActiveTurn
      ? deriveLiveHint({
          statusCard,
          missionPhase,
          approvalItems,
        })
      : "";
    const timestamps = [
      bucket.userMessage?.updatedAt,
      bucket.userMessage?.createdAt,
      finalMessage?.updatedAt,
      finalMessage?.createdAt,
      ...assistantProcessMessages.flatMap((message) => [message.updatedAt, message.createdAt]),
      ...activeCommandCards.flatMap((card) => [card?.updatedAt, card?.createdAt]),
      ...activeProcessCards.flatMap((card) => [card?.updatedAt, card?.createdAt]),
    ];
    const firstTimestamp = parseTimestamp(bucket.userMessage?.createdAt || bucket.userMessage?.updatedAt || assistantMessages[0]?.createdAt);
    const lastTimestamp = latestTimestamp(timestamps);
    const elapsedLabel = firstTimestamp && lastTimestamp && lastTimestamp >= firstTimestamp
      ? formatDurationLabel(lastTimestamp - firstTimestamp)
      : "";
    const normalizedPhase = compactText(missionPhase).toLowerCase();
    const processLabel = [isActiveTurn ? activeProcessLabel(normalizedPhase) : "已处理", elapsedLabel].filter(Boolean).join(" ");
    const summary = summarizeProtocolTurnProcess({
      processItems,
      missionPhase: normalizedPhase,
      activeTurn: isActiveTurn,
      liveHint,
    });
    const turnSurfaces = collectTurnMcpSurfaceGroups(assistantMessages.map((message) => message.sourceCard).filter(Boolean));

    return {
      id: bucket.id,
      anchorMessageId: bucket.userMessage?.id || assistantMessages[0]?.id || bucket.id,
      messageIds: [bucket.userMessage?.id, ...assistantMessages.map((message) => message.id)].filter(Boolean),
      userMessage: bucket.userMessage,
      finalMessage,
      processItems,
      processLabel,
      finalLabel: finalMessage && (processItems.length || liveHint) ? "最终消息" : "",
      liveHint,
      summary,
      collapsedByDefault: !isActiveTurn && Boolean(processItems.length),
      active: isActiveTurn,
      phase: normalizedPhase,
      resultAttachments: turnSurfaces.resultAttachments,
      actionSurfaces: turnSurfaces.actionSurfaces,
      workspaceSurfaces: turnSurfaces.workspaceSurfaces,
      resultBundles: turnSurfaces.resultBundles,
      actionBundles: turnSurfaces.actionBundles,
    };
  });
}

export function isChatConversationCard(card = {}) {
  return isUserMessageCard(card) || isAssistantMessageCard(card);
}

export function formatMainChatTurns({
  conversationCards = [],
  turnActive = false,
  activeProcess = null,
} = {}) {
  const normalizedMessages = asArray(conversationCards)
    .map((card) => messageCardFromRawCard(card))
    .filter((message) => compactText(message.card?.text));

  const buckets = [];
  let currentBucket = null;

  for (const message of normalizedMessages) {
    if (message.role === "user") {
      pushTurn(buckets, currentBucket);
      currentBucket = createTurnBucket(message);
      continue;
    }
    if (!currentBucket) {
      currentBucket = createTurnBucket(message);
    } else {
      currentBucket.assistantMessages.push(message);
    }
  }
  pushTurn(buckets, currentBucket);

  return buckets.map((bucket, index) => {
    const isCurrentTurn = index === buckets.length - 1;
    const isActiveTurn = isCurrentTurn && Boolean(turnActive);
    const assistantMessages = asArray(bucket.assistantMessages);
    const finalMessage = isActiveTurn ? null : assistantMessages[assistantMessages.length - 1] || null;
    const assistantProcessMessages = isActiveTurn ? assistantMessages : assistantMessages.slice(0, -1);
    const activityProcessItems = isActiveTurn
      ? asArray(activeProcess?.items).map((item, itemIndex) => ({
          id: compactText(item?.id || `activity-${itemIndex}`),
          kind: compactText(item?.kind || "activity"),
          processKind: inferProcessKind(`${item?.kind || ""} ${item?.text || item?.label || item?.value || ""}`),
          text: String(item?.text || item?.label || item?.value || "").trim(),
          detail: String(item?.detail || "").trim(),
          time: compactText(item?.time),
          hostId: compactText(item?.hostId),
          tone: compactText(item?.tone || "neutral"),
          status: compactText(item?.status),
          sortTimestamp: item?.updatedAt || item?.createdAt || "",
        })).filter((item) => item.text)
      : [];
    const processItems = sortProcessDisplayItems([
      ...buildAssistantProcessItems(assistantProcessMessages),
      ...activityProcessItems,
    ]);
    const liveHint = isActiveTurn ? compactText(activeProcess?.liveHint || activeProcess?.hint || "") : "";
    const summary = summarizeMainChatProcess({
      processItems,
      activeProcess,
      liveHint,
    });
    const timestamps = [
      bucket.userMessage?.updatedAt,
      bucket.userMessage?.createdAt,
      finalMessage?.updatedAt,
      finalMessage?.createdAt,
      ...assistantProcessMessages.flatMap((message) => [message.updatedAt, message.createdAt]),
    ];
    const firstTimestamp = parseTimestamp(bucket.userMessage?.createdAt || bucket.userMessage?.updatedAt || assistantMessages[0]?.createdAt);
    const lastTimestamp = latestTimestamp(timestamps);
    const elapsedLabel = firstTimestamp && lastTimestamp && lastTimestamp >= firstTimestamp
      ? formatDurationLabel(lastTimestamp - firstTimestamp)
      : "";
    const phase = compactText(activeProcess?.phase || "");
    const turnSurfaces = collectTurnMcpSurfaceGroups(assistantMessages.map((message) => message.sourceCard).filter(Boolean));

    return {
      id: bucket.id,
      anchorMessageId: bucket.userMessage?.id || assistantMessages[0]?.id || bucket.id,
      messageIds: [bucket.userMessage?.id, ...assistantMessages.map((message) => message.id)].filter(Boolean),
      userMessage: bucket.userMessage,
      finalMessage,
      processItems,
      processLabel: [isActiveTurn ? activeProcessLabel(phase) : "已处理", elapsedLabel].filter(Boolean).join(" "),
      finalLabel: finalMessage && (processItems.length || liveHint) ? "最终消息" : "",
      liveHint,
      summary,
      collapsedByDefault: !isActiveTurn && Boolean(processItems.length),
      active: isActiveTurn,
      phase,
      resultAttachments: turnSurfaces.resultAttachments,
      actionSurfaces: turnSurfaces.actionSurfaces,
      workspaceSurfaces: turnSurfaces.workspaceSurfaces,
      resultBundles: turnSurfaces.resultBundles,
      actionBundles: turnSurfaces.actionBundles,
    };
  });
}
