import type { ComponentProps, ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Ctx, useRegisterPane, useWorkspace } from "./workspaceContext";

type WorkspaceValue = NonNullable<ComponentProps<typeof Ctx.Provider>["value"]>;

function workspaceValue(overrides: Partial<WorkspaceValue> = {}): WorkspaceValue {
  return {
    connected: true,
    cfg: { url: "http://gateway", token: "token" },
    setCfg: vi.fn(),
    view: "today",
    setView: vi.fn(),
    paneTarget: null,
    openPane: vi.fn(),
    consumePaneTarget: vi.fn(),
    aiText: "",
    activeResource: undefined,
    registerPane: vi.fn(),
    wikiTarget: null,
    openWiki: vi.fn(),
    consumeWikiTarget: vi.fn(),
    noteSink: null,
    setNoteSink: vi.fn(),
    hiddenViews: [],
    toggleViewHidden: vi.fn(),
    viewOrder: [],
    setViewOrder: vi.fn(),
    notebookTop: "default",
    setNotebookTop: vi.fn(),
    ...overrides,
  };
}

function wrapper(value: WorkspaceValue) {
  return function WorkspaceWrapper({ children }: { children: ReactNode }) {
    return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
  };
}

describe("useWorkspace", () => {
  it("returns the nearest workspace value", () => {
    const value = workspaceValue({ aiText: "active page", activeResource: "wiki" });
    const { result } = renderHook(() => useWorkspace(), { wrapper: wrapper(value) });
    expect(result.current).toBe(value);
    expect(result.current.aiText).toBe("active page");
    expect(result.current.activeResource).toBe("wiki");
  });

  it("fails clearly outside a provider", () => {
    expect(() => renderHook(() => useWorkspace())).toThrow("useWorkspace must be used within <WorkspaceProvider>");
  });
});

describe("useRegisterPane", () => {
  it("publishes on mount and when the projection changes", () => {
    const registerPane = vi.fn();
    const value = workspaceValue({ registerPane });
    const { rerender } = renderHook(({ resource, text }) => useRegisterPane(resource, text), {
      wrapper: wrapper(value),
      initialProps: { resource: "mail" as string | undefined, text: "first" },
    });
    expect(registerPane).toHaveBeenLastCalledWith("mail", "first");

    act(() => rerender({ resource: "mail", text: "second" }));
    expect(registerPane).toHaveBeenLastCalledWith("mail", "second");

    act(() => rerender({ resource: undefined, text: "detached" }));
    expect(registerPane).toHaveBeenLastCalledWith(undefined, "detached");
    expect(registerPane).toHaveBeenCalledTimes(3);
  });

  it("does not republish when resource and text stay equal", () => {
    const registerPane = vi.fn();
    const value = workspaceValue({ registerPane });
    const { rerender } = renderHook(() => useRegisterPane("wiki", "same"), { wrapper: wrapper(value) });
    rerender();
    expect(registerPane).toHaveBeenCalledTimes(1);
  });
});
