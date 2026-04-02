import { flushPromises, mount } from "@vue/test-utils";
import { nextTick, reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ProtocolWorkspacePage from "../src/pages/ProtocolWorkspacePage.vue";

const mocks = vi.hoisted(() => ({
  store: null,
}));

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

function findButtonByText(wrapper, text) {
  return wrapper.findAll("button").find((button) => button.text().includes(text));
}

describe("ProtocolWorkspacePage", () => {
  beforeEach(() => {
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

    expect(wrapper.text()).toContain("nginx 巡检计划");
    expect(wrapper.text()).toContain("已收到计划投影");
    expect(wrapper.text()).toContain("step -> host-agent 映射");
  });

  it("renders the plan widget in the composer dock above the input", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find(".protocol-composer-widgets .protocol-inline-plan-widget").exists()).toBe(true);
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
    await wrapper.get(".send-btn").trigger("click");
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

    expect(wrapper.find(".send-btn.stop-btn").exists()).toBe(false);
    expect(wrapper.text()).toContain("已停止");
    expect(wrapper.find(".send-btn").exists()).toBe(true);
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
    expect(wrapper.text()).toContain("idle | 等待主 Agent 生成计划");
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
    await wrapper.get(".send-btn").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("已在当前会话启动新一轮 mission");
  });

  it("opens the evidence modal from a step card and shows the main-agent plan context", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const stepEvidenceButton = wrapper.findAll(".plan-action").find((button) => button.text().includes("查看证据"));
    expect(stepEvidenceButton).toBeTruthy();

    await stepEvidenceButton.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("主 Agent 计划摘要");
    expect(wrapper.text()).toContain("采集 nginx 错误日志");
  });

  it("opens host evidence directly when a host-agent chip is clicked", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const hostChip = wrapper.findAll(".plan-host-pill").find((button) => button.text().includes("web-01"));
    expect(hostChip).toBeTruthy();

    await hostChip.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Worker 对话 · web-01");
    expect(wrapper.text()).toContain("web-01");
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

    await wrapper.get(".send-btn.stop-btn").trigger("click");
    await nextTick();

    expect(wrapper.text()).toContain("正在中断当前任务...");

    resolveStop({
      ok: true,
      json: async () => ({ accepted: true }),
    });
    await flushPromises();
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

    const stepEvidenceButton = wrapper.findAll(".plan-action").find((button) => button.text().includes("查看证据"));
    expect(stepEvidenceButton).toBeTruthy();

    await stepEvidenceButton.trigger("click");
    await flushPromises();

    const tabs = wrapper.findAll(".modal-tab").map((button) => button.text());
    expect(tabs.join(" ")).toContain("主 Agent 计划摘要");
    expect(tabs.join(" ")).toContain("Worker 对话");
    expect(tabs.join(" ")).toContain("Host Terminal");
    expect(tabs.join(" ")).toContain("审批上下文");
  });
});
