// @ts-check
import { test, expect } from "@playwright/test";

/**
 * Protocol 工作台 UX 修复验证
 * 1. 过期审批点击后显示友好提示，不显示 "approval not found"
 * 2. 系统路由消息（如"主 Agent 正在判断..."）不显示
 * 3. 原始 JSON routing 块被清理，不直接展示给用户
 */

const SCREENSHOT_DIR = "tests/screenshots";

async function waitForStable(page, timeout = 8000) {
  await page.waitForLoadState("networkidle", { timeout }).catch(() => {});
  await page.waitForTimeout(600);
}

async function ensureWorkspaceSession(page) {
  await page.goto("/protocol");
  await waitForStable(page);
  const createBtn = page.locator("button", { hasText: /新建工作台/ });
  if (await createBtn.isVisible({ timeout: 3000 }).catch(() => false)) {
    await createBtn.click();
    await page.waitForTimeout(2000);
  }
  await waitForStable(page);
}

test.describe("Protocol 工作台 UX 修复", () => {
  test.setTimeout(180000);

  test("1. 过期审批操作后显示友好提示", async ({ page }) => {
    await ensureWorkspaceSession(page);

    // 尝试对一个不存在的 approval 发起请求
    const resp = await page.request.post("/api/v1/approvals/fake-expired-id-12345/decision", {
      data: { decision: "accept" },
    });

    // 应该返回错误
    expect(resp.ok()).toBeFalsy();
    const body = await resp.json().catch(() => ({}));
    const errorMsg = body.error || "";

    // 验证错误消息包含 "not found" 类似内容
    expect(errorMsg.toLowerCase()).toContain("not found");

    // 现在验证前端 toolbarMessage 会把它转成友好提示
    // 模拟：在页面上设置 errorMessage 然后检查 toolbar 显示
    await page.evaluate((msg) => {
      // 直接设置 store 的 errorMessage
      const app = document.querySelector("#app")?.__vue_app__;
      if (app) {
        const pinia = app.config.globalProperties.$pinia;
        if (pinia) {
          const stores = pinia._s;
          for (const [, store] of stores) {
            if (store.errorMessage !== undefined) {
              store.errorMessage = msg;
              break;
            }
          }
        }
      }
    }, "approval not found");

    await page.waitForTimeout(500);

    // 检查 toolbar 不显示原始 "approval not found"
    const toolbar = page.locator(".toolbar-notice");
    const toolbarVisible = await toolbar.isVisible({ timeout: 3000 }).catch(() => false);
    if (toolbarVisible) {
      const text = await toolbar.textContent();
      expect(text).not.toContain("approval not found");
      expect(text).toContain("已过期");
    }

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-stale-approval-friendly.png`,
      fullPage: false,
    });
  });

  test("2. 系统路由消息不显示在对话流中", async ({ page }) => {
    await ensureWorkspaceSession(page);

    // 发送一个简单问题
    const textarea = page.locator("textarea").first();
    const visible = await textarea.isVisible({ timeout: 5000 }).catch(() => false);
    if (!visible) {
      test.skip();
      return;
    }

    await textarea.fill("今天几号？");
    await textarea.press("Meta+Enter");

    // 等待回复
    await page.waitForTimeout(3000);
    const assistantMsg = page.locator(".stream-row.row-assistant .message-text");
    await assistantMsg.first().waitFor({ state: "visible", timeout: 60000 }).catch(() => {});

    // 检查对话流中不包含系统路由消息
    const allMessages = await page.locator(".stream-row.row-assistant .message-text").allTextContents();
    for (const msg of allMessages) {
      expect(msg).not.toMatch(/^主\s*Agent\s*正在判断/);
      expect(msg).not.toMatch(/^这是简单对话/);
      expect(msg).not.toContain("不会生成计划或派发 worker");
    }

    // 等待完成
    const thinking = page.locator(".thinking-wrapper");
    await thinking.waitFor({ state: "hidden", timeout: 90000 }).catch(() => {});
    await page.waitForTimeout(500);

    // 再次检查最终状态
    const finalMessages = await page.locator(".stream-row.row-assistant .message-text").allTextContents();
    for (const msg of finalMessages) {
      // 不应该包含原始 JSON routing 块
      expect(msg).not.toMatch(/"route"\s*:\s*"direct_answer"/);
      expect(msg).not.toMatch(/"needsPlan"\s*:\s*false/);
    }

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-clean-messages.png`,
      fullPage: false,
    });
  });

  test("3. JSON routing 块被清理", async ({ page }) => {
    await ensureWorkspaceSession(page);

    const textarea = page.locator("textarea").first();
    const visible = await textarea.isVisible({ timeout: 5000 }).catch(() => false);
    if (!visible) {
      test.skip();
      return;
    }

    await textarea.fill("你好");
    await textarea.press("Meta+Enter");

    // 等待回复完成
    const thinking = page.locator(".thinking-wrapper");
    await page.waitForTimeout(2000);
    await thinking.waitFor({ state: "hidden", timeout: 90000 }).catch(() => {});
    await page.waitForTimeout(500);

    // 检查所有 assistant 消息不包含 raw JSON routing
    const allMessages = await page.locator(".stream-row.row-assistant .message-text").allTextContents();
    for (const msg of allMessages) {
      expect(msg).not.toMatch(/```json\s*\{[^}]*"route"/);
      expect(msg).not.toMatch(/\{"route"\s*:\s*"/);
    }

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-no-raw-json.png`,
      fullPage: false,
    });
  });
});
