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
  it("runs apply with the whole target and consumes it when handled (view matches)", () => {
    ws.paneTarget = { view: "mail", id: "m1" };
    const apply = vi.fn(() => undefined); // void return = handled
    renderHook(() => usePaneTarget("mail", apply));
    expect(apply).toHaveBeenCalledWith({ view: "mail", id: "m1" });
    expect(ws.consumePaneTarget).toHaveBeenCalledTimes(1);
  });

  it("ignores a target for a different view", () => {
    ws.paneTarget = { view: "todo", id: "t1" };
    const apply = vi.fn();
    renderHook(() => usePaneTarget("mail", apply));
    expect(apply).not.toHaveBeenCalled();
    expect(ws.consumePaneTarget).not.toHaveBeenCalled();
  });

  it("does NOT consume when apply returns false (not ready / unhandled)", () => {
    ws.paneTarget = { view: "todo", id: "t1" };
    const apply = vi.fn(() => false); // e.g. row not loaded yet
    renderHook(() => usePaneTarget("todo", apply));
    expect(apply).toHaveBeenCalledWith({ view: "todo", id: "t1" });
    expect(ws.consumePaneTarget).not.toHaveBeenCalled();
  });

  it("passes a dayKey-only target (no id) through to apply", () => {
    ws.paneTarget = { view: "calendar", dayKey: "2026-6-7" };
    const apply = vi.fn(() => undefined);
    renderHook(() => usePaneTarget("calendar", apply));
    expect(apply).toHaveBeenCalledWith({ view: "calendar", dayKey: "2026-6-7" });
    expect(ws.consumePaneTarget).toHaveBeenCalledTimes(1);
  });
});
