import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { SkillsPane } from "./SkillsPane";

// The skill list flows through the (fake) data provider; detail/lifecycle/update
// /delete are query-driven RPCs that go straight to callRpc → fetch. Stub fetch to
// answer those and capture the calls.
let rpcCalls: Array<{ method: string; params: Record<string, unknown> }>;

interface CapturedBody {
  method?: string;
  params?: Record<string, unknown>;
}

const DETAIL = {
  skill: {
    name: "pdf",
    description: "PDF 도구",
    category: "productivity",
    source: "bundled",
    editable: false,
    deletable: false,
  },
  body: "---\nname: pdf\n---\n\n# PDF\n읽고 씁니다.",
  bodyTruncated: false,
  path: "/skills/pdf/SKILL.md",
};
const LIFECYCLE = {
  events: [{ type: "evolved", skillName: "pdf", at: Date.now() - 3_600_000, version: "1.1", detail: "표 추출 개선" }],
  count: 1,
  summary: { system: "Propus", state: "steady", total: 5, genesis: 2, evolved: 3 },
};

beforeEach(() => {
  rpcCalls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as CapturedBody;
      const method = String(body.method ?? "");
      rpcCalls.push({ method, params: body.params ?? {} });
      const payload =
        method === "miniapp.skills.detail" ? DETAIL : method === "miniapp.skills.lifecycle" ? LIFECYCLE : { ok: true };
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

describe("SkillsPane", () => {
  it("lists skills with their origin badge and opens the SKILL.md detail on click", async () => {
    const dataProvider = fakeProvider({
      skills: [
        { name: "pdf", description: "PDF 도구", category: "productivity", source: "bundled", origin: "initial" },
        {
          name: "deep-research",
          description: "리서치",
          source: "managed",
          origin: "genesis",
          editable: true,
          deletable: true,
        },
      ],
    });
    renderWithProviders(<SkillsPane />, { connected: true, dataProvider });

    expect(await screen.findByText("pdf")).toBeInTheDocument();
    expect(screen.getByText("deep-research")).toBeInTheDocument();
    // genesis origin renders the "생성" badge; an initial skill the "최초" badge.
    expect(screen.getByText("생성")).toBeInTheDocument();
    expect(screen.getByText("최초")).toBeInTheDocument();

    await userEvent.click(screen.getByText("pdf"));

    // Opening a row fetches the per-skill detail + lifecycle and renders the body.
    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.skills.detail")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.skills.detail")?.params).toMatchObject({ name: "pdf" });
    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.skills.lifecycle")).toBe(true));
    // Frontmatter is stripped; the prose renders.
    expect(await screen.findByText("PDF")).toBeInTheDocument();
    expect(screen.getByText("읽고 씁니다.")).toBeInTheDocument();
    // The per-skill Propus event shows.
    expect(screen.getByText(/표 추출 개선/)).toBeInTheDocument();
  });

  it("switches to the Propus 로그 view and loads the global lifecycle feed", async () => {
    const dataProvider = fakeProvider({ skills: [{ name: "pdf", origin: "initial" }] });
    renderWithProviders(<SkillsPane />, { connected: true, dataProvider });

    await screen.findByText("pdf");
    await userEvent.click(screen.getByRole("button", { name: "Propus 로그" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.skills.lifecycle")).toBe(true));
    // The global feed (no skillName filter) requested.
    expect(rpcCalls.find((c) => c.method === "miniapp.skills.lifecycle")?.params).not.toHaveProperty("skillName");
    expect(await screen.findByText(/표 추출 개선/)).toBeInTheDocument();
    expect(screen.getByText("정상 관찰 중")).toBeInTheDocument();
  });
});
