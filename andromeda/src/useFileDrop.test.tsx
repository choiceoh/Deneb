import type { DragEvent as ReactDragEvent } from "react";
import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

import { useFileDrop } from "./useFileDrop";

function dragEvent(types: string[] = ["Files"], files: File[] = []) {
  return {
    preventDefault: vi.fn(),
    dataTransfer: {
      types,
      files,
      dropEffect: "move",
    },
  } as unknown as ReactDragEvent;
}

function windowDrag(type: "dragover" | "drop", transferTypes: string[]) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, "dataTransfer", { value: { types: transferTypes } });
  return event;
}

describe("useFileDrop zone state", () => {
  it("starts outside the drop zone", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    expect(result.current.over).toBe(false);
  });

  it("enters and preserves for file drags", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const enter = dragEvent();
    act(() => result.current.dropProps.onDragEnter(enter));
    expect(enter.preventDefault).toHaveBeenCalledOnce();
    expect(result.current.over).toBe(true);

    const leave = dragEvent();
    act(() => result.current.dropProps.onDragLeave(leave));
    expect(result.current.over).toBe(false);
  });

  it("preserves the overlay while a nested target is still entered", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    act(() => {
      result.current.dropProps.onDragEnter(dragEvent());
      result.current.dropProps.onDragEnter(dragEvent());
    });
    expect(result.current.over).toBe(true);
    act(() => result.current.dropProps.onDragLeave(dragEvent()));
    expect(result.current.over).toBe(true);
    act(() => result.current.dropProps.onDragLeave(dragEvent()));
    expect(result.current.over).toBe(false);
  });

  it("rejects lets leave depth become negative", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    act(() => {
      result.current.dropProps.onDragLeave(dragEvent());
      result.current.dropProps.onDragLeave(dragEvent());
      result.current.dropProps.onDragEnter(dragEvent());
    });
    expect(result.current.over).toBe(true);
    act(() => result.current.dropProps.onDragLeave(dragEvent()));
    expect(result.current.over).toBe(false);
  });

  it("ignores non-file enter and leave events", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const enter = dragEvent(["text/plain"]);
    const leave = dragEvent(["text/plain"]);
    act(() => {
      result.current.dropProps.onDragEnter(enter);
      result.current.dropProps.onDragLeave(leave);
    });
    expect(enter.preventDefault).not.toHaveBeenCalled();
    expect(leave.preventDefault).not.toHaveBeenCalled();
    expect(result.current.over).toBe(false);
  });
});

describe("useFileDrop drag-over contract", () => {
  it("when advertises copy for an enabled file zone", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = dragEvent();
    act(() => result.current.dropProps.onDragOver(event));
    expect(event.preventDefault).toHaveBeenCalledOnce();
    expect(event.dataTransfer.dropEffect).toBe("copy");
  });

  it("when advertises none for a disabled file zone", () => {
    const { result } = renderHook(() => useFileDrop(false, vi.fn()));
    const event = dragEvent();
    act(() => result.current.dropProps.onDragOver(event));
    expect(event.preventDefault).toHaveBeenCalledOnce();
    expect(event.dataTransfer.dropEffect).toBe("none");
  });

  it("without alter text drags", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = dragEvent(["text/plain"]);
    act(() => result.current.dropProps.onDragOver(event));
    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(event.dataTransfer.dropEffect).toBe("move");
  });
});

describe("useFileDrop delivery", () => {
  it("when delivers every dropped file in order", () => {
    const onFiles = vi.fn();
    const { result } = renderHook(() => useFileDrop(true, onFiles));
    const files = [new File(["a"], "a.txt", { type: "text/plain" }), new File(["b"], "b.png", { type: "image/png" })];
    act(() => result.current.dropProps.onDragEnter(dragEvent()));
    expect(result.current.over).toBe(true);
    const drop = dragEvent(["Files"], files);
    act(() => result.current.dropProps.onDrop(drop));
    expect(drop.preventDefault).toHaveBeenCalledOnce();
    expect(onFiles).toHaveBeenCalledWith(files);
    expect(result.current.over).toBe(false);
  });

  it("suppresses delivery while disabled but still cancels navigation", () => {
    const onFiles = vi.fn();
    const { result } = renderHook(() => useFileDrop(false, onFiles));
    const file = new File(["a"], "a.txt", { type: "text/plain" });
    const enter = dragEvent();
    act(() => result.current.dropProps.onDragEnter(enter));
    expect(enter.preventDefault).toHaveBeenCalledOnce();
    expect(result.current.over).toBe(false);

    const drop = dragEvent(["Files"], [file]);
    act(() => result.current.dropProps.onDrop(drop));
    expect(drop.preventDefault).toHaveBeenCalledOnce();
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("does not invoke the callback for an empty FileList", () => {
    const onFiles = vi.fn();
    const { result } = renderHook(() => useFileDrop(true, onFiles));
    act(() => result.current.dropProps.onDrop(dragEvent(["Files"], [])));
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("ignores a text drop completely", () => {
    const onFiles = vi.fn();
    const { result } = renderHook(() => useFileDrop(true, onFiles));
    const drop = dragEvent(["text/plain"]);
    act(() => result.current.dropProps.onDrop(drop));
    expect(drop.preventDefault).not.toHaveBeenCalled();
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("when resets nested depth after a successful drop", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    act(() => {
      result.current.dropProps.onDragEnter(dragEvent());
      result.current.dropProps.onDragEnter(dragEvent());
      result.current.dropProps.onDrop(dragEvent(["Files"], [new File(["x"], "x.txt")]));
    });
    expect(result.current.over).toBe(false);
    act(() => result.current.dropProps.onDragEnter(dragEvent()));
    expect(result.current.over).toBe(true);
    act(() => result.current.dropProps.onDragLeave(dragEvent()));
    expect(result.current.over).toBe(false);
  });
});

describe("useFileDrop window guard", () => {
  it.each(["dragover", "drop"] as const)("when prevents window %s navigation for files", (type) => {
    const { unmount } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = windowDrag(type, ["Files"]);
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
    unmount();
  });

  it.each(["dragover", "drop"] as const)("preserves window %s text drags untouched", (type) => {
    const { unmount } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = windowDrag(type, ["text/plain"]);
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    unmount();
  });

  it("when removes the guard after the last drop zone unmounts", () => {
    const first = renderHook(() => useFileDrop(true, vi.fn()));
    const second = renderHook(() => useFileDrop(true, vi.fn()));

    first.unmount();
    const whileSecondMounted = windowDrag("drop", ["Files"]);
    window.dispatchEvent(whileSecondMounted);
    expect(whileSecondMounted.defaultPrevented).toBe(true);

    second.unmount();
    const afterBothUnmount = windowDrag("drop", ["Files"]);
    window.dispatchEvent(afterBothUnmount);
    expect(afterBothUnmount.defaultPrevented).toBe(false);
  });
});
