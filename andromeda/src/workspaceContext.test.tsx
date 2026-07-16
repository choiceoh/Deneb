import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  feedValue,
  feedWriters,
  workspaceValue,
  type FeedValue,
  type FeedWriters,
  type WorkspaceValue,
} from "@/test/workspace";
import { Ctx, FeedCtx, FeedWriteCtx, TileCtx, useAiFeed, useRegisterPane, useWorkspace } from "./workspaceContext";
import type { View } from "./types";

function wrapper(
  value: WorkspaceValue,
  feed: FeedValue,
  tile: View | null = null,
  writers: FeedWriters = feedWriters(),
) {
  return function WorkspaceWrapper({ children }: { children: ReactNode }) {
    return (
      <Ctx.Provider value={value}>
        <FeedWriteCtx.Provider value={writers}>
          <FeedCtx.Provider value={feed}>
            <TileCtx.Provider value={tile}>{children}</TileCtx.Provider>
          </FeedCtx.Provider>
        </FeedWriteCtx.Provider>
      </Ctx.Provider>
    );
  };
}

describe("useWorkspace", () => {
  it("returns the nearest workspace value", () => {
    const value = workspaceValue({ view: "wiki", tiles: ["wiki", "mail"] });
    const { result } = renderHook(() => useWorkspace(), { wrapper: wrapper(value, feedValue()) });
    expect(result.current).toBe(value);
    expect(result.current.view).toBe("wiki");
    expect(result.current.tiles).toEqual(["wiki", "mail"]);
  });

  it("fails clearly outside a provider", () => {
    expect(() => renderHook(() => useWorkspace())).toThrow("useWorkspace must be used within <WorkspaceProvider>");
  });
});

describe("useAiFeed", () => {
  it("exposes the AI projection and active resource", () => {
    const feed = feedValue({ aiText: "active page", activeResource: "wiki" });
    const { result } = renderHook(() => useAiFeed(), { wrapper: wrapper(workspaceValue(), feed) });
    expect(result.current.aiText).toBe("active page");
    expect(result.current.activeResource).toBe("wiki");
  });

  it("fails clearly outside a provider", () => {
    expect(() => renderHook(() => useAiFeed())).toThrow("useAiFeed must be used within <WorkspaceProvider>");
  });
});

describe("useRegisterPane", () => {
  it("publishes under its tile slot on mount and when the projection changes", () => {
    const writers = feedWriters();
    const { rerender } = renderHook(({ resource, text }) => useRegisterPane(resource, text), {
      wrapper: wrapper(workspaceValue(), feedValue(), "mail", writers),
      initialProps: { resource: "mail" as string | undefined, text: "first" },
    });
    expect(writers.registerPane).toHaveBeenLastCalledWith("mail", "mail", "first");

    act(() => rerender({ resource: "mail", text: "second" }));
    expect(writers.registerPane).toHaveBeenLastCalledWith("mail", "mail", "second");

    act(() => rerender({ resource: undefined, text: "detached" }));
    expect(writers.registerPane).toHaveBeenLastCalledWith("mail", undefined, "detached");
    expect(writers.registerPane).toHaveBeenCalledTimes(3);
  });

  it("falls back to the current view when no tile slot is provided", () => {
    const writers = feedWriters();
    renderHook(() => useRegisterPane("wiki", "body"), {
      wrapper: wrapper(workspaceValue({ view: "wiki" }), feedValue(), null, writers),
    });
    expect(writers.registerPane).toHaveBeenLastCalledWith("wiki", "wiki", "body");
  });

  it("does not republish when resource and text stay equal", () => {
    const writers = feedWriters();
    const { rerender } = renderHook(() => useRegisterPane("wiki", "same"), {
      wrapper: wrapper(workspaceValue(), feedValue(), "wiki", writers),
    });
    rerender();
    expect(writers.registerPane).toHaveBeenCalledTimes(1);
  });

  it("unregisters its slot on unmount", () => {
    const writers = feedWriters();
    const { unmount } = renderHook(() => useRegisterPane("mail", "text"), {
      wrapper: wrapper(workspaceValue(), feedValue(), "mail", writers),
    });
    expect(writers.unregisterPane).not.toHaveBeenCalled();
    unmount();
    expect(writers.unregisterPane).toHaveBeenCalledWith("mail");
  });
});
