import { mount, flushPromises } from "@vue/test-utils";
import { nextTick, reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentProfilePage from "../src/pages/AgentProfilePage.vue";

const mocks = vi.hoisted(() => ({
  router: {
    push: vi.fn(),
  },
  store: null,
  confirm: null,
}));

vi.mock("../src/store", () => ({
  useAppStore: () => mocks.store,
}));

vi.mock("vue-router", () => ({
  useRouter: () => mocks.router,
  onBeforeRouteLeave: vi.fn(),
}));

function createCategoryPolicies() {
  return [
    { id: "system_inspection", label: "system_inspection", mode: "allow" },
    { id: "service_read", label: "service_read", mode: "allow" },
    { id: "network_read", label: "network_read", mode: "allow" },
    { id: "file_read", label: "file_read", mode: "allow" },
    { id: "service_mutation", label: "service_mutation", mode: "approval_required" },
    { id: "filesystem_mutation", label: "filesystem_mutation", mode: "approval_required" },
    { id: "package_mutation", label: "package_mutation", mode: "deny" },
  ];
}

function createCapabilityPermissions() {
  return [
    { id: "commandExecution", label: "commandExecution", state: "enabled" },
    { id: "fileRead", label: "fileRead", state: "enabled" },
    { id: "fileSearch", label: "fileSearch", state: "enabled" },
    { id: "fileChange", label: "fileChange", state: "approval_required" },
    { id: "terminal", label: "terminal", state: "enabled" },
    { id: "webSearch", label: "webSearch", state: "enabled" },
    { id: "webOpen", label: "webOpen", state: "enabled" },
    { id: "approval", label: "approval", state: "enabled" },
    { id: "multiAgent", label: "multiAgent", state: "enabled" },
    { id: "plan", label: "plan", state: "enabled" },
    { id: "summary", label: "summary", state: "enabled" },
  ];
}

function createSkills() {
  return [
    {
      id: "ops-triage",
      name: "Ops Triage",
      description: "快速归类问题并给出最小干预路径。",
      source: "built-in",
      enabled: true,
      activationMode: "default_enabled",
    },
    {
      id: "safe-change-review",
      name: "Safe Change Review",
      description: "在执行前做变更影响检查。",
      source: "built-in",
      enabled: false,
      activationMode: "disabled",
    },
  ];
}

function createMcps() {
  return [
    {
      id: "filesystem",
      name: "Filesystem MCP",
      type: "stdio",
      source: "built-in",
      enabled: true,
      permission: "readonly",
      requiresExplicitUserApproval: false,
    },
    {
      id: "docs",
      name: "Docs MCP",
      type: "http",
      source: "local",
      enabled: true,
      permission: "readonly",
      requiresExplicitUserApproval: true,
    },
    {
      id: "metrics",
      name: "Metrics MCP",
      type: "http",
      source: "built-in",
      enabled: false,
      permission: "readwrite",
      requiresExplicitUserApproval: true,
    },
  ];
}

function createProfile(overrides = {}) {
  return {
    id: "main-agent",
    name: "Primary Agent",
    type: "main-agent",
    description: "Default profile for the primary Codex agent",
    runtime: {
      model: "gpt-5.4",
      reasoningEffort: "medium",
      approvalPolicy: "untrusted",
      sandboxMode: "workspace-write",
    },
    systemPrompt: {
      content: "Saved prompt from server.",
      notes: "Keep it concise.",
    },
    commandPermissions: {
      enabled: true,
      defaultMode: "approval_required",
      allowShellWrapper: true,
      allowSudo: true,
      defaultTimeoutSeconds: 60,
      allowedWritableRoots: ["/workspace"],
      categoryPolicies: createCategoryPolicies(),
    },
    capabilityPermissions: createCapabilityPermissions(),
    skills: createSkills(),
    mcps: createMcps(),
    ...overrides,
  };
}

function createPreviewFromProfile(profile) {
  return {
    profileId: profile.id,
    profileType: profile.type,
    systemPrompt: profile.systemPrompt.content,
    systemPromptLines: String(profile.systemPrompt.content || "").split("\n").length,
    commandSummary: profile.commandPermissions.categoryPolicies.map((item) => `${item.label}: ${item.mode}`),
    capabilitySummary: profile.capabilityPermissions.map((item) => `${item.label}: ${item.state}`),
    enabledSkills: profile.skills.filter((item) => item.enabled),
    enabledMcps: profile.mcps.filter((item) => item.enabled),
    runtime: profile.runtime,
  };
}

function createStoreFixture() {
  const initialProfiles = [
    createProfile(),
    createProfile({
      id: "host-agent-default",
      name: "Host Agent Default",
      type: "host-agent-default",
      systemPrompt: {
        content: "Host agent prompt.",
        notes: "",
      },
    }),
  ];
  const state = reactive({
    agentProfiles: initialProfiles,
    activeAgentProfileId: "main-agent",
    agentProfilePreview: createPreviewFromProfile(initialProfiles[0]),
    agentProfilesError: "",
    agentProfileFieldErrors: {},
    agentProfileSaving: false,
    agentProfilePreviewLoading: false,
    agentProfilesLoading: false,
    fetchAgentProfiles: vi.fn(async () => true),
    fetchAgentProfilePreview: vi.fn(async (profileId) => {
      const profile = state.agentProfiles.find((item) => item.id === profileId) || state.activeAgentProfile;
      if (!profile) return null;
      state.agentProfilePreview = createPreviewFromProfile(profile);
      return state.agentProfilePreview;
    }),
    selectAgentProfile: vi.fn((profileId) => {
      const nextId = String(profileId || "");
      if (!state.agentProfiles.some((item) => item.id === nextId)) {
        return false;
      }
      state.activeAgentProfileId = nextId;
      state.agentProfilePreview = null;
      state.agentProfileFieldErrors = {};
      return true;
    }),
    saveAgentProfile: vi.fn(async (profile, options = {}) => {
      state.agentProfileSaving = true;
      const normalized = JSON.parse(JSON.stringify(profile));
      const index = state.agentProfiles.findIndex((item) => item.id === normalized.id);
      if (index >= 0) {
        state.agentProfiles[index] = normalized;
      } else {
        state.agentProfiles.push(normalized);
      }
      state.activeAgentProfileId = normalized.id;
      state.agentProfilePreview = createPreviewFromProfile(normalized);
      state.agentProfileSaving = false;
      state.agentProfileFieldErrors = {};
      state.lastSave = { profile: normalized, options };
      return true;
    }),
    resetAgentProfile: vi.fn(async (profileId) => {
      const nextId = String(profileId || "main-agent");
      const fallback = createProfile({
        id: nextId,
        type: nextId,
      });
      const index = state.agentProfiles.findIndex((item) => item.id === nextId);
      if (index >= 0) {
        state.agentProfiles[index] = fallback;
      } else {
        state.agentProfiles.push(fallback);
      }
      state.activeAgentProfileId = nextId;
      state.agentProfilePreview = createPreviewFromProfile(fallback);
      return true;
    }),
  });

  Object.defineProperty(state, "activeAgentProfile", {
    enumerable: true,
    get() {
      return state.agentProfiles.find((item) => item.id === state.activeAgentProfileId) || state.agentProfiles[0] || null;
    },
  });

  return state;
}

function mountPage() {
  return mount(AgentProfilePage, {
    global: {
      stubs: {
        teleport: true,
      },
    },
  });
}

describe("AgentProfilePage", () => {
  beforeEach(() => {
    mocks.router.push.mockReset();
    mocks.store = createStoreFixture();
    mocks.confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
  });

  it("loads the active profile and shows the saved preview", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(mocks.store.fetchAgentProfiles).toHaveBeenCalledTimes(1);
    expect(mocks.store.fetchAgentProfilePreview).toHaveBeenCalledWith("main-agent");
    expect(wrapper.get('[data-testid="agent-profile-page"]').text()).toContain("Primary Agent");
    expect(wrapper.get('[data-testid="preview-system-prompt"]').text()).toContain("Saved prompt from server.");
  });

  it("shows dirty state and keeps preview in sync with local edits", async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="system-prompt-input"]').setValue("Draft prompt for local edit.");
    await nextTick();

    expect(wrapper.get('[data-testid="dirty-warning"]').text()).toContain("未保存修改");
    expect(wrapper.get('[data-testid="preview-system-prompt"]').text()).toContain("Draft prompt for local edit.");
  });

  it("asks for confirmation on high-risk save and persists the updated preview", async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="system-prompt-input"]').setValue("Safer prompt after review.");
    await nextTick();

    const profile = mocks.store.activeAgentProfile;
    profile.commandPermissions.allowSudo = true;
    profile.runtime.sandboxMode = "danger-full-access";

    await wrapper.get('[data-testid="save-profile-btn"]').trigger("click");
    await flushPromises();

    expect(mocks.confirm).toHaveBeenCalled();
    expect(mocks.store.saveAgentProfile).toHaveBeenCalledTimes(1);
    expect(mocks.store.saveAgentProfile).toHaveBeenCalledWith(expect.any(Object), { riskConfirmed: true });
    expect(wrapper.find('[data-testid="dirty-warning"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="preview-system-prompt"]').text()).toContain("Safer prompt after review.");
  });

  it("asks before switching profile when there are unsaved changes", async () => {
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.get('[data-testid="profile-name-input"]').setValue("Edited name");
    await nextTick();

    mocks.confirm.mockReturnValueOnce(false);
    await wrapper.get('[data-testid="profile-item-host-agent-default"]').trigger("click");

    expect(mocks.store.selectAgentProfile).not.toHaveBeenCalledWith("host-agent-default");
    expect(mocks.store.activeAgentProfileId).toBe("main-agent");

    mocks.confirm.mockReturnValueOnce(true);
    await wrapper.get('[data-testid="profile-item-host-agent-default"]').trigger("click");
    await flushPromises();

    expect(mocks.store.selectAgentProfile).toHaveBeenCalledWith("host-agent-default");
    expect(mocks.store.activeAgentProfileId).toBe("host-agent-default");
    expect(wrapper.get("h1").text()).toContain("Host Agent Default");
  });
});
