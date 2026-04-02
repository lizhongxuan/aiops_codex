import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../src/store";

describe("workspace return path", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  it("remembers the source workspace when jumping into a single-host session and returns to it", async () => {
    const store = useAppStore();
    store.sessionList = [
      {
        id: "workspace-1",
        kind: "workspace",
        title: "协作工作台",
        preview: "发布 nginx",
        status: "running",
        selectedHostId: "server-local",
      },
      {
        id: "single-1",
        kind: "single_host",
        title: "web-01",
        preview: "单机排障",
        status: "completed",
        selectedHostId: "web-01",
      },
    ];
    store.activeSessionId = "workspace-1";
    store.snapshot.sessionId = "workspace-1";
    store.snapshot.kind = "workspace";
    store.snapshot.selectedHostId = "server-local";

    store.activateSession = vi.fn(async (sessionId) => {
      store.activeSessionId = sessionId;
      store.snapshot.sessionId = sessionId;
      store.snapshot.kind = sessionId === "workspace-1" ? "workspace" : "single_host";
      store.snapshot.selectedHostId = sessionId === "workspace-1" ? "server-local" : "web-01";
      return true;
    });
    store.selectHost = vi.fn(async () => true);
    store.createSession = vi.fn(async () => true);

    const jumped = await store.createOrActivateSingleHostSessionForHost("web-01", { id: "web-01" });
    expect(jumped).toBe(true);
    expect(store.workspaceReturnSessionId).toBe("workspace-1");
    expect(store.workspaceReturnSession?.id).toBe("workspace-1");

    const returned = await store.returnToWorkspaceSession();
    expect(returned).toBe(true);
    expect(store.activateSession).toHaveBeenCalledWith("workspace-1");
    expect(store.activeSessionId).toBe("workspace-1");
    expect(store.snapshot.kind).toBe("workspace");
  });
});
