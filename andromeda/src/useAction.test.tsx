import type { ReactNode } from "react";
import { DataProviderScope } from "@/crud";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { fakeProvider } from "./test/util";
import { useAction } from "./useAction";
import { WorkspaceProvider } from "./WorkspaceProvider";

const cfg = { url: "http://test", token: "tok" };

function wrapper({ children }: { children: ReactNode }) {
  return (
    <DataProviderScope dataProvider={fakeProvider()}>
      <WorkspaceProvider connected cfg={cfg} setCfg={() => {}}>
        {children}
      </WorkspaceProvider>
    </DataProviderScope>
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useAction", () => {
  it("does not update hook state when an RPC finishes after unmount", async () => {
    let resolveFetch: (response: Response) => void = () => {};
    const fetchPromise = new Promise<Response>((resolve) => {
      resolveFetch = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(() => fetchPromise),
    );

    const refetch = vi.fn();
    const { result, unmount } = renderHook(() => useAction(refetch), { wrapper });

    let runPromise!: Promise<{ ok: boolean } | undefined>;
    act(() => {
      runPromise = result.current.run<{ ok: boolean }>("miniapp.test");
    });
    expect(result.current.busy).toBe(true);

    unmount();
    const response = new Response(JSON.stringify({ ok: true, payload: { ok: true } }), {
      headers: { "Content-Type": "application/json" },
    });
    const windowValue = globalThis.window;
    vi.stubGlobal("window", undefined);
    try {
      resolveFetch(response);
      await expect(runPromise).resolves.toEqual({ ok: true });
    } finally {
      vi.stubGlobal("window", windowValue);
    }
    expect(refetch).toHaveBeenCalledTimes(1);
  });
});
