import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { denebAuthProvider } from "./authProvider";
import type { GatewayConfig } from "./gateway";
import { useFileDrop } from "./useFileDrop";
import { useRpc } from "./useRpc";
import { useStickyScroll } from "./useStickyScroll";

const connected: GatewayConfig = { url: "http://gateway.test", token: "secret" };

function rpcResponse(payload: unknown): Response {
  return new Response(JSON.stringify({ ok: true, payload }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function failedResponse(message: string, status = 503): Response {
  return new Response(JSON.stringify({ ok: false, error: message }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("denebAuthProvider", () => {
  it.each([
    [{ url: "", token: "" }, false],
    [{ url: "http://gateway.test", token: "" }, false],
    [{ url: "", token: "secret" }, false],
    [connected, true],
  ] as const)("derives configured state from both credentials: %j", async (cfg, configured) => {
    const provider = denebAuthProvider(cfg);

    await expect(provider.login({})).resolves.toEqual({ success: configured });
    const check = await provider.check();
    expect(check.authenticated).toBe(configured);
    if (!configured) {
      expect(check.error).toEqual({ name: "미연결", message: "게이트웨이 URL/토큰을 입력하세요" });
    }
  });

  it("always allows the local logout operation", async () => {
    await expect(denebAuthProvider(connected).logout({})).resolves.toEqual({ success: true });
    await expect(denebAuthProvider({ url: "", token: "" }).logout({})).resolves.toEqual({ success: true });
  });

  it("returns Refine errors unchanged", async () => {
    const error = new Error("permission denied");
    await expect(denebAuthProvider(connected).onError(error)).resolves.toEqual({ error });
  });

  it("does not ping when credentials are incomplete", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);

    await expect(denebAuthProvider({ url: "http://gateway.test", token: "" }).getIdentity?.()).resolves.toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("turns gateway health into the Refine identity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => rpcResponse({ version: "2.7.1", model: "provider/smart" })),
    );

    await expect(denebAuthProvider(connected).getIdentity?.()).resolves.toEqual({
      name: "게이트웨이 v2.7.1",
      model: "provider/smart",
    });
  });

  it("uses a question mark when an older gateway omits its version", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => rpcResponse({ model: "local/tiny" })),
    );

    await expect(denebAuthProvider(connected).getIdentity?.()).resolves.toEqual({
      name: "게이트웨이 v?",
      model: "local/tiny",
    });
  });

  it.each([
    ["network failure", () => Promise.reject(new Error("offline"))],
    ["bad response", () => Promise.resolve(failedResponse("not ready"))],
  ])("fails identity lookup closed on %s", async (_label, implementation) => {
    vi.stubGlobal("fetch", vi.fn(implementation));
    await expect(denebAuthProvider(connected).getIdentity?.()).resolves.toBeNull();
  });
});

type DragLike = {
  dataTransfer?: {
    types: string[];
    files?: File[];
    dropEffect?: string;
  };
  preventDefault: ReturnType<typeof vi.fn>;
};

function drag(types: string[] = ["Files"], files: File[] = []): DragLike {
  return {
    dataTransfer: { types, files, dropEffect: "move" },
    preventDefault: vi.fn(),
  };
}

describe("useFileDrop", () => {
  it("enters and leaves the visible drop state for file drags", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = drag();

    act(() => result.current.dropProps.onDragEnter(event as never));
    expect(result.current.over).toBe(true);
    expect(event.preventDefault).toHaveBeenCalledTimes(1);

    act(() => result.current.dropProps.onDragLeave(event as never));
    expect(result.current.over).toBe(false);
  });

  it("uses a depth counter to avoid child-boundary flicker", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = drag();

    act(() => {
      result.current.dropProps.onDragEnter(event as never);
      result.current.dropProps.onDragEnter(event as never);
    });
    expect(result.current.over).toBe(true);

    act(() => result.current.dropProps.onDragLeave(event as never));
    expect(result.current.over).toBe(true);

    act(() => result.current.dropProps.onDragLeave(event as never));
    expect(result.current.over).toBe(false);
  });

  it("never lets the leave depth fall below zero", () => {
    const { result } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = drag();

    act(() => {
      result.current.dropProps.onDragLeave(event as never);
      result.current.dropProps.onDragLeave(event as never);
      result.current.dropProps.onDragEnter(event as never);
    });

    expect(result.current.over).toBe(true);
  });

  it.each([
    [true, "copy"],
    [false, "none"],
  ])("sets drag-over feedback for enabled=%s", (enabled, expected) => {
    const { result } = renderHook(() => useFileDrop(enabled, vi.fn()));
    const event = drag();

    act(() => result.current.dropProps.onDragOver(event as never));

    expect(event.preventDefault).toHaveBeenCalledTimes(1);
    expect(event.dataTransfer?.dropEffect).toBe(expected);
  });

  it("delivers every dropped file in browser order", () => {
    const onFiles = vi.fn();
    const one = new File(["one"], "one.txt", { type: "text/plain" });
    const two = new File(["two"], "two.pdf", { type: "application/pdf" });
    const { result } = renderHook(() => useFileDrop(true, onFiles));
    const event = drag(["Files"], [one, two]);

    act(() => {
      result.current.dropProps.onDragEnter(event as never);
      result.current.dropProps.onDrop(event as never);
    });

    expect(result.current.over).toBe(false);
    expect(onFiles).toHaveBeenCalledTimes(1);
    expect(onFiles).toHaveBeenCalledWith([one, two]);
  });

  it.each([
    [false, [new File(["one"], "one.txt")]],
    [true, []],
  ])("does not deliver files for enabled=%s and count=%i", (enabled, files) => {
    const onFiles = vi.fn();
    const { result } = renderHook(() => useFileDrop(enabled, onFiles));
    const event = drag(["Files"], files);

    act(() => result.current.dropProps.onDrop(event as never));

    expect(event.preventDefault).toHaveBeenCalledTimes(1);
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("ignores non-file drags without preventing their normal behavior", () => {
    const onFiles = vi.fn();
    const { result } = renderHook(() => useFileDrop(true, onFiles));
    const event = drag(["text/plain"]);

    act(() => {
      result.current.dropProps.onDragEnter(event as never);
      result.current.dropProps.onDragOver(event as never);
      result.current.dropProps.onDragLeave(event as never);
      result.current.dropProps.onDrop(event as never);
    });

    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(result.current.over).toBe(false);
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("guards the window against navigating to a dropped file", () => {
    const { unmount } = renderHook(() => useFileDrop(true, vi.fn()));
    const event = new Event("drop", { bubbles: true, cancelable: true });
    Object.defineProperty(event, "dataTransfer", { value: { types: ["Files"] } });

    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);

    unmount();
    const afterUnmount = new Event("drop", { bubbles: true, cancelable: true });
    Object.defineProperty(afterUnmount, "dataTransfer", { value: { types: ["Files"] } });
    window.dispatchEvent(afterUnmount);
    expect(afterUnmount.defaultPrevented).toBe(false);
  });

  it("keeps the singleton window guard until the final drop zone unmounts", () => {
    const first = renderHook(() => useFileDrop(true, vi.fn()));
    const second = renderHook(() => useFileDrop(true, vi.fn()));
    first.unmount();

    const whileSecondMounted = new Event("dragover", { bubbles: true, cancelable: true });
    Object.defineProperty(whileSecondMounted, "dataTransfer", { value: { types: ["Files"] } });
    window.dispatchEvent(whileSecondMounted);
    expect(whileSecondMounted.defaultPrevented).toBe(true);

    second.unmount();
    const afterBoth = new Event("dragover", { bubbles: true, cancelable: true });
    Object.defineProperty(afterBoth, "dataTransfer", { value: { types: ["Files"] } });
    window.dispatchEvent(afterBoth);
    expect(afterBoth.defaultPrevented).toBe(false);
  });
});

interface ScrollElement {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
  scrollTo: ReturnType<typeof vi.fn>;
}

function scrollElement(overrides: Partial<ScrollElement> = {}): ScrollElement {
  return {
    scrollTop: 0,
    scrollHeight: 1000,
    clientHeight: 300,
    scrollTo: vi.fn(),
    ...overrides,
  };
}

describe("useStickyScroll", () => {
  it("pins a newly attached transcript to its bottom on content change", () => {
    const { result, rerender } = renderHook(({ turn }) => useStickyScroll([turn]), {
      initialProps: { turn: 0 },
    });
    const element = scrollElement();
    Object.defineProperty(result.current.ref, "current", { value: element, writable: true });

    rerender({ turn: 1 });

    expect(element.scrollTop).toBe(1000);
    expect(result.current.atBottom).toBe(true);
  });

  it("stops auto-following after the reader scrolls up", () => {
    const { result, rerender } = renderHook(({ turn }) => useStickyScroll([turn]), {
      initialProps: { turn: 0 },
    });
    const element = scrollElement({ scrollTop: 200 });
    Object.defineProperty(result.current.ref, "current", { value: element, writable: true });

    act(() => result.current.onScroll());
    expect(result.current.atBottom).toBe(false);

    element.scrollTop = 250;
    rerender({ turn: 1 });
    expect(element.scrollTop).toBe(250);
  });

  it.each([
    [660, false],
    [661, true],
    [659, false],
    [0, false],
  ])("classifies scrollTop=%i with the forty-pixel threshold", (scrollTop, expected) => {
    const { result } = renderHook(() => useStickyScroll([]));
    const element = scrollElement({ scrollTop });
    Object.defineProperty(result.current.ref, "current", { value: element, writable: true });

    act(() => result.current.onScroll());

    expect(result.current.atBottom).toBe(expected);
  });

  it("pin resumes following on the next dependency change", () => {
    const { result, rerender } = renderHook(({ turn }) => useStickyScroll([turn]), {
      initialProps: { turn: 0 },
    });
    const element = scrollElement({ scrollTop: 100 });
    Object.defineProperty(result.current.ref, "current", { value: element, writable: true });
    act(() => result.current.onScroll());
    expect(result.current.atBottom).toBe(false);

    act(() => result.current.pin());
    expect(result.current.atBottom).toBe(true);
    rerender({ turn: 1 });
    expect(element.scrollTop).toBe(1000);
  });

  it("smooth-scrolls and resumes following when requested", () => {
    const { result } = renderHook(() => useStickyScroll([]));
    const element = scrollElement({ scrollTop: 100 });
    Object.defineProperty(result.current.ref, "current", { value: element, writable: true });
    act(() => result.current.onScroll());

    act(() => result.current.scrollToBottom());

    expect(element.scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: "smooth" });
    expect(result.current.atBottom).toBe(true);
  });

  it("tolerates scroll callbacks before the ref is attached", () => {
    const { result } = renderHook(() => useStickyScroll([]));
    expect(() => act(() => result.current.onScroll())).not.toThrow();
    expect(() => act(() => result.current.scrollToBottom())).not.toThrow();
    expect(result.current.atBottom).toBe(true);
  });
});

describe("useRpc", () => {
  it("returns successful payloads and clears busy", async () => {
    const fetch = vi.fn(async () => rpcResponse({ rows: [1, 2, 3] }));
    vi.stubGlobal("fetch", fetch);
    const { result } = renderHook(() => useRpc(connected));

    let response: Awaited<ReturnType<typeof result.current.call<{ rows: number[] }>>> | undefined;
    await act(async () => {
      response = await result.current.call<{ rows: number[] }>("miniapp.rows.list", { limit: 3 }, "불러오는 중");
    });

    expect(response).toEqual({ ok: true, data: { rows: [1, 2, 3] } });
    expect(result.current.busy).toBe(false);
    expect(result.current.status).toBe("불러오는 중");
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("sets busy synchronously while the request is pending", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>((done) => (resolve = done))),
    );
    const { result } = renderHook(() => useRpc(connected));

    let pending!: Promise<unknown>;
    act(() => {
      pending = result.current.call("miniapp.slow", undefined, "처리 중");
    });
    expect(result.current.busy).toBe(true);
    expect(result.current.status).toBe("처리 중");

    await act(async () => {
      resolve(rpcResponse({ ok: true }));
      await pending;
    });
    expect(result.current.busy).toBe(false);
  });

  it("preserves status when no pending copy is supplied", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => rpcResponse({ ok: true })),
    );
    const { result } = renderHook(() => useRpc(connected));
    act(() => result.current.setStatus("이전 결과"));

    await act(async () => {
      await result.current.call("miniapp.test");
    });

    expect(result.current.status).toBe("이전 결과");
  });

  it.each([
    ["network errors", () => Promise.reject(new Error("offline")), "오류: offline"],
    ["RPC errors", () => Promise.resolve(failedResponse("permission denied", 403)), "오류: RPC miniapp.test: HTTP 403"],
  ])("turns %s into a failed result", async (_label, implementation, expected) => {
    vi.stubGlobal("fetch", vi.fn(implementation));
    const { result } = renderHook(() => useRpc(connected));

    let response: unknown;
    await act(async () => {
      response = await result.current.call("miniapp.test", undefined, "시작");
    });

    expect(response).toEqual({ ok: false });
    expect(result.current.status).toBe(expected);
    expect(result.current.busy).toBe(false);
  });

  it("rebinds calls to the latest gateway config", async () => {
    const fetch = vi.fn(async () => rpcResponse({ ok: true }));
    vi.stubGlobal("fetch", fetch);
    const { result, rerender } = renderHook(({ cfg }) => useRpc(cfg), { initialProps: { cfg: connected } });
    const oldCall = result.current.call;
    const updated = { url: "http://second.test", token: "second" };

    rerender({ cfg: updated });
    expect(result.current.call).not.toBe(oldCall);
    await act(async () => {
      await result.current.call("miniapp.test");
    });

    expect(fetch).toHaveBeenCalledWith(
      "http://second.test/api/v1/miniapp/rpc",
      expect.objectContaining({ headers: expect.objectContaining({ "X-Deneb-Client-Token": "second" }) }),
    );
  });
});
