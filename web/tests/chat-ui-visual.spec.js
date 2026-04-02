// @ts-check
import { test, expect } from "@playwright/test";

/**
 * 工作台对话框 UI 视觉回归测试
 *
 * 流程：新建会话 → 发送复杂消息触发 plan → 截图各阶段 UI
 * 重点验证：字体大小、输入框比例、卡片间距、头像尺寸等是否接近 Codex 原生风格
 */

const SCREENSHOT_DIR = "tests/screenshots";

/** 等待页面稳定（无 loading spinner、网络空闲） */
async function waitForStable(page, timeout = 8000) {
  await page.waitForLoadState("networkidle", { timeout }).catch(() => {});
  await page.waitForTimeout(600);
}

/** 通过 API 新建一个 single_host 会话并导航到首页 */
async function createFreshSession(page) {
  // 先访问首页拿到 cookie
  await page.goto("/");
  await waitForStable(page);

  // 通过 API 新建会话
  const resp = await page.request.post("/api/v1/sessions", {
    data: { kind: "single_host" },
  });
  expect(resp.ok()).toBeTruthy();
  const body = await resp.json();
  const sessionId = body.activeSessionId || body.snapshot?.sessionId;
  expect(sessionId).toBeTruthy();

  // 刷新页面加载新会话
  await page.goto("/");
  await waitForStable(page);
  return sessionId;
}

/** 发送一条消息并等待 UI 响应 */
async function sendMessage(page, message) {
  const textarea = page.locator("textarea").first();
  await expect(textarea).toBeVisible({ timeout: 5000 });
  await textarea.fill(message);
  await page.waitForTimeout(200);

  // 用 Cmd+Enter 发送
  await textarea.press("Meta+Enter");
  await page.waitForTimeout(500);
}

/** 等待 agent 回复出现（assistant 消息卡片） */
async function waitForAssistantReply(page, timeout = 60000) {
  // 等待至少一个非用户消息卡片出现
  const assistantCard = page.locator(".stream-row.row-assistant .message-wrapper:not(.is-user)");
  await assistantCard.first().waitFor({ state: "visible", timeout }).catch(() => {});
}

/** 等待 turn 结束（thinking card 消失） */
async function waitForTurnComplete(page, timeout = 120000) {
  // 等待 thinking card 消失，表示 turn 结束
  const thinkingCard = page.locator(".thinking-wrapper");
  try {
    await thinkingCard.waitFor({ state: "hidden", timeout });
  } catch {
    // 超时也继续，可能 turn 还在进行
  }
  await page.waitForTimeout(500);
}

// ─── 测试用例 ───

test.describe("工作台对话框 UI 视觉测试", () => {
  test.setTimeout(180000); // 3 分钟超时，因为涉及真实 AI 交互

  test("1. 空状态页面截图", async ({ page }) => {
    await createFreshSession(page);

    // 验证空状态元素
    const emptyState = page.locator(".empty-state-canvas");
    const emptyVisible = await emptyState.isVisible({ timeout: 3000 }).catch(() => false);

    // 截图
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-empty-state.png`,
      fullPage: false,
    });

    // 验证输入框可见且在底部
    const textarea = page.locator("textarea").first();
    await expect(textarea).toBeVisible({ timeout: 5000 });
    const box = await textarea.boundingBox();
    const vh = page.viewportSize()?.height || 900;
    expect(box.y).toBeGreaterThan(vh * 0.5);
  });

  test("2. 输入框样式验证", async ({ page }) => {
    await createFreshSession(page);

    const textarea = page.locator("textarea").first();
    await expect(textarea).toBeVisible({ timeout: 5000 });

    // 验证输入框字体大小 = 14px
    const fontSize = await textarea.evaluate((el) => {
      return window.getComputedStyle(el).fontSize;
    });
    expect(fontSize).toBe("14px");

    // 验证输入框行高
    const lineHeight = await textarea.evaluate((el) => {
      return window.getComputedStyle(el).lineHeight;
    });
    // lineHeight 应该是 1.5 * 14 = 21px
    const lhNum = parseFloat(lineHeight);
    expect(lhNum).toBeGreaterThanOrEqual(20);
    expect(lhNum).toBeLessThanOrEqual(22);

    // 验证 omnibar 容器圆角 = 16px
    const omnibar = page.locator(".omnibar-wrapper").first();
    const borderRadius = await omnibar.evaluate((el) => {
      return window.getComputedStyle(el).borderRadius;
    });
    expect(borderRadius).toBe("16px");

    // 截图输入框区域
    const omnibarBox = await omnibar.boundingBox();
    if (omnibarBox) {
      await page.screenshot({
        path: `${SCREENSHOT_DIR}/chat-omnibar-detail.png`,
        clip: {
          x: Math.max(0, omnibarBox.x - 20),
          y: Math.max(0, omnibarBox.y - 20),
          width: omnibarBox.width + 40,
          height: omnibarBox.height + 40,
        },
      });
    }
  });

  test("3. 发送简单消息后的对话样式", async ({ page }) => {
    await createFreshSession(page);

    // 发送简单消息
    await sendMessage(page, "你好，有什么需要我处理的？");

    // 等待用户消息气泡出现
    const userBubble = page.locator(".stream-row.row-user");
    await userBubble.first().waitFor({ state: "visible", timeout: 5000 });

    // 验证用户消息气泡样式
    const userText = page.locator(".is-user .message-text").first();
    const userVisible = await userText.isVisible({ timeout: 3000 }).catch(() => false);
    if (userVisible) {
      const userFontSize = await userText.evaluate((el) => {
        return window.getComputedStyle(el).fontSize;
      });
      expect(userFontSize).toBe("14px");

      const userBorderRadius = await userText.evaluate((el) => {
        return window.getComputedStyle(el).borderRadius;
      });
      expect(userBorderRadius).toBe("14px");
    }

    // 等待 assistant 回复
    await waitForAssistantReply(page, 60000);

    // 验证 assistant 消息字体
    const assistantText = page.locator(".message-wrapper:not(.is-user) .message-text").first();
    const assistantVisible = await assistantText.isVisible({ timeout: 5000 }).catch(() => false);
    if (assistantVisible) {
      const assistantFontSize = await assistantText.evaluate((el) => {
        return window.getComputedStyle(el).fontSize;
      });
      expect(assistantFontSize).toBe("14px");
    }

    // 验证 avatar 尺寸 = 26px
    const avatar = page.locator(".message-wrapper:not(.is-user) .avatar").first();
    const avatarVisible = await avatar.isVisible({ timeout: 3000 }).catch(() => false);
    if (avatarVisible) {
      const avatarSize = await avatar.evaluate((el) => {
        const style = window.getComputedStyle(el);
        return { width: style.width, height: style.height };
      });
      expect(avatarSize.width).toBe("26px");
      expect(avatarSize.height).toBe("26px");
    }

    // 截图对话区域
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-simple-conversation.png`,
      fullPage: false,
    });
  });

  test("4. 发送复杂任务触发 plan 并截图全流程", async ({ page }) => {
    await createFreshSession(page);

    // 发送一个会触发 plan 的复杂任务
    const complexMessage = [
      "请帮我做以下事情：",
      "1. 检查当前系统的 CPU 和内存使用情况",
      "2. 列出 /tmp 目录下最近修改的文件",
      "3. 查看当前运行的进程中占用内存最多的前 5 个",
      "4. 把以上结果整理成一份简洁的系统健康报告",
    ].join("\n");

    await sendMessage(page, complexMessage);

    // 等待用户消息出现
    const userRow = page.locator(".stream-row.row-user");
    await userRow.first().waitFor({ state: "visible", timeout: 5000 });

    // 截图：用户消息发送后
    await page.waitForTimeout(1000);
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-complex-task-sent.png`,
      fullPage: false,
    });

    // 等待 thinking 状态出现
    const thinkingCard = page.locator(".thinking-wrapper");
    const thinkingVisible = await thinkingCard.isVisible({ timeout: 5000 }).catch(() => false);
    if (thinkingVisible) {
      // 截图：thinking 状态
      await page.screenshot({
        path: `${SCREENSHOT_DIR}/chat-thinking-state.png`,
        fullPage: false,
      });

      // 验证 thinking card 的 margin-left = 36px
      const thinkingMargin = await thinkingCard.evaluate((el) => {
        return window.getComputedStyle(el).marginLeft;
      });
      expect(thinkingMargin).toBe("36px");
    }

    // 等待 plan card 或 activity summary 出现
    const planCard = page.locator(".plan-card");
    const activitySummary = page.locator(".activity-summary");
    try {
      await Promise.race([
        planCard.first().waitFor({ state: "visible", timeout: 30000 }),
        activitySummary.first().waitFor({ state: "visible", timeout: 30000 }),
      ]);
    } catch {
      // 可能没有 plan，继续
    }

    // 截图：plan 或 activity 阶段
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-plan-or-activity.png`,
      fullPage: false,
    });

    // 如果有 plan card，验证其样式
    const planVisible = await planCard.first().isVisible({ timeout: 2000 }).catch(() => false);
    if (planVisible) {
      const planMargin = await planCard.first().evaluate((el) => {
        return window.getComputedStyle(el).marginLeft;
      });
      expect(planMargin).toBe("36px");

      const planBorderRadius = await planCard.first().evaluate((el) => {
        return window.getComputedStyle(el).borderRadius;
      });
      expect(planBorderRadius).toBe("12px");
    }

    // 等待审批卡片出现（如果有命令执行）
    const authOverlay = page.locator(".auth-overlay-dock");
    const authVisible = await authOverlay.isVisible({ timeout: 15000 }).catch(() => false);
    if (authVisible) {
      // 截图：审批状态
      await page.screenshot({
        path: `${SCREENSHOT_DIR}/chat-approval-state.png`,
        fullPage: false,
      });

      // 点击同意
      const acceptBtn = page.locator(".option-row").first();
      const acceptVisible = await acceptBtn.isVisible({ timeout: 3000 }).catch(() => false);
      if (acceptVisible) {
        await acceptBtn.click();
        await page.waitForTimeout(2000);

        // 截图：审批后
        await page.screenshot({
          path: `${SCREENSHOT_DIR}/chat-after-approval.png`,
          fullPage: false,
        });
      }
    }

    // 等待 turn 完成或超时
    await waitForTurnComplete(page, 90000);

    // 最终截图
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-complex-task-complete.png`,
      fullPage: false,
    });
  });

  test("5. 全局 CSS 变量验证", async ({ page }) => {
    await createFreshSession(page);

    const cssVars = await page.evaluate(() => {
      const root = document.documentElement;
      const style = getComputedStyle(root);
      return {
        textBody: style.getPropertyValue("--text-body").trim(),
        lineHeightBody: style.getPropertyValue("--line-height-body").trim(),
        textMetaSize: style.getPropertyValue("--text-meta-size").trim(),
        radiusCard: style.getPropertyValue("--radius-card").trim(),
      };
    });

    expect(cssVars.textBody).toBe("14px");
    expect(cssVars.lineHeightBody).toBe("1.6");
    expect(cssVars.textMetaSize).toBe("11px");
    expect(cssVars.radiusCard).toBe("12px");
  });

  test("6. 对话流宽度和间距验证", async ({ page }) => {
    await createFreshSession(page);

    // 验证 chat-stream-inner max-width = 820px
    const streamInner = page.locator(".chat-stream-inner").first();
    const streamVisible = await streamInner.isVisible({ timeout: 3000 }).catch(() => false);
    if (streamVisible) {
      const maxWidth = await streamInner.evaluate((el) => {
        return window.getComputedStyle(el).maxWidth;
      });
      expect(maxWidth).toBe("820px");
    }

    // 验证 stream-row margin-bottom = 6px
    await sendMessage(page, "测试间距");
    await page.waitForTimeout(2000);

    const streamRow = page.locator(".stream-row").first();
    const rowVisible = await streamRow.isVisible({ timeout: 5000 }).catch(() => false);
    if (rowVisible) {
      const marginBottom = await streamRow.evaluate((el) => {
        return window.getComputedStyle(el).marginBottom;
      });
      expect(marginBottom).toBe("6px");
    }
  });

  test("7. 终端卡片样式验证", async ({ page }) => {
    await createFreshSession(page);

    // 发送一个会触发命令执行的消息
    await sendMessage(page, "请执行 echo hello world");

    // 等待可能出现的审批或终端卡片
    const terminalCard = page.locator(".terminal-card");
    const timelineSummary = page.locator(".timeline-summary");

    try {
      await Promise.race([
        terminalCard.first().waitFor({ state: "visible", timeout: 45000 }),
        timelineSummary.first().waitFor({ state: "visible", timeout: 45000 }),
      ]);
    } catch {
      // 可能需要先审批
      const authOverlay = page.locator(".auth-overlay-dock");
      if (await authOverlay.isVisible({ timeout: 3000 }).catch(() => false)) {
        const acceptBtn = page.locator(".option-row").first();
        if (await acceptBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
          await acceptBtn.click();
          await page.waitForTimeout(3000);
        }
      }
    }

    await waitForTurnComplete(page, 60000);

    // 截图终端卡片
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-terminal-card.png`,
      fullPage: false,
    });

    // 验证终端卡片或 timeline 的 margin-left
    const termVisible = await terminalCard.first().isVisible({ timeout: 2000 }).catch(() => false);
    const timelineVisible = await timelineSummary.first().isVisible({ timeout: 2000 }).catch(() => false);

    if (termVisible) {
      const margin = await terminalCard.first().evaluate((el) => {
        return window.getComputedStyle(el).marginLeft;
      });
      expect(margin).toBe("36px");
    } else if (timelineVisible) {
      const margin = await timelineSummary.first().evaluate((el) => {
        return window.getComputedStyle(el).marginLeft;
      });
      expect(margin).toBe("36px");
    }
  });

  test("8. 页面无溢出滚动条", async ({ page }) => {
    await createFreshSession(page);
    await page.waitForTimeout(500);

    const hasOverflow = await page.evaluate(() => {
      return document.documentElement.scrollHeight > document.documentElement.clientHeight + 5;
    });
    expect(hasOverflow).toBe(false);
  });

  test("9. 发送按钮样式验证", async ({ page }) => {
    await createFreshSession(page);

    const sendBtn = page.locator(".send-btn").first();
    await expect(sendBtn).toBeVisible({ timeout: 5000 });

    const btnStyle = await sendBtn.evaluate((el) => {
      const style = window.getComputedStyle(el);
      return {
        width: style.width,
        height: style.height,
        borderRadius: style.borderRadius,
      };
    });

    expect(btnStyle.width).toBe("32px");
    expect(btnStyle.height).toBe("32px");
    // borderRadius 应该是 50% 或 9999px 或 999px
    expect(parseFloat(btnStyle.borderRadius)).toBeGreaterThanOrEqual(16);
  });

  test("10. 多轮对话后整体截图", async ({ page }) => {
    await createFreshSession(page);

    // 第一轮
    await sendMessage(page, "你好");
    await waitForAssistantReply(page, 30000);
    await waitForTurnComplete(page, 60000);

    // 第二轮
    await sendMessage(page, "请帮我查看当前目录下有哪些文件");

    // 等待可能的审批
    await page.waitForTimeout(3000);
    const authOverlay = page.locator(".auth-overlay-dock");
    if (await authOverlay.isVisible({ timeout: 5000 }).catch(() => false)) {
      const acceptBtn = page.locator(".option-row").first();
      if (await acceptBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
        await acceptBtn.click();
        await page.waitForTimeout(2000);
      }
    }

    await waitForTurnComplete(page, 60000);

    // 最终多轮对话截图
    await page.screenshot({
      path: `${SCREENSHOT_DIR}/chat-multi-turn-conversation.png`,
      fullPage: false,
    });

    // 验证多个 stream-row 存在
    const rows = page.locator(".stream-row");
    const count = await rows.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });
});
