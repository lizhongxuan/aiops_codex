// @ts-check
import { test, expect } from "@playwright/test";
import {
  createProtocolFixtureSessions,
  createProtocolFixtureState,
  openFixturePage,
} from "./helpers/uiFixtureHarness";

const SCREENSHOT_DIR = "tests/screenshots";

async function openProtocolVisualFixture(page, fixture) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await openFixturePage(page, "/protocol", fixture);
}

async function getBox(locator) {
  const box = await locator.boundingBox();
  expect(box, `expected ${locator} to have a bounding box`).not.toBeNull();
  return box;
}

function createRemediationVisualCards() {
  return [
    {
      id: "user-remediate-1",
      type: "UserMessageCard",
      role: "user",
      text: "redis 抖动后，我想先看根因再看验证面板。",
      createdAt: "2026-04-03T13:10:00Z",
      updatedAt: "2026-04-03T13:10:00Z",
    },
    {
      id: "assistant-remediate-1",
      type: "AssistantMessageCard",
      role: "assistant",
      text: "我把 remediation bundle 和验证面板整理出来。",
      payload: {
        resultBundles: [
          {
            id: "redis-remediation-visual",
            bundleKind: "remediation_bundle",
            source: "protocol",
            mcpServer: "ops-console",
            summary: "redis 缓存命中率抖动修复面板",
            rootCause: "连接池抖动导致请求重试放大",
            confidence: "0.91",
            subject: {
              type: "service",
              name: "redis",
              env: "prod",
            },
            recentActivities: [
              { id: "act-1", label: "已收集最近 5 分钟错误率", detail: "上升 3%" },
              { id: "act-2", label: "已核对连接池配置", detail: "超出安全阈值" },
              { id: "act-3", label: "已定位慢查询", detail: "峰值出现在 12:58" },
              { id: "act-4", label: "已生成修复建议", detail: "建议先降载再验证" },
              { id: "act-5", label: "已准备验证面板", detail: "等待执行后刷新" },
              { id: "act-6", label: "最终进度", detail: "等待你确认审批" },
            ],
            sections: [
              {
                kind: "root_cause",
                title: "根因",
                cards: [
                  {
                    id: "root-cause-card-1",
                    uiKind: "readonly_summary",
                    title: "根因说明",
                    summary: "连接池抖动放大了请求重试。",
                  },
                ],
              },
              {
                kind: "recommended_actions",
                title: "推荐操作",
                cards: [
                  {
                    id: "recommend-card-1",
                    uiKind: "action_panel",
                    title: "推荐控制卡",
                    summary: "先进行受控重启。",
                    scope: {
                      service: "redis",
                      env: "prod",
                    },
                    actions: [
                      {
                        id: "restart-redis",
                        label: "重启 redis",
                        intent: "restart_service",
                        mutation: true,
                        approvalMode: "required",
                        confirmText: "确认后将进入审批并执行 redis 重启。",
                        permissionPath: "mcp.ops.service.restart",
                        target: {
                          label: "web-02 / redis",
                        },
                      },
                    ],
                  },
                ],
              },
              {
                kind: "control_panels",
                title: "控制面板",
                cards: [
                  {
                    id: "control-card-1",
                    uiKind: "action_panel",
                    title: "重启控制面板",
                    summary: "这张卡会直接触发审批路径。",
                    scope: {
                      service: "redis",
                      hostId: "web-02",
                    },
                    action: {
                      id: "restart-redis-control",
                      label: "重启 redis",
                      intent: "restart_service",
                      mutation: true,
                      approvalMode: "required",
                      confirmText: "确认后将把重启申请加入右侧审批栏。",
                      permissionPath: "mcp.ops.service.restart",
                      target: {
                        label: "web-02 / redis",
                      },
                    },
                  },
                ],
              },
              {
                kind: "validation_panels",
                title: "验证面板",
                cards: [
                  {
                    id: "validation-card-1",
                    uiKind: "readonly_chart",
                    title: "验证结果",
                    summary: "检查结果会在刷新后更新。",
                    freshness: {
                      label: "刚拉取",
                      capturedAt: "2026-04-03T13:10:05Z",
                    },
                    visual: {
                      kind: "table",
                      columns: ["指标", "当前值", "阈值"],
                      rows: [
                        ["错误率", "1.2%", "< 2%"],
                        ["P95", "180ms", "< 220ms"],
                      ],
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
      createdAt: "2026-04-03T13:10:10Z",
      updatedAt: "2026-04-03T13:10:10Z",
    },
  ];
}

test.describe("Protocol UI visual fixtures", () => {
  test("waiting approval screenshot keeps the thread compact and the right rail prominent", async ({ page }) => {
    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState(),
      sessions: createProtocolFixtureSessions(),
    });

    const workspace = page.getByTestId("protocol-workspace-page");
    const processFold = page.getByTestId("protocol-process-fold-turn-user-1");
    const turnStream = page.locator(".protocol-turn-stream");
    const approvalRail = page.getByTestId("protocol-approval-rail");
    const approvalCard = page.getByTestId("protocol-approval-approval-card-1");

    await expect(workspace).toBeVisible();
    await expect(processFold).toBeVisible();
    await expect(processFold).toContainText("审批详情已收进右侧审批面板");
    await expect(processFold).not.toContainText("执行 systemctl reload nginx");
    await expect(turnStream).not.toContainText("执行 systemctl reload nginx");
    await expect(approvalRail).toBeVisible();
    await expect(approvalCard).toBeVisible();
    await expect(approvalCard).toContainText("systemctl reload nginx");

    const streamBox = await getBox(turnStream);
    const railBox = await getBox(approvalRail);
    expect(railBox.x).toBeGreaterThan(streamBox.x + streamBox.width * 0.58);
    expect(railBox.width).toBeGreaterThan(240);
    expect(railBox.y).toBeLessThan(220);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-waiting-approval.png`,
      fullPage: false,
    });
  });

  test("refresh notice and history boundary screenshot keeps long protocol work discoverable", async ({ page }) => {
    const state = createProtocolFixtureState();
    for (let index = 2; index <= 8; index += 1) {
      state.cards.push(
        {
          id: `history-user-${index}`,
          type: "UserMessageCard",
          role: "user",
          text: `历史问题 ${index}`,
          createdAt: `2026-04-03T11:${String(index).padStart(2, "0")}:00Z`,
          updatedAt: `2026-04-03T11:${String(index).padStart(2, "0")}:00Z`,
        },
        {
          id: `history-assistant-${index}`,
          type: "AssistantMessageCard",
          role: "assistant",
          text: `历史结果 ${index}`,
          createdAt: `2026-04-03T11:${String(index).padStart(2, "0")}:30Z`,
          updatedAt: `2026-04-03T11:${String(index).padStart(2, "0")}:30Z`,
        },
      );
    }
    state.lastActivityAt = "2026-04-03T11:08:30Z";

    await openProtocolVisualFixture(page, {
      state,
      sessions: createProtocolFixtureSessions(),
    });

    const scrollContainer = page.locator(".protocol-chat-container");
    await scrollContainer.evaluate((element) => {
      element.scrollTop = Math.max(element.scrollHeight - element.clientHeight - 320, 0);
      element.dispatchEvent(new Event("scroll", { bubbles: true }));
    });

    state.cards.push(
      {
        id: "user-unread-1",
        type: "UserMessageCard",
        role: "user",
        text: "请补充一条新的执行结果。",
        createdAt: "2026-04-03T13:20:00Z",
        updatedAt: "2026-04-03T13:20:00Z",
      },
      {
        id: "assistant-unread-1",
        type: "AssistantMessageCard",
        role: "assistant",
        text: "我已补充新的执行结果，并保留未读提示。",
        createdAt: "2026-04-03T13:20:05Z",
        updatedAt: "2026-04-03T13:20:05Z",
      },
    );
    state.lastActivityAt = "2026-04-03T13:20:05Z";

    await page.locator(".toolbar-refresh").click();
    await expect(page.locator(".toolbar-notice")).toContainText("工作台状态已刷新");
    await expect(page.locator(".protocol-turn-stream")).toContainText("请补充一条新的执行结果。");

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-refresh-boundary.png`,
      fullPage: false,
    });
  });

  test("composer widgets keep plan projection and background agents docked above the omnibar", async ({ page }) => {
    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState(),
      sessions: createProtocolFixtureSessions(),
    });

    const composerWidgets = page.locator(".protocol-composer-widgets");
    const planWidget = page.locator(".protocol-inline-plan-widget");
    const backgroundAgentsCard = page.locator(".protocol-background-agents-card");
    const omnibar = page.locator('[data-testid="omnibar-input"]');

    await expect(composerWidgets).toBeVisible();
    await expect(planWidget).toBeVisible();
    await expect(planWidget).toContainText("工作台计划投影");
    await expect(planWidget).toContainText("共 2 个任务");
    await expect(backgroundAgentsCard).toBeVisible();
    await expect(backgroundAgentsCard).toContainText("后台 Agent");
    await expect(backgroundAgentsCard).toContainText("web-01");
    await expect(backgroundAgentsCard).toContainText("web-02");
    await expect(omnibar).toBeVisible();

    const widgetsBox = await getBox(composerWidgets);
    const planBox = await getBox(planWidget);
    const agentsBox = await getBox(backgroundAgentsCard);
    const omnibarBox = await getBox(omnibar);
    expect(planBox.x).toBeGreaterThanOrEqual(widgetsBox.x - 6);
    expect(planBox.width).toBeLessThanOrEqual(widgetsBox.width + 12);
    expect(agentsBox.x).toBeGreaterThanOrEqual(widgetsBox.x - 6);
    expect(omnibarBox.x).toBeGreaterThanOrEqual(widgetsBox.x - 6);
    expect(omnibarBox.y).toBeGreaterThan(agentsBox.y + agentsBox.height - 8);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-composer-widgets.png`,
      fullPage: false,
    });
  });

  test("completed turn screenshot keeps the final answer prominent while process details stay collapsed", async ({ page }) => {
    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState({
        approvals: [],
        cards: [
          {
            id: "user-1",
            type: "UserMessageCard",
            role: "user",
            text: "帮我汇总上一轮 nginx 巡检结果",
            createdAt: "2026-04-03T12:00:00Z",
            updatedAt: "2026-04-03T12:00:00Z",
          },
          {
            id: "assistant-1a",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "我先整理刚才收集到的证据。",
            createdAt: "2026-04-03T12:00:10Z",
            updatedAt: "2026-04-03T12:00:10Z",
          },
          {
            id: "assistant-1b",
            type: "AssistantMessageCard",
            role: "assistant",
            text: "结论是 service-a 的 upstream timeout 导致告警抖动。",
            createdAt: "2026-04-03T12:00:30Z",
            updatedAt: "2026-04-03T12:00:30Z",
          },
        ],
        runtime: {
          turn: { active: false, phase: "completed", hostId: "server-local" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {},
        },
      }),
      sessions: createProtocolFixtureSessions(),
    });

    const turn = page.getByTestId("protocol-turn-turn-user-1");
    const processToggle = page.locator('[data-testid="protocol-process-fold-turn-user-1"] .protocol-process-toggle');
    const finalDivider = page.locator(".protocol-final-divider-label");
    const finalRow = page.locator(".protocol-turn-final .row-assistant");

    await expect(turn).toContainText("结论是 service-a 的 upstream timeout 导致告警抖动。");
    await expect(finalDivider).toContainText("最终消息");
    await expect(finalRow).toBeVisible();
    await expect(processToggle).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator('[data-testid="protocol-process-item-assistant-1a-process-0"]')).toHaveCount(0);

    const turnBox = await getBox(turn);
    const finalRowBox = await getBox(finalRow);
    expect(finalRowBox.y).toBeGreaterThan(turnBox.y);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-completed-turn.png`,
      fullPage: false,
    });
  });

  test("starter state screenshot keeps the workspace-ready context visible", async ({ page }) => {
    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState({
        approvals: [],
        cards: [],
        runtime: {
          turn: { active: false, phase: "idle", hostId: "server-local" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {},
        },
      }),
      sessions: createProtocolFixtureSessions(),
    });

    const workspace = page.getByTestId("protocol-workspace-page");
    const omnibar = page.locator('[data-testid="omnibar-input"]');
    const approvalRail = page.getByTestId("protocol-approval-rail");

    await expect(workspace).toContainText("server-local 已连接，工作台已就绪。");
    await expect(workspace).toContainText("当前没有待审批操作。");
    await expect(omnibar).toBeVisible();
    await expect(approvalRail).toBeVisible();

    const workspaceBox = await getBox(workspace);
    const approvalBox = await getBox(approvalRail);
    expect(approvalBox.y).toBeGreaterThan(workspaceBox.y);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-starter-state.png`,
      fullPage: false,
    });
  });

  test("global MCP drawer scaffold exposes enabled MCP and recent operation sections", async ({ page }) => {
    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState({
        approvals: [],
        cards: [],
        runtime: {
          turn: { active: false, phase: "idle", hostId: "server-local" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {},
        },
      }),
      sessions: createProtocolFixtureSessions(),
    });

    await page.locator('.header-icon-btn[title="Skills & MCP"]').click();
    await expect(page.locator(".app-mcp-drawer.is-open")).toBeVisible();
    await expect(page.getByTestId("app-mcp-enabled-list")).toBeVisible();
    await expect(page.getByTestId("app-mcp-recent-list")).toBeVisible();
    await expect(page.getByTestId("app-mcp-enabled-list")).toContainText("启用中的 MCP");
    await expect(page.getByTestId("app-mcp-recent-list")).toContainText("最近操作");
  });

  test("remediation bundle screenshot keeps recent activity and validation panels in the bundle shell", async ({ page }) => {
    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState({
        approvals: [],
        cards: createRemediationVisualCards(),
        runtime: {
          turn: { active: true, phase: "waiting_input", hostId: "server-local" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {},
        },
      }),
      sessions: createProtocolFixtureSessions(),
    });

    const bundle = page.getByTestId("mcp-bundle-recent-activity-strip");
    await expect(bundle).toBeVisible();
    await expect(bundle).toContainText("最终进度");
    await expect(page.locator(".mcp-remediation-bundle-card")).toHaveCount(1);
    await expect(page.getByTestId("mcp-bundle-section-root_cause")).toBeVisible();
    await expect(page.getByTestId("mcp-bundle-expand-more")).toContainText("展开剩余 2 个分区");

    await page.getByTestId("mcp-bundle-expand-more").click();
    await expect(page.getByTestId("mcp-bundle-section-control_panels")).toBeVisible();
    await expect(page.getByTestId("mcp-bundle-section-validation_panels")).toBeVisible();

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-remediation-bundle.png`,
      fullPage: false,
    });
  });

  test("history boundary screenshot keeps the compact sentinel and history entry visible", async ({ page }) => {
    const cards = [];
    for (let index = 1; index <= 10; index += 1) {
      cards.push(
        {
          id: `user-${index}`,
          type: "UserMessageCard",
          role: "user",
          text: `历史问题 ${index}`,
          createdAt: `2026-04-03T08:0${index}:00Z`,
          updatedAt: `2026-04-03T08:0${index}:00Z`,
        },
        {
          id: `assistant-${index}`,
          type: "AssistantMessageCard",
          role: "assistant",
          text: `历史结果 ${index}`,
          createdAt: `2026-04-03T08:0${index}:30Z`,
          updatedAt: `2026-04-03T08:0${index}:30Z`,
        },
      );
    }

    await openProtocolVisualFixture(page, {
      state: createProtocolFixtureState({
        approvals: [],
        cards,
        runtime: {
          turn: { active: false, phase: "completed", hostId: "server-local" },
          codex: { status: "connected", retryAttempt: 0, retryMax: 5 },
          activity: {},
        },
      }),
      sessions: createProtocolFixtureSessions(),
    });

    const sentinel = page.getByTestId("protocol-history-sentinel");
    const historyOpen = page.getByTestId("protocol-history-open");

    await expect(sentinel).toBeVisible();
    await expect(sentinel).toContainText("更早上下文已折叠");
    await expect(sentinel).toContainText("加载更早消息");
    await expect(historyOpen).toBeVisible();
    await expect(historyOpen).toContainText("查看完整历史");
    await expect(page.getByTestId("protocol-turn-turn-user-10")).toBeVisible();
    await expect(page.getByTestId("protocol-turn-turn-user-1")).toHaveCount(0);

    const sentinelBox = await getBox(sentinel);
    expect(sentinelBox.height).toBeLessThan(140);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-visual-history-boundary.png`,
      fullPage: false,
    });
  });
});
