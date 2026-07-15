import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { ObservePane } from "./ObservePane";

// ObservePane is query-driven: callRpc → fetch for behavior + logs. Stub fetch
// so the connected render (summary, tools, warn/error logs, period switcher,
// empty/error/retry) is deterministic without a gateway.
const BEHAVIOR = {
  runs: 12,
  proactiveRuns: 3,
  compactedRuns: 1,
  tools: [
    { name: "wiki", calls: 8, errors: 0, avgMs: 42 },
    { name: "web", calls: 5, errors: 2, avgMs: 900 },
  ],
};
const LOGS = {
  lines: [
    { level: "WARN", msg: "slow tool web", runId: "r1" },
    { level: "ERROR", msg: "provider timeout", runId: "r2" },
  ],
  count: 2,
};

type FetchFn = (url: string, init?: RequestInit) => Promise<Response>;

function stubFetch(handler?: (method: string, params: Record<string, unknown>) => unknown) {
  const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body ?? "{}")) as {
      method?: string;
      params?: Record<string, unknown>;
    };
    const method = String(body.method ?? "");
    const params = body.params ?? {};
    let payload: unknown;
    if (handler) {
      payload = handler(method, params);
    } else if (method === "miniapp.observe.behavior") {
      payload = BEHAVIOR;
    } else if (method === "miniapp.observe.logs") {
      payload = LOGS;
    } else {
      payload = {};
    }
    return new Response(JSON.stringify({ ok: true, payload }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as FetchFn;
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock as unknown as ReturnType<typeof vi.fn>;
}

beforeEach(() => {
  stubFetch();
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("ObservePane", () => {
  it("shows the disconnected state when not connected", () => {
    renderWithProviders(<ObservePane />, { connected: false });
    expect(screen.getByText("미연결")).toBeInTheDocument();
  });

  it("renders behavior summary, tools, and warn/error logs", async () => {
    renderWithProviders(<ObservePane />, { connected: true });

    expect(await screen.findByText("최근 7일 동작")).toBeInTheDocument();
    expect(screen.getByText("실행 12회 · 능동 3 · 압축 1")).toBeInTheDocument();
    expect(screen.getByText("wiki")).toBeInTheDocument();
    expect(screen.getByText("8회 · 평균 42ms")).toBeInTheDocument();
    expect(screen.getByText("web")).toBeInTheDocument();
    expect(screen.getByText("5회 · 2 오류 · 평균 900ms")).toBeInTheDocument();
    expect(screen.getByText("WARN")).toBeInTheDocument();
    expect(screen.getByText("slow tool web")).toBeInTheDocument();
    expect(screen.getByText("ERROR")).toBeInTheDocument();
    expect(screen.getByText("provider timeout")).toBeInTheDocument();
  });

  it("re-queries with days=1 when switching the period", async () => {
    const fetchMock = stubFetch((_method, params) => {
      if (_method === "miniapp.observe.behavior") {
        return { ...BEHAVIOR, runs: params.days === 1 ? 2 : 12 };
      }
      return LOGS;
    });

    renderWithProviders(<ObservePane />, { connected: true });
    expect(await screen.findByText("최근 7일 동작")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "1일" }));
    expect(await screen.findByText("최근 1일 동작")).toBeInTheDocument();
    expect(screen.getByText("실행 2회 · 능동 3 · 압축 1")).toBeInTheDocument();

    await waitFor(() => {
      const bodies = fetchMock.mock.calls.map((c) => JSON.parse(String((c[1] as RequestInit)?.body ?? "{}")));
      const daysParams = bodies
        .filter((b: { method?: string }) => b.method === "miniapp.observe.behavior")
        .map((b: { params?: { days?: number } }) => b.params?.days);
      expect(daysParams).toContain(1);
    });
  });

  it("shows empty copy when both behavior and logs are empty", async () => {
    stubFetch((method) => {
      if (method === "miniapp.observe.behavior") {
        return { runs: 0, proactiveRuns: 0, compactedRuns: 0, tools: [] };
      }
      return { lines: [], count: 0 };
    });

    renderWithProviders(<ObservePane />, { connected: true });
    expect(await screen.findByText("아직 관찰된 동작이 없습니다.")).toBeInTheDocument();
  });

  it("shows an error with retry when both RPCs fail", async () => {
    let fail = true;
    stubFetch(() => {
      if (fail) throw new Error("observe offline");
      return BEHAVIOR;
    });
    // Override: rejected promises need fetch to reject, not return ok:false.
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
        if (fail) {
          return new Response(JSON.stringify({ ok: false, error: { code: "E", message: "observe offline" } }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        const method = String(body.method ?? "");
        const payload = method === "miniapp.observe.behavior" ? BEHAVIOR : LOGS;
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<ObservePane />, { connected: true });
    expect(await screen.findByText(/관찰 데이터를 불러오지 못했습니다/)).toBeInTheDocument();

    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "다시 시도" }));
    expect(await screen.findByText("최근 7일 동작")).toBeInTheDocument();
  });
});
