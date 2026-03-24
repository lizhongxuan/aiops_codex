import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const baseURL = process.env.AIOPS_BASE_URL || "http://127.0.0.1:18080";
const workspaceDir = path.join(os.homedir(), ".aiops_codex");

class HTTPSession {
  constructor(name) {
    this.name = name;
    this.cookie = "";
  }

  async request(pathname, options = {}) {
    const headers = new Headers(options.headers || {});
    if (this.cookie) {
      headers.set("cookie", this.cookie);
    }
    let body;
    if (options.json !== undefined) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(options.json);
    }
    const response = await fetch(new URL(pathname, baseURL), {
      method: options.method || "GET",
      headers,
      body,
      redirect: "manual",
    });
    this.captureCookie(response);
    const text = await response.text();
    let data = text;
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = text;
      }
    }
    return {
      status: response.status,
      data,
    };
  }

  captureCookie(response) {
    const rawCookies =
      typeof response.headers.getSetCookie === "function"
        ? response.headers.getSetCookie()
        : [response.headers.get("set-cookie")].filter(Boolean);
    if (!rawCookies.length) {
      return;
    }
    const parts = rawCookies.map((cookie) => cookie.split(";", 1)[0]);
    this.cookie = parts.join("; ");
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForState(session, label, predicate, timeoutMs = 90000) {
  const startedAt = Date.now();
  let lastState;
  while (Date.now() - startedAt < timeoutMs) {
    const response = await session.request("/api/v1/state");
    assert(response.status === 200, `${label}: state request failed with ${response.status}`);
    lastState = response.data;
    if (await predicate(lastState)) {
      return lastState;
    }
    await sleep(1000);
  }
  throw new Error(`${label}: timed out after ${timeoutMs}ms`);
}

async function newAuthedSession(name) {
  const session = new HTTPSession(name);
  const response = await session.request("/api/v1/state");
  assert(response.status === 200, `${name}: initial state failed with ${response.status}`);
  assert(response.data.sessionId, `${name}: missing sessionId`);
  assert(response.data.auth.connected, `${name}: GPT auth is not connected`);
  return {
    session,
    state: response.data,
  };
}

async function ensureFileAbsent(filePath) {
  try {
    await fs.unlink(filePath);
  } catch (error) {
    if (error && error.code !== "ENOENT") {
      throw error;
    }
  }
}

async function testLoginSuccessAndRefresh() {
  const { session, state } = await newAuthedSession("login-success");
  const second = await session.request("/api/v1/state");
  assert(second.status === 200, "refresh: second state failed");
  assert(second.data.sessionId === state.sessionId, "refresh: sessionId changed after reload");
  assert(second.data.auth.connected, "refresh: auth.connected became false after reload");
  console.log(`PASS login-success session=${state.sessionId}`);
}

async function testLoginFailure() {
  const session = new HTTPSession("login-failure");
  const response = await session.request("/api/v1/auth/login", {
    method: "POST",
    json: {
      mode: "chatgptAuthTokens",
      accessToken: "",
      chatgptAccountId: "",
    },
  });
  assert(response.status === 400, `login-failure: expected 400, got ${response.status}`);
  assert(
    typeof response.data?.error === "string" && response.data.error.includes("required"),
    `login-failure: unexpected error payload ${JSON.stringify(response.data)}`,
  );
  console.log("PASS login-failure");
}

async function testServerLocalDialogue() {
  const { session } = await newAuthedSession("server-local-dialogue");
  const response = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: "请执行 pwd，并告诉我当前工作区路径。",
    },
  });
  assert(response.status === 202, `server-local-dialogue: expected 202, got ${response.status}`);
  const state = await waitForState(
    session,
    "server-local-dialogue",
    (snapshot) =>
      snapshot.cards.some(
        (card) =>
          card.type === "StepCard" &&
          /pwd/.test(card.command || "") &&
          (card.output || "").includes(".aiops_codex"),
      ) ||
      snapshot.cards.some(
        (card) =>
          card.type === "MessageCard" &&
          card.role === "assistant" &&
          (card.text || "").includes(".aiops_codex"),
      ),
  );
  assert(state.cards.length > 0, "server-local-dialogue: no cards returned");
  console.log("PASS server-local-dialogue");
}

async function testFileApprovalFlow() {
  const filename = `approval-script-${Date.now()}.txt`;
  const filePath = path.join(workspaceDir, filename);
  await ensureFileAbsent(filePath);

  const { session } = await newAuthedSession("file-approval");
  const response = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: `请在当前工作区创建文件 ${filename}，内容为 hello-from-smoke。`,
    },
  });
  assert(response.status === 202, `file-approval: expected 202, got ${response.status}`);

  const pending = await waitForState(
    session,
    "file-approval pending",
    (snapshot) => snapshot.approvals.some((approval) => approval.type === "file_change" && approval.status === "pending"),
  );
  const approval = pending.approvals.find((item) => item.type === "file_change" && item.status === "pending");
  assert(approval, "file-approval: missing pending approval");

  const decision = await session.request(`/api/v1/approvals/${approval.id}/decision`, {
    method: "POST",
    json: { decision: "accept" },
  });
  assert(decision.status === 200, `file-approval: approval decision failed with ${decision.status}`);

  const completed = await waitForState(
    session,
    "file-approval completed",
    async (snapshot) => {
      const current = snapshot.approvals.find((item) => item.id === approval.id);
      if (!current || current.status !== "accept") {
        return false;
      }
      try {
        const content = await fs.readFile(filePath, "utf8");
        return content.includes("hello-from-smoke");
      } catch {
        return false;
      }
    },
  );
  assert(
    completed.approvals.some((item) => item.id === approval.id && item.status === "accept"),
    "file-approval: approval did not transition to accept",
  );
  console.log(`PASS file-approval file=${filename}`);
}

async function testCommandApprovalFlow() {
  const filename = `command-approval-${Date.now()}.txt`;
  const filePath = path.join(workspaceDir, filename);
  await fs.writeFile(filePath, "keep-me\n", "utf8");

  const { session } = await newAuthedSession("command-approval");
  const response = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: `请直接执行 shell 命令 rm ${filePath} 删除这个文件，不要改成 patch，也不要换成别的方案。`,
    },
  });
  assert(response.status === 202, `command-approval: expected 202, got ${response.status}`);

  const pending = await waitForState(
    session,
    "command-approval pending",
    (snapshot) => snapshot.approvals.some((approval) => approval.type === "command" && approval.status === "pending"),
  );
  const approval = pending.approvals.find((item) => item.type === "command" && item.status === "pending");
  assert(approval, "command-approval: missing pending approval");

  const declineDecision = approval.decisions.includes("cancel") ? "cancel" : "decline";
  const decision = await session.request(`/api/v1/approvals/${approval.id}/decision`, {
    method: "POST",
    json: { decision: declineDecision },
  });
  assert(decision.status === 200, `command-approval: decision failed with ${decision.status}`);

  const completed = await waitForState(
    session,
    "command-approval completed",
    (snapshot) => {
      const current = snapshot.approvals.find((item) => item.id === approval.id);
      return !!current && current.status !== "pending";
    },
  );
  assert(
    completed.approvals.some((item) => item.id === approval.id && item.status !== "pending"),
    "command-approval: approval stayed pending",
  );
  await fs.access(filePath);
  console.log(`PASS command-approval decision=${declineDecision}`);
}

async function testWorkspaceBoundary() {
  const outsidePath = path.join(os.tmpdir(), `aiops-codex-boundary-${Date.now()}.txt`);
  await ensureFileAbsent(outsidePath);

  const { session, state: initialState } = await newAuthedSession("workspace-boundary");
  const initialCards = Array.isArray(initialState.cards) ? initialState.cards : [];
  const initialCardCount = initialCards.length;

  const response = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: `请在 ${outsidePath} 创建一个文本文件，内容写 hello-boundary。`,
    },
  });
  assert(response.status === 202, `workspace-boundary: expected 202, got ${response.status}`);

  const completed = await waitForState(
    session,
    "workspace-boundary completed",
    (snapshot) => {
      const cards = Array.isArray(snapshot.cards) ? snapshot.cards : [];
      return (
        cards.length > initialCardCount &&
        cards.some(
          (card) => card.type === "ResultCard" || (card.type === "MessageCard" && card.role === "assistant"),
        )
      );
    },
  );

  let outsideExists = true;
  try {
    await fs.access(outsidePath);
  } catch {
    outsideExists = false;
  }
  assert(!outsideExists, `workspace-boundary: file unexpectedly created at ${outsidePath}`);
  const completedCards = Array.isArray(completed.cards) ? completed.cards : [];
  assert(completedCards.length > initialCardCount, "workspace-boundary: no new result cards appeared");
  console.log(`PASS workspace-boundary path=${outsidePath}`);
}

async function testApproveForSession() {
  const filename = `session-grant-${Date.now()}.txt`;
  const filePath = path.join(workspaceDir, filename);
  const prompt = `请直接执行 shell 命令 rm ${filePath} 删除这个文件，不要改成 patch，也不要换成别的方案。`;

  const { session } = await newAuthedSession("approve-for-session");
  await fs.writeFile(filePath, "delete-me-once\n", "utf8");

  let response = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: prompt,
    },
  });
  assert(response.status === 202, `approve-for-session: first request expected 202, got ${response.status}`);

  let pending = await waitForState(
    session,
    "approve-for-session pending",
    (snapshot) =>
      snapshot.approvals.some(
        (approval) => approval.type === "command" && approval.status === "pending" && (approval.command || "").includes(filePath),
      ),
  );
  let approval = pending.approvals.find(
    (item) => item.type === "command" && item.status === "pending" && (item.command || "").includes(filePath),
  );
  assert(approval, "approve-for-session: missing first pending approval");

  let decision = await session.request(`/api/v1/approvals/${approval.id}/decision`, {
    method: "POST",
    json: { decision: "accept_session" },
  });
  assert(decision.status === 200, `approve-for-session: grant decision failed with ${decision.status}`);

  await waitForState(
    session,
    "approve-for-session first run completed",
    async (snapshot) => {
      const current = snapshot.approvals.find((item) => item.id === approval.id);
      if (!current || current.status !== "accepted_for_session") {
        return false;
      }
      try {
        await fs.access(filePath);
        return false;
      } catch {
        return true;
      }
    },
  );

  await fs.writeFile(filePath, "delete-me-twice\n", "utf8");
  response = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: prompt,
    },
  });
  assert(response.status === 202, `approve-for-session: second request expected 202, got ${response.status}`);

  const autoApproved = await waitForState(
    session,
    "approve-for-session auto approval",
    async (snapshot) => {
      const matchedApprovals = snapshot.approvals.filter(
        (item) => item.type === "command" && (item.command || "").includes(filePath),
      );
      const hasPending = matchedApprovals.some((item) => item.status === "pending");
      const hasAutoAccept = matchedApprovals.some((item) => item.status === "accepted_for_session_auto");
      if (hasPending || !hasAutoAccept) {
        return false;
      }
      try {
        await fs.access(filePath);
        return false;
      } catch {
        return true;
      }
    },
  );
  assert(
    autoApproved.approvals.some(
      (item) => item.type === "command" && (item.command || "").includes(filePath) && item.status === "accepted_for_session_auto",
    ),
    "approve-for-session: second execution was not auto-approved",
  );
  console.log(`PASS approve-for-session file=${filename}`);
}

async function main() {
  console.log(`Running smoke tests against ${baseURL}`);
  await testLoginSuccessAndRefresh();
  await testLoginFailure();
  await testServerLocalDialogue();
  await testFileApprovalFlow();
  await testCommandApprovalFlow();
  await testApproveForSession();
  await testWorkspaceBoundary();
  console.log("ALL PASS smoke_mvp_0324");
}

main().catch((error) => {
  console.error("SMOKE FAILED", error);
  process.exitCode = 1;
});
