import { createRef, type RefObject } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { GatewayConfig } from "./gateway";
import { useAttachPipeline, useComposerBehavior, useModels } from "./useChatSurface";

const cfg: GatewayConfig = { url: "http://gateway.test", token: "secret" };

function rpcResponse(payload: unknown): Response {
  return new Response(JSON.stringify({ ok: true, payload }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  document.body.innerHTML = "";
});

describe("useModels", () => {
  it("without request the registry while disconnected", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);

    const { result } = renderHook(() => useModels(cfg, false));

    expect(result.current.models).toBeNull();
    expect(result.current.model).toBe("");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("loads the registry and selects its current model", async () => {
    const payload = {
      current: "provider/smart",
      roles: [],
      sections: [{ title: "General", models: [{ id: "provider/smart", label: "Smart" }] }],
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => rpcResponse(payload)),
    );

    const { result } = renderHook(() => useModels(cfg, true));

    await waitFor(() => expect(result.current.models).toEqual(payload));
    expect(result.current.model).toBe("provider/smart");
  });

  it("preserves an explicit selection across a registry refresh", async () => {
    const first = {
      current: "provider/fast",
      roles: [],
      sections: [{ title: "General", models: [{ id: "provider/fast", label: "Fast" }] }],
    };
    const second = {
      current: "provider/smart",
      roles: [],
      sections: [{ title: "General", models: [{ id: "provider/smart", label: "Smart" }] }],
    };
    const fetch = vi.fn().mockResolvedValueOnce(rpcResponse(first)).mockResolvedValueOnce(rpcResponse(second));
    vi.stubGlobal("fetch", fetch);
    const { result, rerender } = renderHook(({ token }) => useModels({ ...cfg, token }, true), {
      initialProps: { token: "one" },
    });
    await waitFor(() => expect(result.current.model).toBe("provider/fast"));
    act(() => result.current.setModel("custom/chosen"));

    rerender({ token: "two" });

    await waitFor(() => expect(result.current.models).toEqual(second));
    expect(result.current.model).toBe("custom/chosen");
  });

  it("clears registry data on disconnect without erasing selection", async () => {
    const payload = { current: "provider/smart", roles: [], sections: [] };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => rpcResponse(payload)),
    );
    const { result, rerender } = renderHook(({ connected }) => useModels(cfg, connected), {
      initialProps: { connected: true },
    });
    await waitFor(() => expect(result.current.models).toEqual(payload));

    rerender({ connected: false });

    expect(result.current.models).toBeNull();
    expect(result.current.model).toBe("provider/smart");
  });

  it("fails soft for older or unavailable gateways", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("missing", { status: 404 })),
    );
    const { result } = renderHook(() => useModels(cfg, true));

    await waitFor(() => expect(result.current.models).toBeNull());
    expect(result.current.model).toBe("");
  });

  it("ignores a late result after unmount", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>((done) => (resolve = done))),
    );
    const { unmount } = renderHook(() => useModels(cfg, true));

    unmount();
    await act(async () => {
      resolve(rpcResponse({ current: "late", roles: [], sections: [] }));
      await Promise.resolve();
    });
  });

  it("reloads when URL or token changes", async () => {
    const fetch = vi.fn(async () => rpcResponse({ current: "model", roles: [], sections: [] }));
    vi.stubGlobal("fetch", fetch);
    const { rerender } = renderHook(({ gateway }) => useModels(gateway, true), {
      initialProps: { gateway: cfg },
    });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));

    rerender({ gateway: { ...cfg, url: "http://second.test" } });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    rerender({ gateway: { url: "http://second.test", token: "second" } });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
  });
});

function textarea(): HTMLTextAreaElement {
  const element = document.createElement("textarea");
  Object.defineProperty(element, "scrollHeight", { value: 84, configurable: true });
  document.body.appendChild(element);
  return element;
}

describe("useComposerBehavior", () => {
  it("when autosizes the visible composer to its scroll height", () => {
    const element = textarea();
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;

    renderHook(() => useComposerBehavior(ref, { input: "two\nlines", busy: false, hidden: false }));

    expect(element.style.height).toBe("84px");
  });

  it("remeasures whenever input changes", () => {
    const element = textarea();
    let height = 40;
    Object.defineProperty(element, "scrollHeight", { get: () => height, configurable: true });
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    const { rerender } = renderHook(({ input }) => useComposerBehavior(ref, { input, busy: false, hidden: false }), {
      initialProps: { input: "one" },
    });
    expect(element.style.height).toBe("40px");

    height = 120;
    rerender({ input: "one\ntwo\nthree" });

    expect(element.style.height).toBe("120px");
  });

  it("without measure a hidden composer", () => {
    const element = textarea();
    element.style.height = "55px";
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;

    renderHook(() => useComposerBehavior(ref, { input: "text", busy: false, hidden: true }));

    expect(element.style.height).toBe("55px");
  });

  it("remeasures when a hidden chat is revealed", () => {
    const element = textarea();
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    const { rerender } = renderHook(({ hidden }) => useComposerBehavior(ref, { input: "text", busy: false, hidden }), {
      initialProps: { hidden: true },
    });
    expect(element.style.height).toBe("");

    rerender({ hidden: false });

    expect(element.style.height).toBe("84px");
  });

  it("focuses the full chat when it is revealed", () => {
    const element = textarea();
    const focus = vi.spyOn(element, "focus");
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    const { rerender } = renderHook(
      ({ hidden }) => useComposerBehavior(ref, { input: "", busy: false, hidden, focusOnReveal: true }),
      { initialProps: { hidden: true } },
    );

    rerender({ hidden: false });

    expect(focus).toHaveBeenCalledTimes(1);
  });

  it("without steal focus for the side panel", () => {
    const element = textarea();
    const focus = vi.spyOn(element, "focus");
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    renderHook(() => useComposerBehavior(ref, { input: "", busy: false, hidden: false, focusOnReveal: false }));
    expect(focus).not.toHaveBeenCalled();
  });

  it("restores focus when a busy turn ends and focus fell to body", () => {
    const element = textarea();
    const focus = vi.spyOn(element, "focus");
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    const { rerender } = renderHook(({ busy }) => useComposerBehavior(ref, { input: "", busy, hidden: false }), {
      initialProps: { busy: true },
    });
    document.body.focus();

    rerender({ busy: false });

    expect(focus).toHaveBeenCalledTimes(1);
  });

  it("does not steal a deliberate focus move when busy ends", () => {
    const element = textarea();
    const other = document.createElement("button");
    document.body.appendChild(other);
    const focus = vi.spyOn(element, "focus");
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    const { rerender } = renderHook(({ busy }) => useComposerBehavior(ref, { input: "", busy, hidden: false }), {
      initialProps: { busy: true },
    });
    other.focus();

    rerender({ busy: false });

    expect(focus).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(other);
  });

  it("does not focus a hidden composer when busy ends", () => {
    const element = textarea();
    const focus = vi.spyOn(element, "focus");
    const ref = { current: element } as RefObject<HTMLTextAreaElement>;
    const { rerender } = renderHook(({ busy }) => useComposerBehavior(ref, { input: "", busy, hidden: true }), {
      initialProps: { busy: true },
    });

    rerender({ busy: false });

    expect(focus).not.toHaveBeenCalled();
  });

  it("when tolerates a ref that has not mounted yet", () => {
    const ref = createRef<HTMLTextAreaElement>();
    expect(() =>
      renderHook(() => useComposerBehavior(ref, { input: "text", busy: false, hidden: false, focusOnReveal: true })),
    ).not.toThrow();
  });
});

interface AttachHarnessProps {
  connected?: boolean;
  busy?: boolean;
  input?: string;
  setInput?: (value: string) => void;
  setAttaching?: (value: boolean) => void;
  pin?: () => void;
  capture?: (
    file: { name: string; mimeType: string; base64: string },
    caption: string,
    previewUrl?: string,
  ) => Promise<void>;
  onBatchDone?: () => void;
}

function attachHarness(overrides: AttachHarnessProps = {}) {
  const props = {
    connected: true,
    busy: false,
    input: "  파일 설명  ",
    setInput: vi.fn(),
    setAttaching: vi.fn(),
    pin: vi.fn(),
    capture: vi.fn(async () => {}),
    onBatchDone: vi.fn(),
    ...overrides,
  };
  const hook = renderHook((current: typeof props) => useAttachPipeline(current), { initialProps: props });
  return { props, ...hook };
}

describe("useAttachPipeline", () => {
  it.each([
    [{ connected: false }, [new File(["x"], "image.png", { type: "image/png" })]],
    [{ busy: true }, [new File(["x"], "image.png", { type: "image/png" })]],
    [{}, []],
  ] as const)("ignores intake outside the ready state", async (overrides, files) => {
    const { result, props } = attachHarness(overrides);

    await act(async () => result.current.attachFiles([...files]));

    expect(props.capture).not.toHaveBeenCalled();
    expect(props.setAttaching).not.toHaveBeenCalled();
    expect(props.onBatchDone).not.toHaveBeenCalled();
  });

  it("shows unsupported-file feedback without entering attaching state", async () => {
    const { result, props } = attachHarness();
    const executable = new File(["binary"], "program.exe", { type: "application/x-msdownload" });

    await act(async () => result.current.attachFiles([executable]));

    expect(result.current.attachNote).toBe("program.exe — 지원하지 않는 형식이라 건너뜀");
    expect(props.setAttaching).not.toHaveBeenCalled();
    expect(props.capture).not.toHaveBeenCalled();
  });

  it("clears skip feedback after six seconds", async () => {
    vi.useFakeTimers();
    const { result } = attachHarness();
    const executable = new File(["binary"], "program.exe", { type: "application/x-msdownload" });
    await act(async () => result.current.attachFiles([executable]));
    expect(result.current.attachNote).not.toBe("");

    act(() => vi.advanceTimersByTime(6000));

    expect(result.current.attachNote).toBe("");
  });

  it("stages supported files as chips without sending", async () => {
    const { result, props } = attachHarness();
    const image = new File(["pixels"], "photo.png", { type: "image/png" });

    await act(async () => result.current.attachFiles([image]));

    expect(result.current.staged).toHaveLength(1);
    expect(result.current.staged[0]).toMatchObject({ name: "photo.png", mimeType: "image/png" });
    expect(props.capture).not.toHaveBeenCalled();
    expect(props.setAttaching).not.toHaveBeenCalled();
  });

  it("removes a staged chip", async () => {
    const { result } = attachHarness();
    await act(async () => result.current.attachFiles([new File(["a"], "a.png", { type: "image/png" })]));
    const id = result.current.staged[0]!.id;

    act(() => result.current.removeStaged(id));

    expect(result.current.staged).toHaveLength(0);
  });

  it("when uses the first non-audio file as the batch caption target", async () => {
    const { result, props } = attachHarness();
    const audio = new File(["voice"], "voice.mp3", { type: "audio/mpeg" });
    const image = new File(["pixels"], "photo.png", { type: "image/png" });
    const document = new File(["document"], "brief.txt", { type: "text/plain" });

    await act(async () => result.current.attachFiles([audio, image, document]));
    await act(async () => result.current.sendStaged());

    expect(props.setInput).toHaveBeenCalledWith("");
    expect(props.setAttaching).toHaveBeenNthCalledWith(1, true);
    expect(props.setAttaching).toHaveBeenLastCalledWith(false);
    expect(props.capture).toHaveBeenCalledTimes(3);
    expect(props.capture).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ name: "voice.mp3", mimeType: "audio/mpeg" }),
      "",
      undefined,
    );
    expect(props.capture).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ name: "photo.png", mimeType: "image/png" }),
      "파일 설명",
      expect.any(String), // 이미지의 object-URL 썸네일이 함께 흐른다
    );
    expect(props.capture).toHaveBeenNthCalledWith(
      3,
      expect.objectContaining({ name: "brief.txt", mimeType: "text/plain" }),
      "",
      undefined,
    );
    expect(props.pin).toHaveBeenCalledTimes(3);
    expect(props.onBatchDone).toHaveBeenCalledTimes(1);
    expect(result.current.staged).toHaveLength(0);
  });

  it("without consume composer text for an audio-only batch", async () => {
    const { result, props } = attachHarness();
    const audio = new File(["voice"], "voice.wav", { type: "audio/wav" });

    await act(async () => result.current.attachFiles([audio]));
    await act(async () => result.current.sendStaged());

    expect(props.setInput).not.toHaveBeenCalled();
    expect(props.capture).toHaveBeenCalledWith(expect.objectContaining({ name: "voice.wav" }), "", undefined);
  });

  it("sends a bare base64 payload with inferred MIME", async () => {
    const { result, props } = attachHarness({ input: "" });
    const image = new File(["hello"], "PHOTO.PNG", { type: "application/octet-stream" });

    await act(async () => result.current.attachFiles([image]));
    await act(async () => result.current.sendStaged());

    expect(props.capture).toHaveBeenCalledWith(
      { name: "PHOTO.PNG", mimeType: "image/png", base64: "aGVsbG8=" },
      "",
      expect.any(String),
    );
  });

  it("when blocks synchronous re-entry while a batch is active", async () => {
    let release!: () => void;
    const capture = vi.fn(() => new Promise<void>((resolve) => (release = resolve)));
    const { result } = attachHarness({ capture });
    const image = new File(["image"], "image.png", { type: "image/png" });

    await act(async () => result.current.attachFiles([image]));
    let first!: Promise<void>;
    act(() => {
      first = result.current.sendStaged();
    });
    await waitFor(() => expect(result.current.attachingRef.current).toBe(true));
    // Re-entry while the batch is in flight is a no-op (staged is already
    // drained; a second call must not double-run the loop).
    await act(async () => result.current.sendStaged());
    await waitFor(() => expect(capture).toHaveBeenCalledTimes(1));

    await act(async () => {
      release();
      await first;
    });
    expect(capture).toHaveBeenCalledTimes(1);
  });

  it("continues after one capture rejects and reports the skipped file", async () => {
    const capture = vi.fn().mockRejectedValueOnce(new Error("capture failed")).mockResolvedValueOnce(undefined);
    const { result, props } = attachHarness({ capture });
    const first = new File(["one"], "one.png", { type: "image/png" });
    const second = new File(["two"], "two.png", { type: "image/png" });

    await act(async () => result.current.attachFiles([first, second]));
    await act(async () => result.current.sendStaged());

    expect(capture).toHaveBeenCalledTimes(2);
    expect(result.current.attachNote).toBe("one.png — 읽기 실패라 건너뜀");
    expect(props.onBatchDone).toHaveBeenCalledTimes(1);
  });

  it("always leaves attaching state when capture fails", async () => {
    const capture = vi.fn(async () => {
      throw new Error("capture failed");
    });
    const { result, props } = attachHarness({ capture });
    const image = new File(["image"], "image.png", { type: "image/png" });

    await act(async () => result.current.attachFiles([image]));
    await act(async () => result.current.sendStaged());

    expect(result.current.attachingRef.current).toBe(false);
    expect(props.setAttaching).toHaveBeenNthCalledWith(1, true);
    expect(props.setAttaching).toHaveBeenLastCalledWith(false);
  });

  it("clears a file input so the same selection can be chosen again", () => {
    const { result, props } = attachHarness({ connected: false });
    const input = document.createElement("input");
    input.type = "file";
    Object.defineProperty(input, "files", {
      value: [new File(["image"], "image.png", { type: "image/png" })],
      configurable: true,
    });
    Object.defineProperty(input, "value", { value: "C:\\fakepath\\image.png", writable: true, configurable: true });

    act(() => result.current.onPick({ target: input } as never));

    expect(input.value).toBe("");
    expect(props.capture).not.toHaveBeenCalled();
  });

  it("cancels a pending feedback timer on unmount", async () => {
    vi.useFakeTimers();
    const { result, unmount } = attachHarness();
    const executable = new File(["binary"], "program.exe", { type: "application/x-msdownload" });
    await act(async () => result.current.attachFiles([executable]));
    expect(vi.getTimerCount()).toBe(1);

    unmount();

    expect(vi.getTimerCount()).toBe(0);
  });
});
