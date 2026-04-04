// @ts-check
import { test, expect } from "@playwright/test";
import {
  createChatFixtureSessions,
  createChatFixtureState,
  openFixturePage,
} from "./helpers/uiFixtureHarness";

const SCREENSHOT_DIR = "tests/screenshots";

async function openVisualFixture(page, fixture) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await openFixturePage(page, "/", fixture);
}

async function pasteText(locator, text) {
  await locator.evaluate((element, payload) => {
    const pasteEvent = new Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(pasteEvent, "clipboardData", {
      configurable: true,
      value: {
        getData: () => payload,
      },
    });
    element.dispatchEvent(pasteEvent);
    element.value = payload;
    element.dispatchEvent(new Event("input", { bubbles: true }));
  }, text);
}

const PATH_LIST = [
  "/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue",
  "/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/usePasteAssist.js",
  "/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue",
  "/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue",
].join("\n");

test.describe("Chat UI visual fixtures", () => {
  test("active turn screenshot keeps process and dock structure stable", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState(),
      sessions: createChatFixtureSessions(),
    });

    const turn = page.getByTestId("chat-turn-turn-user-main-1");
    const processFold = page.getByTestId("chat-process-fold-turn-user-main-1");

    await expect(turn).toBeVisible();
    await expect(turn.locator(".message-wrapper.is-user")).toBeVisible();
    await expect(processFold).toBeVisible();
    await expect(processFold).toContainText("正在思考");
    await expect(page.locator(".chat-process-body")).toHaveCount(1);
    await expect(page.locator(".chat-composer-dock")).toBeVisible();
    await expect(page.locator(".omnibar-wrapper")).toBeVisible();
    await expect(page.locator(".chat-composer-plan")).toContainText("共 2 个任务");

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-active-turn.png`,
      fullPage: false,
    });
  });

  test("running process fold screenshot keeps the thinking state compact", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState(),
      sessions: createChatFixtureSessions(),
    });

    const processFold = page.getByTestId("chat-process-fold-turn-user-main-1");

    await expect(processFold).toBeVisible();
    await expect(processFold).toContainText("正在思考");
    await expect(page.locator(".chat-process-body")).toHaveCount(1);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-running-process-fold.png`,
      fullPage: false,
    });
  });

  test("completed turn screenshot keeps the final answer prominent", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        cards: [
          {
            id: "user-main-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我看下 nginx 中间件的状态，并给我一个处理建议。",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-main-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先对比日志、upstream 指标和最近一次 reload 记录。",
            createdAt: "2026-04-03T10:00:05Z",
            updatedAt: "2026-04-03T10:00:05Z",
          },
          {
            id: "assistant-main-2",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "已经确认 nginx 本体正常，异常集中在 service-a upstream timeout。",
            createdAt: "2026-04-03T10:00:20Z",
            updatedAt: "2026-04-03T10:00:20Z",
          },
        ],
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    const turn = page.getByTestId("chat-turn-turn-user-main-1");
    const processFold = page.getByTestId("chat-process-fold-turn-user-main-1");
    const finalTurn = page.locator(".chat-turn-final");

    await expect(turn).toBeVisible();
    await expect(turn.locator(".message-wrapper.is-user")).toBeVisible();
    await expect(processFold).toBeVisible();
    await expect(processFold).toContainText("已处理");
    await expect(page.locator(".chat-process-body")).toHaveCount(0);
    await expect(finalTurn).toBeVisible();
    await expect(finalTurn.locator(".message-wrapper")).toBeVisible();
    await expect(finalTurn).toContainText(
      "已经确认 nginx 本体正常，异常集中在 service-a upstream timeout。",
    );

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-completed-turn.png`,
      fullPage: false,
    });
  });

  test("approval overlay screenshot keeps the thread lightweight", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        cards: [
          {
            id: "user-main-1",
            type: "UserMessageCard",
            role: "user",
            text: "请帮我 reload nginx 并确认结果。",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "approval-card-1",
            type: "CommandApprovalCard",
            status: "pending",
            hostId: "web-01",
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
        approvals: [{ id: "approval-1", status: "pending", itemId: "approval-card-1" }],
        runtime: {
          turn: { active: true, phase: "waiting_approval", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    await expect(page.getByTestId("chat-turn-turn-user-main-1")).toBeVisible();
    await expect(page.locator(".chat-stream")).not.toContainText("systemctl reload nginx");
    await expect(page.locator(".auth-overlay-dock")).toBeVisible();
    await expect(page.locator(".auth-overlay-dock .option-row").first()).toBeVisible();

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-approval-overlay.png`,
      fullPage: false,
    });
  });

  test("MCP drawer screenshot keeps the bundle panel readable without leaving chat", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        cards: [
          {
            id: "user-main-1",
            type: "UserMessageCard",
            role: "user",
            text: "我想看一下 nginx 的完整监控面板。",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
          {
            id: "assistant-main-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我把完整 bundle 挂出来。",
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
                          id: "overview-card-1",
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
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    await page.getByTestId("mcp-bundle-open-detail").click();
    const drawer = page.getByTestId("chat-mcp-surface-drawer");

    await expect(drawer).toBeVisible();
    await expect(drawer).toContainText("nginx 监控聚合面板");
    await expect(drawer).toContainText("概览");
    await expect(drawer).toContainText("平稳");

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-mcp-drawer.png`,
      fullPage: false,
    });
  });

  test("bundle drawer screenshot keeps long tables and forms inside the drawer", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        cards: [
          {
            id: "user-main-1",
            type: "UserMessageCard",
            role: "user",
            text: "给我看一个包含表格和表单的完整 bundle。",
            createdAt: "2026-04-03T10:20:00Z",
            updatedAt: "2026-04-03T10:20:00Z",
          },
          {
            id: "assistant-main-1",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我把长表格和表单都收进 drawer。",
            createdAt: "2026-04-03T10:20:05Z",
            updatedAt: "2026-04-03T10:20:05Z",
            payload: {
              resultBundles: [
                {
                  id: "bundle-heavy-1",
                  bundleKind: "monitor_bundle",
                  summary: "nginx 监控聚合面板",
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
                          id: "overview-card-1",
                          uiKind: "readonly_summary",
                          title: "当前状态",
                          summary: "平稳",
                        },
                      ],
                    },
                    {
                      kind: "trends",
                      title: "趋势",
                      cards: [
                        {
                          id: "trend-card-1",
                          uiKind: "readonly_chart",
                          title: "请求趋势",
                          visual: { kind: "timeseries" },
                        },
                      ],
                    },
                    {
                      kind: "alerts",
                      title: "告警",
                      cards: [
                        {
                          id: "alerts-card-1",
                          uiKind: "readonly_chart",
                          title: "当前告警表",
                          visual: {
                            kind: "table",
                            columns: ["时间", "级别", "对象"],
                            rows: [
                              ["10:20", "warning", "nginx"],
                              ["10:21", "info", "upstream"],
                            ],
                          },
                        },
                      ],
                    },
                    {
                      kind: "changes",
                      title: "变更",
                      cards: [
                        {
                          id: "change-card-1",
                          uiKind: "form_panel",
                          title: "结构化表单",
                          summary: "表单会进入 drawer，而不是撑开正文。",
                          actions: [
                            {
                              id: "confirm-nginx",
                              label: "确认 nginx",
                              intent: "refresh",
                              mutation: false,
                              permissionPath: "mcp.ops.service.confirm",
                            },
                          ],
                          form: {
                            confirmDescription: "这里放一个表单摘要。",
                            fields: [
                              {
                                id: "field-service",
                                name: "service",
                                label: "服务名",
                                type: "text",
                                defaultValue: "nginx",
                              },
                            ],
                          },
                        },
                      ],
                    },
                    {
                      kind: "dependencies",
                      title: "依赖",
                      cards: [
                        {
                          id: "dependency-card-1",
                          uiKind: "readonly_summary",
                          title: "下游依赖",
                          summary: "这里应该保持在 drawer 中。",
                        },
                      ],
                    },
                  ],
                },
              ],
            },
          },
        ],
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    await expect(page.getByTestId("mcp-bundle-expand-more")).toContainText("展开剩余 3 个分区");
    await page.getByTestId("mcp-bundle-open-detail").click();
    const drawer = page.getByTestId("chat-mcp-surface-drawer");

    await expect(drawer).toBeVisible();
    await page.locator('[data-testid="chat-mcp-surface-drawer"] [data-testid="mcp-bundle-expand-more"]').click();
    await expect(page.locator('[data-testid="chat-mcp-surface-drawer"] [data-testid="mcp-status-table-head"]')).toBeVisible();
    await expect(page.locator('[data-testid="chat-mcp-surface-drawer"] [data-testid="mcp-action-form-card"]')).toBeVisible();
    await expect(page.locator(".chat-stream")).not.toContainText("当前告警表");

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-bundle-drawer-heavy.png`,
      fullPage: false,
    });
  });

  test("global MCP drawer scaffold exposes enabled MCP and recent operation sections", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        cards: [
          {
            id: "user-main-1",
            type: "UserMessageCard",
            role: "user",
            text: "请帮我固定 nginx 面板到全局 drawer。",
            createdAt: "2026-04-03T10:00:00Z",
            updatedAt: "2026-04-03T10:00:00Z",
          },
        ],
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    await page.locator('.header-icon-btn[title="Skills & MCP"]').click();
    await expect(page.locator(".app-mcp-drawer.is-open")).toBeVisible();
    await expect(page.getByTestId("app-mcp-enabled-list")).toBeVisible();
    await expect(page.getByTestId("app-mcp-recent-list")).toBeVisible();
    await expect(page.getByTestId("app-mcp-enabled-list")).toContainText("启用中的 MCP");
    await expect(page.getByTestId("app-mcp-recent-list")).toContainText("最近操作");
  });

  test("history boundary screenshot keeps the compact sentinel visible", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        cards: [
          ...Array.from({ length: 11 }, (_value, index) => ({
            id: `user-old-${index}`,
            type: "UserMessageCard",
            role: "user",
            text: index === 0 ? "最早的一条聊天记录" : `历史消息 ${index}`,
            createdAt: `2026-04-03T09:${String(index).padStart(2, "0")}:00Z`,
            updatedAt: `2026-04-03T09:${String(index).padStart(2, "0")}:00Z`,
          })),
        ],
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {
            viewedFiles: [],
            searchedWebQueries: [],
            searchedContentQueries: [],
          },
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    await page.locator(".chat-container").evaluate((el) => {
      el.scrollTop = 0;
    });

    const sentinel = page.getByTestId("chat-history-sentinel");
    await expect(sentinel).toBeVisible();
    await expect(sentinel).toContainText(/更早上下文已折叠|已到会话开头/);
    await expect(sentinel.locator(".chat-history-sentinel-actions")).toBeVisible();
    await expect(sentinel.locator(".chat-history-sentinel-btn")).toHaveCount(1);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-history-boundary.png`,
      fullPage: false,
    });
  });

  test("omnibar screenshot keeps the path artifact hint and focus recovery visible", async ({ page }) => {
    await openVisualFixture(page, {
      state: createChatFixtureState({
        runtime: {
          turn: { active: false, phase: "completed", hostId: "web-01" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {},
        },
      }),
      sessions: createChatFixtureSessions(),
    });

    const input = page.getByTestId("omnibar-input");

    await pasteText(input, PATH_LIST);
    await expect(page.getByTestId("omnibar-attachment-indicator")).toContainText("4 个路径");
    await expect(page.getByTestId("omnibar-artifact-pill")).toContainText("路径 4");

    await input.blur();
    await input.focus();

    await expect(page.getByTestId("omnibar-focus-hint")).toContainText("已恢复输入焦点");
    await expect(page.getByTestId("omnibar-focus-hint")).toContainText("路径仍待处理");

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-visual-omnibar-path-hint.png`,
      fullPage: false,
    });
  });
});
