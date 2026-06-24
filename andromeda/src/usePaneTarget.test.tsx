import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";

// Drive the hook by stubbing the workspace it reads from. vi.hoisted lets the
// vi.mock factory (hoisted above imports) reference this shared handle.
const ws = vi.hoisted(() => ({
  paneTarget: null as null | { view: string; id?: string | number; dayKey?: string },
  consumePaneTarget: vi.fn(),
}));
vi.mock("./workspaceContext", () => ({ useWorkspace: () => ws }));

import { usePaneTarget } from "./usePaneTarget";

afterEach(() => {
  ws.paneTarget = null;
  vi.clearAllMocks();
});

describe("usePaneTarget", () => {
  it("runs apply with (id, target) and consumes when the view matches", () => {
    ws.paneTarget = { view: "mail", id: "m1" };
    const apply = vi.fn();
    renderHook(() => usePaneTarget("mail", apply));
    expect(apply).toHaveBeenCalledWith("m1", { view: "mail", id: "m1" });
    expect(ws.consumePaneTarget).toHaveBeenCalledTimes(1);
  });

  it("ignores a target for a different view", () => {
    ws.paneTarget = { view: "todo", id: "t1" };
    const apply = vi.fn();
    renderHook(() => usePaneTarget("mail", apply));
    expect(apply).not.toHaveBeenCalled();
    expect(ws.consumePaneTarget).not.toHaveBeenCalled();
  });

  it("applies but does NOT consume while the pane's query is still loading", () => {
    ws.paneTarget = { view: "todo", id: "t1" };
    const apply = vi.fn();
    renderHook(() => usePaneTarget("todo", apply, true));
    expect(apply).toHaveBeenCalledWith("t1", { view: "todo", id: "t1" });
    expect(ws.consumePaneTarget).not.toHaveBeenCalled();
  });

  it("fires for a dayKey-only target with no id (e.g. calendar)", () => {
    ws.paneTarget = { view: "calendar", dayKey: "2026-6-7" };
    const apply = vi.fn();
    renderHook(() => usePaneTarget("calendar", apply));
    expect(apply).toHaveBeenCalledWith(undefined, { view: "calendar", dayKey: "2026-6-7" });
    expect(ws.consumePaneTarget).toHaveBeenCalledTimes(1);
  });
});
