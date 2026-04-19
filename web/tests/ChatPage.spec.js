import { mount, flushPromises } from "@vue/test-utils";
import { nextTick, reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChatPage from "../src/pages/ChatPage.vue";
import { formatMainChatTurns } from "../src/lib/chatTurnFormatter";

const mocks = vi.hoisted(() => ({
  store: null,
  terminalRef: {
    takeover: vi.fn(async () => true),
    reconnect: vi.fn(() => true),
  },
}));

vi.mock("../src/store", () => ({
  useAppStore: () => mocks.store,
}));

vi.mock("../src/components/CardItem.vue", () => ({
  default: {
    props: ["card"],
    emits: ["approval"],
    template: `
      <div class="card-item-stub" :data-card-id="card?.id">
        <span class="card-item-stub-id">{{ card?.id }}</span>
        <button
          v-if="card?.type === 'CommandApprovalCard' || card?.type === 'FileChangeApprovalCard'"
          type="button"
          class="card-item-approval-action"
          @click="$emit('approval', { approvalId: card?.approval?.requestId, decision: 'accept' })"
        >
          approve
        </button>
      </div>
    `,
  },
}));

vi.mock("../src/components/Omnibar.vue", () => ({
  default: {
    props: ["modelValue"],
    emits: ["update:modelValue"],
    template: '<div class="omnibar-stub">omnibar</div>',
  },
}));

vi.mock("../src/components/ThinkingCard.vue", () => ({
  default: {
    props: ["card"],
    template: '<div class="thinking-card-stub">{{ card?.phase }} {{ card?.hint }}</div>',
  },
}));

vi.mock("../src/components/MessageCard.vue", () => ({
  default: {
    props: ["card"],
    template: '<div class="message-card-stub">{{ card?.text }}</div>',
  },
}));

vi.mock("../src/components/PlanCard.vue", () => ({
  default: {
    props: ["card"],
    template: '<div class="plan-card-stub">{{ card?.id }}</div>',
  },
}));


vi.mock("naive-ui", () => ({
  NBadge: {
    name: "NBadge",
    props: { value: [Number, String], dot: Boolean },
    template: '<span class="n-badge-stub"><slot /></span>',
  },
  NDrawer: {
    name: "NDrawer",
    props: { show: Boolean, placement: String, width: Number, maskClosable: Boolean },
    emits: ["update:show"],
    template: '<div v-if="show" data-testid="chat-mcp-surface-drawer"><slot /></div>',
  },
  NDrawerContent: {
    name: "NDrawerContent",
    props: { title: String, nativeScrollbar: Boolean, closable: Boolean },
    template: '<div class="n-drawer-content-stub"><slot /></div>',
  },
}));
const workspaceHostTerminalStub = {
  name: "WorkspaceHostTerminal",
  props: ["hostId", "hostName", "panelHeight"],
  emits: ["connected", "disconnected", "error"],
  setup(props, { expose }) {
    expose(mocks.terminalRef);
    return { props };
  },
  template:
    '<div class="workspace-terminal-stub" :data-host-id="hostId" :data-host-name="hostName" :data-panel-height="panelHeight">terminal</div>',
};

function createStoreFixture(overrides = {}) {
  const state = reactive({
    snapshot: {
      kind: "single_host",
      sessionId: "single-1",
      selectedHostId: "web-01",
      auth: { connected: true },
      config: { codexAlive: true },
      hosts: [
        { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        { id: "web-02", name: "web-02", status: "online", executable: true, terminalCapable: true },
      ],
      cards: [],
      approvals: [],
    },
    runtime: {
      turn: { active: false, phase: "idle", pendingStart: false },
      codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
      activity: {
        viewedFiles: [],
        searchedWebQueries: [],
        searchedContentQueries: [],
      },
    },
    loading: false,
    sending: false,
    noticeMessage: "",
    errorMessage: "",
    fetchState: vi.fn(async () => true),
    connectWs: vi.fn(),
    setTurnPhase: vi.fn(),
    markTurnPendingStart: vi.fn((phase = "thinking") => {
      state.runtime.turn.active = false;
      state.runtime.turn.phase = phase;
      state.runtime.turn.pendingStart = true;
    }),
    clearTurnPendingStart: vi.fn(() => {
      state.runtime.turn.pendingStart = false;
    }),
    resetActivity: vi.fn(),
    canSend: true,
    ...overrides,
  });

  Object.defineProperty(state, "selectedHost", {
    get() {
      return state.snapshot.hosts.find((host) => host.id === state.snapshot.selectedHostId) || {
        id: state.snapshot.selectedHostId,
        name: state.snapshot.selectedHostId,
        status: "offline",
        executable: false,
        terminalCapable: false,
      };
    },
  });

  Object.defineProperty(state, "canOpenTerminal", {
    get() {
      const host = state.selectedHost;
      return host.status === "online" && (host.terminalCapable || host.executable);
    },
  });

  return state;
}

function mountChatPage() {
  return mount(ChatPage, {
    global: {
      stubs: {
        WorkspaceHostTerminal: workspaceHostTerminalStub,
      },
    },
  });
}

describe("ChatPage command previews", () => {
  beforeEach(() => {
    mocks.store = createStoreFixture();
    mocks.terminalRef.takeover.mockClear();
    mocks.terminalRef.reconnect.mockClear();
    if (typeof global.ResizeObserver === "undefined") {
      global.ResizeObserver = class {
        observe() {}
        disconnect() {}
      };
    }
  });

  it("shows a compact live terminal preview for the running command", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-cpu-1",
            type: "UserMessageCard",
            role: "user",
            text: "查看主机CPU",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "cmd-cpu-live-1",
            type: "CommandCard",
            hostId: "web-01",
            status: "running",
            command: "/bin/zsh -lc ps -Ao %cpu,comm | sort -nr | head -n 10",
            output: [
              "PID COMMAND %CPU",
              "123 WindowServer 50.1",
              "456 Codex Helper 44.2",
              "789 Codex Helper 29.3",
            ].join("\n"),
            createdAt: "2026-04-16T10:00:03Z",
            updatedAt: "2026-04-16T10:00:06Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "executing", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    const liveStatus = wrapper.get('[data-testid="codex-activity-section"]');
    expect(liveStatus.text()).toContain("Working for");
    expect(wrapper.get('[data-testid="chat-live-terminal-preview"]').text()).toContain("ps -Ao %cpu,comm | sort -nr | head -n 10");
    expect(wrapper.get('[data-testid="chat-live-terminal-preview"]').text()).toContain("WindowServer 50.1");
    expect(wrapper.find(".chat-composer-hint").exists()).toBe(false);
  });

  it("normalizes detail.display into structured process item view models", () => {
    const turns = formatMainChatTurns({
      conversationCards: [
        {
          id: "user-display-1",
          type: "UserMessageCard",
          role: "user",
          text: "查看运行结果",
          createdAt: "2026-04-16T10:00:00Z",
          updatedAt: "2026-04-16T10:00:00Z",
        },
        {
          id: "assistant-display-final-1",
          type: "AssistantMessageCard",
          role: "assistant",
          text: "结果已经整理完毕。",
          createdAt: "2026-04-16T10:00:20Z",
          updatedAt: "2026-04-16T10:00:20Z",
        },
      ],
      commandCards: [
        {
          id: "command-display-1",
          type: "CommandCard",
          hostId: "web-01",
          status: "completed",
          command: "ps -Ao %cpu,comm | sort -nr | head -n 10",
          output: "WindowServer 50.1",
          detail: {
            display: {
              summary: "查询到 3 条结果",
              activity: "searching",
              blocks: [
                { kind: "text", text: "最近查询：nginx latest status" },
                {
                  kind: "kv_list",
                  title: "状态摘要",
                  items: [
                    { label: "状态", value: "running" },
                    { label: "耗时", value: "12s" },
                  ],
                },
                {
                  kind: "search_queries",
                  title: "查询记录",
                  items: [{ query: "nginx latest status" }],
                },
                {
                  kind: "link_list",
                  title: "参考链接",
                  items: [{ label: "Nginx docs", url: "https://nginx.org/en/docs/" }],
                },
                { kind: "warning", title: "提醒", text: "结果可能需要复核" },
                {
                  kind: "file_preview",
                  title: "app.log",
                  items: [{ path: "/var/log/app.log", text: "line 1\nline 2" }],
                },
                {
                  kind: "file_diff_summary",
                  title: "server.go",
                  items: [{ path: "server.go", added: 12, removed: 4, summary: "+12 -4" }],
                },
                {
                  kind: "result_stats",
                  title: "结果统计",
                  items: [
                    { label: "命中", value: 3 },
                    { label: "来源", value: 2 },
                  ],
                },
                { kind: "command", title: "已执行命令", text: "ps -Ao %cpu,comm | sort -nr | head -n 10" },
              ],
            },
          },
          createdAt: "2026-04-16T10:00:03Z",
          updatedAt: "2026-04-16T10:00:06Z",
        },
      ],
      turnActive: false,
    });

    expect(turns).toHaveLength(1);
    expect(turns[0].processItems).toHaveLength(1);
    expect(turns[0].processItems[0].display.summary).toBe("查询到 3 条结果");
    expect(turns[0].processItems[0].display.blocks.map((block) => block.kind)).toEqual([
      "text",
      "kv_list",
      "search_queries",
      "link_list",
      "warning",
      "file_preview",
      "file_diff_summary",
      "result_stats",
      "command",
    ]);
  });

  it("hides the live terminal preview once the command has completed", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-cpu-1",
            type: "UserMessageCard",
            role: "user",
            text: "查看主机CPU",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "assistant-cpu-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "CPU 当前不是打满状态。",
            createdAt: "2026-04-16T10:00:08Z",
            updatedAt: "2026-04-16T10:00:08Z",
          },
          {
            id: "cmd-cpu-done-1",
            type: "CommandCard",
            hostId: "web-01",
            status: "completed",
            command: "top -l 1 | grep '^CPU usage'",
            output: "CPU usage: 13.7% user, 13.75% sys, 73.17% idle",
            createdAt: "2026-04-16T10:00:03Z",
            updatedAt: "2026-04-16T10:00:06Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="chat-live-terminal-preview"]').exists()).toBe(false);
    expect(wrapper.find(".chat-composer-hint").exists()).toBe(false);
  });

  it("keeps completed command previews inside the process fold", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-cpu-1",
            type: "UserMessageCard",
            role: "user",
            text: "查看主机CPU",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "assistant-cpu-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "CPU 当前不是打满状态。",
            createdAt: "2026-04-16T10:00:08Z",
            updatedAt: "2026-04-16T10:00:08Z",
          },
          {
            id: "cmd-cpu-done-1",
            type: "CommandCard",
            hostId: "web-01",
            status: "completed",
            command: "ps -Ao %cpu,comm | sort -nr | head -n 10",
            output: [
              "PID COMMAND %CPU",
              "123 WindowServer 50.1",
              "456 Codex Helper 44.2",
              "789 Codex Helper 29.3",
            ].join("\n"),
            createdAt: "2026-04-16T10:00:03Z",
            updatedAt: "2026-04-16T10:00:06Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    await wrapper.get('[data-testid="chat-process-fold-turn-user-cpu-1"] .chat-process-toggle').trigger("click");
    expect(wrapper.get('[data-testid="chat-process-command-row-command-cmd-cpu-done-1"]').text()).toContain("已运行 ps -Ao %cpu,comm | sort -nr | head -n 10");
    expect(wrapper.find('[data-testid="chat-process-terminal-command-cmd-cpu-done-1"]').exists()).toBe(false);

    await wrapper.get('[data-testid="chat-process-command-row-command-cmd-cpu-done-1"]').trigger("click");
    expect(wrapper.get('[data-testid="chat-process-terminal-command-cmd-cpu-done-1"]').text()).toContain("WindowServer 50.1");
  });

  it("renders structured display blocks in the process fold while keeping command preview intact", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-display-2",
            type: "UserMessageCard",
            role: "user",
            text: "查看运行结果",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "assistant-display-2",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结果已经整理完毕。",
            createdAt: "2026-04-16T10:00:20Z",
            updatedAt: "2026-04-16T10:00:20Z",
          },
          {
            id: "command-display-2",
            type: "CommandCard",
            hostId: "web-01",
            status: "completed",
            command: "ps -Ao %cpu,comm | sort -nr | head -n 10",
            output: [
              "PID COMMAND %CPU",
              "123 WindowServer 50.1",
            ].join("\n"),
            detail: {
              display: {
                summary: "查询到 3 条结果",
                blocks: [
                  { kind: "text", text: "最近查询：nginx latest status" },
                  {
                    kind: "kv_list",
                    title: "状态摘要",
                    items: [
                      { label: "状态", value: "running" },
                      { label: "耗时", value: "12s" },
                    ],
                  },
                  {
                    kind: "search_queries",
                    title: "查询记录",
                    items: [{ query: "nginx latest status" }],
                  },
                  {
                    kind: "link_list",
                    title: "参考链接",
                    items: [{ label: "Nginx docs", url: "https://nginx.org/en/docs/" }],
                  },
                  { kind: "warning", title: "提醒", text: "结果可能需要复核" },
                  {
                    kind: "file_preview",
                    title: "app.log",
                    items: [{ path: "/var/log/app.log", text: "line 1\nline 2" }],
                  },
                  {
                    kind: "file_diff_summary",
                    title: "server.go",
                    items: [{ path: "server.go", added: 12, removed: 4, summary: "+12 -4" }],
                  },
                  {
                    kind: "result_stats",
                    title: "结果统计",
                    items: [{ label: "命中", value: 3 }],
                  },
                ],
              },
            },
            createdAt: "2026-04-16T10:00:03Z",
            updatedAt: "2026-04-16T10:00:06Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    await wrapper.get('[data-testid="chat-process-fold-turn-user-display-2"] .chat-process-toggle').trigger("click");
    await nextTick();

    const processFold = wrapper.get('[data-testid="chat-process-fold-turn-user-display-2"]');
    expect(processFold.text()).toContain("最近查询：nginx latest status");
    expect(processFold.text()).toContain("状态摘要");
    expect(processFold.text()).toContain("状态");
    expect(processFold.text()).toContain("running");
    expect(processFold.text()).toContain("查询记录");
    expect(processFold.text()).toContain("参考链接");
    expect(processFold.text()).toContain("结果可能需要复核");
    expect(processFold.text()).toContain("/var/log/app.log");
    expect(processFold.text()).toContain("server.go");
    expect(processFold.text()).toContain("+12 -4");
    expect(processFold.text()).toContain("结果统计");

    expect(processFold.get('[data-testid="chat-process-command-row-command-command-display-2"]').text()).toContain("已运行 ps -Ao %cpu,comm | sort -nr | head -n 10");
    expect(processFold.find('[data-testid="chat-process-terminal-command-command-display-2"]').exists()).toBe(false);

    await processFold.get('[data-testid="chat-process-command-row-command-command-display-2"]').trigger("click");
    expect(processFold.get('[data-testid="chat-process-terminal-command-command-display-2"]').text()).toContain("WindowServer 50.1");
  });

  it("keeps the single-host active turn lightweight while the live status stays visible", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我查一下 nginx 的情况",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-prelude-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先查一下最新网页信息，再给你一个简洁结论。",
            createdAt: "2026-04-03T10:00:01Z",
            updatedAt: "2026-04-03T10:00:01Z",
          },
          {
            id: "thinking-1",
            type: "ThinkingCard",
            phase: "thinking",
            hint: "我先查一下最新网页信息，再给你一个简洁结论。",
            createdAt: "2026-04-03T10:00:02Z",
            updatedAt: "2026-04-03T10:00:02Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "thinking" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [{ query: "nginx latest status" }],
          searchedContentQueries: [],
          currentSearchQuery: "nginx latest status",
          currentSearchKind: "web",
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.get('[data-testid="chat-turn-turn-user-1"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="chat-process-fold-turn-user-1"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("我先查一下最新网页信息，再给你一个简洁结论。");
    expect(wrapper.find('[data-card-id="thinking-1"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="codex-activity-section"]').text()).toContain("Working for");
    expect(wrapper.get('[data-testid="codex-activity-section"]').text()).not.toContain("正在思考");
    expect(wrapper.get('[data-testid="codex-activity-section"]').text()).toContain("正在搜索网页（nginx latest status）");
    expect(wrapper.findAll('[data-testid="chat-live-status-card"]')).toHaveLength(1);
    expect(
      wrapper.element.querySelector(
        '[data-testid="chat-turn-turn-user-1"] .chat-turn-live-status [data-testid="chat-live-status-card"]',
      ),
    ).toBeTruthy();

  });

  it("keeps the live status card visible while the turn is finalizing", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我汇总一下",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先整理结果。",
            createdAt: "2026-04-03T10:00:05Z",
            updatedAt: "2026-04-03T10:00:05Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "finalizing" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.get('[data-testid="codex-activity-section"]').text()).toContain("Working for");
    expect(wrapper.get('[data-testid="codex-activity-section"]').text()).not.toContain("正在整理结果");
  });

  it("renders tool invocation progress lines under the live status for single-host turns", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        toolInvocations: [
          {
            id: "inv-search-1",
            name: "web_search",
            status: "completed",
            inputSummary: "A股 指数 快照",
            startedAt: "2026-04-16T10:00:01Z",
            completedAt: "2026-04-16T10:00:02Z",
          },
          {
            id: "inv-open-1",
            name: "open_page",
            status: "running",
            inputSummary: "https://finance.example.com/ashare-close",
            startedAt: "2026-04-16T10:00:03Z",
          },
        ],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "看下A股行情",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "thinking", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    const liveStatus = wrapper.get('[data-testid="codex-activity-section"]').text();
    expect(liveStatus).toContain("已搜索网页（A股 指数 快照）");
    expect(liveStatus).toContain("正在浏览网页（https://finance.example.com/ashare-close）");
  });

  it("streams the in-progress final answer while moving finished search context into the turn fold", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-btc-live-1",
            type: "UserMessageCard",
            role: "user",
            text: "查看一下BTC的今天行情",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "assistant-btc-live-prelude-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先抓取 CoinGecko 和 CoinMarketCap 的报价。",
            status: "completed",
            createdAt: "2026-04-16T10:00:02Z",
            updatedAt: "2026-04-16T10:00:02Z",
          },
          {
            id: "assistant-btc-live-final-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "截至 2026-04-16 18:29 CST，BTC 约 $74.45k，日内偏强。",
            status: "inProgress",
            createdAt: "2026-04-16T10:00:04Z",
            updatedAt: "2026-04-16T10:00:05Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "finalizing", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [{ query: "BTC price today" }],
          searchedContentQueries: [],
          currentSearchQuery: "BTC price today",
          currentSearchKind: "web",
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.text()).toContain("截至 2026-04-16 18:29 CST，BTC 约 $74.45k");
    const processFold = wrapper.get('[data-testid="chat-process-fold-turn-user-btc-live-1"]');
    expect(processFold.text()).toContain("已搜索网页（BTC price today）");
    expect(processFold.text()).not.toContain("正在搜索网页（BTC price today）");
    expect(wrapper.text()).not.toContain("我先抓取 CoinGecko 和 CoinMarketCap 的报价。");
    expect(wrapper.find('[data-testid="codex-activity-section"]').exists()).toBe(false);
    expect(
      wrapper.element.querySelector(
        '[data-testid="chat-turn-turn-user-btc-live-1"] .chat-turn-live-status [data-testid="chat-live-status-card"]',
      ),
    ).toBeFalsy();

    mocks.store.runtime.turn.active = false;
    mocks.store.runtime.turn.phase = "completed";
    await flushPromises();

    const completedFold = wrapper.get('[data-testid="chat-process-fold-turn-user-btc-live-1"]');
    expect(completedFold.find(".chat-process-surface").exists()).toBe(true);
    expect(completedFold.text()).toContain("已搜索网页（BTC price today）");
  });

  it("opens the MCP surface drawer from bundle detail, pin and refresh actions", async () => {
    const drawerEvents = [];
    const handler = (event) => drawerEvents.push(event.detail);
    window.addEventListener("codex:open-mcp-drawer", handler);

    try {
      mocks.store = createStoreFixture({
        snapshot: {
          kind: "single_host",
          sessionId: "single-1",
          selectedHostId: "web-01",
          auth: { connected: true },
          config: { codexAlive: true },
          hosts: [{ id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true }],
          cards: [
            {
              id: "user-1",
              type: "UserMessageCard",
              role: "user",
              text: "我想看 nginx 的完整监控面板。",
              createdAt: "2026-04-03T10:00:00Z",
              updatedAt: "2026-04-03T10:00:00Z",
            },
            {
              id: "assistant-1",
              type: "AssistantMessageCard",
              role: "assistant",
              text: "我把监控面板和动作一起挂出来。",
              createdAt: "2026-04-03T10:00:05Z",
              updatedAt: "2026-04-03T10:00:05Z",
              payload: {
                resultBundles: [
                  {
                    id: "bundle-1",
                    bundleKind: "monitor_bundle",
                    summary: "nginx 监控聚合面板",
                    subject: {
                      type: "service",
                      name: "nginx",
                      env: "prod",
                    },
                    freshness: {
                      label: "刚拉取",
                      capturedAt: "2026-04-03T10:00:05Z",
                    },
                    sections: [
                      {
                        kind: "overview",
                        title: "概览",
                        cards: [
                          {
                            id: "bundle-card-1",
                            uiKind: "readonly_summary",
                            title: "当前状态",
                            summary: "平稳",
                          },
                        ],
                      },
                    ],
                  },
                ],
              },
            },
          ],
          approvals: [],
        },
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      });

      const wrapper = mountChatPage();
      await flushPromises();

      expect(wrapper.get('[data-testid="mcp-bundle-subject"]').text()).toContain("nginx / prod");
      expect(wrapper.find('[data-testid="chat-mcp-surface-drawer"]').exists()).toBe(false);

      await wrapper.get('[data-testid="mcp-bundle-open-detail"]').trigger("click");
      await flushPromises();

      expect(wrapper.get('[data-testid="chat-mcp-surface-drawer"]').text()).toContain("nginx 监控聚合面板");
      expect(drawerEvents[0]).toMatchObject({
        source: "chat-mcp-surface",
        pin: false,
        surface: expect.objectContaining({
          kind: "bundle",
          title: "nginx 监控聚合面板",
        }),
      });

      await wrapper.get('[data-testid="chat-mcp-surface-pin"]').trigger("click");
      await flushPromises();

      expect(wrapper.get('[data-testid="chat-mcp-surface-drawer"]').text()).toContain("已固定");
      expect(drawerEvents.some((detail) => detail.pin === true)).toBe(true);

      await wrapper.get('[data-testid="chat-mcp-surface-refresh"]').trigger("click");
      await flushPromises();

      expect(mocks.store.fetchState).toHaveBeenCalled();
      expect(mocks.store.noticeMessage).toContain("已刷新");
      expect(wrapper.text()).toContain("已刷新");
    } finally {
      window.removeEventListener("codex:open-mcp-drawer", handler);
    }
  });

  it("keeps the final answer visible while completed turn details stay collapsed by default", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我看下 nginx",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先检查日志和 upstream 指标。",
            createdAt: "2026-04-03T10:00:05Z",
            updatedAt: "2026-04-03T10:00:05Z",
          },
          {
            id: "assistant-2",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "已经确认 nginx 本体正常，异常集中在 service-a upstream timeout。",
            createdAt: "2026-04-03T10:00:20Z",
            updatedAt: "2026-04-03T10:00:20Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    const processFold = wrapper.get('[data-testid="chat-process-fold-turn-user-1"]');

    expect(processFold.text()).toContain("已处理");
    expect(processFold.find(".chat-process-body").exists()).toBe(false);
    expect(wrapper.text()).toContain("已经确认 nginx 本体正常，异常集中在 service-a upstream timeout。");
    expect(wrapper.text()).not.toContain("我先检查日志和 upstream 指标。");

    await processFold.get(".chat-process-toggle").trigger("click");
    await nextTick();

    expect(processFold.find(".chat-process-surface").exists()).toBe(true);
    expect(processFold.find(".chat-process-body").exists()).toBe(true);
    expect(processFold.text()).toContain("我先检查日志和 upstream 指标。");
  });

  it("rehydrates completed process lines from snapshot activity after refresh", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-near-1",
            type: "UserMessageCard",
            role: "user",
            text: "查看今天山寨币near行情",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "assistant-near-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "截至 2026-04-16 当前搜索快照，NEAR Protocol（NEAR）报价约 $1.35–$1.43，24h 偏弱下跌。",
            createdAt: "2026-04-16T10:00:12Z",
            updatedAt: "2026-04-16T10:00:13Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          searchCount: 2,
          viewedFiles: [
            { url: "https://coinmarketcap.com/currencies/near-protocol/" },
          ],
          searchedWebQueries: [
            { query: "NEAR Protocol price today USD" },
            { query: "Binance NEAR USDT price" },
          ],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    const processFold = wrapper.get('[data-testid="chat-process-fold-turn-user-near-1"]');
    expect(processFold.text()).toContain("已处理");

    await processFold.get(".chat-process-toggle").trigger("click");
    await nextTick();

    expect(processFold.text()).toContain("已搜索网页（NEAR Protocol price today USD）");
    expect(processFold.text()).toContain("已搜索网页（Binance NEAR USDT price）");
    expect(processFold.text()).toContain("已浏览（https://coinmarketcap.com/currencies/near-protocol/）");
  });

  it("normalizes completed process lines into past tense and keeps them on one level", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        toolInvocations: [
          {
            id: "inv-search-1",
            name: "web_search",
            status: "completed",
            inputSummary: "BTC price today",
            startedAt: "2026-04-16T10:00:01Z",
            completedAt: "2026-04-16T10:00:02Z",
          },
          {
            id: "inv-open-1",
            name: "open_page",
            status: "running",
            inputSummary: "https://coinmarketcap.com/currencies/bitcoin/",
            startedAt: "2026-04-16T10:00:03Z",
          },
        ],
        cards: [
          {
            id: "user-btc-history-1",
            type: "UserMessageCard",
            role: "user",
            text: "查看一下BTC的今天行情",
            createdAt: "2026-04-16T10:00:00Z",
            updatedAt: "2026-04-16T10:00:00Z",
          },
          {
            id: "assistant-btc-history-prelude-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先抓取 CoinGecko 和 CoinMarketCap 的报价。",
            createdAt: "2026-04-16T10:00:02Z",
            updatedAt: "2026-04-16T10:00:02Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "thinking", hostId: "web-01", startedAt: "2026-04-16T10:00:00Z" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    mocks.store.snapshot.cards.push({
      id: "assistant-btc-history-final-1",
      type: "AssistantMessageCard",
      role: "assistant",
      text: "截至 2026-04-16 18:29 CST，BTC 约 $74.45k，日内偏强。",
      createdAt: "2026-04-16T10:00:06Z",
      updatedAt: "2026-04-16T10:00:06Z",
    });
    mocks.store.runtime.turn.active = false;
    mocks.store.runtime.turn.phase = "completed";
    await flushPromises();

    const processFold = wrapper.get('[data-testid="chat-process-fold-turn-user-btc-history-1"]');
    if (!processFold.find(".chat-process-surface").exists()) {
      await processFold.get(".chat-process-toggle").trigger("click");
      await nextTick();
    }

    expect(processFold.text()).toContain("已搜索网页（BTC price today）");
    expect(processFold.text()).toContain("已浏览网页（https://coinmarketcap.com/currencies/bitcoin/）");
    expect(processFold.text()).not.toContain("正在搜索网页");
    expect(processFold.text()).not.toContain("正在浏览网页");
    expect(processFold.find(".chat-search-fold").exists()).toBe(false);
    expect(processFold.find(".chat-process-live").exists()).toBe(false);
  });

  it("shows an unread pill instead of forcing scroll when new turn content arrives off-screen", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "先看看 nginx",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先看一下。",
            createdAt: "2026-04-03T10:00:05Z",
            updatedAt: "2026-04-03T10:00:05Z",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    const scrollContainer = wrapper.get(".chat-container").element;
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

    await wrapper.get(".chat-container").trigger("scroll");

    mocks.store.snapshot.cards.push({
      id: "assistant-2",
      type: "AssistantMessageCard",
      role: "assistant",
      text: "已经确认 service-a 的 upstream 有抖动。",
      createdAt: "2026-04-03T10:01:00Z",
      updatedAt: "2026-04-03T10:01:00Z",
    });
    await flushPromises();

    expect(wrapper.get('[data-testid="chat-unread-pill"]').text()).toContain("1 条新结果");
    expect(wrapper.get('[data-testid="chat-unread-divider"]').text()).toContain("未读更新");

    await wrapper.get('[data-testid="chat-unread-pill"]').trigger("click");
    await nextTick();
    await flushPromises();

    expect(currentScrollTop).toBe(1200);
    expect(scrollTopWrites.at(-1)).toBe(1200);
    expect(wrapper.find('[data-testid="chat-unread-pill"]').exists()).toBe(false);
  });

  it("keeps the rendered turn DOM bounded when a long history is expanded", async () => {
    const cards = [];
    for (let index = 1; index <= 30; index += 1) {
      cards.push(
        {
          id: `user-long-${index}`,
          type: "UserMessageCard",
          role: "user",
          text: `长会话问题 ${index}`,
          createdAt: `2026-04-03T09:${String(index).padStart(2, "0")}:00Z`,
          updatedAt: `2026-04-03T09:${String(index).padStart(2, "0")}:00Z`,
        },
        {
          id: `assistant-long-${index}`,
          type: "AssistantMessageCard",
          role: "assistant",
          text: `长会话结果 ${index}`,
          createdAt: `2026-04-03T09:${String(index).padStart(2, "0")}:30Z`,
          updatedAt: `2026-04-03T09:${String(index).padStart(2, "0")}:30Z`,
        },
      );
    }

    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards,
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    const scrollContainer = wrapper.get(".chat-container").element;
    let currentScrollTop = 5400;
    const scrollTopWrites = [];
    Object.defineProperty(scrollContainer, "clientHeight", { value: 720, configurable: true });
    Object.defineProperty(scrollContainer, "scrollHeight", { value: 7200, configurable: true });
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

    await wrapper.get(".chat-container").trigger("scroll");
    await flushPromises();

    const turnCount = wrapper.findAll(".chat-turn-group").length;
    expect(turnCount).toBeLessThan(30);

    const beforeLoad = currentScrollTop;
    await wrapper.get('[data-testid="chat-history-sentinel-load-older"]').trigger("click");
    await flushPromises();

    expect(currentScrollTop).toBeGreaterThanOrEqual(beforeLoad);
    expect(scrollTopWrites.length).toBeGreaterThan(0);
    expect(wrapper.find('[data-testid="chat-virtual-top-spacer"]').exists()).toBe(true);
    expect(wrapper.findAll(".chat-turn-group").length).toBeLessThan(30);
  });

  it("renders the plan widget inside the composer dock", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "plan-1",
            type: "PlanCard",
            items: [{ step: "检查 nginx", status: "running" }],
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: true, phase: "planning" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.findComponent({ name: "ChatComposerDock" }).exists()).toBe(true);
    expect(wrapper.find(".plan-card-stub").exists()).toBe(true);
    expect(wrapper.find(".omnibar-stub").exists()).toBe(true);
  });

  it("keeps resolved approvals and terminal output out of the main chat thread body", async () => {
    mocks.store = createStoreFixture({
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [
          { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
        ],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我看下 nginx",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先检查当前状态。",
            createdAt: "2026-04-03T10:00:05Z",
            updatedAt: "2026-04-03T10:00:05Z",
          },
          {
            id: "approval-resolved",
            type: "CommandApprovalCard",
            status: "accepted",
            command: "systemctl reload nginx",
            approval: { requestId: "approval-1" },
          },
          {
            id: "command-1",
            type: "CommandCard",
            title: "systemctl status nginx",
            summary: "检查 nginx 状态",
            output: "active (running)",
          },
        ],
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.text()).not.toContain("approval-resolved");
    expect(wrapper.text()).not.toContain("command-1");
    expect(wrapper.text()).not.toContain("最近命令输出已收进终端面板");
  });

  it("keeps pending approvals out of the thread while leaving the overlay actionable", async () => {
    const originalFetch = global.fetch;
    global.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ accepted: true }),
    }));

    try {
      mocks.store = createStoreFixture({
        snapshot: {
          kind: "single_host",
          sessionId: "single-1",
          selectedHostId: "web-01",
          auth: { connected: true },
          config: { codexAlive: true },
          hosts: [
            { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
          ],
          cards: [
            {
              id: "user-1",
              type: "UserMessageCard",
              role: "user",
              text: "请帮我 reload nginx 并确认结果",
              createdAt: "2026-04-03T10:00:00Z",
              updatedAt: "2026-04-03T10:00:00Z",
            },
            {
              id: "approval-pending-1",
              type: "CommandApprovalCard",
              status: "pending",
              text: "需要批准 reload nginx",
              command: "systemctl reload nginx",
              approval: {
                requestId: "approval-1",
                decisions: ["accept", "accept_session", "decline"],
              },
              createdAt: "2026-04-03T10:00:10Z",
              updatedAt: "2026-04-03T10:00:10Z",
            },
          ],
          approvals: [{ id: "approval-1", status: "pending", itemId: "approval-pending-1" }],
        },
        runtime: {
          turn: { active: true, phase: "waiting_approval" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      });

      const wrapper = mountChatPage();
      await flushPromises();

      expect(wrapper.find(".chat-stream").text()).not.toContain("approval-pending-1");
      expect(wrapper.get('[data-testid="codex-approval-inline"]').text()).toContain("systemctl reload nginx");
      expect(wrapper.find(".omnibar-stub").exists()).toBe(false);

      await wrapper.get('[data-testid="codex-approval-inline"] .codex-submit-btn').trigger("click");
      await flushPromises();

      expect(mocks.store.setTurnPhase).toHaveBeenCalledWith("executing");
      expect(global.fetch).toHaveBeenCalledWith(
        "/api/v1/approvals/approval-1/decision",
        expect.objectContaining({
          method: "POST",
          credentials: "include",
        }),
      );
    } finally {
      global.fetch = originalFetch;
    }
  });

  it("renders a synthetic MCP approval overlay for mutation actions and dismisses it locally", async () => {
    const originalFetch = global.fetch;
    global.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ accepted: true }),
    }));

    try {
      mocks.store = createStoreFixture({
        snapshot: {
          kind: "single_host",
          sessionId: "single-1",
          selectedHostId: "web-01",
          auth: { connected: true },
          config: { codexAlive: true },
          hosts: [
            { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
          ],
          cards: [
            {
              id: "user-1",
              type: "UserMessageCard",
              role: "user",
              text: "请把这次中间件控制面板也一起展示出来。",
              createdAt: "2026-04-03T10:00:00Z",
              updatedAt: "2026-04-03T10:00:00Z",
            },
            {
              id: "assistant-1",
              type: "AssistantMessageCard",
              role: "assistant",
              text: "我已经整理出控制面板和结果 bundle。",
              createdAt: "2026-04-03T10:00:08Z",
              updatedAt: "2026-04-03T10:00:08Z",
              payload: {
                actionSurfaces: [
                  {
                    id: "mcp-action-1",
                    uiKind: "action_panel",
                    title: "控制面板",
                    summary: "这里会承载 mutation action。",
                    actions: [
                      {
                      id: "restart-nginx",
                      label: "重启 nginx",
                      intent: "restart_service",
                      mutation: true,
                      approvalMode: "required",
                      confirmText: "确认后将申请审批并重启 nginx",
                      permissionPath: "mcp.ops.service.restart",
                      target: {
                        kind: "service",
                        label: "web-01 / nginx",
                      },
                      },
                    ],
                  },
                ],
                resultBundles: [
                  {
                    id: "bundle-1",
                    bundleKind: "monitor_bundle",
                    summary: "nginx 监控概览",
                    subject: {
                      type: "service",
                      name: "nginx",
                      env: "prod",
                    },
                    sections: [
                      {
                        kind: "overview",
                        title: "概览",
                        cards: [
                          {
                            id: "bundle-card-1",
                            uiKind: "readonly_summary",
                            title: "当前状态",
                            summary: "稳定",
                          },
                        ],
                      },
                    ],
                  },
                ],
              },
            },
          ],
          approvals: [],
        },
        runtime: {
          turn: { active: false, phase: "completed" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      });

      const wrapper = mountChatPage();
      await flushPromises();

      expect(wrapper.get(".mcp-control-panel-card").text()).toContain("控制面板");
      expect(wrapper.get('[data-testid="mcp-bundle-subject"]').text()).toContain("nginx");
      expect(wrapper.get('[data-testid="mcp-bundle-summary"]').text()).toContain("nginx 监控概览");
      expect(wrapper.find('[data-testid="chat-mcp-approval-overlay"]').exists()).toBe(false);

      await wrapper.get('[data-testid="mcp-control-panel-action"]').trigger("click");
      await flushPromises();

      const overlay = wrapper.get('[data-testid="chat-mcp-approval-overlay"]');
      expect(overlay.text()).toContain("web-01 / nginx");
      expect(overlay.text()).toContain("mcp.ops.service.restart");
      expect(wrapper.find(".auth-overlay-dock .card-item-stub").exists()).toBe(false);
      expect(global.fetch).not.toHaveBeenCalled();

      await wrapper.get('[data-testid="chat-mcp-approval-reject"]').trigger("click");
      await flushPromises();

      expect(wrapper.find('[data-testid="chat-mcp-approval-overlay"]').exists()).toBe(false);
      expect(wrapper.text()).not.toContain("需要批准");
    } finally {
      global.fetch = originalFetch;
    }
  });

  it("shows an away summary after the user returns and surfaces the history sentinel", async () => {
    vi.useFakeTimers();
    const originalVisibilityState = document.visibilityState;

    try {
      mocks.store = createStoreFixture({
        sessionList: [
          { id: "single-1", kind: "single_host", title: "Nginx chat", status: "running" },
          { id: "single-0", kind: "single_host", title: "Earlier chat", status: "completed" },
        ],
        snapshot: {
          kind: "single_host",
          sessionId: "single-1",
          selectedHostId: "web-01",
          auth: { connected: true },
          config: { codexAlive: true },
          hosts: [
            { id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true },
          ],
          cards: Array.from({ length: 11 }, (_value, index) => ({
            id: `notice-${index}`,
            type: "NoticeCard",
            text: index === 0 ? "帮我看下 nginx" : `历史消息 ${index}`,
            createdAt: `2026-04-03T10:${String(index).padStart(2, "0")}:00Z`,
            updatedAt: `2026-04-03T10:${String(index).padStart(2, "0")}:00Z`,
          })),
          approvals: [],
        },
        runtime: {
          turn: { active: false, phase: "completed" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      });

      const wrapper = mountChatPage();
      await flushPromises();

      expect(wrapper.get('[data-testid="chat-history-sentinel"]').text()).toContain("更早上下文已折叠");

      Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
      document.dispatchEvent(new Event("visibilitychange"));
      vi.advanceTimersByTime(60_000);

      mocks.store.snapshot.cards.push(
        {
          id: "user-2",
          type: "UserMessageCard",
          role: "user",
          text: "继续看下 upstream timeout",
          createdAt: "2026-04-03T10:01:00Z",
          updatedAt: "2026-04-03T10:01:00Z",
        },
        {
          id: "assistant-2",
          type: "AssistantMessageCard",
          role: "assistant",
          text: "已经确认最新异常来自 service-a 的 upstream timeout。",
          createdAt: "2026-04-03T10:01:05Z",
          updatedAt: "2026-04-03T10:01:05Z",
        },
      );

      Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
      document.dispatchEvent(new Event("visibilitychange"));
      await nextTick();
      await flushPromises();

      expect(wrapper.get('[data-testid="chat-away-summary"]').text()).toContain("你离开期间有新进展");
      expect(wrapper.get('[data-testid="chat-away-summary"]').text()).toContain("service-a 的 upstream timeout");
    } finally {
      Object.defineProperty(document, "visibilityState", { value: originalVisibilityState, configurable: true });
      vi.useRealTimers();
    }
  });

  it("opens session history from the history sentinel action", async () => {
    mocks.store = createStoreFixture({
      sessionList: [
        {
          id: "single-1",
          kind: "single_host",
          title: "当前会话",
          status: "completed",
          preview: "当前会话",
          lastActivityAt: "2026-04-03T10:00:00Z",
        },
        {
          id: "single-2",
          kind: "single_host",
          title: "更早会话",
          status: "completed",
          preview: "更早会话",
          lastActivityAt: "2026-04-03T09:30:00Z",
        },
      ],
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [{ id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true }],
        cards: Array.from({ length: 11 }, (_value, index) => ({
          id: `user-old-${index}`,
          type: "UserMessageCard",
          role: "user",
          text: index === 0 ? "最早的一条聊天记录" : `历史消息 ${index}`,
          createdAt: `2026-04-03T09:${String(index).padStart(2, "0")}:00Z`,
          updatedAt: `2026-04-03T09:${String(index).padStart(2, "0")}:00Z`,
        })),
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const handler = vi.fn();
    window.addEventListener("codex:open-session-history", handler);

    try {
      const wrapper = mountChatPage();
      await flushPromises();

      await wrapper.get('[data-testid="chat-history-sentinel-open"]').trigger("click");

      expect(handler).toHaveBeenCalledTimes(1);
      expect(handler.mock.calls[0][0].detail).toMatchObject({
        source: "chat-history-sentinel",
      });
    } finally {
      window.removeEventListener("codex:open-session-history", handler);
    }
  });

  it("loads older chat entries from the compact history boundary and then reaches the session start", async () => {
    mocks.store = createStoreFixture({
      sessionList: [
        { id: "single-1", kind: "single_host", title: "当前会话", status: "completed" },
        { id: "single-0", kind: "single_host", title: "更早会话", status: "completed" },
      ],
      snapshot: {
        kind: "single_host",
        sessionId: "single-1",
        selectedHostId: "web-01",
        auth: { connected: true },
        config: { codexAlive: true },
        hosts: [{ id: "web-01", name: "web-01", status: "online", executable: true, terminalCapable: true }],
        cards: Array.from({ length: 11 }, (_value, index) => ({
          id: `user-${index}`,
          type: "UserMessageCard",
          role: "user",
          text: index === 0 ? "最早的一条消息" : `消息 ${index}`,
          createdAt: `2026-04-03T10:${String(index).padStart(2, "0")}:00Z`,
          updatedAt: `2026-04-03T10:${String(index).padStart(2, "0")}:00Z`,
        })),
        approvals: [],
      },
      runtime: {
        turn: { active: false, phase: "completed" },
        codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
        activity: {
          viewedFiles: [],
          searchedWebQueries: [],
          searchedContentQueries: [],
        },
      },
    });

    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.get('[data-testid="chat-history-sentinel"]').text()).toContain("更早上下文已折叠");
    expect(wrapper.find('[data-testid="chat-history-sentinel-load-older"]').exists()).toBe(true);
    expect(wrapper.text()).not.toContain("最早的一条消息");

    await wrapper.get('[data-testid="chat-history-sentinel-load-older"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="chat-history-sentinel"]').text()).toContain("已到会话开头");
    expect(wrapper.text()).toContain("最早的一条消息");
  });
});
