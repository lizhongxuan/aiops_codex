import { flushPromises, mount } from "@vue/test-utils";
import { nextTick, reactive, ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ProtocolConversationPane from "../src/components/protocol-workspace/ProtocolConversationPane.vue";
import { useChatHistoryPager } from "../src/composables/useChatHistoryPager";
import ProtocolWorkspacePage from "../src/pages/ProtocolWorkspacePage.vue";

const mocks = vi.hoisted(() => ({
  store: null,
}));
const DISMISSED_STATUS_BANNER_STORAGE_KEY = "codex:protocol-workspace-dismissed-status-banners:v1";

vi.mock("../src/store", () => ({
  useAppStore: () => mocks.store,
}));

function createPlanCard() {
  return {
    id: "workspace-plan-1",
    type: "PlanCard",
    title: "nginx 巡检计划",
    text: "巡检计划已生成，准备派发到 host-agent。",
    items: [
      {
        step: "web-01 [task-1] 采集 nginx 错误日志",
        status: "running",
      },
      {
        step: "web-02 [task-2] 执行 systemctl reload nginx",
        status: "waiting_approval",
      },
    ],
      detail: {
        goal: "帮我执行一轮全网 nginx 巡检，重点关注错误日志。",
        risk: "reload 前需要确认错误日志采集完成，避免掩盖现场。",
        validation: "确认 nginx error log 无新增 upstream timeout。",
        rollback: "如 reload 后异常，立即回滚到上一个 nginx 配置快照。",
        version: "plan-v3",
        structured_process: [
        "task-1 [running] @web-01 采集 nginx 错误日志",
        "task-2 [waiting_approval] @web-02 执行 systemctl reload nginx",
      ],
      task_host_bindings: [
        { taskId: "task-1", hostId: "web-01", status: "running", title: "采集 nginx 错误日志" },
        { taskId: "task-2", hostId: "web-02", status: "waiting_approval", title: "执行 systemctl reload nginx" },
      ],
      dispatch_events: [
        {
          id: "dispatch-1",
          createdAt: "2026-03-31T02:17:00Z",
          summary: "Dispatcher 下发任务",
          detail: "accepted 2, activated 1",
          hostId: "web-01",
          taskId: "task-1",
        },
      ],
    },
    createdAt: "2026-03-31T02:16:00Z",
    updatedAt: "2026-03-31T02:16:00Z",
  };
}

function createProcessCards() {
  return [
    {
      id: "process-web-01",
      type: "ProcessLineCard",
      title: "web-01",
      text: "正在分析 nginx 错误日志",
      summary: "采集错误日志并回传摘要",
      status: "inProgress",
      hostId: "web-01",
      kvRows: [
        { key: "主机", value: "web-01" },
        { key: "状态", value: "执行中" },
        { key: "任务", value: "采集 nginx 错误日志" },
      ],
      detail: {
        dispatch: {
          hostId: "web-01",
          request: {
            title: "采集 nginx 错误日志",
            summary: "journalctl -u nginx --since '-10m'",
          },
        },
        worker: {
          conversation: [
            {
              id: "worker-conv-1",
              createdAt: "2026-03-31T02:18:00Z",
              summary: "Host analysis",
              text: "检测到 3 条 upstream timeout，继续收集上下文。",
            },
          ],
          terminal: {
            status: "running",
            activeTaskId: "task-1",
            output: ["journalctl -u nginx --since '-10m'", "timeout on upstream service-a"],
          },
        },
      },
      createdAt: "2026-03-31T02:18:00Z",
      updatedAt: "2026-03-31T02:18:00Z",
    },
    {
      id: "process-web-02",
      type: "ProcessLineCard",
      title: "web-02",
      text: "等待 reload 审批",
      summary: "执行 systemctl reload nginx",
      status: "inProgress",
      hostId: "web-02",
      kvRows: [
        { key: "主机", value: "web-02" },
        { key: "状态", value: "等待审批/输入" },
        { key: "任务", value: "执行 systemctl reload nginx" },
      ],
      detail: {
        dispatch: {
          hostId: "web-02",
          request: {
            title: "执行 systemctl reload nginx",
            summary: "systemctl reload nginx",
          },
        },
        worker: {
          transcript: ["approval required before reload"],
          terminal: {
            status: "waiting_approval",
            activeTaskId: "task-2",
            output: ["approval required", "reload blocked until user decision"],
          },
        },
      },
      createdAt: "2026-03-31T02:19:00Z",
      updatedAt: "2026-03-31T02:19:00Z",
    },
  ];
}

function createCommandCards() {
  return [
    {
      id: "cmd-uptime-1",
      type: "CommandCard",
      title: "Command execution",
      command: "/bin/zsh -lc uptime",
      text: "11:19  3 users, load averages: 2.92 2.72 2.49",
      output: "11:19  3 users, load averages: 2.92 2.72 2.49\n",
      stdout: "11:19  3 users, load averages: 2.92 2.72 2.49\n",
      status: "completed",
      hostId: "server-local",
      cwd: "/Users/lizhongxuan/Desktop/aiops-codex",
      exitCode: 0,
      durationMs: 23,
      createdAt: "2026-04-08T03:19:00Z",
      updatedAt: "2026-04-08T03:19:00Z",
    },
  ];
}

function createApprovalCards() {
  return [
    {
      id: "approval-card-1",
      type: "CommandApprovalCard",
      status: "pending",
      hostId: "web-02",
      text: "需要批准 web-02 reload nginx",
      command: "systemctl reload nginx",
      approval: {
        requestId: "approval-1",
        decisions: ["accept", "accept_session", "decline"],
      },
      detail: {
        riskLevel: "high",
        targetSummary: "web-02 / service nginx",
        targetEnvironment: "prod / cn-prod-a",
        blastRadius: "web-02 / ingress",
        dryRunSupported: true,
        dryRunSummary: "systemctl reload nginx --test",
        rollbackHint: "如 reload 后异常，立即恢复上一个 nginx 配置并重启。",
        verifyStrategies: ["service_health", "log_check"],
        verificationSources: ["coroot_health", "metric_check", "health_probe", "log_check"],
      },
      createdAt: "2026-03-31T02:20:00Z",
      updatedAt: "2026-03-31T02:20:00Z",
    },
    {
      id: "approval-card-2",
      type: "CommandApprovalCard",
      status: "pending",
      hostId: "db-04",
      text: "需要批准 db-04 切主脚本",
      command: "failover-master.sh",
      approval: {
        requestId: "approval-2",
        decisions: ["accept", "decline"],
      },
      createdAt: "2026-03-31T02:21:00Z",
      updatedAt: "2026-03-31T02:21:00Z",
    },
  ];
}

function createVerificationRecords() {
  return [
    {
      id: "verify-approval-card-1",
      actionEventId: "approval-card-1",
      status: "passed",
      strategy: "service_health",
      successCriteria: ["执行 nginx -t", "确认 5xx 与 upstream error rate 稳定"],
      findings: ["reload 后服务健康检查通过。"],
      rollbackHint: "如异常，恢复上一个 nginx 配置并重新加载服务。",
      metadata: {
        approvalId: "approval-1",
        cardId: "approval-card-1",
        hostId: "web-02",
        hostName: "web-02",
        targetSummary: "web-02 / service nginx",
        verificationSources: ["coroot_health", "metric_check", "health_probe", "log_check"],
        endedAt: "2026-03-31T02:21:30Z",
      },
      createdAt: "2026-03-31T02:21:30Z",
    },
  ];
}

function createFailedVerificationRecords() {
  return [
    {
      id: "verify-approval-card-failed",
      actionEventId: "approval-card-failed",
      status: "failed",
      strategy: "health_probe",
      successCriteria: ["服务健康探针恢复", "错误日志不再新增"],
      findings: ["reload 后健康探针仍然失败。"],
      rollbackHint: "建议先恢复上一个 nginx 配置并重新加载服务。",
      metadata: {
        approvalId: "approval-failed",
        cardId: "approval-card-failed",
        hostId: "web-02",
        hostName: "web-02",
        targetSummary: "web-02 / service nginx",
        verificationSources: ["coroot_health", "metric_check", "health_probe", "log_check"],
        nextStepSuggestion: "先复核健康探针和错误日志，再决定是否立即回滚。",
        endedAt: "2026-03-31T02:23:30Z",
      },
      createdAt: "2026-03-31T02:23:30Z",
    },
  ];
}

function createVerificationSummaryCards() {
  return [
    {
      id: "verification-card-failed",
      type: "VerificationCard",
      title: "自动验证失败",
      text: "web-02 / service nginx 自动验证失败。\n\n结论：reload 后健康探针仍然失败。",
      summary: "web-02 / service nginx 自动验证失败。",
      status: "failed",
      hostId: "web-02",
      detail: {
        verificationId: "verify-approval-card-failed",
      },
      createdAt: "2026-03-31T02:23:30Z",
      updatedAt: "2026-03-31T02:23:30Z",
    },
    {
      id: "rollback-card-failed",
      type: "RollbackCard",
      title: "回滚建议",
      text: "建议先恢复上一个 nginx 配置并重新加载服务。\n\n下一步建议：先复核健康探针和错误日志，再决定是否立即回滚。",
      summary: "web-02 / service nginx",
      status: "warning",
      hostId: "web-02",
      detail: {
        verificationId: "verify-approval-card-failed",
      },
      createdAt: "2026-03-31T02:23:31Z",
      updatedAt: "2026-03-31T02:23:31Z",
    },
  ];
}

function createChoiceCard() {
  return {
    id: "choice-card-1",
    type: "ChoiceCard",
    requestId: "choice-1",
    title: "请选择处理方式",
    status: "pending",
    questions: [
      {
        header: "推荐方案",
        question: "你更希望先怎么处理 nginx 中间件？",
        isOther: true,
        options: [
          {
            label: "继续采集日志",
            value: "collect_more_logs",
          },
          {
            label: "推荐：重载并观察",
            value: "reload_observe",
            description: "适合配置已更新、希望先验证是否恢复的情况。",
          },
        ],
      },
    ],
    createdAt: "2026-03-31T02:20:30Z",
    updatedAt: "2026-03-31T02:20:30Z",
  };
}

function createSyntheticMcpSurfaceCards() {
  return [
    {
      id: "user-mcp-1",
      type: "UserMessageCard",
      role: "user",
      text: "请给我 nginx 的监控面板，并提供一个可审批的控制动作。",
      createdAt: "2026-04-03T12:30:00Z",
      updatedAt: "2026-04-03T12:30:00Z",
    },
    {
      id: "assistant-mcp-1",
      type: "AssistantMessageCard",
      role: "assistant",
      text: "我已为你聚合了监控面板，也准备了一个需要审批的控制动作。",
      payload: {
        actionSurfaces: [
          {
            id: "mcp-action-surface-1",
            placement: "inline_action",
            uiKind: "action_panel",
            source: "workspace",
            mcpServer: "metrics-prod",
            title: "MCP 控制面板",
            summary: "对 nginx 执行受控重启前，先进入右侧审批栏。",
            scope: {
              service: "nginx",
              env: "prod",
              hostId: "web-02",
            },
            freshness: {
              label: "刚拉取",
              capturedAt: "2026-04-03T12:30:05Z",
            },
            actions: [
              {
                id: "restart-nginx",
                label: "重启 nginx",
                intent: "restart_service",
                mutation: true,
                approvalMode: "required",
                confirmText: "确认后将把重启申请加入右侧审批栏。",
                permissionPath: "mcp.ops.service.restart",
                target: {
                  label: "web-02 / nginx",
                },
                params: {
                  service: "nginx",
                  host: "web-02",
                },
              },
            ],
          },
        ],
        resultBundles: [
          {
            id: "mcp-monitor-bundle-1",
            placement: "inline_final",
            bundleKind: "monitor_bundle",
            source: "workspace",
            mcpServer: "metrics-prod",
            summary: "nginx 监控聚合面板",
            subject: {
              type: "service",
              name: "nginx",
              env: "prod",
            },
            freshness: {
              label: "刚拉取",
              capturedAt: "2026-04-03T12:30:05Z",
            },
            sections: [
              {
                id: "overview-1",
                kind: "overview",
                title: "概览",
                cards: [
                  {
                    id: "overview-card-1",
                    uiKind: "readonly_summary",
                    title: "当前状态",
                    summary: "nginx 当前处于可观察状态。",
                  },
                ],
              },
              {
                id: "trends-1",
                kind: "trends",
                title: "趋势",
                cards: [
                  {
                    id: "trend-card-1",
                    uiKind: "readonly_chart",
                    title: "请求趋势",
                    summary: "请求量最近 5 分钟保持平稳。",
                  },
                ],
              },
            ],
          },
        ],
      },
      createdAt: "2026-04-03T12:30:10Z",
      updatedAt: "2026-04-03T12:30:10Z",
    },
  ];
}

function createHistoryTurns(count) {
  return Array.from({ length: count }, (_value, index) => ({
    id: `turn-${index + 1}`,
    anchorMessageId: `user-${index + 1}`,
    messageIds: [`user-${index + 1}`, `assistant-${index + 1}`],
    userMessage: {
      id: `user-${index + 1}`,
      card: {
        id: `user-${index + 1}`,
        type: "UserMessageCard",
        role: "user",
        text: `问题 ${index + 1}`,
      },
    },
    finalMessage: {
      id: `assistant-${index + 1}`,
      card: {
        id: `assistant-${index + 1}`,
        type: "AssistantMessageCard",
        role: "assistant",
        text: `结果 ${index + 1}`,
      },
    },
    processItems: [{ id: `process-${index + 1}`, text: `过程 ${index + 1}` }],
    processLabel: "已处理",
    finalLabel: "最终消息",
    liveHint: "",
    summary: "已记录 1 条过程细项",
    collapsedByDefault: true,
    active: false,
    phase: "completed",
  }));
}

function createStoreFixture(overrides = {}) {
  const { snapshot: snapshotOverrides = null, runtime: runtimeOverrides = null, ...restOverrides } = overrides;
  const state = reactive({
    snapshot: {
      kind: "workspace",
      sessionId: "workspace-1",
      selectedHostId: "server-local",
      auth: { connected: true, planType: "pro" },
      config: { codexAlive: true },
      hosts: [
        { id: "web-01", name: "web-01", address: "10.0.0.1", status: "online", executable: true },
        { id: "web-02", name: "web-02", address: "10.0.0.2", status: "online", executable: true },
        { id: "db-04", name: "db-04", address: "10.0.0.4", status: "online", executable: true },
      ],
      approvals: [
        { id: "approval-1", status: "pending", itemId: "approval-card-1" },
        { id: "approval-2", status: "pending", itemId: "approval-card-2" },
      ],
      verificationRecords: [],
      cards: [
        {
          id: "user-1",
          type: "UserMessageCard",
          role: "user",
          text: "帮我执行一轮全网的 nginx 巡检，重点关注错误日志。",
          createdAt: "2026-03-31T02:15:00Z",
          updatedAt: "2026-03-31T02:15:00Z",
        },
        {
          id: "assistant-1",
          type: "AssistantMessageCard",
          role: "assistant",
          text: "好的，我已经接管任务，正在为您编排执行计划。",
          createdAt: "2026-03-31T02:15:30Z",
          updatedAt: "2026-03-31T02:15:30Z",
        },
        createPlanCard(),
        ...createProcessCards(),
        ...createApprovalCards(),
      ],
    },
    runtime: {
      turn: {
        active: true,
        phase: "waiting_approval",
        pendingStart: false,
      },
      codex: {
        status: "connected",
      },
    },
    sessionList: [
      { id: "workspace-1", kind: "workspace", title: "Nginx workspace", preview: "巡检", status: "running" },
      { id: "workspace-0", kind: "workspace", title: "Yesterday", preview: "旧会话", status: "completed" },
    ],
    activeSessionId: "workspace-1",
    loading: false,
    sending: false,
    noticeMessage: "",
    errorMessage: "",
    fetchState: vi.fn(async () => true),
    fetchSessions: vi.fn(async () => true),
    createSession: vi.fn(async () => true),
    activateSession: vi.fn(async () => true),
    setTurnPhase: vi.fn((phase) => {
      state.runtime.turn.active = !["idle", "completed", "failed", "aborted"].includes(String(phase || ""));
      state.runtime.turn.phase = phase;
      state.runtime.turn.pendingStart = false;
    }),
    markTurnPendingStart: vi.fn((phase = "thinking") => {
      state.runtime.turn.active = false;
      state.runtime.turn.phase = phase;
      state.runtime.turn.pendingStart = true;
    }),
    clearTurnPendingStart: vi.fn(() => {
      state.runtime.turn.pendingStart = false;
    }),
    resetActivity: vi.fn(),
    ...restOverrides,
  });

  if (snapshotOverrides) {
    state.snapshot = reactive({
      ...state.snapshot,
      ...snapshotOverrides,
    });
  }

  if (runtimeOverrides) {
    state.runtime = reactive({
      ...state.runtime,
      ...runtimeOverrides,
      turn: {
        ...state.runtime.turn,
        ...(runtimeOverrides.turn || {}),
      },
      codex: {
        ...state.runtime.codex,
        ...(runtimeOverrides.codex || {}),
      },
    });
  }

  return state;
}

function mountPage() {
  return mount(ProtocolWorkspacePage, {
    global: {
      stubs: {
        teleport: true,
      },
    },
  });
}

function mountConversationPane(props = {}) {
  return mount(ProtocolConversationPane, {
    props: {
      title: "Protocol conversation",
      subtitle: "对话流、计划映射和后台执行状态",
      messages: [],
      formattedTurns: [],
      conversationCards: [],
      planCards: [],
      stepItems: [],
      backgroundAgents: [],
      runningAgents: [],
      planSummary: [],
      planSummaryLabel: "",
      statusCard: null,
      draft: "",
      draftPlaceholder: "继续输入需求、约束或补充说明",
      sending: false,
      busy: false,
      primaryActionOverride: "",
      showComposer: false,
      allowFollowUp: false,
      emptyLabel: "这里会显示主 Agent 的对话流。",
      starterCard: null,
      ...props,
    },
    global: {
      stubs: {
        Omnibar: { template: '<div class="omnibar-stub" />' },
        MessageCard: { template: '<div class="message-card-stub" />' },
        ThinkingCard: { template: '<div class="thinking-card-stub" />' },
        ProtocolInlinePlanWidget: { template: '<div class="protocol-inline-plan-widget-stub" />' },
        ProtocolBackgroundAgentsCard: { template: '<div class="protocol-background-agents-card-stub" />' },
        ProtocolTurnGroup: {
          props: ["turn"],
          template: `
            <article class="protocol-turn-group-stub" :data-testid="'protocol-turn-' + turn.id">
              <div class="protocol-turn-final-stub">{{ turn.finalMessage?.card?.text || "" }}</div>
            </article>
          `,
        },
      },
    },
  });
}

function findButtonByText(wrapper, text) {
  return wrapper.findAll("button").find((button) => button.text().includes(text));
}

async function expandPlanWidget(wrapper) {
  const summary = wrapper.find(".protocol-inline-plan-widget .plan-widget-summary");
  if (summary.exists() && !wrapper.find(".protocol-inline-plan-widget .plan-widget-body").exists()) {
    await summary.trigger("click");
    await flushPromises();
  }
}

describe("ProtocolWorkspacePage", () => {
  beforeEach(() => {
    window.localStorage?.removeItem(DISMISSED_STATUS_BANNER_STORAGE_KEY);
    mocks.store = createStoreFixture();
    global.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({}),
    }));
  });

  it("renders the new chat-first workspace layout with multiple approval cards", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-workspace-page"]').text()).toContain("待审批决策");
    expect(wrapper.text()).toContain("共 2 个任务，已完成 0 个");
    expect(wrapper.get('[data-testid="protocol-approval-approval-card-1"]').text()).toContain("systemctl reload nginx");
    expect(wrapper.get('[data-testid="protocol-approval-approval-card-2"]').text()).toContain("failover-master.sh");
    expect(wrapper.text()).toContain("web-01");
    expect(wrapper.text()).toContain("web-02");
  });

  it("renders lane-first runtime policy state and final-gate gaps in the workspace shell", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        currentMode: "analysis",
        currentStage: "planning",
        currentLane: "plan",
        requiredNextTool: "update_plan",
        finalGateStatus: "blocked",
        missingRequirements: ["缺少计划产物", "缺少 assumptions"],
        turnPolicy: {
          intentClass: "design",
          lane: "plan",
          requiredTools: ["update_plan"],
          requiredNextTool: "update_plan",
          finalGateStatus: "blocked",
          missingRequirements: ["缺少计划产物", "缺少 assumptions"],
          classificationReason: "检测到方案/设计类请求，直接进入 plan lane",
        },
        cards: [
          {
            id: "user-design-1",
            type: "UserMessageCard",
            role: "user",
            text: "给我一个订单服务延迟排障方案，要求有回滚和 10 分钟窗口。",
            createdAt: "2026-04-16T05:00:00Z",
            updatedAt: "2026-04-16T05:00:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "thinking",
          pendingStart: false,
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const policyCard = wrapper.get('[data-testid="protocol-runtime-policy"]');
    expect(policyCard.text()).toContain("方案规划中");
    expect(policyCard.text()).toContain("缺少计划产物");
    expect(policyCard.text()).toContain("缺少 assumptions");
    expect(policyCard.text()).toContain("计划更新");
    expect(wrapper.get('[data-testid="protocol-runtime-pill"]').text()).toContain("方案规划中");
  });

  it("opens the prompt debug drawer with tool visibility and prompt envelope details", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        currentMode: "analysis",
        currentStage: "collecting_evidence",
        currentLane: "readonly",
        requiredNextTool: "web_search",
        finalGateStatus: "blocked",
        missingRequirements: ["缺少外部实时证据"],
        turnPolicy: {
          intentClass: "factual",
          lane: "readonly",
          requiredTools: ["web_search"],
          requiredNextTool: "web_search",
          finalGateStatus: "blocked",
          missingRequirements: ["缺少外部实时证据"],
          classificationReason: "检测到实时/外部事实请求，必须先搜索证据",
        },
        promptEnvelope: {
          staticSections: [{ name: "System", content: "你是协作工作台 AI。" }],
          laneSections: [{ name: "Lane", content: "当前处于 readonly lane。" }],
          runtimePolicy: { name: "RuntimePolicy", content: "intentClass=factual\nlane=readonly" },
          contextAttachments: [{ name: "RequiredNextTool", content: "web_search" }],
          visibleTools: [{ name: "web_search", reason: "本轮 policy 必需工具" }],
          hiddenTools: [{ name: "orchestrator_dispatch_tasks", reason: "当前 lane=readonly，工具未对模型暴露" }],
          compressionState: "summary_only",
          tokenEstimate: 512,
          currentLane: "readonly",
          intentClass: "factual",
          finalGateStatus: "blocked",
          missingRequirements: ["缺少外部实时证据"],
        },
        cards: [
          {
            id: "user-factual-1",
            type: "UserMessageCard",
            role: "user",
            text: "最新 BTC 价格是多少？",
            createdAt: "2026-04-16T05:10:00Z",
            updatedAt: "2026-04-16T05:10:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "thinking",
          pendingStart: false,
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="protocol-prompt-debug-button"]').trigger("click");
    await flushPromises();

    const drawer = wrapper.get('[data-testid="protocol-prompt-debug-drawer"]');
    expect(drawer.text()).toContain("Prompt Debug");
    expect(drawer.text()).toContain("Runtime Policy");
    expect(drawer.text()).toContain("外部搜索");
    expect(drawer.text()).toContain("缺少外部实时证据");

    const promptContextTab = wrapper.findAll(".drawer-tab").find((button) => button.text().includes("Prompt Context"));
    expect(promptContextTab).toBeTruthy();
    await promptContextTab.trigger("click");
    await flushPromises();

    expect(drawer.text()).toContain("summary_only");

    const toolVisibilityTab = wrapper.findAll(".drawer-tab").find((button) => button.text().includes("Tool Visibility"));
    expect(toolVisibilityTab).toBeTruthy();
    await toolVisibilityTab.trigger("click");
    await flushPromises();

    expect(drawer.text()).toContain("orchestrator_dispatch_tasks");
  });

  it("does not render the incident summary module even when incident metadata exists", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        currentMode: "analysis",
        currentStage: "waiting_plan_approval",
        incidentEvents: [
          {
            id: "incident-stage-1",
            type: "stage.changed",
            stage: "waiting_plan_approval",
            summary: "planning -> waiting_plan_approval",
            createdAt: "2026-03-31T02:16:30Z",
          },
        ],
      },
    });
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="incident-summary-card"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Incident 协同状态");
  });

  it("dismisses the workspace failure banner without removing the error card", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        sessionId: "workspace-1",
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "看下 server-local 的主机状态",
            createdAt: "2026-04-08T02:00:00Z",
            updatedAt: "2026-04-08T02:00:00Z",
          },
          {
            id: "error-readonly",
            type: "ErrorCard",
            title: "Workspace readonly failed",
            text: "context deadline exceeded",
            message: "context deadline exceeded",
            status: "failed",
            createdAt: "2026-04-08T02:01:00Z",
            updatedAt: "2026-04-08T02:01:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "failed",
        },
        codex: {
          status: "connected",
        },
      },
    });
    const wrapper = mountPage();
    await flushPromises();

    const banner = wrapper.get(".workspace-status-banner.danger");
    expect(banner.text()).toContain("Workspace readonly failed");

    await banner.get('button[aria-label="关闭提示"]').trigger("click");
    await nextTick();

    expect(wrapper.find(".workspace-status-banner").exists()).toBe(false);
    expect(mocks.store.snapshot.cards.some((card) => card.id === "error-readonly")).toBe(true);
    expect(JSON.parse(window.localStorage.getItem(DISMISSED_STATUS_BANNER_STORAGE_KEY))).toContain("workspace-1:error-readonly");
  });

  it("renders the active mission as a process fold inside the main agent thread", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const processFold = wrapper.get('[data-testid="protocol-process-fold-turn-user-1"]');

    expect(wrapper.get('[data-testid="protocol-turn-turn-user-1"]').exists()).toBe(true);
    expect(processFold.text()).toContain("等待审批");
    expect(processFold.text()).toContain("db-04 正在等待审批");
    expect(processFold.text()).toContain("failover-master.sh");
    expect(processFold.text()).not.toContain("正在分析 nginx 错误日志");
    expect(processFold.text()).not.toContain("等待 reload 审批");
    expect(processFold.text()).not.toContain("执行 systemctl reload nginx");
  });

  it("renders evidence citation chips under final conclusions and opens evidence detail", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        evidenceSummaries: [
          {
            id: "evidence-nginx-timeout",
            citationKey: "E-EVIDENCE-NGINX-TIMEOUT",
            kind: "command_result",
            sourceKind: "command",
            sourceRef: "cmd-nginx-timeout",
            title: "nginx timeout 摘要",
            summary: "最近 5 分钟 service-a upstream timeout 抬升。",
            content: "error.log 中持续出现 upstream timed out while reading response header from upstream",
            createdAt: "2026-03-31T02:17:10Z",
          },
        ],
        cards: [
          {
            id: "user-evidence-1",
            type: "UserMessageCard",
            role: "user",
            text: "给我一个带证据引用的结论。",
            createdAt: "2026-03-31T02:17:00Z",
            updatedAt: "2026-03-31T02:17:00Z",
          },
          {
            id: "assistant-evidence-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论：nginx upstream timeout 来自 service-a。证据见 [E-EVIDENCE-NGINX-TIMEOUT]。",
            detail: {
              evidenceId: "evidence-nginx-timeout",
            },
            createdAt: "2026-03-31T02:17:20Z",
            updatedAt: "2026-03-31T02:17:20Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const evidenceStrip = wrapper.get('[data-testid="protocol-turn-evidence-turn-user-evidence-1"]');
    expect(evidenceStrip.text()).toContain("引用证据");

    const evidenceButton = wrapper.findAll("button").find((button) => button.text().includes("E-EVIDENCE-NGINX-TIMEOUT"));
    expect(evidenceButton).toBeTruthy();

    await evidenceButton.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("证据摘要 · E-EVIDENCE-NGINX-TIMEOUT");
    expect(modal.text()).toContain("nginx timeout 摘要");
    expect(modal.text()).toContain("最近 5 分钟 service-a upstream timeout 抬升");
    expect(modal.text()).toContain("error.log 中持续出现 upstream timed out");
  });

  it("can pin evidence detail into the drawer without changing the main thread body", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        evidenceSummaries: [
          {
            id: "evidence-nginx-timeout",
            citationKey: "E-EVIDENCE-NGINX-TIMEOUT",
            kind: "command_result",
            sourceKind: "command",
            sourceRef: "cmd-nginx-timeout",
            title: "nginx timeout 摘要",
            summary: "最近 5 分钟 service-a upstream timeout 抬升。",
            content: "error.log 中持续出现 upstream timed out while reading response header from upstream",
            createdAt: "2026-03-31T02:17:10Z",
          },
        ],
        cards: [
          {
            id: "user-evidence-pin",
            type: "UserMessageCard",
            role: "user",
            text: "把证据固定到侧栏里。",
            createdAt: "2026-03-31T02:17:00Z",
            updatedAt: "2026-03-31T02:17:00Z",
          },
          {
            id: "assistant-evidence-pin",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论：nginx upstream timeout 来自 service-a。证据见 [E-EVIDENCE-NGINX-TIMEOUT]。",
            detail: {
              evidenceId: "evidence-nginx-timeout",
            },
            createdAt: "2026-03-31T02:17:20Z",
            updatedAt: "2026-03-31T02:17:20Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const evidenceButton = wrapper.findAll("button").find((button) => button.text().includes("E-EVIDENCE-NGINX-TIMEOUT"));
    await evidenceButton.trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="protocol-evidence-pin"]').trigger("click");
    await flushPromises();

    expect(wrapper.find(".protocol-evidence-modal").exists()).toBe(false);
    const drawer = wrapper.get('[data-testid="protocol-evidence-drawer"]');
    expect(drawer.text()).toContain("证据摘要 · E-EVIDENCE-NGINX-TIMEOUT");
    expect(drawer.text()).toContain("nginx timeout 摘要");
    expect(drawer.text()).toContain("error.log 中持续出现 upstream timed out");
    expect(wrapper.text()).toContain("已固定到证据抽屉");
  });

  it("keeps background agents in the composer widget instead of repeating them inside the thread", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const thread = wrapper.get(".protocol-turn-stream");
    const backgroundCard = wrapper.get(".protocol-composer-widgets .protocol-background-agents-card");

    expect(backgroundCard.text()).toContain("后台 Agent");
    expect(backgroundCard.text()).toContain("web-01");
    expect(backgroundCard.text()).toContain("web-02");
    expect(thread.text()).not.toContain("采集错误日志并回传摘要");
    expect(thread.text()).not.toContain("执行 systemctl reload nginx");
  });

  it("opens an agent-centric detail modal when a background agent is selected", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const backgroundCard = wrapper.get(".protocol-composer-widgets .protocol-background-agents-card");
    const firstAgent = backgroundCard.get(".background-agent");
    await firstAgent.trigger("click");
    await nextTick();

    expect(wrapper.text()).toContain("BACKGROUND AGENT");
    expect(wrapper.text()).toContain("分配任务信息");
    expect(wrapper.text()).toContain("与 AI 的对话信息");
    expect(wrapper.text()).toContain("审核信息");
    expect(wrapper.text()).toContain("当前状态 / 最近活动");
    const detailText = wrapper.text();
    expect(
      detailText.includes("采集 nginx 错误日志") ||
        detailText.includes("执行 systemctl reload nginx"),
    ).toBe(true);
    expect(detailText.includes("Host analysis") || detailText.includes("approval required before reload")).toBe(true);
    expect(detailText.includes("执行中") || detailText.includes("等待审批") || detailText.includes("waiting_approval")).toBe(true);
    expect(wrapper.text()).not.toContain("执行详情 · agent-local");
    expect(wrapper.text()).not.toContain("执行详情 · web-01");
    expect(wrapper.text()).not.toContain("命令执行详情 · web-01");
  });

  it("renders pending choice cards above the protocol composer and submits structured follow-up", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "workspace",
        sessionId: "workspace-1",
        selectedHostId: "server-local",
        auth: { connected: true, planType: "pro" },
        config: { codexAlive: true },
        hosts: [{ id: "web-01", name: "web-01", address: "10.0.0.1", status: "online", executable: true }],
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "告诉我 nginx 当前更适合先做哪步。",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          {
            id: "assistant-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先给你一个结构化追问，确认后继续推进。",
            createdAt: "2026-03-31T02:15:30Z",
            updatedAt: "2026-03-31T02:15:30Z",
          },
          createChoiceCard(),
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_input",
        },
        codex: {
          status: "connected",
        },
      },
    });
    const wrapper = mountPage();
    await flushPromises();

    const choiceStack = wrapper.get('[data-testid="protocol-choice-stack"]');
    expect(choiceStack.text()).toContain("推荐：重载并观察");

    await choiceStack.get(".choice-note-toggle").trigger("click");
    await choiceStack.get(".choice-note-input").setValue("如果 reload，需要先避开业务高峰。");
    await choiceStack.get(".n-button--primary-type").trigger("click");

    expect(global.fetch).toHaveBeenCalledWith(
      "/api/v1/choices/choice-1/answer",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    );
    const [, request] = global.fetch.mock.calls[0];
    expect(JSON.parse(request.body)).toEqual({
      answers: [
        {
          value: "reload_observe",
          label: "推荐：重载并观察",
          isOther: false,
          note: "如果 reload，需要先避开业务高峰。",
        },
      ],
    });
    expect(mocks.store.fetchState).toHaveBeenCalled();
    expect(mocks.store.fetchSessions).toHaveBeenCalled();
  });

  it("prevents duplicate choice submissions while the first request is pending", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "workspace",
        sessionId: "workspace-1",
        selectedHostId: "server-local",
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "确认处理方式。",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          createChoiceCard(),
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_input",
        },
      },
    });
    let resolveFetch;
    global.fetch = vi.fn(() => new Promise((resolve) => {
      resolveFetch = resolve;
    }));
    const wrapper = mountPage();
    await flushPromises();

    const submit = wrapper.get(".n-button--primary-type");
    await submit.trigger("click");
    await nextTick();
    await submit.trigger("click");

    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(submit.attributes("disabled")).toBeDefined();

    resolveFetch({ ok: true, json: async () => ({}) });
    await flushPromises();
  });

  it("shows choice submit errors without clearing the selected option", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "workspace",
        sessionId: "workspace-1",
        selectedHostId: "server-local",
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "确认处理方式。",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          createChoiceCard(),
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_input",
        },
      },
    });
    global.fetch = vi.fn(async () => ({
      ok: false,
      json: async () => ({ error: "choice backend failed" }),
    }));
    const wrapper = mountPage();
    await flushPromises();

    const choiceStack = wrapper.get('[data-testid="protocol-choice-stack"]');
    await choiceStack.findAll(".n-radio")[1].trigger("click");
    await choiceStack.get(".n-button--primary-type").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("choice backend failed");
    expect(choiceStack.find(".n-radio--checked").exists()).toBe(true);
  });

  it("allows answering a waiting ask_user_question from the composer", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "workspace",
        sessionId: "workspace-1",
        selectedHostId: "server-local",
        auth: { connected: true, planType: "pro" },
        config: { codexAlive: true },
        hosts: [{ id: "server-local", name: "server-local", status: "online", executable: true }],
        approvals: [],
        agentLoop: {
          id: "loop-workspace-1",
          sessionId: "workspace-1",
          status: "waiting_user",
          mode: "answer",
        },
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "你有办法修复 pg 不同步的问题吗？",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          createChoiceCard(),
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_input",
          pendingStart: false,
        },
      },
    });
    const wrapper = mountPage();
    await flushPromises();

    const input = wrapper.get('[data-testid="omnibar-input"]');
    expect(input.attributes("placeholder")).toContain("等待澄清回答");
    await input.setValue("只问能力");

    const primary = wrapper.get('[data-testid="omnibar-primary-action"]');
    expect(primary.attributes("disabled")).toBeUndefined();
    await primary.trigger("click");

    expect(global.fetch).toHaveBeenCalledWith(
      "/api/v1/chat/message",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    );
    const [, request] = global.fetch.mock.calls[0];
    expect(JSON.parse(request.body)).toMatchObject({
      message: "只问能力",
    });
    expect(mocks.store.resetActivity).not.toHaveBeenCalled();
  });

  it("shows an unread pill instead of forcing scroll when new results arrive off-screen", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const scrollContainer = wrapper.get(".protocol-chat-container").element;
    let currentScrollTop = 520;
    const scrollTopWrites = [];
    Object.defineProperty(scrollContainer, "scrollHeight", { value: 1200, configurable: true });
    Object.defineProperty(scrollContainer, "clientHeight", { value: 400, configurable: true });
    Object.defineProperty(scrollContainer, "scrollTop", {
      get() {
        return currentScrollTop;
      },
      set(value) {
        currentScrollTop = value;
        scrollTopWrites.push(value);
      },
      configurable: true,
    });

    await wrapper.get(".protocol-chat-container").trigger("scroll");

    mocks.store.snapshot.cards.push({
      id: "assistant-2",
      type: "AssistantMessageCard",
      role: "assistant",
      text: "我又补充了一条执行结果，确认 web-01 的日志异常来自 service-a。",
      createdAt: "2026-03-31T02:21:30Z",
      updatedAt: "2026-03-31T02:21:30Z",
    });
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-unread-pill"]').text()).toContain("1 条新结果");
    expect(wrapper.get('[data-testid="protocol-unread-divider"]').text()).toContain("未读更新");
    expect(currentScrollTop).toBe(520);
    expect(scrollTopWrites).toHaveLength(0);

    await wrapper.get('[data-testid="protocol-unread-pill"]').trigger("click");
    await flushPromises();

    expect(currentScrollTop).toBe(1200);
    expect(scrollTopWrites.at(-1)).toBe(1200);
    expect(wrapper.find('[data-testid="protocol-unread-pill"]').exists()).toBe(false);
  });

  it("auto-activates the most recent workspace session when /protocol opens on a non-workspace session", async () => {
    const store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        cards: [],
        approvals: [],
      },
      runtime: {
        turn: {
          active: false,
          phase: "idle",
        },
        codex: {
          status: "connected",
        },
      },
      sessionList: [
        { id: "single-1", kind: "single_host", title: "普通会话", preview: "hello", status: "completed" },
        { id: "workspace-9", kind: "workspace", title: "最近工作台", preview: "巡检", status: "completed" },
      ],
    });
    store.fetchSessions = vi.fn(async () => true);
    store.activateSession = vi.fn(async (sessionId) => {
      store.activeSessionId = sessionId;
      store.snapshot.kind = "workspace";
      store.snapshot.sessionId = sessionId;
      return true;
    });
    mocks.store = store;

    const wrapper = mountPage();
    await flushPromises();

    expect(store.fetchSessions).toHaveBeenCalled();
    expect(store.activateSession).toHaveBeenCalledWith("workspace-9");
    expect(wrapper.text()).toContain("已切换到最近的协作工作台");
    expect(wrapper.text()).toContain("待审批决策");
  });

  it("auto-creates a workspace session when /protocol opens without an existing workspace", async () => {
    const store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        cards: [],
        approvals: [],
      },
      runtime: {
        turn: {
          active: false,
          phase: "idle",
        },
        codex: {
          status: "connected",
        },
      },
      sessionList: [{ id: "single-1", kind: "single_host", title: "普通会话", preview: "hello", status: "completed" }],
    });
    store.fetchSessions = vi.fn(async () => true);
    store.createSession = vi.fn(async () => {
      store.activeSessionId = "workspace-new";
      store.snapshot.kind = "workspace";
      store.snapshot.sessionId = "workspace-new";
      return true;
    });
    mocks.store = store;

    const wrapper = mountPage();
    await flushPromises();

    expect(store.fetchSessions).toHaveBeenCalled();
    expect(store.createSession).toHaveBeenCalledWith("workspace");
    expect(wrapper.text()).toContain("已自动创建新的协作工作台");
    expect(wrapper.text()).toContain("待审批决策");
  });

  it("shows an inline runtime status card in the conversation while the mission is planning", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "有哪些主机在线",
            createdAt: "2026-04-01T02:15:00Z",
            updatedAt: "2026-04-01T02:15:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "planning",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("正在规划步骤");
    expect(wrapper.text()).toContain("主 Agent 正在理解你的问题并生成 plan");
  });

  it("shows a direct-reply status without rendering the plan widget for simple conversation", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "你好",
            createdAt: "2026-04-01T02:15:00Z",
            updatedAt: "2026-04-01T02:15:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "thinking",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("正在思考");
    expect(wrapper.get('[data-testid="protocol-live-status-card"]').text()).toContain("正在思考");
    expect(wrapper.text()).not.toContain("工作台计划投影");
  });

  it("shows a live starter context instead of an empty placeholder when the workspace has no messages yet", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        cards: [],
        selectedHostId: "server-local",
      },
      runtime: {
        turn: {
          active: false,
          phase: "idle",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("server-local 已连接，工作台已就绪。");
    expect(wrapper.text()).toContain("可以直接问我当前状态，或者描述你想处理的问题。");
    expect(wrapper.text()).toContain("当前没有待审批操作。");
    expect(wrapper.text()).not.toContain("这里会显示主 Agent 的对话");
  });

  it("falls back to plan card items when structured_process is not ready yet", async () => {
    const store = createStoreFixture();
    const planCard = store.snapshot.cards.find((card) => card.type === "PlanCard");
    planCard.detail = {
      ...planCard.detail,
      structured_process: [],
      task_host_bindings: [],
    };
    mocks.store = store;

    const wrapper = mountPage();
    await flushPromises();
    await expandPlanWidget(wrapper);

    expect(wrapper.text()).toContain("共 2 个任务，已完成 0 个");
    expect(wrapper.text()).toContain("采集 nginx 错误日志");
    expect(wrapper.text()).toContain("执行 systemctl reload nginx");
  });

  it("renders a plan projection placeholder card before step mappings are fully synchronized", async () => {
    const store = createStoreFixture();
    const planCard = store.snapshot.cards.find((card) => card.type === "PlanCard");
    planCard.items = [];
    planCard.detail = {
      ...planCard.detail,
      structured_process: [],
      task_host_bindings: [],
    };
    mocks.store = store;

    const wrapper = mountPage();
    await flushPromises();
    await expandPlanWidget(wrapper);

    expect(wrapper.text()).toContain("nginx 巡检计划");
    expect(wrapper.text()).toContain("已收到计划投影");
    expect(wrapper.text()).toContain("step -> host-agent 映射");
    expect(wrapper.find(".protocol-inline-plan-widget .plan-host-pill").exists()).toBe(false);
  });

  it("renders the plan widget in the composer dock above the input", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find(".protocol-composer-widgets .protocol-inline-plan-widget").exists()).toBe(true);
    expect(wrapper.find(".protocol-composer-widgets .protocol-inline-plan-widget .plan-widget-body").exists()).toBe(false);
  });

  it("expands the plan widget with useful plan details and opens full plan evidence", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await expandPlanWidget(wrapper);

    const widget = wrapper.get(".protocol-composer-widgets .protocol-inline-plan-widget");
    expect(widget.text()).toContain("计划摘要");
    expect(widget.text()).toContain("风险");
    expect(widget.text()).toContain("验证");
    expect(widget.text()).toContain("回滚");
    expect(widget.text()).toContain("reload 前需要确认错误日志采集完成");
    expect(widget.text()).toContain("确认 nginx error log 无新增 upstream timeout");
    expect(widget.text()).not.toContain("PlannerSession");

    const fullPlanButton = wrapper.findAll(".plan-widget-action").find((button) => button.text().includes("查看完整计划"));
    expect(fullPlanButton).toBeTruthy();
    await fullPlanButton.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("主 Agent 计划摘要");
    expect(modal.text()).toContain("风险");
    expect(modal.text()).toContain("reload 前需要确认错误日志采集完成");
    expect(modal.text()).toContain("验证");
    expect(modal.text()).toContain("确认 nginx error log 无新增 upstream timeout");
    expect(modal.text()).toContain("回滚");
    expect(modal.text()).toContain("上一个 nginx 配置快照");
  });

  it("paginates protocol history from a compact boundary down to the session start", async () => {
    const turns = createHistoryTurns(6);

    const wrapper = mountConversationPane({
      formattedTurns: turns,
      showComposer: false,
    });
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-history-sentinel"]').text()).toContain("更早上下文已折叠 2 条消息");
    expect(wrapper.get('[data-testid="protocol-history-load-older"]').text()).toContain("加载更早消息");
    expect(wrapper.get('[data-testid="protocol-turn-turn-5"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="protocol-turn-turn-1"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="protocol-history-open"]').text()).toContain("查看完整历史");

    await wrapper.get('[data-testid="protocol-history-open"]').trigger("click");
    expect(wrapper.emitted("open-history")).toHaveLength(1);

    await wrapper.get(".protocol-chat-container").trigger("scroll");
    await wrapper.get('[data-testid="protocol-history-load-older"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-history-sentinel"]').text()).toContain("已到会话开头");
    expect(wrapper.get('[data-testid="protocol-turn-turn-1"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="protocol-history-load-older"]').exists()).toBe(false);
  });

  it("preserves the viewport offset when older protocol history is prepended", async () => {
    const items = ref(createHistoryTurns(6));
    let pager;
    const scrollContainer = ref({
      currentScrollTop: 520,
      clientHeight: 300,
      get scrollTop() {
        return this.currentScrollTop;
      },
      set scrollTop(value) {
        this.currentScrollTop = value;
      },
      get scrollHeight() {
        return (pager?.visibleItems.value.length || 0) * 100 + 600;
      },
    });

    pager = useChatHistoryPager({
      items,
      scrollContainer,
      initialCount: 4,
      pageSize: 2,
    });

    await pager.loadOlder();

    expect(scrollContainer.value.currentScrollTop).toBe(720);
    expect(pager.visibleItems.value.map((item) => item.id)).toEqual([
      "turn-1",
      "turn-2",
      "turn-3",
      "turn-4",
      "turn-5",
      "turn-6",
    ]);
  });

  it("virtualizes long protocol turn lists while keeping the latest window visible", async () => {
    const turns = Array.from({ length: 30 }, (_value, index) => ({
      id: `turn-${index + 1}`,
      anchorMessageId: `user-${index + 1}`,
      messageIds: [`user-${index + 1}`, `assistant-${index + 1}`],
      userMessage: {
        id: `user-${index + 1}`,
        card: {
          id: `user-${index + 1}`,
          type: "UserMessageCard",
          role: "user",
          text: `问题 ${index + 1}`,
        },
      },
      finalMessage: {
        id: `assistant-${index + 1}`,
        card: {
          id: `assistant-${index + 1}`,
          type: "AssistantMessageCard",
          role: "assistant",
          text: `结果 ${index + 1}`,
        },
      },
      processItems: [{ id: `process-${index + 1}`, text: `过程 ${index + 1}` }],
      processLabel: "已处理",
      finalLabel: "最终消息",
      liveHint: "",
      summary: "已记录 1 条过程细项",
      collapsedByDefault: true,
      active: false,
      phase: "completed",
    }));

    const wrapper = mountConversationPane({
      formattedTurns: turns,
      showComposer: false,
    });
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-history-sentinel"]').exists()).toBe(true);

    for (let loadCount = 0; loadCount < 3; loadCount += 1) {
      await wrapper.get('[data-testid="protocol-history-load-older"]').trigger("click");
      await flushPromises();
    }

    const scrollContainer = wrapper.get(".protocol-chat-container").element;
    Object.defineProperty(scrollContainer, "clientHeight", { value: 720, configurable: true });
    Object.defineProperty(scrollContainer, "scrollHeight", { value: 7200, configurable: true });
    Object.defineProperty(scrollContainer, "scrollTop", { value: 5400, writable: true, configurable: true });

    await wrapper.get(".protocol-chat-container").trigger("scroll");
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-turn-turn-30"]').exists()).toBe(true);
    expect(wrapper.findAll(".protocol-turn-group-stub").length).toBeLessThan(15);
    expect(wrapper.find('[data-testid="protocol-turn-turn-9"]').exists()).toBe(false);
  });

  it("keeps completed turns collapsed by default while leaving the final answer visible", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我汇总上一轮 nginx 巡检结果",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          {
            id: "assistant-1a",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先整理刚才收集到的证据。",
            createdAt: "2026-03-31T02:15:30Z",
            updatedAt: "2026-03-31T02:15:30Z",
          },
          {
            id: "assistant-1b",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论是 service-a 的 upstream timeout 导致告警抖动。",
            createdAt: "2026-03-31T02:16:00Z",
            updatedAt: "2026-03-31T02:16:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const toggle = wrapper.get('[data-testid="protocol-process-fold-turn-user-1"] .protocol-process-toggle');

    expect(toggle.attributes("aria-expanded")).toBe("false");
    expect(wrapper.text()).toContain("结论是 service-a 的 upstream timeout 导致告警抖动。");
    expect(wrapper.find('[data-testid="protocol-process-item-assistant-1a-process-0"]').exists()).toBe(false);

    await toggle.trigger("click");
    await flushPromises();

    expect(toggle.attributes("aria-expanded")).toBe("true");
    expect(wrapper.get('[data-testid="protocol-process-item-assistant-1a-process-0"]').text()).toContain("我先整理刚才收集到的证据。");
  });

  it("sends a new message from the main agent composer", async () => {
    mocks.store = createStoreFixture({
      runtime: {
        turn: {
          active: false,
          phase: "idle",
        },
        codex: {
          status: "connected",
        },
      },
    });
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get(".omnibar-input").setValue("继续确认异常主机的日志来源");
    await wrapper.get('[data-testid="omnibar-primary-action"]').trigger("click");
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith(
      "/api/v1/chat/message",
      expect.objectContaining({
        method: "POST",
      }),
    );
    expect(mocks.store.fetchState).toHaveBeenCalled();
    expect(mocks.store.fetchSessions).toHaveBeenCalled();
  });

  it("falls back to send mode when the turn phase is aborted even if active was left true", async () => {
    mocks.store = createStoreFixture({
      runtime: {
        turn: {
          active: true,
          phase: "aborted",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="omnibar-primary-action"]').classes()).not.toContain("n-button--error-type");
    expect(wrapper.text()).toContain("已停止");
    expect(wrapper.find('[data-testid="omnibar-primary-action"]').exists()).toBe(true);
  });

  it("does not leak the previous mission plan into the latest user turn", async () => {
    mocks.store = createStoreFixture({
      runtime: {
        turn: {
          active: false,
          phase: "idle",
        },
        codex: {
          status: "connected",
        },
      },
      snapshot: {
        approvals: [],
        cards: [
          {
            id: "user-old",
            type: "UserMessageCard",
            role: "user",
            text: "帮我执行一轮全网的 nginx 巡检，重点关注错误日志。",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          createPlanCard(),
          ...createProcessCards(),
          ...createApprovalCards(),
          {
            id: "notice-old",
            type: "NoticeCard",
            title: "Mission stopped",
            text: "当前工作台 mission 已停止，相关主 Agent / worker 会话已收到取消信号。",
            status: "notice",
            createdAt: "2026-03-31T02:22:00Z",
            updatedAt: "2026-03-31T02:22:00Z",
          },
          {
            id: "user-new",
            type: "UserMessageCard",
            role: "user",
            text: "看下CPU",
            createdAt: "2026-03-31T02:23:00Z",
            updatedAt: "2026-03-31T02:23:00Z",
          },
        ],
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).not.toContain("采集 nginx 错误日志");
    expect(wrapper.find('[data-testid="protocol-approval-approval-card-1"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("分析中 | 等待主 Agent 生成计划");
  });

  it("shows the latest fatal reason and restart hint after a stopped mission", async () => {
    mocks.store = createStoreFixture({
      runtime: {
        turn: {
          active: false,
          phase: "aborted",
        },
        codex: {
          status: "connected",
        },
      },
      snapshot: {
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "看下CPU",
            createdAt: "2026-04-01T01:15:00Z",
            updatedAt: "2026-04-01T01:15:00Z",
          },
          {
            id: "err-1",
            type: "ErrorCard",
            title: "远程主机连接超时",
            text: "远程主机心跳超时，当前操作已中断，可重试或刷新主机状态。",
            status: "failed",
            createdAt: "2026-04-01T01:15:05Z",
            updatedAt: "2026-04-01T01:15:05Z",
          },
          {
            id: "notice-1",
            type: "NoticeCard",
            title: "Mission stopped",
            text: "当前工作台 mission 已停止，相关主 Agent / worker 会话已收到取消信号。",
            status: "notice",
            createdAt: "2026-04-01T01:15:06Z",
            updatedAt: "2026-04-01T01:15:06Z",
          },
        ],
        approvals: [],
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("远程主机连接超时");
    expect(wrapper.text()).toContain("远程主机心跳超时");
    expect(wrapper.text()).toContain("启动一轮新的 mission");
  });

  it("announces a new mission when sending after the previous one stopped", async () => {
    mocks.store = createStoreFixture({
      runtime: {
        turn: {
          active: false,
          phase: "aborted",
        },
        codex: {
          status: "connected",
        },
      },
      snapshot: {
        approvals: [],
        cards: [
          {
            id: "notice-1",
            type: "NoticeCard",
            title: "Mission stopped",
            text: "当前工作台 mission 已停止，相关主 Agent / worker 会话已收到取消信号。",
            status: "notice",
            createdAt: "2026-04-01T01:15:06Z",
            updatedAt: "2026-04-01T01:15:06Z",
          },
        ],
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get(".omnibar-input").setValue("重新看下CPU");
    await wrapper.get('[data-testid="omnibar-primary-action"]').trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("已在当前会话启动新一轮 mission");
  });

  it("opens the evidence modal from a step card and shows the dispatched sub-agent task", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await expandPlanWidget(wrapper);

    const stepEvidenceButton = wrapper.findAll(".plan-action").find((button) => button.text().includes("查看证据"));
    expect(stepEvidenceButton).toBeTruthy();

    await stepEvidenceButton.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("任务派发证据");
    expect(wrapper.text()).toContain("发送给子 Agent 的任务");
    expect(wrapper.text()).toContain("采集 nginx 错误日志");
    expect(wrapper.text()).toContain("journalctl -u nginx");
  });

  it("opens host evidence directly when a host-agent chip is clicked", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await expandPlanWidget(wrapper);

    const hostChip = wrapper.findAll(".plan-host-pill").find((button) => button.text().includes("web-01"));
    expect(hostChip).toBeTruthy();

    await hostChip.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("执行详情 · web-01");
    expect(wrapper.text()).toContain("web-01 对话");
    expect(wrapper.text()).toContain("web-01");
  });

  it("opens evidence modal from a completed-turn process item without pushing raw details back into the thread", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我汇总上一轮 nginx 巡检结果",
            createdAt: "2026-03-31T02:15:00Z",
            updatedAt: "2026-03-31T02:15:00Z",
          },
          {
            id: "assistant-1a",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先整理刚才收集到的证据。",
            createdAt: "2026-03-31T02:15:30Z",
            updatedAt: "2026-03-31T02:15:30Z",
          },
          {
            id: "assistant-1b",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论是 service-a 的 upstream timeout 导致告警抖动。",
            createdAt: "2026-03-31T02:16:00Z",
            updatedAt: "2026-03-31T02:16:00Z",
          },
          createPlanCard(),
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="protocol-process-fold-turn-user-1"] .protocol-process-toggle').trigger("click");
    await flushPromises();

    const processItem = wrapper.get('[data-testid="protocol-process-item-assistant-1a-process-0"]');
    expect(processItem.text()).toContain("我先整理刚才收集到的证据");

    await processItem.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("过程详情");
    expect(wrapper.text()).toContain("我先整理刚才收集到的证据。");
    expect(wrapper.text()).not.toContain("当前还没有可用的计划摘要。");
  });

  it("keeps approval, evidence and timeline detail blocks out of the main thread body", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [{ id: "approval-1", status: "pending", itemId: "approval-card-1" }],
        evidenceSummaries: [
          {
            id: "evidence-approval-1",
            citationKey: "E-EVIDENCE-APPROVAL-1",
            title: "reload 审批证据",
            summary: "reload 风险与验证策略摘要",
          },
        ],
        incidentEvents: [
          {
            id: "evt-approval-queued",
            type: "approval.requested",
            status: "warning",
            title: "reload 已进入审批队列",
            summary: "web-02 reload nginx 正等待人工确认",
            targetId: "approval-1",
            hostId: "web-02",
            createdAt: "2026-03-31T02:20:30Z",
          },
        ],
        cards: [
          {
            id: "user-surface-copy",
            type: "UserMessageCard",
            role: "user",
            text: "给我这轮变更的最终判断",
            createdAt: "2026-03-31T02:20:00Z",
            updatedAt: "2026-03-31T02:20:00Z",
          },
          {
            id: "assistant-surface-copy",
            type: "AssistantMessageCard",
            role: "assistant",
            text: [
              "审批上下文：",
              "- 审批ID：approval-1",
              "- 风险级别：high",
              "- 目标环境：prod / cn-prod-a",
              "- 回滚建议：恢复上一个 nginx 配置",
              "",
              "证据摘要：",
              "- Evidence ID：evidence-approval-1",
              "- Citation：E-EVIDENCE-APPROVAL-1",
              "",
              "时间线：",
              "- reload 已进入审批队列",
            ].join("\n"),
            detail: {
              approvalId: "approval-1",
              evidenceId: "evidence-approval-1",
              citationKey: "E-EVIDENCE-APPROVAL-1",
            },
            createdAt: "2026-03-31T02:20:05Z",
            updatedAt: "2026-03-31T02:20:05Z",
          },
          {
            id: "assistant-final-copy",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论：先执行 nginx -t，再决定是否 reload。",
            createdAt: "2026-03-31T02:20:20Z",
            updatedAt: "2026-03-31T02:20:20Z",
          },
          createApprovalCards()[0],
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("结论：先执行 nginx -t，再决定是否 reload。");
    expect(wrapper.text()).not.toContain("审批上下文：");
    expect(wrapper.text()).not.toContain("风险级别：high");
    expect(wrapper.text()).not.toContain("Evidence ID：evidence-approval-1");
    expect(wrapper.text()).toContain("web-02 等待审批");
    expect(wrapper.text()).toContain("systemctl reload nginx");
    expect(wrapper.get('[data-testid="protocol-approval-approval-card-1"]').exists()).toBe(true);
  });

  it("shows command names in the event stream and opens terminal output from command details", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        selectedHostId: "server-local",
        hosts: [{ id: "server-local", name: "server-local", status: "online", executable: true }],
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "继续查看主机状态",
            createdAt: "2026-04-08T03:18:00Z",
            updatedAt: "2026-04-08T03:18:00Z",
          },
          ...createCommandCards(),
          {
            id: "process-cmd-uptime-1",
            type: "ProcessLineCard",
            text: "已处理 1 个命令",
            status: "completed",
            hostId: "server-local",
            createdAt: "2026-04-08T03:19:01Z",
            updatedAt: "2026-04-08T03:19:01Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("uptime");
    expect(timeline.text()).toContain("load averages");

    const commandEvent = wrapper.findAll(".timeline-item").find((button) => button.text().includes("uptime"));
    expect(commandEvent).toBeTruthy();

    await commandEvent.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("命令执行详情");
    expect(modal.text()).toContain("uptime");
    expect(modal.text()).toContain("load averages");
    expect(modal.text()).not.toContain("暂无终端输出");
  });

  it("opens tool invocation evidence from the event stream", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        selectedHostId: "server-local",
        hosts: [{ id: "server-local", name: "server-local", status: "online", executable: true }],
        approvals: [],
        cards: [
          {
            id: "cmd-readonly-1",
            type: "CommandCard",
            status: "completed",
            command: "pg_isready",
            hostId: "server-local",
            detail: { tool: "readonly_host_inspect", readonly: true },
            createdAt: "2026-04-08T04:10:00Z",
            updatedAt: "2026-04-08T04:10:01Z",
          },
        ],
        toolInvocations: [
          {
            id: "tool-choice-1",
            name: "ask_user_question",
            status: "waiting_user",
            inputJson: JSON.stringify({
              questions: [{ question: "你是只问能力，还是要开始只读诊断？" }],
            }),
            outputJson: JSON.stringify({ status: "waiting_user" }),
            inputSummary: "你是只问能力，还是要开始只读诊断？",
            outputSummary: "等待用户回答",
            evidenceId: "evidence-choice-1",
            startedAt: "2026-04-08T04:00:00Z",
          },
        ],
        evidenceSummaries: [
          {
            id: "evidence-choice-1",
            invocationId: "tool-choice-1",
            kind: "ask_user_question",
            title: "确认意图",
            summary: "等待用户回答",
            content: "你是只问能力，还是要开始只读诊断？",
            createdAt: "2026-04-08T04:00:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_input",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("澄清问题");
    expect(timeline.text()).toContain("等待用户确认");

    const toolEvent = wrapper.findAll(".timeline-item").find((button) => button.text().includes("澄清问题"));
    expect(toolEvent).toBeTruthy();

    await toolEvent.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("澄清问题");
    expect(modal.text()).toContain("输入");
    expect(modal.text()).toContain("输出");
    expect(modal.text()).toContain("原始 evidence");
    expect(modal.text()).toContain("关联审批");
    expect(modal.text()).toContain("关联 worker");
    expect(modal.text()).toContain("关联计划");
    expect(modal.text()).toContain("你是只问能力，还是要开始只读诊断？");
  });

  it("opens readonly host inspect evidence with a user-facing tool name", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        selectedHostId: "server-local",
        hosts: [{ id: "server-local", name: "server-local", status: "online", executable: true }],
        approvals: [],
        cards: [],
        toolInvocations: [
          {
            id: "tool-readonly-1",
            name: "readonly_host_inspect",
            status: "completed",
            inputJson: JSON.stringify({
              hostId: "server-local",
              target: "postgresql_replication",
              command: "pg_isready",
              cwd: "/tmp",
            }),
            outputJson: JSON.stringify({
              status: "completed",
              summary: "replication status checked",
              output: "accepting connections",
            }),
            inputSummary: "检查 PostgreSQL replication 状态",
            outputSummary: "未发现明显 replication lag",
            evidenceId: "evidence-readonly-1",
            startedAt: "2026-04-08T04:10:00Z",
            completedAt: "2026-04-08T04:10:01Z",
          },
        ],
        evidenceSummaries: [
          {
            id: "evidence-readonly-1",
            invocationId: "tool-readonly-1",
            kind: "readonly_host_inspect",
            title: "只读主机检查",
            summary: "未发现明显 replication lag",
            content: "pg_isready\naccepting connections",
            createdAt: "2026-04-08T04:10:01Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("只读主机检查");
    expect(timeline.text()).not.toContain("readonly_host_inspect");
    expect(timeline.findAll(".timeline-item").filter((button) => button.text().includes("只读主机检查"))).toHaveLength(1);

    const toolEvent = wrapper.findAll(".timeline-item").find((button) => button.text().includes("只读主机检查"));
    expect(toolEvent).toBeTruthy();

    await toolEvent.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("只读主机检查");
    expect(modal.text()).toContain("postgresql_replication");
    expect(modal.text()).toContain("pg_isready");
    const outputTab = wrapper.findAll(".modal-tab").find((button) => button.text().includes("输出"));
    expect(outputTab).toBeTruthy();
    await outputTab.trigger("click");
    await flushPromises();
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("未发现明显 replication lag");
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("原始 evidence");
  });

  it("opens DispatchWorkers invocation evidence with full worker task context", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        selectedHostId: "server-local",
        hosts: [{ id: "server-local", name: "server-local", status: "online", executable: true }],
        approvals: [],
        cards: [],
        toolInvocations: [
          {
            id: "tool-dispatch-1",
            name: "orchestrator_dispatch_tasks",
            status: "completed",
            inputJson: JSON.stringify({
              missionTitle: "PG 同步修复",
              tasks: [
                {
                  taskId: "task-pg-1",
                  hostId: "web-01",
                  title: "检查 PG 同步",
                  instruction: "请在 web-01 上检查 PostgreSQL replication slot 和 WAL 延迟，不要修改任何配置。",
                  constraints: ["只读执行", "不要重启 PostgreSQL"],
                },
              ],
            }),
            outputJson: JSON.stringify({ accepted: 1, queued: 0 }),
            inputSummary: "派发 1 个 PG 同步修复任务",
            outputSummary: "accepted=1 queued=0",
            evidenceId: "evidence-dispatch-1",
            startedAt: "2026-04-08T05:10:00Z",
            completedAt: "2026-04-08T05:10:01Z",
          },
        ],
        evidenceSummaries: [
          {
            id: "evidence-dispatch-1",
            invocationId: "tool-dispatch-1",
            kind: "orchestrator_dispatch_tasks",
            title: "任务派发",
            summary: "accepted=1 queued=0",
            content: "任务全文：请在 web-01 上检查 PostgreSQL replication slot 和 WAL 延迟，不要修改任何配置。",
            createdAt: "2026-04-08T05:10:01Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("任务派发");
    expect(timeline.text()).toContain("派发 1 个 PG 同步修复任务");

    const toolEvent = wrapper.findAll(".timeline-item").find((button) => button.text().includes("任务派发"));
    expect(toolEvent).toBeTruthy();
    await toolEvent.trigger("click");
    await flushPromises();

    const workerTab = wrapper.findAll(".modal-tab").find((button) => button.text().includes("关联 worker"));
    expect(workerTab).toBeTruthy();
    await workerTab.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("关联 Worker");
    expect(modal.text()).toContain("检查 PG 同步");
    expect(modal.text()).toContain("web-01");
    expect(modal.text()).toContain("请在 web-01 上检查 PostgreSQL replication slot 和 WAL 延迟，不要修改任何配置。");
    expect(modal.text()).toContain("只读执行 / 不要重启 PostgreSQL");
  });

  it("focuses the matching approval card when the timeline event targets an approval id", async () => {
    const store = createStoreFixture();
    const planCard = store.snapshot.cards.find((card) => card.id === "workspace-plan-1");
    planCard.detail.dispatch_events = [
      {
        id: "dispatch-approval-2",
        createdAt: "2026-03-31T02:21:30Z",
        summary: "切主脚本已进入审批队列",
        detail: "等待 db-04 审批",
        hostId: "db-04",
        approvalId: "approval-2",
      },
    ];
    mocks.store = store;

    const wrapper = mountPage();
    await flushPromises();

    const timelineEvent = wrapper.findAll(".timeline-item").find((button) => button.text().includes("切主脚本已进入审批队列"));
    expect(timelineEvent).toBeTruthy();

    await timelineEvent.trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="protocol-approval-approval-card-2"]').classes()).toContain("active");
    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("failover-master.sh");
    expect(modal.text()).toContain("approval-2");
  });

  it("renders plan approval cards in the approval rail without session authorization", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [{ id: "approval-plan-1", status: "pending", itemId: "plan-approval-1" }],
        cards: [
          {
            id: "user-plan-approval",
            type: "UserMessageCard",
            role: "user",
            text: "先做计划，审批后再执行。",
            createdAt: "2026-04-08T05:00:00Z",
            updatedAt: "2026-04-08T05:00:00Z",
          },
          {
            id: "plan-approval-1",
            type: "PlanApprovalCard",
            status: "pending",
            title: "批准 PG 同步修复计划",
            text: "先只读确认复制状态，审批后派发 worker。",
            summary: "PG 同步修复计划",
            approval: {
              requestId: "approval-plan-1",
              type: "plan_exit",
              decisions: ["accept", "decline"],
            },
            detail: {
              validation: "确认 replication lag 恢复正常。",
              risk: "数据库同步操作有生产风险。",
            },
            createdAt: "2026-04-08T05:00:10Z",
            updatedAt: "2026-04-08T05:00:10Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_approval",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const approvalCard = wrapper.get('[data-testid="protocol-approval-plan-approval-1"]');
    expect(approvalCard.text()).toContain("批准 PG 同步修复计划");
    expect(approvalCard.text()).toContain("计划:");
    expect(approvalCard.text()).not.toContain("授权");

    const detailButton = approvalCard.findAll("button").find((button) => button.text().includes("详情"));
    expect(detailButton).toBeTruthy();
    await detailButton.trigger("click");
    await flushPromises();
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("确认 replication lag 恢复正常");

    const rejectButton = approvalCard.findAll("button").find((button) => button.text().includes("拒绝"));
    expect(rejectButton).toBeTruthy();
    await rejectButton.trigger("click");
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith(
      "/api/v1/approvals/approval-plan-1/decision",
      expect.objectContaining({
        method: "POST",
      }),
    );
    const [, request] = global.fetch.mock.calls.at(-1);
    expect(JSON.parse(request.body)).toEqual({ decision: "decline" });
    expect(wrapper.text()).toContain("计划已拒绝，等待主 Agent 调整方案。");
  });

  it("keeps previous command events visible when a new mission starts", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        selectedHostId: "server-local",
        hosts: [{ id: "server-local", name: "server-local", status: "online", executable: true }],
        approvals: [],
        cards: [
          {
            id: "user-old",
            type: "UserMessageCard",
            role: "user",
            text: "继续查看主机状态",
            createdAt: "2026-04-08T03:18:00Z",
            updatedAt: "2026-04-08T03:18:00Z",
          },
          ...createCommandCards(),
          {
            id: "user-new",
            type: "UserMessageCard",
            role: "user",
            text: "你有办法修复 pg 不同步的问题吗？",
            createdAt: "2026-04-08T04:25:00Z",
            updatedAt: "2026-04-08T04:25:00Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "planning",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("uptime");
    expect(timeline.text()).toContain("load averages");
    expect(wrapper.get('[data-testid="protocol-live-status-card"]').text()).toContain("正在规划步骤");
  });

  it("opens approval evidence and submits decisions from the right rail", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const approvalCard = wrapper.get('[data-testid="protocol-approval-approval-card-1"]');
    const detailButton = approvalCard.findAll("button").find((button) => button.text().includes("详情"));
    const acceptButton = approvalCard.findAll("button").find((button) => button.text().includes("同意执行"));

    expect(detailButton).toBeTruthy();
    expect(acceptButton).toBeTruthy();

    await detailButton.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("审批上下文 · web-02");
    expect(wrapper.text()).toContain("审批上下文");
    expect(wrapper.text()).toContain("风险级别");
    expect(wrapper.text()).toContain("目标范围");
    expect(wrapper.text()).toContain("Dry-run 摘要");
    expect(wrapper.text()).toContain("验证策略");
    expect(wrapper.text()).toContain("验证来源");
    expect(wrapper.text()).toContain("Host Terminal");

    await acceptButton.trigger("click");
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith(
      "/api/v1/approvals/approval-1/decision",
      expect.objectContaining({
        method: "POST",
      }),
    );
  });

  it("keeps the mission waiting for approval when a high-risk action is rejected for insufficient permission", async () => {
    const store = createStoreFixture({
      runtime: {
        turn: {
          active: false,
          phase: "waiting_approval",
          pendingStart: false,
        },
        codex: {
          status: "connected",
        },
      },
    });
    mocks.store = store;
    global.fetch = vi.fn((url) => {
      if (url === "/api/v1/approvals/approval-1/decision") {
        return Promise.resolve({
          ok: false,
          json: async () => ({ error: "权限不足：当前用户不能批准高风险动作" }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({}),
      });
    });

    const wrapper = mountPage();
    await flushPromises();

    const approvalCard = wrapper.get('[data-testid="protocol-approval-approval-card-1"]');
    const acceptButton = approvalCard.findAll("button").find((button) => button.text().includes("同意执行"));
    expect(acceptButton).toBeTruthy();

    await acceptButton.trigger("click");
    await flushPromises();

    expect(store.fetchState).not.toHaveBeenCalled();
    expect(store.runtime.turn.phase).toBe("waiting_approval");
    expect(wrapper.text()).toContain("权限不足：当前用户不能批准高风险动作");
    expect(wrapper.get('[data-testid="protocol-approval-approval-card-1"]').exists()).toBe(true);

    const detailButton = wrapper
      .get('[data-testid="protocol-approval-approval-card-1"]')
      .findAll("button")
      .find((button) => button.text().includes("详情"));
    expect(detailButton).toBeTruthy();

    await detailButton.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("审批上下文");
    expect(modal.text()).toContain("风险级别");
    expect(modal.text()).toContain("回滚提示");
  });

  it("shows related verification results inside approval evidence", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        verificationRecords: createVerificationRecords(),
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const approvalCard = wrapper.get('[data-testid="protocol-approval-approval-card-1"]');
    const detailButton = approvalCard.findAll("button").find((button) => button.text().includes("详情"));
    expect(detailButton).toBeTruthy();

    await detailButton.trigger("click");
    await flushPromises();

    const verificationTab = wrapper.findAll(".modal-tab").find((button) => button.text().includes("验证结果"));
    expect(verificationTab).toBeTruthy();

    await verificationTab.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("验证结果");
    expect(modal.text()).toContain("验证通过");
    expect(modal.text()).toContain("service_health");
    expect(modal.text()).toContain("coroot_health");
    expect(modal.text()).toContain("reload 后服务健康检查通过");
    expect(modal.text()).toContain("恢复上一个 nginx 配置并重新加载服务");
  });

  it("allows jumping between approval evidence, verification evidence and timeline", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        verificationRecords: createVerificationRecords(),
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const approvalCard = wrapper.get('[data-testid="protocol-approval-approval-card-1"]');
    const detailButton = approvalCard.findAll("button").find((button) => button.text().includes("详情"));
    expect(detailButton).toBeTruthy();

    await detailButton.trigger("click");
    await flushPromises();

    let modal = wrapper.get(".protocol-evidence-modal");
    const openVerificationButton = modal.findAll("button").find((button) => button.text().includes("查看验证结果"));
    expect(openVerificationButton).toBeTruthy();

    await openVerificationButton.trigger("click");
    await flushPromises();

    modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("验证结果 · web-02");
    expect(modal.text()).toContain("service_health");

    const focusTimelineButton = modal.findAll("button").find((button) => button.text().includes("定位时间线事件"));
    expect(focusTimelineButton).toBeTruthy();

    await focusTimelineButton.trigger("click");
    await flushPromises();

    expect(wrapper.find(".protocol-evidence-modal").exists()).toBe(false);
    expect(wrapper.get('[data-testid="protocol-event-verification-verify-approval-card-1"]').classes()).toContain("active");
  });

  it("opens verification evidence directly from the timeline", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        verificationRecords: createVerificationRecords(),
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const verificationEvent = wrapper.get('[data-testid="protocol-event-verification-verify-approval-card-1"]');
    await verificationEvent.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("验证结果 · web-02");
    expect(wrapper.get(".modal-tab.active").text()).toContain("验证结果");
    expect(wrapper.text()).toContain("reload 后服务健康检查通过");
  });

  it("replays readonly -> plan approval -> execution -> verification success while keeping rollback hints in plan-preview state", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        hosts: [
          { id: "web-01", name: "web-01", address: "10.0.0.1", status: "online", executable: true },
        ],
        toolInvocations: [
          {
            id: "inv-readonly-success",
            name: "readonly_host_inspect",
            status: "completed",
            hostId: "web-01",
            inputSummary: "pg_isready",
            outputSummary: "未发现 replication lag",
            evidenceId: "evidence-readonly-success",
            startedAt: "2026-04-09T10:01:00Z",
            completedAt: "2026-04-09T10:01:30Z",
          },
        ],
        evidenceSummaries: [
          {
            id: "evidence-readonly-success",
            invocationId: "inv-readonly-success",
            citationKey: "E-EVIDENCE-READONLY-SUCCESS",
            kind: "readonly_host_inspect",
            title: "PG 只读巡检结果",
            summary: "pg_isready 通过，未发现 replication lag。",
            content: "pg_isready: accepting connections\nreplication lag: 0s",
            createdAt: "2026-04-09T10:01:30Z",
          },
        ],
        verificationRecords: [
          {
            id: "verify-success-1",
            actionEventId: "cmd-exec-success",
            status: "passed",
            strategy: "service_health",
            successCriteria: ["执行 nginx -t", "确认健康探针恢复正常"],
            findings: ["reload 后服务健康检查通过。"],
            metadata: {
              cardId: "cmd-exec-success",
              hostId: "web-01",
              hostName: "web-01",
              targetSummary: "web-01 / service nginx",
              verificationSources: ["coroot_health", "metric_check", "health_probe", "log_check"],
              endedAt: "2026-04-09T10:04:30Z",
            },
            createdAt: "2026-04-09T10:04:30Z",
          },
        ],
        cards: [
          {
            id: "user-success-path",
            type: "UserMessageCard",
            role: "user",
            text: "先只读检查，再审批执行 nginx 修复动作。",
            createdAt: "2026-04-09T10:00:00Z",
            updatedAt: "2026-04-09T10:00:00Z",
          },
          {
            id: "cmd-readonly-success",
            type: "CommandCard",
            title: "readonly_host_inspect",
            command: "pg_isready",
            summary: "Exit code: 0",
            status: "completed",
            hostId: "web-01",
            detail: {
              tool: "readonly_host_inspect",
              readonly: true,
              evidenceId: "evidence-readonly-success",
            },
            createdAt: "2026-04-09T10:01:00Z",
            updatedAt: "2026-04-09T10:01:30Z",
          },
          {
            id: "plan-success",
            type: "PlanCard",
            title: "PG 修复计划",
            text: "先只读确认复制状态，再审批进入执行。",
            summary: "计划包含只读确认、审批和 nginx reload。",
            items: [
              { step: "web-01 [task-1] 只读确认复制状态", status: "completed" },
              { step: "web-01 [task-2] 执行 nginx -t && systemctl reload nginx", status: "completed" },
            ],
            detail: {
              goal: "确认 PG 与 nginx 状态，再安全执行 reload。",
              validation: "确认健康探针恢复正常且 5xx 没有升高。",
              rollback: "如 reload 后异常，恢复上一个 nginx 配置。",
              dispatch_events: [
                {
                  id: "evt-plan-approved",
                  createdAt: "2026-04-09T10:02:40Z",
                  summary: "计划已审批通过",
                  detail: "进入执行模式并派发 reload 任务",
                  hostId: "web-01",
                },
                {
                  id: "evt-dispatch-success",
                  createdAt: "2026-04-09T10:03:00Z",
                  summary: "Dispatcher 下发任务",
                  detail: "已派发 nginx reload 到 web-01",
                  hostId: "web-01",
                  taskId: "task-2",
                },
              ],
            },
            createdAt: "2026-04-09T10:02:00Z",
            updatedAt: "2026-04-09T10:03:00Z",
          },
          {
            id: "assistant-dispatch-success",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "计划已审批通过，正在派发 1 个任务到 web-01 执行修复。",
            createdAt: "2026-04-09T10:03:05Z",
            updatedAt: "2026-04-09T10:03:05Z",
          },
          {
            id: "cmd-exec-success",
            type: "CommandCard",
            title: "Command execution",
            command: "nginx -t && systemctl reload nginx",
            text: "syntax is ok",
            output: "nginx: configuration file /etc/nginx/nginx.conf test is successful",
            stdout: "nginx: configuration file /etc/nginx/nginx.conf test is successful",
            status: "completed",
            hostId: "web-01",
            cwd: "/etc/nginx",
            exitCode: 0,
            durationMs: 420,
            createdAt: "2026-04-09T10:03:20Z",
            updatedAt: "2026-04-09T10:03:40Z",
          },
          {
            id: "verification-card-passed",
            type: "VerificationCard",
            title: "自动验证通过",
            text: "web-01 / service nginx 自动验证通过。\n\n结论：reload 后服务健康检查通过。",
            summary: "web-01 / service nginx 自动验证通过。",
            status: "completed",
            hostId: "web-01",
            detail: {
              verificationId: "verify-success-1",
            },
            createdAt: "2026-04-09T10:04:30Z",
            updatedAt: "2026-04-09T10:04:30Z",
          },
          {
            id: "assistant-final-success",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论：只读检查未发现异常，计划审批后执行成功，自动验证通过。",
            createdAt: "2026-04-09T10:04:40Z",
            updatedAt: "2026-04-09T10:04:40Z",
          },
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("结论：只读检查未发现异常，计划审批后执行成功，自动验证通过。");
    expect(wrapper.find('[data-testid="protocol-incident-insights"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("验证失败");
    expect(wrapper.get('[data-testid="protocol-approval-rail"]').text()).toContain("当前没有待处理的审批");

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("只读主机检查");
    expect(timeline.text()).toContain("计划已审批通过");
    expect(timeline.text()).toContain("验证通过");

    await wrapper.get('[data-testid="protocol-event-tool-invocation-inv-readonly-success"]').trigger("click");
    await flushPromises();
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("只读主机检查");
    const outputTab = wrapper.findAll(".modal-tab").find((button) => button.text().includes("输出"));
    expect(outputTab).toBeTruthy();
    await outputTab.trigger("click");
    await flushPromises();
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("未发现 replication lag");

    await wrapper.get(".protocol-evidence-modal .close-btn").trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="protocol-event-verification-verify-success-1"]').trigger("click");
    await flushPromises();
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("验证通过");
    expect(wrapper.get(".protocol-evidence-modal").text()).toContain("coroot_health");
    expect(wrapper.get(".protocol-evidence-modal").text()).not.toContain("恢复上一个 nginx 配置");
  });

  it("renders verification and rollback cards in the main thread after a failed verification", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        verificationRecords: createFailedVerificationRecords(),
        cards: [
          {
            id: "user-verify-failed",
            type: "UserMessageCard",
            role: "user",
            text: "执行 nginx reload 并确认状态。",
            createdAt: "2026-03-31T02:22:00Z",
            updatedAt: "2026-03-31T02:22:00Z",
          },
          ...createVerificationSummaryCards(),
        ],
      },
      runtime: {
        turn: {
          active: false,
          phase: "completed",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("自动验证失败");
    expect(wrapper.text()).toContain("健康探针仍然失败");
    expect(wrapper.text()).toContain("回滚建议");
    expect(wrapper.text()).toContain("先复核健康探针和错误日志，再决定是否立即回滚");

    const verificationEvent = wrapper.get('[data-testid="protocol-event-verification-verify-approval-card-failed"]');
    await verificationEvent.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("验证结果 · web-02");
    expect(wrapper.text()).toContain("验证来源");
    expect(wrapper.text()).toContain("下一步建议");
    expect(wrapper.text()).toContain("建议先恢复上一个 nginx 配置并重新加载服务");
  });

  it("can jump from verification evidence back to the approval rail", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        verificationRecords: createVerificationRecords(),
      },
    });

    const wrapper = mountPage();
    await flushPromises();

    const verificationEvent = wrapper.get('[data-testid="protocol-event-verification-verify-approval-card-1"]');
    await verificationEvent.trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    const focusApprovalButton = modal.findAll("button").find((button) => button.text().includes("定位审批卡"));
    expect(focusApprovalButton).toBeTruthy();

    await focusApprovalButton.trigger("click");
    await flushPromises();

    expect(wrapper.find(".protocol-evidence-modal").exists()).toBe(false);
    expect(wrapper.get('[data-testid="protocol-approval-approval-card-1"]').classes()).toContain("active");
  });

  it("projects synthetic MCP approval rail and timeline updates from action surfaces", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        approvals: [],
        cards: createSyntheticMcpSurfaceCards(),
      },
      runtime: {
        turn: {
          active: true,
          phase: "waiting_input",
        },
        codex: {
          status: "connected",
        },
      },
    });

    const drawerEvents = [];
    const drawerHandler = (event) => drawerEvents.push(event.detail);
    window.addEventListener("codex:open-mcp-drawer", drawerHandler);
    try {
      const wrapper = mountPage();
      await flushPromises();

      const turn = wrapper.get('[data-testid="protocol-turn-turn-user-mcp-1"]');
      expect(turn.text()).toContain("MCP 控制面板");
      expect(turn.text()).toContain("nginx 监控聚合面板");
      expect(wrapper.get('[data-testid="mcp-control-panel-action"]').text()).toContain("重启 nginx");
      expect(wrapper.get('[data-testid="mcp-bundle-subject"]').text()).toContain("nginx / prod");

      const fetchStateCalls = mocks.store.fetchState.mock.calls.length;

      await wrapper.get('[data-testid="mcp-bundle-action"]').trigger("click");
      await flushPromises();
      expect(mocks.store.fetchState.mock.calls.length).toBe(fetchStateCalls + 1);

      await wrapper.get('[data-testid="mcp-bundle-pin"]').trigger("click");
      await flushPromises();
      expect(drawerEvents).toHaveLength(1);
      expect(drawerEvents[0]).toMatchObject({
        source: "protocol-mcp-surface",
        pin: true,
        surface: {
          kind: "bundle",
        },
      });

      await wrapper.get('[data-testid="mcp-bundle-open-detail"]').trigger("click");
      await flushPromises();
      expect(wrapper.get(".protocol-evidence-modal").text()).toContain("MCP 面板");
      expect(wrapper.get(".protocol-evidence-modal").text()).toContain("nginx / prod");
      expect(wrapper.get(".modal-tab.active").text()).toContain("MCP 面板");

      await wrapper.get(".protocol-evidence-modal .close-btn").trigger("click");
      await flushPromises();
      expect(wrapper.find(".protocol-evidence-modal").exists()).toBe(false);

      await wrapper.get('[data-testid="mcp-control-panel-action"]').trigger("click");
      await flushPromises();

      expect(wrapper.get('[data-testid="protocol-approval-rail"]').text()).toContain("重启 nginx");
      expect(wrapper.get('[data-testid="protocol-approval-rail"]').text()).toContain("待处理");
      expect(wrapper.get('[data-testid="protocol-event-timeline"]').text()).toContain("重启 nginx 已进入审批队列");
      expect(wrapper.findAll(".approval-card")).toHaveLength(1);

      const syntheticApproval = wrapper
        .findAll(".approval-card")
        .find((card) => card.text().includes("重启 nginx"));
      expect(syntheticApproval).toBeTruthy();

      const acceptButton = syntheticApproval.findAll("button").find((button) => button.text().includes("同意执行"));
      expect(acceptButton).toBeTruthy();

      await acceptButton.trigger("click");
      await flushPromises();

      expect(wrapper.get('[data-testid="protocol-approval-rail"]').text()).toContain("当前没有待处理的审批");
      expect(wrapper.get('[data-testid="protocol-workspace-page"]').text()).toContain("已通过审批，执行结果会在当前会话回写。");
      expect(wrapper.get('[data-testid="protocol-event-timeline"]').text()).toContain("重启 nginx 已通过审批");
    } finally {
      window.removeEventListener("codex:open-mcp-drawer", drawerHandler);
    }
  });

  it("shows immediate feedback when stopping the workspace turn", async () => {
    let resolveStop;
    global.fetch = vi.fn((url) => {
      if (url === "/api/v1/chat/stop") {
        return new Promise((resolve) => {
          resolveStop = resolve;
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({}),
      });
    });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="omnibar-primary-action"]').trigger("click");
    await nextTick();

    expect(wrapper.text()).toContain("正在中断当前任务...");

    resolveStop({
      ok: true,
      json: async () => ({ accepted: true }),
    });
    await flushPromises();
  });

  it("projects cancel failure incident events into the timeline after stopping the workspace turn", async () => {
    const activeCommand = {
      id: "cmd-cancel-1",
      type: "CommandCard",
      title: "Command execution",
      command: "systemctl reload nginx",
      output: "reloading...",
      status: "inProgress",
      hostId: "web-02",
      cwd: "/etc/nginx",
      createdAt: "2026-04-10T06:00:00Z",
      updatedAt: "2026-04-10T06:00:00Z",
    };
    const cancelledCommand = {
      ...activeCommand,
      output: "reloading...\n任务已中断\ncommand cancelled",
      stderr: "command cancelled",
      status: "cancelled",
      cancelled: true,
      exitCode: 130,
      updatedAt: "2026-04-10T06:00:05Z",
    };
    const stopNotice = {
      id: "turn-aborted-turn-stop-1",
      type: "NoticeCard",
      title: "Mission stopped",
      text: "当前工作台 mission 已停止，相关主 Agent / worker 会话已收到取消信号。",
      status: "notice",
      createdAt: "2026-04-10T06:00:05Z",
      updatedAt: "2026-04-10T06:00:05Z",
    };
    const cancelEvents = [
      {
        id: "cancel-signal-failed-1",
        type: "cancel.signal_failed",
        status: "warning",
        title: "取消信号发送失败",
        summary: "未能向 web-02 发送取消信号，步骤 cmd-cancel-1 可能仍在执行",
        hostId: "web-02",
        metadata: {
          cardId: "cmd-cancel-1",
          hostId: "web-02",
        },
        createdAt: "2026-04-10T06:00:05Z",
      },
      {
        id: "cancel-partial-failure-1",
        type: "cancel.partial_failure",
        status: "warning",
        title: "取消未获远端确认",
        summary: "步骤 cmd-cancel-1 未返回取消确认，已在本地强制标记为 cancelled",
        hostId: "web-02",
        metadata: {
          cardId: "cmd-cancel-1",
          hostId: "web-02",
        },
        createdAt: "2026-04-10T06:00:08Z",
      },
    ];

    const store = createStoreFixture({
      snapshot: {
        selectedHostId: "web-02",
        hosts: [{ id: "web-02", name: "web-02", address: "10.0.0.2", status: "online", executable: true }],
        approvals: [],
        incidentEvents: [],
        cards: [
          {
            id: "user-stop-1",
            type: "UserMessageCard",
            role: "user",
            text: "停止当前 reload 任务",
            createdAt: "2026-04-10T05:59:30Z",
            updatedAt: "2026-04-10T05:59:30Z",
          },
          activeCommand,
        ],
      },
      runtime: {
        turn: {
          active: true,
          phase: "executing",
          pendingStart: false,
        },
        codex: {
          status: "connected",
        },
      },
    });
    store.fetchState = vi.fn(async () => {
      store.snapshot = reactive({
        ...store.snapshot,
        incidentEvents: cancelEvents,
        cards: [
          {
            id: "user-stop-1",
            type: "UserMessageCard",
            role: "user",
            text: "停止当前 reload 任务",
            createdAt: "2026-04-10T05:59:30Z",
            updatedAt: "2026-04-10T05:59:30Z",
          },
          cancelledCommand,
          stopNotice,
        ],
      });
      store.runtime.turn.active = false;
      store.runtime.turn.phase = "aborted";
      store.runtime.turn.pendingStart = false;
      return true;
    });
    store.fetchSessions = vi.fn(async () => true);
    mocks.store = store;

    global.fetch = vi.fn((url) => {
      if (url === "/api/v1/chat/stop") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ accepted: true }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: async () => ({}),
      });
    });

    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="omnibar-primary-action"]').trigger("click");
    await flushPromises();
    await nextTick();

    expect(store.fetchState).toHaveBeenCalled();
    expect(wrapper.find('[data-testid="incident-summary-card"]').exists()).toBe(false);

    const timeline = wrapper.get('[data-testid="protocol-event-timeline"]');
    expect(timeline.text()).toContain("取消信号发送失败");
    expect(timeline.text()).toContain("取消未获远端确认");

    await wrapper.get('[data-testid="protocol-event-incident-cancel-partial-failure-1"]').trigger("click");
    await flushPromises();

    const modal = wrapper.get(".protocol-evidence-modal");
    expect(modal.text()).toContain("命令执行详情");
    expect(modal.text()).toContain("systemctl reload nginx");
    expect(modal.text()).toContain("command cancelled");
    expect(modal.text()).not.toContain("暂无终端输出");
  });

  it("focuses the next approval card after the current one is accepted", async () => {
    const store = createStoreFixture();
    store.fetchState = vi.fn(async () => {
      store.snapshot = reactive({
        ...store.snapshot,
        approvals: [{ id: "approval-2", status: "pending", itemId: "approval-card-2" }],
        cards: store.snapshot.cards.filter((card) => card.id !== "approval-card-1"),
      });
      return true;
    });
    store.fetchSessions = vi.fn(async () => true);
    mocks.store = store;

    const wrapper = mountPage();
    await flushPromises();

    const firstApproval = wrapper.get('[data-testid="protocol-approval-approval-card-1"]');
    const acceptButton = firstApproval.findAll("button").find((button) => button.text().includes("同意执行"));
    expect(acceptButton).toBeTruthy();

    await acceptButton.trigger("click");
    await flushPromises();
    await nextTick();

    expect(wrapper.get('[data-testid="protocol-approval-approval-card-2"]').classes()).toContain("active");
  });

  it("renders four evidence tabs without legacy planner wording", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await expandPlanWidget(wrapper);

    const stepEvidenceButton = wrapper.findAll(".plan-action").find((button) => button.text().includes("查看证据"));
    expect(stepEvidenceButton).toBeTruthy();

    await stepEvidenceButton.trigger("click");
    await flushPromises();

    const tabs = wrapper.findAll(".modal-tab").map((button) => button.text());
    expect(tabs.join(" ")).toContain("任务派发");
    expect(tabs.join(" ")).toContain("Worker 对话");
    expect(tabs.join(" ")).toContain("Host Terminal");
    expect(tabs.join(" ")).toContain("审批上下文");
  });
});
