import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { RsiPane } from "./RsiPane";

// RsiPane is query-driven: it hits three RPCs straight through callRpc → fetch
// (rsi.status, skills.lifecycle, self_improvement_coding.list). Stub fetch to
// answer them so the connected render (health scoreboard, L4 candidate drill,
// candidate detail modal) is deterministically exercisable without a gateway.
const STATUS = {
  turning: 1,
  layers: [
    {
      key: "L4",
      title: "소스 자가편집",
      state: "LIVE",
      diagnosis: "제안 전용 자동수리 큐가 흐른다",
      detail: "코딩 후보를 제안 전용으로 큐잉",
    },
  ],
  health: {
    evolves7d: 3,
    confirmed7d: 2,
    rolledBack7d: 0,
    genesis7d: 1,
    metaRevisions7d: 0,
    confirmRate: 0.8,
    falseAcceptRate: 0.1,
    resolvedEvolves7d: 5,
    thrash: false,
    autoAdoptFrozen: false,
  },
};
const CODING = {
  candidates: [
    {
      id: "cand-1",
      title: "도구 지연 개선",
      source: "tool-quality:latency:web",
      status: "proposed",
      scope: "code",
      autoDispatch: true,
      proposedChange: "web 도구 타임아웃 상향",
    },
  ],
};

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
      const method = String(body.method ?? "");
      const payload =
        method === "miniapp.rsi.status"
          ? STATUS
          : method === "miniapp.self_improvement_coding.list"
            ? CODING
            : { events: [] };
      return new Response(JSON.stringify({ ok: true, payload }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("RsiPane", () => {
  // The sandbox has no gateway; the disconnected path is what's deterministically
  // testable (CLAUDE.md) — it must guide, not spin or blank.
  it("shows the disconnected state when not connected", () => {
    renderWithProviders(<RsiPane />, { connected: false });
    expect(screen.getByText("미연결")).toBeInTheDocument();
  });

  it("renders the health scoreboard and opens/closes the candidate detail modal", async () => {
    renderWithProviders(<RsiPane />, { connected: true });

    // The 7-day evolution-health scoreboard renders from rsi.status.health.
    expect(await screen.findByText("진화 건강 (7일)")).toBeInTheDocument();
    expect(screen.getByText("확정률")).toBeInTheDocument();

    // Expand the L4 layer to reveal the coding-candidate drill, then click the row.
    await userEvent.click(screen.getByText(/소스 자가편집/));
    const row = await screen.findByText("도구 지연 개선");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await userEvent.click(row);

    // Clicking the candidate opens the detail modal (provenance shows there).
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("출처")).toBeInTheDocument();

    // Closing dismisses it.
    await userEvent.click(within(dialog).getByRole("button", { name: "닫기" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});
