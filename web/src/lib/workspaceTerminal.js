export function normalizeWorkspaceTerminalOutput(source) {
  if (source === null || source === undefined) {
    return "";
  }
  if (Array.isArray(source)) {
    return source.map((item) => String(item ?? "").replace(/\r\n/g, "\n")).join("\n");
  }
  if (typeof source === "object") {
    const fields = ["output", "stdout", "text", "summary", "value", "data"];
    for (const field of fields) {
      const value = source[field];
      if (typeof value === "string" && value.trim()) {
        return value;
      }
      if (Array.isArray(value) && value.length) {
        return value.map((item) => String(item ?? "")).join("\n");
      }
    }
    if (Array.isArray(source.lines) && source.lines.length) {
      return source.lines.map((item) => String(item ?? "")).join("\n");
    }
    if (Array.isArray(source.messages) && source.messages.length) {
      return source.messages
        .map((item) => {
          if (typeof item === "string") {
            return item;
          }
          if (item && typeof item === "object") {
            return String(item.text || item.message || item.summary || "").trim();
          }
          return "";
        })
        .filter(Boolean)
        .join("\n");
    }
  }
  return String(source).replace(/\r\n/g, "\n");
}

export function normalizeWorkspaceTerminalLines(source) {
  const output = normalizeWorkspaceTerminalOutput(source);
  if (!output) {
    return [];
  }
  return output.split("\n");
}

export async function createWorkspaceTerminalSession({ hostId, cwd = "~", shell = "/bin/zsh", cols = 120, rows = 36 }) {
  const response = await fetch("/api/v1/terminal/sessions", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      hostId,
      cwd,
      shell,
      cols,
      rows,
    }),
  });

  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || "无法创建终端会话");
  }
  return data;
}

export function openWorkspaceTerminalSocket(sessionId, handlers = {}) {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  const socket = new WebSocket(`${protocol}://${window.location.host}/api/v1/terminal/ws?sessionId=${encodeURIComponent(sessionId)}`);

  socket.onopen = () => {
    handlers.onOpen?.();
  };

  socket.onmessage = (event) => {
    try {
      const message = JSON.parse(event.data);
      switch (message.type) {
        case "ready":
          handlers.onReady?.(message);
          break;
        case "output":
          handlers.onOutput?.(message);
          break;
        case "exit":
          handlers.onExit?.(message);
          break;
        case "status":
          handlers.onStatus?.(message);
          break;
        case "error":
          handlers.onError?.(message);
          break;
        default:
          handlers.onMessage?.(message);
      }
    } catch (error) {
      handlers.onRawMessage?.(event.data, error);
    }
  };

  socket.onclose = () => {
    handlers.onClose?.();
  };

  socket.onerror = (event) => {
    handlers.onSocketError?.(event);
  };

  return socket;
}
