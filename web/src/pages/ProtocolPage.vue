<script setup>
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import CardItem from "../components/CardItem.vue";
import {
  protocolDispatchSummaryView,
  protocolBoardNodes,
  protocolDagPaths,
  protocolHostCards,
  protocolHostDetails,
  protocolMainAgentMessages,
  protocolMission,
  protocolPlanDetailView,
  protocolPlanSummaryView,
  protocolRelayThreads,
  toneClass,
} from "../data/opsWorkspace";

const router = useRouter();

const hostCards = ref(
  protocolHostCards.map((card) => ({
    ...card,
    approval: card.approval
      ? {
          ...card.approval,
          previewLines: [...card.approval.previewLines],
        }
      : null,
  })),
);

const hostDetails = ref(
  Object.fromEntries(
    Object.entries(protocolHostDetails).map(([hostId, detail]) => [
      hostId,
      {
        ...detail,
        terminal: {
          ...detail.terminal,
          output: [...detail.terminal.output],
          history: detail.terminal.history.map((item) => ({ ...item })),
        },
        conversation: detail.conversation.map((item) => ({ ...item })),
      },
    ]),
  ),
);

const selectedHostId = ref(hostCards.value[0]?.hostId || "");
const isHostDrawerOpen = ref(false);
const isPlanDetailOpen = ref(false);
const isPlannerTraceOpen = ref(false);
const isDispatchDetailOpen = ref(false);
const focusedDispatchHostId = ref(protocolRelayThreads[0]?.hostId || "");
const approvalDetailHostId = ref("");
const composerDraft = ref("批准 web-08 的当前 reload 命令后，再把第二批 canary 节点放进来观察。");
const commandNotifications = ref([
  { id: "n1", time: "19:45", tone: "warning", text: "10.10.4.28 执行 sudo nginx -s reload 等待审批" },
  { id: "n2", time: "19:45", tone: "success", text: "10.10.4.27 执行 sudo nginx -s reload 已完成" },
  { id: "n3", time: "19:45", tone: "info", text: "10.10.4.29 执行健康探针，当前 3 / 5 已完成" },
  { id: "n4", time: "19:44", tone: "neutral", text: "10.10.6.24 等待 dispatch，尚未开始执行" },
]);

const selectedHostCard = computed(
  () => hostCards.value.find((card) => card.hostId === selectedHostId.value) || hostCards.value[0] || null,
);

const selectedHostDetail = computed(() => {
  const card = selectedHostCard.value;
  if (!card) return null;
  const detail = hostDetails.value[card.hostId] || null;
  if (!detail) return null;
  return {
    ...card,
    ...detail,
  };
});

const planSummaryView = computed(() => protocolPlanSummaryView);
const planDetailView = computed(() => protocolPlanDetailView);
const dispatchSummaryView = computed(() => protocolDispatchSummaryView);

const approvalCard = computed(() => {
  if (approvalDetailHostId.value) {
    return hostCards.value.find((card) => card.hostId === approvalDetailHostId.value) || null;
  }
  return hostCards.value.find((card) => card.approval) || null;
});

const approvalDetail = computed(() => {
  const card = approvalCard.value;
  if (!card) return null;
  return {
    ...card,
    detail: hostDetails.value[card.hostId] || null,
  };
});

const dispatchCards = computed(() =>
  protocolRelayThreads.map((thread) => ({
    id: thread.id,
    hostId: thread.hostId,
    host: thread.host,
    summary: thread.request.summary,
    status: thread.status,
    tone: thread.tone,
  })),
);

const focusedDispatchThread = computed(
  () => protocolRelayThreads.find((thread) => thread.hostId === focusedDispatchHostId.value) || protocolRelayThreads[0] || null,
);

const dispatchHostDetailView = computed(() => {
  const thread = focusedDispatchThread.value;
  if (!thread) return null;
  return {
    ...thread,
    waitingApproval: thread.status === "等待审批",
    latestReceipt: thread.events[thread.events.length - 1] || null,
  };
});

const workerReadonlyDetailView = computed(() => {
  const detail = selectedHostDetail.value;
  if (!detail) return null;
  return {
    ...detail,
    mode: "readonly",
    jumpLabel: "切到单机对话",
  };
});

const conversationCards = computed(() =>
  protocolMainAgentMessages.map((message) => {
    if (message.role === "system") {
      return {
        id: message.id,
        role: "assistant",
        card: {
          id: message.id,
          type: "NoticeCard",
          text: message.body,
        },
      };
    }

    return {
      id: message.id,
      role: message.role,
      card: {
        id: message.id,
        type: message.role === "user" ? "UserMessageCard" : "AssistantMessageCard",
        role: message.role,
        text: message.title ? `${message.title}\n\n${message.body}` : message.body,
      },
    };
  }),
);

const completionCards = computed(() =>
  hostCards.value
    .filter((card) => card.statusLabel !== "排队中")
    .slice(0, 4)
    .map((card) => ({
      id: `completion-${card.hostId}`,
      hostId: card.hostId,
      host: card.name,
      status: card.statusLabel,
      tone: card.tone,
      summary: card.latestOutput,
    })),
);

function openTerminal(hostId) {
  router.push(`/terminal/${hostId}`);
}

function openSingleHostChat(host) {
  closeHostDrawer();
  if (!host) return;
  router.push({
    path: "/",
    query: {
      hostId: host.hostId,
      hostName: host.name,
      hostAddress: host.ip,
    },
  });
}

function openHostDrawer(hostId, options = {}) {
  selectedHostId.value = hostId;
  if (options.closeDispatch) {
    isDispatchDetailOpen.value = false;
  }
  isHostDrawerOpen.value = true;
}

function closeHostDrawer() {
  isHostDrawerOpen.value = false;
}

function openPlanDetail() {
  isPlanDetailOpen.value = true;
}

function closePlanDetail() {
  isPlanDetailOpen.value = false;
}

function openPlannerTrace() {
  isPlannerTraceOpen.value = true;
}

function closePlannerTrace() {
  isPlannerTraceOpen.value = false;
}

function openDispatchDetail(hostId = protocolRelayThreads[0]?.hostId || "") {
  focusedDispatchHostId.value = hostId;
  isDispatchDetailOpen.value = true;
}

function closeDispatchDetail() {
  isDispatchDetailOpen.value = false;
}

function openApprovalDetail(hostId) {
  approvalDetailHostId.value = hostId;
}

function closeApprovalDetail() {
  approvalDetailHostId.value = "";
}

function appendConversation(hostId, message) {
  const detail = hostDetails.value[hostId];
  if (!detail) return;
  detail.conversation = [...detail.conversation, message];
}

function prependNotification(notification) {
  commandNotifications.value = [notification, ...commandNotifications.value].slice(0, 12);
}

function decideApproval(hostId, decision) {
  const card = hostCards.value.find((item) => item.hostId === hostId);
  const detail = hostDetails.value[hostId];
  if (!card) return;

  if (decision === "deny") {
    card.tone = "danger";
    card.statusLabel = "已拒绝";
    card.phase = "等待重规划";
    card.latestOutput = "当前命令已拒绝，主 Agent 正在生成替代方案。";
    card.approval = null;
    prependNotification({
      id: `notice-${hostId}-deny`,
      time: "19:46",
      tone: "danger",
      text: `${card.ip} 执行 ${card.latestCommand} 已拒绝`,
    });
    if (detail) {
      detail.terminal.output = [
        ...detail.terminal.output,
        "",
        "[decision] command denied",
        "main-agent is replanning a safer fallback path",
      ];
      appendConversation(hostId, {
        id: `${hostId}-deny`,
        role: "system",
        label: "系统",
        time: "19:46",
        body: "用户拒绝了当前命令，主 Agent 已收到回执并开始重规划。",
      });
    }
  } else {
    const grantedLabel = decision === "grant" ? "已放行" : "已批准";
    card.tone = "info";
    card.statusLabel = grantedLabel;
    card.phase = "重载执行中";
    card.latestOutput = "审批已通过，host-agent 正在继续执行 reload。";
    card.approval = null;
    prependNotification({
      id: `notice-${hostId}-${decision}`,
      time: "19:46",
      tone: "info",
      text: `${card.ip} 执行 sudo nginx -s reload ${decision === "grant" ? "已放行" : "已批准"}`,
    });
    if (detail) {
      detail.terminal.output = [
        ...detail.terminal.output,
        "",
        "web-08$ sudo nginx -s reload",
        "reload succeeded",
        "health probes queued",
      ];
      detail.terminal.history = [
        ...detail.terminal.history,
        { label: "19:46", command: "sudo nginx -s reload", result: "running" },
      ];
      appendConversation(hostId, {
        id: `${hostId}-${decision}`,
        role: "system",
        label: "系统",
        time: "19:46",
        body:
          decision === "grant"
            ? "已对当前命令放行，仅对这一次命令生效。"
            : "用户已批准当前命令，host-agent 正在继续执行。",
      });
    }
  }

  approvalDetailHostId.value = "";
}

function hostToneClass(card) {
  return toneClass(card.tone);
}

function streamRowClass(role) {
  return role === "user" ? "row-user" : "row-assistant";
}

function chatRoleClass(role) {
  switch (role) {
    case "user":
      return "is-user";
    case "assistant":
      return "is-assistant";
    case "system":
      return "is-system";
    case "host":
      return "is-host";
    default:
      return "";
  }
}
</script>

<template>
  <section class="ops-page">
    <div class="ops-page-inner protocol-compact-shell">
      <div class="protocol-compact-layout">
        <article class="ops-card protocol-chat-panel">
          <div class="protocol-panel-head">
            <div>
              <h3 class="ops-card-title">主 Agent</h3>
            </div>
            <div class="ops-actions">
              <span class="ops-pill is-info">{{ protocolMission.scope }}</span>
              <span class="ops-pill is-info">{{ protocolMission.projectionMode }}</span>
              <span class="ops-pill is-warning">{{ protocolMission.approvalMode }}</span>
              <button class="ops-button ghost small" @click="openPlanDetail">查看计划详情</button>
            </div>
          </div>

          <div class="chat-container protocol-chat-container">
            <div class="chat-stream-inner protocol-chat-inner">
              <div class="chat-stream">
                <template v-for="entry in conversationCards" :key="entry.id">
                  <div class="stream-row" :class="streamRowClass(entry.role)">
                    <CardItem :card="entry.card" />
                  </div>

                  <div v-if="entry.id === 'assistant-2'" class="stream-row row-assistant">
                    <section class="protocol-inline-stream-group">
                      <div class="protocol-section-top">
                        <span class="ops-subcard-label">前台投影入口</span>
                      </div>

                      <div class="protocol-summary-entry-list">
                        <button class="protocol-summary-entry" @click="openPlanDetail">
                          <div class="protocol-summary-entry-head">
                            <strong>{{ planSummaryView.label }}</strong>
                            <span class="ops-mini-pill" :class="toneClass(planSummaryView.tone)">{{ planSummaryView.status }}</span>
                          </div>
                          <p>{{ planSummaryView.caption }}</p>
                        </button>

                        <button class="protocol-summary-entry" @click="openDispatchDetail()">
                          <div class="protocol-summary-entry-head">
                            <strong>{{ dispatchSummaryView.label }}</strong>
                            <span class="ops-mini-pill" :class="toneClass(dispatchSummaryView.tone)">已受理 {{ dispatchSummaryView.accepted }}</span>
                          </div>
                          <p>{{ dispatchSummaryView.caption }}</p>
                        </button>
                      </div>
                    </section>
                  </div>

                  <div v-if="entry.id === 'assistant-2'" class="stream-row row-assistant">
                    <section class="protocol-inline-stream-group">
                      <div class="protocol-section-top">
                        <span class="ops-subcard-label">主 Agent 派发给子 agent</span>
                        <button class="ops-button ghost small" @click="openDispatchDetail()">查看派发详情</button>
                      </div>

                      <div class="protocol-inline-card-list">
                        <button
                          v-for="item in dispatchCards"
                          :key="item.id"
                          class="protocol-inline-card"
                          @click="openDispatchDetail(item.hostId)"
                        >
                          <strong>{{ item.host }}</strong>
                          <span class="protocol-inline-card-text">{{ item.summary }}</span>
                          <span class="ops-mini-pill" :class="toneClass(item.tone)">{{ item.status }}</span>
                        </button>
                      </div>
                    </section>
                  </div>

                  <div v-if="entry.id === 'system-1'" class="stream-row row-assistant">
                    <section class="protocol-inline-stream-group">
                      <div class="protocol-section-top">
                        <span class="ops-subcard-label">子 agent 完成情况</span>
                      </div>

                      <div class="protocol-inline-card-list">
                        <button
                          v-for="item in completionCards"
                          :key="item.id"
                          class="protocol-inline-card"
                          @click="openHostDrawer(item.hostId)"
                        >
                          <strong>{{ item.host }}</strong>
                          <span class="protocol-inline-card-text">{{ item.summary }}</span>
                          <span class="ops-mini-pill" :class="toneClass(item.tone)">{{ item.status }}</span>
                        </button>
                      </div>
                    </section>
                  </div>
                </template>
              </div>
            </div>
          </div>

          <div class="protocol-chat-dock">
            <div class="protocol-composer-box">
              <div class="protocol-composer-row">
                <textarea
                  v-model="composerDraft"
                  class="protocol-composer-input"
                  rows="1"
                  placeholder="要求后续变更"
                ></textarea>
              </div>
              <div class="protocol-composer-toolbar">
                <div class="protocol-composer-left">
                  <button class="protocol-composer-tool">+</button>
                  <span class="protocol-composer-tag">@ Fleet · {{ protocolMission.scope }}</span>
                </div>
                <div class="protocol-composer-right">
                  <button class="protocol-composer-tool-text">插入经验包</button>
                  <button class="protocol-composer-tool-text">追加主机</button>
                  <button class="protocol-composer-send">↑</button>
                </div>
              </div>
            </div>
          </div>
        </article>

        <aside class="protocol-compact-side">
          <article class="ops-card protocol-hosts-panel">
            <div class="protocol-panel-head">
              <div>
                <h3 class="ops-card-title">Host Agents</h3>
              </div>
            </div>

            <div class="protocol-host-list">
              <button
                v-for="card in hostCards"
                :key="card.hostId"
                class="protocol-host-row"
                :class="hostToneClass(card)"
                @click="openHostDrawer(card.hostId)"
              >
                <div class="protocol-host-row-top">
                  <div class="protocol-host-meta">
                    <strong>{{ card.name }}</strong>
                    <span>{{ card.ip }}</span>
                  </div>
                  <span class="ops-pill" :class="hostToneClass(card)">{{ card.statusLabel }}</span>
                </div>

                <div class="protocol-host-compact">
                  <p class="protocol-host-line protocol-host-command">{{ card.phase }} · {{ card.latestCommand }}</p>
                </div>

                <div v-if="card.approval" class="protocol-host-actions">
                  <button class="ops-button primary protocol-mini-btn" @click.stop="decideApproval(card.hostId, 'approve')">是</button>
                  <button class="ops-button ghost protocol-mini-btn" @click.stop="decideApproval(card.hostId, 'grant')">授权免审</button>
                  <button class="ops-button ghost protocol-mini-btn" @click.stop="decideApproval(card.hostId, 'deny')">否</button>
                  <button class="ops-button ghost protocol-mini-btn" @click.stop="openApprovalDetail(card.hostId)">详情</button>
                </div>
              </button>
            </div>
          </article>

          <article class="ops-card protocol-notice-panel">
            <div class="protocol-panel-head">
              <div>
                <h3 class="ops-card-title">命令通知</h3>
              </div>
            </div>

            <div class="protocol-notice-list">
              <article
                v-for="notice in commandNotifications"
                :key="notice.id"
                class="protocol-notice-item"
                :class="toneClass(notice.tone)"
              >
                <strong>{{ notice.time }}</strong>
                <p>{{ notice.text }}</p>
              </article>
            </div>
          </article>
        </aside>
      </div>
    </div>

    <div v-if="isPlanDetailOpen" class="protocol-overlay" @click.self="closePlanDetail">
      <div class="protocol-modal">
        <div class="protocol-modal-head">
          <div>
            <h3>{{ planDetailView.title }}</h3>
            <p class="ops-detail-copy">默认展示结构化过程，原始 Planner 轨迹作为第二层入口查看。</p>
          </div>
          <div class="ops-actions">
            <button class="ops-button ghost" @click="openPlannerTrace">查看原始 Planner 轨迹</button>
            <button class="ops-button ghost" @click="closePlanDetail">关闭</button>
          </div>
        </div>

        <div class="protocol-plan-grid">
          <section class="protocol-plan-summary-card">
            <div class="protocol-plan-summary-head">
              <div>
                <span class="ops-subcard-label">计划摘要</span>
                <h4>{{ planDetailView.goal }}</h4>
              </div>
              <div class="protocol-plan-summary-meta">
                <span class="ops-pill is-info">{{ planDetailView.version }}</span>
                <span class="ops-pill">{{ planDetailView.generatedAt }}</span>
              </div>
            </div>
            <div class="protocol-plan-meta-grid">
              <article>
                <span class="ops-subcard-label">前台会话</span>
                <strong>{{ planDetailView.ownerSessionLabel }}</strong>
              </article>
              <article>
                <span class="ops-subcard-label">后台 PlannerSession</span>
                <strong>{{ planDetailView.plannerSessionLabel }}</strong>
                <p>{{ planDetailView.rawPlannerTraceRef.threadId }}</p>
              </article>
              <article>
                <span class="ops-subcard-label">DAG 摘要</span>
                <strong>{{ planDetailView.dagSummary.nodes }} 个节点</strong>
                <p>运行中 {{ planDetailView.dagSummary.running }} · 待审批 {{ planDetailView.dagSummary.waitingApproval }} · 排队 {{ planDetailView.dagSummary.queued }}</p>
              </article>
            </div>
          </section>

          <section class="protocol-plan-detail-block">
            <div class="protocol-section-top">
              <span class="ops-subcard-label">结构化过程</span>
            </div>
            <div class="protocol-plan-process-list">
              <article v-for="item in planDetailView.structuredProcess" :key="item.id" class="protocol-plan-process-card">
                <div class="protocol-plan-process-head">
                  <strong>{{ item.label }}</strong>
                  <span class="ops-mini-pill">{{ item.risk }}风险</span>
                </div>
                <p>{{ item.summary }}</p>
                <div class="protocol-detail-chip-list">
                  <span v-for="host in item.hosts" :key="`${item.id}-${host}`" class="protocol-detail-chip">{{ host }}</span>
                </div>
                <div class="protocol-detail-meta-list">
                  <div v-if="item.mcpHits.length">
                    <span class="ops-subcard-label">监控 MCP</span>
                    <ul>
                      <li v-for="hit in item.mcpHits" :key="hit">{{ hit }}</li>
                    </ul>
                  </div>
                  <div v-if="item.skillHits.length">
                    <span class="ops-subcard-label">Skills</span>
                    <ul>
                      <li v-for="hit in item.skillHits" :key="hit">{{ hit }}</li>
                    </ul>
                  </div>
                  <div v-if="item.packHits.length">
                    <span class="ops-subcard-label">经验包</span>
                    <ul>
                      <li v-for="hit in item.packHits" :key="hit">{{ hit }}</li>
                    </ul>
                  </div>
                </div>
              </article>
            </div>
          </section>

          <section class="protocol-plan-detail-block">
            <div class="protocol-section-top">
              <span class="ops-subcard-label">监控 MCP / Skills / 经验包命中</span>
            </div>
            <div class="protocol-plan-meta-grid">
              <article>
                <span class="ops-subcard-label">监控 MCP</span>
                <ul class="protocol-detail-list">
                  <li v-for="hit in planDetailView.contextHits.mcps" :key="hit">{{ hit }}</li>
                </ul>
              </article>
              <article>
                <span class="ops-subcard-label">Skills</span>
                <ul class="protocol-detail-list">
                  <li v-for="hit in planDetailView.contextHits.skills" :key="hit">{{ hit }}</li>
                </ul>
              </article>
              <article>
                <span class="ops-subcard-label">经验包</span>
                <ul class="protocol-detail-list">
                  <li v-for="hit in planDetailView.contextHits.packs" :key="hit">{{ hit }}</li>
                </ul>
              </article>
            </div>
          </section>

          <section class="protocol-plan-detail-block">
            <div class="protocol-section-top">
              <span class="ops-subcard-label">计划步骤</span>
            </div>
            <div class="protocol-plan-step-list">
              <article v-for="step in planDetailView.steps" :key="step.id" class="protocol-plan-step-card">
                <div class="protocol-plan-step-head">
                  <strong>{{ step.title }}</strong>
                  <span class="ops-mini-pill">{{ step.risk }}风险</span>
                </div>
                <p>{{ step.summary }}</p>
                <div class="protocol-plan-step-meta">
                  <span>目标主机：{{ step.hosts.join(" / ") }}</span>
                  <span>审批：{{ step.approvals }}</span>
                  <span>DAG：{{ step.dagNode }}</span>
                </div>
              </article>
            </div>
          </section>

          <section class="protocol-plan-detail-block">
            <div class="protocol-section-top">
              <span class="ops-subcard-label">任务拆分 DAG 摘要</span>
            </div>
            <div class="protocol-board-shell">
              <div class="protocol-board-canvas">
                <svg class="protocol-board-svg" viewBox="0 0 820 532" aria-hidden="true">
                  <path v-for="(path, index) in protocolDagPaths" :key="index" :d="path" />
                </svg>

                <div
                  v-for="node in protocolBoardNodes"
                  :key="node.id"
                  class="protocol-dag-node"
                  :class="`is-${node.status}`"
                  :style="{ left: `${node.x}px`, top: `${node.y}px`, width: `${node.w}px`, height: `${node.h}px` }"
                >
                  <div class="protocol-dag-node-top">
                    <strong>{{ node.label }}</strong>
                    <span>{{ node.detail }}</span>
                  </div>
                  <p>{{ node.summary }}</p>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>

    <div v-if="isPlannerTraceOpen" class="protocol-overlay protocol-overlay-front" @click.self="closePlannerTrace">
      <div class="protocol-modal protocol-trace-modal">
        <div class="protocol-modal-head">
          <div>
            <h3>{{ planDetailView.rawPlannerTraceRef.title }}</h3>
            <p class="ops-detail-copy">{{ planDetailView.rawPlannerTraceRef.sessionId }} · {{ planDetailView.rawPlannerTraceRef.threadId }}</p>
          </div>
          <button class="ops-button ghost" @click="closePlannerTrace">关闭</button>
        </div>

        <div class="protocol-trace-list">
          <article v-for="trace in planDetailView.rawPlannerTrace" :key="trace.id" class="protocol-trace-item">
            <div class="protocol-trace-item-head">
              <strong>{{ trace.title }}</strong>
              <span class="ops-mini-pill">{{ trace.kind }}</span>
            </div>
            <span class="protocol-trace-time">{{ trace.time }}</span>
            <p>{{ trace.detail }}</p>
          </article>
        </div>
      </div>
    </div>

    <div v-if="isDispatchDetailOpen && dispatchHostDetailView" class="protocol-overlay" @click.self="closeDispatchDetail">
      <div class="protocol-host-drawer protocol-dispatch-drawer">
        <div class="protocol-drawer-header">
          <div>
            <h3>派发详情</h3>
            <p class="ops-detail-copy">按主机维度查看主 Agent 派发的任务、约束和最近回执。</p>
            <div class="protocol-drawer-pills">
              <span class="ops-pill is-info">accepted {{ dispatchSummaryView.accepted }}</span>
              <span class="ops-pill">activated {{ dispatchSummaryView.activated }}</span>
              <span class="ops-pill">queued {{ dispatchSummaryView.queued }}</span>
            </div>
          </div>
          <div class="ops-actions">
            <button class="ops-button primary" @click="closeDispatchDetail">关闭</button>
          </div>
        </div>

        <div class="protocol-dispatch-layout">
          <aside class="protocol-dispatch-host-list">
            <button
              v-for="thread in protocolRelayThreads"
              :key="thread.id"
              class="protocol-dispatch-host-item"
              :class="{ active: focusedDispatchHostId === thread.hostId }"
              @click="focusedDispatchHostId = thread.hostId"
            >
              <div class="protocol-dispatch-host-top">
                <strong>{{ thread.host }}</strong>
                <span class="ops-mini-pill" :class="toneClass(thread.tone)">{{ thread.status }}</span>
              </div>
              <p>{{ thread.request.summary }}</p>
            </button>
          </aside>

          <section class="protocol-dispatch-detail-panel">
            <div class="protocol-dispatch-detail-head">
              <div>
                <span class="ops-subcard-label">当前主机</span>
                <h4>{{ dispatchHostDetailView.host }} · {{ dispatchHostDetailView.ip }}</h4>
              </div>
              <div class="ops-actions">
                <button class="ops-button ghost small" @click="openHostDrawer(dispatchHostDetailView.hostId, { closeDispatch: true })">查看只读详情</button>
              </div>
            </div>

            <div class="protocol-dispatch-detail-grid">
              <article class="protocol-approval-block">
                <span class="ops-subcard-label">任务标题</span>
                <strong>{{ dispatchHostDetailView.request.title }}</strong>
                <p class="ops-detail-copy">{{ dispatchHostDetailView.nodeLabel }}</p>
              </article>

              <article class="protocol-approval-block">
                <span class="ops-subcard-label">instruction</span>
                <p class="ops-detail-copy">{{ dispatchHostDetailView.request.summary }}</p>
              </article>

              <article class="protocol-approval-block">
                <span class="ops-subcard-label">当前状态</span>
                <strong>{{ dispatchHostDetailView.status }}</strong>
                <p class="ops-detail-copy">
                  {{ dispatchHostDetailView.waitingApproval ? "该主机已进入审批门槛，等待当前命令放行。" : "当前主机已受理任务，仍按 host 串行执行。"
                  }}
                </p>
              </article>

              <article class="protocol-approval-block">
                <span class="ops-subcard-label">constraints</span>
                <ul class="protocol-detail-list">
                  <li v-for="constraint in dispatchHostDetailView.request.constraints" :key="constraint">{{ constraint }}</li>
                </ul>
              </article>

              <article class="protocol-approval-block">
                <span class="ops-subcard-label">最近回执</span>
                <div class="protocol-dispatch-event-list">
                  <article v-for="event in dispatchHostDetailView.events" :key="event.id" class="protocol-dispatch-event-item">
                    <div class="protocol-dispatch-event-head">
                      <strong>{{ event.summary }}</strong>
                      <span>{{ event.time }}</span>
                    </div>
                    <p>{{ event.detail }}</p>
                  </article>
                </div>
              </article>
            </div>
          </section>
        </div>
      </div>
    </div>

    <div v-if="isHostDrawerOpen && workerReadonlyDetailView" class="protocol-overlay" @click.self="closeHostDrawer">
      <div class="protocol-host-drawer">
        <div class="protocol-drawer-header">
          <div>
            <h3>{{ workerReadonlyDetailView.name }} · {{ workerReadonlyDetailView.ip }}</h3>
            <p class="ops-detail-copy">WorkerSession 只读详情：展示 transcript、终端上下文和审批信息；如需继续对话，请切到单机对话页。</p>
            <div class="protocol-drawer-pills">
              <span class="ops-pill">{{ workerReadonlyDetailView.os }}</span>
              <span class="ops-pill">CPU {{ workerReadonlyDetailView.cpuSummary }}</span>
              <span class="ops-pill">内存 {{ workerReadonlyDetailView.memorySummary }}</span>
              <span class="ops-pill">磁盘 {{ workerReadonlyDetailView.diskSummary }}</span>
              <span class="ops-pill">agent {{ workerReadonlyDetailView.agentVersion }}</span>
            </div>
          </div>
          <div class="ops-actions">
            <button class="ops-button ghost" @click="openSingleHostChat(workerReadonlyDetailView)">{{ workerReadonlyDetailView.jumpLabel }}</button>
            <button class="ops-button ghost" :disabled="!workerReadonlyDetailView.terminalAvailable" @click="openTerminal(workerReadonlyDetailView.hostId)">打开终端</button>
            <button class="ops-button ghost" :disabled="!workerReadonlyDetailView.approval" @click="openApprovalDetail(workerReadonlyDetailView.hostId)">审批详情</button>
            <button class="ops-button primary" @click="closeHostDrawer">关闭</button>
          </div>
        </div>

        <div class="protocol-drawer-body">
          <div class="protocol-terminal-panel">
            <div class="protocol-terminal-command">
              <span class="ops-subcard-label">当前命令</span>
              <strong>{{ workerReadonlyDetailView.terminal.currentCommand }}</strong>
              <p class="ops-detail-copy">{{ workerReadonlyDetailView.latestOutput }}</p>
            </div>

            <div class="protocol-terminal-output">
              <pre>{{ workerReadonlyDetailView.terminal.output.join("\n") }}</pre>
            </div>

            <div class="protocol-terminal-history">
              <div v-for="item in workerReadonlyDetailView.terminal.history" :key="`${item.label}-${item.command}`" class="protocol-history-item">
                <span>{{ item.label }}</span>
                <code>{{ item.command }}</code>
                <strong>{{ item.result }}</strong>
              </div>
            </div>
          </div>

          <div class="protocol-conversation-panel">
            <div class="protocol-conversation-header">
              <h4>WorkerSession 只读会话</h4>
              <p class="ops-detail-copy">这里不允许直接继续给子 agent 发消息；如需交互，请切到单机对话页。</p>
            </div>

            <div class="protocol-conversation-list">
              <article
                v-for="message in workerReadonlyDetailView.conversation"
                :key="message.id"
                class="protocol-chat-bubble"
                :class="chatRoleClass(message.role)"
              >
                <div class="protocol-chat-bubble-top">
                  <strong>{{ message.label }}</strong>
                  <span>{{ message.time }}</span>
                </div>
                <p>{{ message.body }}</p>
              </article>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="approvalDetail && approvalDetail.approval" class="protocol-overlay protocol-overlay-front" @click.self="closeApprovalDetail">
      <div class="protocol-approval-modal">
        <div class="protocol-modal-head">
          <div>
            <h3>{{ approvalDetail.name }} · {{ approvalDetail.ip }}</h3>
            <p class="ops-detail-copy">审批默认在卡片上就地处理，只有细看时才打开这个弹窗。</p>
          </div>
          <button class="ops-button ghost" @click="closeApprovalDetail">关闭</button>
        </div>

        <div class="protocol-approval-grid">
          <div class="protocol-approval-block">
            <span class="ops-subcard-label">命令全文</span>
            <pre class="protocol-inline-command">{{ approvalDetail.approval.previewLines.join("\n") }}</pre>
          </div>

          <div class="protocol-approval-block">
            <span class="ops-subcard-label">执行理由</span>
            <p class="ops-detail-copy">{{ approvalDetail.detail?.approvalReason || "主 Agent 已将作用域收敛到当前命令。" }}</p>
          </div>

          <div class="protocol-approval-block">
            <span class="ops-subcard-label">最近终端上下文</span>
            <pre class="protocol-inline-command">{{ approvalDetail.detail?.terminal.output.slice(-4).join("\n") }}</pre>
          </div>
        </div>

        <div class="ops-card-actions">
          <button class="ops-button primary" @click="decideApproval(approvalDetail.hostId, 'approve')">是</button>
          <button class="ops-button ghost" @click="decideApproval(approvalDetail.hostId, 'grant')">授权免审</button>
          <button class="ops-button ghost" @click="decideApproval(approvalDetail.hostId, 'deny')">否</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* ===== 布局骨架 ===== */
.protocol-compact-shell {
  max-width: 1500px;
}

/* 覆盖全局 .ops-page 的 padding 和滚动 */
.ops-page:has(.protocol-compact-shell) {
  padding: 8px 16px 0;
  overflow: hidden;
  height: calc(100vh - 48px); /* 减去顶部导航栏高度 */
}

.protocol-compact-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 16px;
  align-items: stretch;
  height: calc(100vh - 64px); /* 整个布局撑满视口 */
  overflow: hidden;
}

.protocol-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 0;
  flex-shrink: 0;
}

/* ===== 三个主面板 ===== */
.protocol-chat-panel,
.protocol-hosts-panel,
.protocol-notice-panel {
  padding: 12px 14px;
}

.protocol-chat-panel {
  position: relative;
  padding: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.protocol-chat-panel > .protocol-panel-head {
  padding: 10px 14px;
  margin-bottom: 0;
  border-bottom: 1px solid #e5eaf1;
}

.protocol-chat-bubble {
  padding: 10px 14px;
  border-radius: 12px;
  border: 1px solid var(--border-color);
  background: #ffffff;
}

.protocol-chat-bubble.is-user {
  background: #f6f9ff;
  border-color: #d7e4ff;
}

.protocol-chat-bubble.is-system {
  background: #f8fafc;
  border-color: #e5eaf1;
}

.protocol-chat-bubble-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.protocol-chat-bubble-top strong {
  color: var(--text-main);
  font-size: 13px;
}

.protocol-chat-bubble-top span {
  font-size: 11px;
  color: var(--text-meta);
}

.protocol-chat-bubble h4 {
  margin: 4px 0 2px;
  font-size: 13px;
  color: var(--text-main);
}

.protocol-chat-bubble p {
  margin: 0;
  color: var(--text-subtle);
  font-size: 13px;
  line-height: 1.5;
}

.protocol-chat-container {
  flex: 1;
  overflow-y: auto;
  padding: 12px 14px;
}

.protocol-chat-inner {
  max-width: 860px;
}

.protocol-chat-dock {
  position: relative;
  flex-shrink: 0;
  padding: 8px 14px 10px;
  background: #ffffff;
  bottom: auto;
  left: auto;
  right: auto;
  width: auto;
}

.protocol-composer-box {
  max-width: 860px;
  border: 1px solid #e2e8f0;
  border-radius: 14px;
  background: #f8fafc;
  overflow: hidden;
  transition: border-color 0.2s;
}

.protocol-composer-box:focus-within {
  border-color: #94a3b8;
  background: #ffffff;
}

.protocol-composer-row {
  padding: 10px 14px 4px;
}

.protocol-composer-input {
  width: 100%;
  border: none;
  outline: none;
  background: transparent;
  font: inherit;
  font-size: 14px;
  line-height: 1.5;
  color: var(--text-main);
  resize: none;
  min-height: 22px;
  max-height: 120px;
}

.protocol-composer-input::placeholder {
  color: #94a3b8;
}

.protocol-composer-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px 6px;
}

.protocol-composer-left,
.protocol-composer-right {
  display: flex;
  align-items: center;
  gap: 4px;
}

.protocol-composer-tool {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  color: var(--text-subtle);
  font-size: 16px;
  cursor: pointer;
  transition: background 0.15s;
}

.protocol-composer-tool:hover {
  background: #f1f5f9;
}

.protocol-composer-tag {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 8px;
  border-radius: 6px;
  background: #eff6ff;
  color: #2563eb;
  font-size: 12px;
  font-weight: 500;
}

.protocol-composer-tool-text {
  display: inline-flex;
  align-items: center;
  height: 28px;
  padding: 0 10px;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: var(--text-subtle);
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s;
}

.protocol-composer-tool-text:hover {
  background: #f1f5f9;
  color: var(--text-main);
}

.protocol-composer-send {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  border: none;
  background: #0f172a;
  color: #ffffff;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
}

.protocol-composer-send:hover {
  opacity: 0.85;
}

.protocol-omnibar-wrapper {
  max-width: 860px;
}

.protocol-omnibar-tools {
  gap: 8px;
}

.protocol-mini-action {
  min-height: 28px;
  padding: 0 8px;
  font-size: 12px;
}

.protocol-inline-stream-group {
  width: min(100%, 820px);
  margin-left: 40px;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
}

.protocol-section-top {
  margin-bottom: 6px;
}

.protocol-inline-card-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.protocol-summary-entry-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.protocol-summary-entry {
  display: flex;
  flex-direction: column;
  gap: 5px;
  width: 100%;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid #dbe5f2;
  background: #ffffff;
  text-align: left;
  cursor: pointer;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.protocol-summary-entry:hover {
  transform: translateY(-1px);
  border-color: #c9d8eb;
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.06);
}

.protocol-summary-entry p {
  margin: 0;
  color: var(--text-subtle);
  font-size: 12px;
  line-height: 1.5;
}

.protocol-summary-entry-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.protocol-summary-entry-head strong {
  font-size: 13px;
}

.protocol-inline-card {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 6px 10px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  background: #ffffff;
  text-align: left;
  cursor: pointer;
}

.protocol-inline-card strong {
  color: var(--text-main);
  font-size: 12px;
  white-space: nowrap;
}

.protocol-inline-card-text {
  min-width: 0;
  font-size: 11px;
  line-height: 1.3;
  color: var(--text-subtle);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.protocol-dispatch-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.protocol-composer-panel {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid #e6ebf2;
}

.protocol-composer {
  width: 100%;
  border: 1px solid var(--border-color);
  border-radius: 18px;
  padding: 14px 16px;
  font: inherit;
  font-size: 14px;
  line-height: 1.65;
  color: var(--text-main);
  background: #f8fafc;
  resize: vertical;
  box-sizing: border-box;
}

.protocol-compact-side {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  overflow: hidden;
}

.protocol-host-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
  overflow-y: auto;
  padding-right: 4px;
}

.protocol-notice-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1;
  overflow-y: auto;
}

.protocol-hosts-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}

.protocol-notice-panel {
  flex: 0 0 auto;
  max-height: 220px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.protocol-host-row {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid var(--border-color);
  background: #ffffff;
  text-align: left;
  cursor: pointer;
}

.protocol-host-row.is-warning {
  background: #fff8ea;
  border-color: #f3d28a;
}

.protocol-host-row.is-info {
  background: #f6f9ff;
  border-color: #d8e4ff;
}

.protocol-host-row.is-success {
  background: #f5fcf7;
  border-color: #cfe9d6;
}

.protocol-host-row.is-danger {
  background: #fff3f2;
  border-color: #f1c4c0;
}

.protocol-host-row.is-neutral {
  background: #f8fafc;
}

.protocol-host-row-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.protocol-host-meta {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.protocol-host-meta strong {
  color: var(--text-main);
  font-size: 12px;
}

.protocol-host-meta span,
.protocol-host-line {
  font-size: 10.5px;
  color: var(--text-subtle);
}

.protocol-host-line {
  margin: 0;
  line-height: 1.3;
}

.protocol-host-compact {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.protocol-host-command {
  color: var(--text-main);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.protocol-host-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}

.protocol-mini-btn {
  min-height: 24px;
  padding: 0 8px;
  font-size: 10.5px;
}

.protocol-host-row :deep(.ops-pill),
.protocol-inline-card :deep(.ops-mini-pill) {
  min-height: 22px;
  padding: 0 7px;
  font-size: 10.5px;
}

.protocol-notice-item {
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  background: #ffffff;
}

.protocol-notice-item strong {
  display: block;
  margin-bottom: 2px;
  font-size: 11px;
  color: var(--text-meta);
}

.protocol-notice-item p {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-main);
}

.protocol-notice-item.is-warning {
  background: #fff8ea;
}

.protocol-notice-item.is-success {
  background: #f5fcf7;
}

.protocol-notice-item.is-info {
  background: #f6f9ff;
}

.protocol-notice-item.is-danger {
  background: #fff3f2;
}

.protocol-overlay {
  position: fixed;
  inset: 0;
  display: flex;
  justify-content: center;
  padding: 18px;
  background: rgba(15, 23, 42, 0.2);
  backdrop-filter: blur(6px);
  z-index: 30;
}

.protocol-overlay-front {
  z-index: 40;
}

.protocol-modal,
.protocol-approval-modal {
  width: min(860px, 100%);
  align-self: center;
  border-radius: 16px;
  border: 1px solid rgba(226, 232, 240, 0.92);
  background: rgba(255, 255, 255, 0.98);
  box-shadow: 0 24px 70px rgba(15, 23, 42, 0.18);
  padding: 20px;
  max-height: calc(100vh - 40px);
  overflow: auto;
}

.protocol-modal-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.protocol-modal-head h3 {
  margin: 0;
  font-size: 18px;
  color: var(--text-main);
}

.protocol-plan-grid {
  display: grid;
  gap: 12px;
}

.protocol-plan-summary-card,
.protocol-plan-detail-block,
.protocol-trace-item,
.protocol-dispatch-host-item,
.protocol-dispatch-event-item {
  border-radius: 12px;
  border: 1px solid #e5eaf1;
  background: #f8fafc;
}

.protocol-plan-summary-card,
.protocol-plan-detail-block {
  padding: 14px;
}

.protocol-plan-summary-head,
.protocol-plan-process-head,
.protocol-plan-step-head,
.protocol-dispatch-detail-head,
.protocol-dispatch-event-head,
.protocol-trace-item-head,
.protocol-dispatch-host-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.protocol-plan-summary-head h4,
.protocol-dispatch-detail-head h4 {
  margin: 4px 0 0;
  font-size: 15px;
  color: var(--text-main);
}

.protocol-plan-summary-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.protocol-plan-meta-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;
}

.protocol-plan-meta-grid article {
  padding: 10px 12px;
  border-radius: 10px;
  background: #ffffff;
  border: 1px solid #e5eaf1;
}

.protocol-plan-meta-grid strong {
  display: block;
  margin-top: 6px;
  color: var(--text-main);
  font-size: 13px;
}

.protocol-plan-meta-grid p {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-subtle);
}

.protocol-plan-process-list,
.protocol-plan-step-list,
.protocol-trace-list,
.protocol-dispatch-event-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.protocol-plan-process-card,
.protocol-plan-step-card,
.protocol-trace-item {
  padding: 12px;
  background: #ffffff;
}

.protocol-plan-process-card p,
.protocol-plan-step-card p,
.protocol-trace-item p {
  margin: 5px 0 0;
  color: var(--text-subtle);
  font-size: 13px;
  line-height: 1.6;
}

.protocol-detail-chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
}

.protocol-detail-chip {
  display: inline-flex;
  align-items: center;
  min-height: 20px;
  padding: 0 6px;
  border-radius: 999px;
  background: #eff6ff;
  color: #1d4ed8;
  font-size: 11px;
  font-weight: 600;
}

.protocol-detail-meta-list {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
  margin-top: 8px;
}

.protocol-detail-meta-list > div {
  padding: 8px;
  border-radius: 8px;
  background: #f8fafc;
  border: 1px solid #e5eaf1;
}

.protocol-detail-meta-list ul,
.protocol-detail-list {
  margin: 8px 0 0;
  padding-left: 18px;
  color: var(--text-subtle);
}

.protocol-detail-list li + li,
.protocol-detail-meta-list li + li {
  margin-top: 6px;
}

.protocol-plan-step-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 12px;
  color: var(--text-meta);
  font-size: 12px;
}

.protocol-trace-modal {
  width: min(760px, 100%);
}

.protocol-trace-item-head strong {
  color: var(--text-main);
}

.protocol-trace-time {
  display: inline-block;
  margin-top: 6px;
  font-size: 12px;
  color: var(--text-meta);
}

.protocol-board-shell {
  overflow-x: auto;
}

.protocol-board-canvas {
  position: relative;
  width: 820px;
  min-height: 532px;
}

.protocol-board-svg {
  position: absolute;
  inset: 0;
  width: 820px;
  height: 532px;
}

.protocol-board-svg path {
  fill: none;
  stroke: #d6e0ee;
  stroke-width: 2;
  stroke-linecap: round;
}

.protocol-dag-node {
  position: absolute;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid var(--border-color);
  background: #ffffff;
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.05);
  box-sizing: border-box;
}

.protocol-dag-node.is-completed {
  background: #f1fdf4;
  border-color: #b7ebca;
}

.protocol-dag-node.is-running {
  background: #f0f6ff;
  border-color: #c5dcff;
}

.protocol-dag-node.is-waiting_approval {
  background: #fff6e7;
  border-color: #f9cf8b;
}

.protocol-dag-node.is-queued,
.protocol-dag-node.is-pending {
  background: #f8fafc;
}

.protocol-dag-node-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.protocol-dag-node-top strong {
  font-size: 13px;
  color: var(--text-main);
}

.protocol-dag-node-top span {
  font-size: 11px;
  color: var(--text-meta);
}

.protocol-dag-node p {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: var(--text-subtle);
}

.protocol-host-drawer {
  width: min(88vw, 1440px);
  max-height: calc(100vh - 36px);
  overflow: hidden;
  border-radius: 16px;
  border: 1px solid rgba(226, 232, 240, 0.9);
  background: rgba(255, 255, 255, 0.96);
  box-shadow: 0 28px 80px rgba(15, 23, 42, 0.18);
  display: flex;
  flex-direction: column;
}

.protocol-dispatch-drawer {
  width: min(1060px, 100%);
}

.protocol-dispatch-layout {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  min-height: 0;
  flex: 1;
}

.protocol-dispatch-host-list {
  padding: 14px;
  border-right: 1px solid #e5eaf1;
  background: rgba(248, 250, 252, 0.85);
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow: auto;
}

.protocol-dispatch-host-item {
  width: 100%;
  padding: 10px 12px;
  text-align: left;
  cursor: pointer;
  background: #ffffff;
  transition: border-color 0.18s ease, transform 0.18s ease, box-shadow 0.18s ease;
}

.protocol-dispatch-host-item.active {
  border-color: #c7d8f2;
  box-shadow: 0 6px 16px rgba(37, 99, 235, 0.08);
  transform: translateX(2px);
}

.protocol-dispatch-host-item p {
  margin: 5px 0 0;
  color: var(--text-subtle);
  font-size: 12px;
  line-height: 1.5;
}

.protocol-dispatch-detail-panel {
  padding: 14px 18px;
  overflow: auto;
}

.protocol-dispatch-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;
}

.protocol-dispatch-event-item {
  padding: 10px 12px;
  background: #ffffff;
}

.protocol-dispatch-event-item p {
  margin: 5px 0 0;
  color: var(--text-subtle);
  font-size: 12px;
  line-height: 1.5;
}

.protocol-drawer-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding: 16px 18px 12px;
  border-bottom: 1px solid #e5eaf1;
}

.protocol-drawer-header h3 {
  margin: 0;
  font-size: 18px;
  color: var(--text-main);
}

.protocol-drawer-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.protocol-drawer-body {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(340px, 0.8fr);
  gap: 0;
  min-height: 0;
  flex: 1;
}

.protocol-terminal-panel,
.protocol-conversation-panel {
  min-width: 0;
  padding: 14px 16px;
}

.protocol-terminal-panel {
  border-right: 1px solid #e5eaf1;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.protocol-terminal-command,
.protocol-approval-block {
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid #e5eaf1;
  background: #f8fafc;
}

.protocol-terminal-command strong {
  display: block;
  margin-top: 5px;
  color: var(--text-main);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}

.protocol-terminal-output {
  min-height: 200px;
  border-radius: 10px;
  background: #0f172a;
  padding: 12px;
  overflow: auto;
}

.protocol-terminal-output pre {
  margin: 0;
  color: #e2e8f0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
}

.protocol-terminal-history {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.protocol-history-item {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr) auto;
  gap: 6px;
  align-items: center;
  padding: 6px 8px;
  border-radius: 6px;
  background: #f8fafc;
  border: 1px solid #e5eaf1;
}

.protocol-history-item span,
.protocol-history-item strong {
  font-size: 11px;
}

.protocol-history-item code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 11px;
  color: var(--text-main);
}

.protocol-conversation-header {
  margin-bottom: 8px;
}

.protocol-conversation-header h4 {
  margin: 0 0 2px;
  font-size: 14px;
  color: var(--text-main);
}

.protocol-conversation-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: calc(100vh - 220px);
  overflow-y: auto;
  padding-right: 4px;
}

.protocol-approval-grid {
  display: grid;
  gap: 8px;
  margin-bottom: 10px;
}

.protocol-inline-command {
  margin: 0;
  padding: 6px 8px;
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.04);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 11px;
  line-height: 1.5;
  color: var(--text-main);
  white-space: pre-wrap;
}

@media (max-width: 1439px) {
  .protocol-compact-layout {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 960px) {
  .protocol-panel-head,
  .protocol-modal-head,
  .protocol-drawer-header {
    flex-direction: column;
  }

  .protocol-inline-stream-group {
    margin-left: 0;
  }

  .protocol-summary-entry-list,
  .protocol-plan-meta-grid,
  .protocol-detail-meta-list,
  .protocol-dispatch-detail-grid,
  .protocol-dispatch-layout {
    grid-template-columns: minmax(0, 1fr);
  }

  .protocol-drawer-body {
    grid-template-columns: minmax(0, 1fr);
  }

  .protocol-dispatch-host-list {
    border-right: none;
    border-bottom: 1px solid #e5eaf1;
  }

  .protocol-terminal-panel {
    border-right: none;
    border-bottom: 1px solid #e5eaf1;
  }
}
</style>
