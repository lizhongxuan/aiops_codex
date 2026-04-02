import { mount, flushPromises } from "@vue/test-utils";
import { nextTick, reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChatPage from "../src/pages/ChatPage.vue";

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
    template: '<div class="card-item-stub">{{ card?.id }}</div>',
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
    template: '<div class="thinking-card-stub">{{ card?.phase }}</div>',
  },
}));

vi.mock("../src/components/PlanCard.vue", () => ({
  default: {
    props: ["card"],
    template: '<div class="plan-card-stub">{{ card?.id }}</div>',
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
      turn: { active: false, phase: "idle" },
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

describe("ChatPage terminal dock", () => {
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

  it("toggles the terminal dock from the toolbar and Ctrl+`", async () => {
    const wrapper = mountChatPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="chat-terminal-dock"]').exists()).toBe(false);

    await wrapper.get('[data-testid="chat-terminal-toggle"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="chat-terminal-dock"]').exists()).toBe(true);
    expect(mocks.terminalRef.takeover).toHaveBeenCalled();

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "`", code: "Backquote", ctrlKey: true, bubbles: true }));
    await nextTick();

    expect(wrapper.find('[data-testid="chat-terminal-dock"]').exists()).toBe(false);
  });

  it("resizes the terminal dock by dragging the resize handle", async () => {
    const wrapper = mountChatPage();
    await flushPromises();

    await wrapper.get('[data-testid="chat-terminal-toggle"]').trigger("click");
    await flushPromises();

    const dock = wrapper.get('[data-testid="chat-terminal-dock"]');
    expect(dock.attributes("style")).toContain("height: 320px");

    await wrapper.get('[data-testid="chat-terminal-resizer"]').trigger("mousedown", { clientY: 600 });
    window.dispatchEvent(new MouseEvent("mousemove", { clientY: 520, bubbles: true }));
    await nextTick();

    expect(wrapper.get('[data-testid="chat-terminal-dock"]').attributes("style")).toContain("height: 400px");

    window.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
  });

  it("switches the dock target when the selected host changes", async () => {
    const wrapper = mountChatPage();
    await flushPromises();

    await wrapper.get('[data-testid="chat-terminal-toggle"]').trigger("click");
    await flushPromises();

    expect(wrapper.get(".workspace-terminal-stub").attributes("data-host-id")).toBe("web-01");

    mocks.store.snapshot.selectedHostId = "web-02";
    await flushPromises();

    expect(wrapper.get(".workspace-terminal-stub").attributes("data-host-id")).toBe("web-02");
  });
});
