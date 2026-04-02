const baseURL = process.env.AIOPS_BASE_URL || "http://127.0.0.1:8080";

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
    return { status: response.status, data };
  }

  captureCookie(response) {
    const rawCookies =
      typeof response.headers.getSetCookie === "function"
        ? response.headers.getSetCookie()
        : [response.headers.get("set-cookie")].filter(Boolean);
    if (!rawCookies.length) {
      return;
    }
    this.cookie = rawCookies.map((cookie) => cookie.split(";", 1)[0]).join("; ");
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

async function waitForState(session, label, predicate, timeoutMs = 60000) {
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
  throw new Error(`${label}: timed out after ${timeoutMs}ms; last snapshot=${JSON.stringify(lastState)}`);
}

function collectTexts(cards = []) {
  return cards
    .map((card) => String(card?.text || card?.message || card?.summary || card?.title || "").trim())
    .filter(Boolean)
    .join("\n");
}

async function main() {
  const session = new HTTPSession("workspace-main-agent");

  const initial = await session.request("/api/v1/state");
  assert(initial.status === 200, `initial state failed with ${initial.status}`);
  assert(initial.data?.auth?.connected, "GPT auth is not connected");

  const created = await session.request("/api/v1/sessions", {
    method: "POST",
    json: { kind: "workspace" },
  });
  assert(created.status === 200, `workspace create failed with ${created.status}: ${JSON.stringify(created.data)}`);
  assert(created.data?.snapshot?.kind === "workspace", `expected workspace snapshot, got ${JSON.stringify(created.data)}`);
  console.log(`PASS workspace-create session=${created.data.snapshot.sessionId}`);

  const stateAnswer = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: "有哪些主机在线",
    },
  });
  assert(stateAnswer.status === 202, `state-question: expected 202, got ${stateAnswer.status}`);

  const stateAfterAnswer = await waitForState(session, "state-question answer", (snapshot) => {
    const text = collectTexts(snapshot.cards);
    return text.includes("我先直接读取 ai-server 当前状态投影") && text.includes("在线主机");
  });
  console.log(`PASS workspace-state-answer cards=${stateAfterAnswer.cards.length}`);

  const simpleGreeting = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: "你好",
    },
  });
  assert(simpleGreeting.status === 202, `simple-greeting: expected 202, got ${simpleGreeting.status}: ${JSON.stringify(simpleGreeting.data)}`);

  const simpleReplyState = await waitForState(session, "simple-greeting reply", (snapshot) => {
    const assistantReplies = (snapshot.cards || []).filter((card) => card.type === "AssistantMessageCard");
    return assistantReplies.length >= 2 && snapshot.runtime?.turn?.active !== true;
  });
  const simpleMissionCards = (simpleReplyState.cards || []).filter((card) => card.type === "PlanCard");
  assert(simpleMissionCards.length === 0, `simple-greeting: expected no plan card, got ${JSON.stringify(simpleMissionCards)}`);
  console.log(`PASS workspace-simple-direct phase=${simpleReplyState.runtime?.turn?.phase || "unknown"}`);

  const complexTask = await session.request("/api/v1/chat/message", {
    method: "POST",
    json: {
      hostId: "server-local",
      message: "帮我执行一轮全网 nginx 巡检，重点关注错误日志。",
    },
  });
  assert(complexTask.status === 202, `complex-task: expected 202, got ${complexTask.status}: ${JSON.stringify(complexTask.data)}`);

  const planningState = await waitForState(session, "complex-task planning", (snapshot) => {
    const text = collectTexts(snapshot.cards);
    return (
      snapshot.runtime?.turn?.phase === "planning" ||
      snapshot.runtime?.turn?.phase === "waiting_approval" ||
      snapshot.cards.some((card) => card.type === "PlanCard") ||
      text.includes("主 Agent 正在生成计划") ||
      text.includes("已收到计划投影")
    );
  });

  const cardText = collectTexts(planningState.cards);
  const hasPlanProjection = planningState.cards.some((card) => card.type === "PlanCard") || cardText.includes("主 Agent 正在生成计划");
  assert(hasPlanProjection, `complex-task: expected plan/projection signal, got ${cardText}`);
  console.log(
    `PASS workspace-complex-task phase=${planningState.runtime?.turn?.phase || "unknown"} approvals=${planningState.approvals?.length || 0}`,
  );
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
