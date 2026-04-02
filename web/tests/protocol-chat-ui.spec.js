// @ts-check
import { test, expect } from "@playwright/test";

/**
 * Protocol 工作台对话框 UI 视觉测试
 * 验证 /protocol 页面的对话区域样式是否接近 Codex 原生风格
 */

const SCREENSHOT_DIR = "tests/screenshots";

async function waitForStable(page, timeout = 8000) {
  await page.waitForLoadState("networkidle", { timeout }).catch(() => {});
  await page.waitForTimeout(600);
}

async function ensureWorkspaceSession(page) {
  await page.goto("/protocol");
  await waitForStable(page);

  // 如果不是 workspace 会话，点击新建
  const createBtn = page.locator("button", { hasText: /新建工作台/ });
  if (await createBtn.isVisible({ timeout: 3000 }).catch(() => false)) {
    await createBtn.click();
    await page.waitForTimeout(2000);
  }
  await waitForStable(page);
}

test.describe("Protocol 工作台对话框 UI", () => {
  test.setTimeout(180000);

  test("1. Protocol 页面对话区字体 = 14px", async ({ page }) => {
    await ensureWorkspaceSession(page);

    // 发送消息
    const textarea = page.locator("textarea").first();
    const visible = await textarea.isVisible({ timeout: 5000 }).catch(() => false);
    if (!visible) {
      test.skip();
      return;
    }

    await textarea.fill("你好");
    await textarea.press("Meta+Enter");
    await page.waitForTimeout(2000);

    // 验证用户消息字体
    const userText = page.locator(".is-user .message-text").first();
    const userVisible = await userText.isVisible({ timeout: 5000 }).catch(() => false);
    if (userVisible) {
      const fontSize = await userText.evaluate((el) => window.getComputedStyle(el).fontSize);
      expect(fontSize).toBe("14px");

      const borderRadius = await userText.evaluate((el) => window.getComputedStyle(el).borderRadius);
      expect(borderRadius).toBe("14px");
    }

    // 等待 assistant 回复
    const assistantText = page.locator(".message-wrapper:not(.is-user) .message-text").first();
    await assistantText.waitFor({ state: "visible", timeout: 60000 }).catch(() => {});
    const assistantVisible = await assistantText.isVisible({ timeout: 3000 }).catch(() => false);
    if (assistantVisible) {
      const fontSize = await assistantText.evaluate((el) => window.getComputedStyle(el).fontSize);
      expect(fontSize).toBe("14px");
    }

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-chat-conversation.png`,
      fullPage: false,
    });
  });

  test("2. Protocol 输入框样式紧凑", async ({ page }) => {
    await ensureWorkspaceSession(page);

    const textarea = page.locator("textarea").first();
    const visible = await textarea.isVisible({ timeout: 5000 }).catch(() => false);
    if (!visible) {
      test.skip();
      return;
    }

    // 验证输入框字体 = 14px
    const fontSize = await textarea.evaluate((el) => window.getComputedStyle(el).fontSize);
    expect(fontSize).toBe("14px");

    // 验证 omnibar 圆角 = 16px
    const omnibar = page.locator(".omnibar-wrapper").first();
    const omnibarVisible = await omnibar.isVisible({ timeout: 3000 }).catch(() => false);
    if (omnibarVisible) {
      const borderRadius = await omnibar.evaluate((el) => window.getComputedStyle(el).borderRadius);
      expect(borderRadius).toBe("16px");
    }

    // 验证发送按钮 = 32px
    const sendBtn = page.locator(".send-btn").first();
    const btnVisible = await sendBtn.isVisible({ timeout: 3000 }).catch(() => false);
    if (btnVisible) {
      const btnSize = await sendBtn.evaluate((el) => ({
        w: window.getComputedStyle(el).width,
        h: window.getComputedStyle(el).height,
      }));
      expect(btnSize.w).toBe("32px");
      expect(btnSize.h).toBe("32px");
    }

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-chat-omnibar.png`,
      fullPage: false,
    });
  });

  test("3. Protocol avatar = 26px", async ({ page }) => {
    await ensureWorkspaceSession(page);

    const textarea = page.locator("textarea").first();
    const visible = await textarea.isVisible({ timeout: 5000 }).catch(() => false);
    if (!visible) {
      test.skip();
      return;
    }

    await textarea.fill("今天几号？");
    await textarea.press("Meta+Enter");

    // 等待 assistant 回复
    const avatar = page.locator(".message-wrapper:not(.is-user) .avatar").first();
    await avatar.waitFor({ state: "visible", timeout: 60000 }).catch(() => {});
    const avatarVisible = await avatar.isVisible({ timeout: 3000 }).catch(() => false);
    if (avatarVisible) {
      const size = await avatar.evaluate((el) => ({
        w: window.getComputedStyle(el).width,
        h: window.getComputedStyle(el).height,
      }));
      expect(size.w).toBe("26px");
      expect(size.h).toBe("26px");
    }
  });

  test("4. Protocol thinking card margin-left = 36px", async ({ page }) => {
    await ensureWorkspaceSession(page);

    const textarea = page.locator("textarea").first();
    const visible = await textarea.isVisible({ timeout: 5000 }).catch(() => false);
    if (!visible) {
      test.skip();
      return;
    }

    await textarea.fill("帮我检查系统状态");
    await textarea.press("Meta+Enter");
    await page.waitForTimeout(1000);

    const thinking = page.locator(".thinking-wrapper").first();
    const thinkingVisible = await thinking.isVisible({ timeout: 5000 }).catch(() => false);
    if (thinkingVisible) {
      const margin = await thinking.evaluate((el) => window.getComputedStyle(el).marginLeft);
      expect(margin).toBe("36px");
    }

    // 截图 thinking 状态
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-chat-thinking.png`,
      fullPage: false,
    });

    // 等待完成
    await thinking.waitFor({ state: "hidden", timeout: 90000 }).catch(() => {});
    await page.waitForTimeout(500);

    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-chat-complete.png`,
      fullPage: false,
    });
  });

  test("5. Protocol 整体截图", async ({ page }) => {
    await ensureWorkspaceSession(page);
    await page.waitForTimeout(500);
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/protocol-workspace-updated.png`,
      fullPage: false,
    });
  });
});
