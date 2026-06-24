import { describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useAsyncOnOpen } from "./useAsyncOnOpen";

describe("useAsyncOnOpen", () => {
  it("loads data on open when enabled", async () => {
    const { result } = renderHook(() => useAsyncOnOpen(() => Promise.resolve(42), [], { enabled: true }));
    expect(result.current[0]).toBeNull();
    await waitFor(() => expect(result.current[0]).toBe(42));
  });

  it("does not run the loader when disabled", () => {
    const load = vi.fn(() => Promise.resolve(1));
    const { result } = renderHook(() => useAsyncOnOpen(load, [], { enabled: false }));
    expect(load).not.toHaveBeenCalled();
    expect(result.current[0]).toBeNull();
  });

  it("swallows errors but reports them via onError", async () => {
    const onError = vi.fn();
    const { result } = renderHook(() => useAsyncOnOpen(() => Promise.reject(new Error("boom")), [], { onError }));
    await waitFor(() => expect(onError).toHaveBeenCalledTimes(1));
    expect(result.current[0]).toBeNull();
  });

  it("exposes setData for imperative updates", async () => {
    const { result } = renderHook(() => useAsyncOnOpen(() => Promise.resolve("a"), []));
    await waitFor(() => expect(result.current[0]).toBe("a"));
    act(() => result.current[1]("b"));
    expect(result.current[0]).toBe("b");
  });
});
