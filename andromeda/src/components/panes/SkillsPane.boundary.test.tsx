import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fakeProvider, renderWithProviders } from "@/test/util";
import type { SkillDetailResponse, SkillLifecycleEvent, SkillRow, SkillsLifecycleResponse } from "@/types";
import { SkillsPane } from "./SkillsPane";

type RpcCall = { method: string; params: Record<string, unknown> };

const editableSkill: SkillRow = {
  name: "contract-review",
  description: "계약의 위험과 협상 포인트를 점검합니다.",
  category: "productivity",
  source: "workspace",
  version: "2.4",
  origin: "genesis",
  homepage: "https://skills.example/contract-review",
  tags: ["legal", "risk"],
  relatedSkills: ["decision-premortem"],
  dependencySummary: ["python>=3.12"],
  installSummary: ["uv sync"],
  editable: true,
  deletable: true,
  curatorState: "active",
  totalUses: 12,
  lastUsedAt: Date.now() - 60_000,
  evolveCount: 3,
  lastEvolvedAt: Date.now() - 3_600_000,
  createdAt: Date.now() - 86_400_000,
};

const defaultDetail: SkillDetailResponse = {
  skill: editableSkill,
  body: "---\nname: contract-review\ndescription: frontmatter\n---\n\n# 계약 검토\n\n위험과 협상 포인트를 확인합니다.",
  bodyTruncated: false,
  path: "/workspace/skills/contract-review/SKILL.md",
};

const events: SkillLifecycleEvent[] = [
  {
    type: "genesis",
    skillName: "contract-review",
    at: Date.now() - 86_400_000,
    version: "1.0",
    detail: "계약 위험 분석 스킬 생성",
    route: "genesis",
    evidence: "반복된 계약 검토 요청 5건",
  },
  {
    type: "evolved",
    skillName: "contract-review",
    at: Date.now() - 3_600_000,
    version: "2.4",
    detail: "위험도와 협상 대안 분리",
    route: "evolve",
    evidence: "출력 형식 개선 피드백",
  },
];

const lifecycle: SkillsLifecycleResponse = {
  events,
  count: events.length,
  summary: {
    system: "Propus",
    state: "needs_validation",
    total: 11,
    genesis: 4,
    evolved: 5,
    review: 2,
    attention: 0,
    latestAt: Date.now() - 60_000,
    doctrineVersion: "propus-v3",
    sourcePapers: ["paper-a", "paper-b"],
    filteredSources: ["low-signal"],
    qualityGates: ["tests", "review"],
    nextCue: "회귀 테스트를 확인하세요.",
  },
};

function reply(payload: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => (ok ? { ok: true, payload } : { ok: false, error: String(payload) }),
  } as Response;
}

function installRpc(
  calls: RpcCall[],
  handlers: Partial<Record<string, (params: Record<string, unknown>) => Response | Promise<Response>>> = {},
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const request = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      const method = request.method ?? "";
      const params = request.params ?? {};
      calls.push({ method, params });
      if (handlers[method]) return handlers[method]!(params);
      if (method === "miniapp.skills.detail") return reply(defaultDetail);
      if (method === "miniapp.skills.lifecycle") return reply(lifecycle);
      if (method === "miniapp.skills.update") {
        return reply({ ...defaultDetail, body: String(params.body ?? "") });
      }
      if (method === "miniapp.skills.delete") return reply({ deleted: true });
      return reply({});
    }),
  );
}

function renderSkills(rows: SkillRow[] = [editableSkill], connected = true) {
  return renderWithProviders(<SkillsPane />, {
    connected,
    cfg: connected ? { url: "http://test", token: "tok" } : { url: "", token: "" },
    dataProvider: fakeProvider({ skills: rows }),
  });
}

async function openSkill(name = "contract-review") {
  if (!screen.queryByText(name)) renderSkills();
  await userEvent.click(await screen.findByText(name));
  return screen.findByRole("dialog", { name });
}

describe("SkillsPane boundary behavior", () => {
  let calls: RpcCall[];

  beforeEach(() => {
    calls = [];
    localStorage.clear();
    installRpc(calls);
  });

  afterEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  describe("catalog projection", () => {
    it("renders an explicit empty state when no skills are discovered", async () => {
      renderSkills([]);
      expect(await screen.findByText("사용할 수 있는 스킬이 없습니다.")).toBeInTheDocument();
    });

    it("renders source, category, mutability, version, dependencies, evolution and usage metadata", async () => {
      renderSkills();
      await screen.findByText("contract-review");
      expect(screen.getByText(/productivity · 워크스페이스/)).toHaveTextContent(
        "productivity · 워크스페이스 · 수정/삭제 가능 · v2.4 · 요구 1개 · 설치 1개 · 진화 3회 · 사용 12회",
      );
    });

    it.each([
      ["managed", "관리형"],
      ["workspace", "워크스페이스"],
      ["agents-skills-personal", "개인"],
      ["agents-skills-project", "프로젝트"],
      ["bundled", "기본 제공"],
      ["plugin", "플러그인"],
      ["extra", "추가"],
      ["future-source", "future-source"],
    ])("maps source %s to %s without dropping future values", async (source, label) => {
      renderSkills([{ name: `skill-${source}`, source }]);
      await screen.findByText(`skill-${source}`);
      expect(screen.getByText(label)).toBeInTheDocument();
    });

    it("omits blank and zero-valued metadata instead of rendering noisy separators", async () => {
      renderSkills([
        {
          name: "minimal",
          category: "  ",
          source: " ",
          version: "",
          totalUses: 0,
          evolveCount: 0,
          dependencySummary: [],
          installSummary: [],
        },
      ]);
      const row = (await screen.findByText("minimal")).closest("tr")!;
      expect(within(row).getAllByRole("cell")[1]).toHaveTextContent("");
      expect(within(row).getAllByRole("cell")[1]).not.toHaveTextContent("·");
    });

    it("uses a safe fallback for a missing skill name and does not open an invalid detail", async () => {
      renderSkills([{ description: "name missing" }]);
      expect(await screen.findByText("—")).toBeInTheDocument();
      await userEvent.click(screen.getByText("—"));
      expect(calls.filter((call) => call.method === "miniapp.skills.detail")).toHaveLength(0);
    });

    it("when distinguishes genesis and original origin badges", async () => {
      renderSkills([
        { name: "generated", origin: "genesis" },
        { name: "bundled", origin: "initial" },
      ]);
      await screen.findByText("generated");
      expect(screen.getByText("생성")).toBeInTheDocument();
      expect(screen.getByText("최초")).toBeInTheDocument();
    });

    it("when publishes the visible catalog into the AI pane projection", async () => {
      renderSkills([
        { name: "pdf", description: "PDF 읽기", category: "productivity", source: "bundled" },
        { name: "research", description: "근거 조사", totalUses: 4 },
      ]);
      await screen.findByText("pdf");
      expect(document.body).toHaveTextContent("pdf");
      // The actual projection is exercised by WorkspaceProvider tests; this verifies
      // serialization inputs remain on-screen and no row is silently filtered.
      expect(screen.getByText("research")).toBeInTheDocument();
      expect(screen.getByText(/사용 4회/)).toBeInTheDocument();
    });
  });

  describe("detail loading and document rendering", () => {
    it("requests detail and per-skill lifecycle with the selected name", async () => {
      await openSkill();
      await waitFor(() => expect(calls.some((call) => call.method === "miniapp.skills.lifecycle")).toBe(true));
      expect(calls).toEqual(
        expect.arrayContaining([
          { method: "miniapp.skills.detail", params: { name: "contract-review" } },
          {
            method: "miniapp.skills.lifecycle",
            params: { limit: 30, skillName: "contract-review" },
          },
        ]),
      );
    });

    it("strips complete YAML frontmatter before rendering markdown prose", async () => {
      const dialog = await openSkill();
      expect(await within(dialog).findByRole("heading", { name: "계약 검토" })).toBeInTheDocument();
      expect(within(dialog).getByText("위험과 협상 포인트를 확인합니다.")).toBeInTheDocument();
      expect(within(dialog).queryByText(/description: frontmatter/)).not.toBeInTheDocument();
    });

    it("when strips a UTF-8 BOM before recognizing frontmatter", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, body: "\uFEFF---\nname: bom\n---\n\n# BOM 본문" }),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByRole("heading", { name: "BOM 본문" })).toBeInTheDocument();
      expect(within(dialog).queryByText(/name: bom/)).not.toBeInTheDocument();
    });

    it("preserves an unterminated frontmatter document visible instead of deleting user content", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, body: "---\nname: broken\n# 여전히 보여야 함" }),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByText(/name: broken/)).toBeInTheDocument();
      expect(dialog).toHaveTextContent("여전히 보여야 함");
    });

    it("renders a body without frontmatter unchanged", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, body: "# 순수 문서\n\n그대로 표시" }),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByRole("heading", { name: "순수 문서" })).toBeInTheDocument();
      expect(within(dialog).getByText("그대로 표시")).toBeInTheDocument();
    });

    it("renders an explicit unavailable body state for whitespace-only content", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, body: "   \n\t" }),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByText("SKILL.md 본문을 읽을 수 없습니다.")).toBeInTheDocument();
      expect(within(dialog).getByRole("button", { name: "수정" })).toBeDisabled();
    });

    it("keeps a truncated document readable but disables unsafe editing", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, bodyTruncated: true }),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByText("(문서가 길어 일부만 표시합니다)")).toBeInTheDocument();
      expect(within(dialog).getByRole("button", { name: "수정" })).toBeDisabled();
      expect(dialog).toHaveTextContent("문서가 길어 일부만 표시되어 수정할 수 없습니다.");
    });

    it("renders all non-empty facts and the canonical path", async () => {
      const dialog = await openSkill();
      await within(dialog).findByText(/productivity · 워크스페이스 · v2.4/);
      expect(dialog).toHaveTextContent("홈페이지 https://skills.example/contract-review");
      expect(dialog).toHaveTextContent("태그 legal · risk");
      expect(dialog).toHaveTextContent("관련 스킬 decision-premortem");
      expect(dialog).toHaveTextContent("요구조건 python>=3.12");
      expect(dialog).toHaveTextContent("설치 힌트 uv sync");
      expect(dialog).toHaveTextContent("앱에서 수정/삭제 가능");
      expect(dialog).toHaveTextContent("상태 활성");
      expect(dialog).toHaveTextContent("/workspace/skills/contract-review/SKILL.md");
    });

    it.each([
      ["active", "상태 활성"],
      ["stale", "상태 정체"],
      ["archived", "상태 보관됨"],
    ])("when maps curator state %s", async (curatorState, label) => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, skill: { ...editableSkill, curatorState } }),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByText(label)).toBeInTheDocument();
    });

    it("shows a transport error instead of leaving an indefinite loading message", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => Promise.reject(new Error("detail offline")),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByText("스킬을 불러오지 못했습니다: detail offline")).toBeInTheDocument();
      expect(within(dialog).queryByText("불러오는 중…")).not.toBeInTheDocument();
    });

    it("treats lifecycle enrichment as best-effort when detail succeeds", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () => Promise.reject(new Error("lifecycle offline")),
      });
      const dialog = await openSkill();
      expect(await within(dialog).findByRole("heading", { name: "계약 검토" })).toBeInTheDocument();
      expect(within(dialog).getByText("이 스킬의 Propus 활동이 아직 없습니다.")).toBeInTheDocument();
      expect(within(dialog).queryByText(/lifecycle offline/)).not.toBeInTheDocument();
    });

    it("closes the detail without mutating catalog state", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getAllByRole("button", { name: "닫기" })[0]);
      expect(screen.queryByRole("dialog", { name: "contract-review" })).not.toBeInTheDocument();
      expect(screen.getByText("contract-review")).toBeInTheDocument();
    });
  });

  describe("guarded edit and delete mutations", () => {
    it("enters edit mode with the exact unstripped document", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "수정" }));
      expect(within(dialog).getByRole("textbox")).toHaveValue(defaultDetail.body);
      expect(within(dialog).getByRole("button", { name: "저장" })).toBeEnabled();
    });

    it("cancels edits and restores the server-provided body", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "수정" }));
      fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "temporary draft" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "취소" }));
      expect(within(dialog).queryByDisplayValue("temporary draft")).not.toBeInTheDocument();
      expect(within(dialog).getByRole("heading", { name: "계약 검토" })).toBeInTheDocument();
    });

    it("disables save for a whitespace-only draft", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "수정" }));
      fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: " \n\t " } });
      expect(within(dialog).getByRole("button", { name: "저장" })).toBeDisabled();
    });

    it("updates with exact body text, exits edit mode and reports success", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "수정" }));
      fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "# 새 계약 검토\n\n정확한 본문" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));

      await waitFor(() =>
        expect(calls).toContainEqual({
          method: "miniapp.skills.update",
          params: { name: "contract-review", body: "# 새 계약 검토\n\n정확한 본문" },
        }),
      );
      expect(await within(dialog).findByText("저장했습니다.")).toBeInTheDocument();
      expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
      expect(within(dialog).getByRole("heading", { name: "새 계약 검토" })).toBeInTheDocument();
    });

    it("preserves the draft and reports an update failure for retry", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.update": () => reply("write denied", false),
      });
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "수정" }));
      fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "# 보존할 초안" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
      expect(await within(dialog).findByText(/HTTP 500/)).toBeInTheDocument();
      expect(within(dialog).getByRole("textbox")).toHaveValue("# 보존할 초안");
      expect(within(dialog).getByRole("button", { name: "저장" })).toBeEnabled();
    });

    it("opens a nested irreversible-delete confirmation", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      const confirm = screen.getByRole("dialog", { name: "스킬 삭제" });
      expect(confirm).toHaveTextContent("contract-review 스킬 디렉터리를 삭제합니다. 되돌릴 수 없습니다.");
      expect(calls.filter((call) => call.method === "miniapp.skills.delete")).toHaveLength(0);
    });

    it("cancels delete without closing the underlying detail", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      const confirm = screen.getByRole("dialog", { name: "스킬 삭제" });
      await userEvent.click(within(confirm).getByRole("button", { name: "취소" }));
      expect(screen.queryByRole("dialog", { name: "스킬 삭제" })).not.toBeInTheDocument();
      expect(dialog).toBeInTheDocument();
    });

    it("deletes by name and closes both modals on success", async () => {
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      const confirm = screen.getByRole("dialog", { name: "스킬 삭제" });
      await userEvent.click(within(confirm).getByRole("button", { name: "삭제" }));
      await waitFor(() =>
        expect(calls).toContainEqual({ method: "miniapp.skills.delete", params: { name: "contract-review" } }),
      );
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "contract-review" })).not.toBeInTheDocument());
    });

    it("reports delete failure and returns to a usable detail", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.delete": () => reply("delete denied", false),
      });
      const dialog = await openSkill();
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      await userEvent.click(
        within(screen.getByRole("dialog", { name: "스킬 삭제" })).getByRole("button", { name: "삭제" }),
      );
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "스킬 삭제" })).not.toBeInTheDocument());
      expect(within(dialog).getByText(/HTTP 500/)).toBeInTheDocument();
      expect(within(dialog).getByRole("button", { name: "삭제" })).toBeEnabled();
    });

    it.each([
      [{ editable: true, deletable: false }, true, false, "앱에서 수정 가능"],
      [{ editable: false, deletable: true }, false, true, "앱에서 삭제 가능"],
      [{ editable: false, deletable: false }, false, false, ""],
    ])("when honors independent mutation capabilities %#", async (flags, edit, del, fact) => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.detail": () => reply({ ...defaultDetail, skill: { ...editableSkill, ...flags } }),
      });
      const dialog = await openSkill();
      await within(dialog).findByRole("heading", { name: "계약 검토" });
      expect(Boolean(within(dialog).queryByRole("button", { name: "수정" }))).toBe(edit);
      expect(Boolean(within(dialog).queryByRole("button", { name: "삭제" }))).toBe(del);
      if (fact) expect(dialog).toHaveTextContent(fact);
    });
  });

  describe("Propus feed semantics", () => {
    async function openPropus() {
      renderSkills();
      await userEvent.click(screen.getByRole("button", { name: "Propus 로그" }));
    }

    it("without fetch lifecycle while disconnected", async () => {
      renderSkills([], false);
      await userEvent.click(screen.getByRole("button", { name: "Propus 로그" }));
      expect(screen.getByText("미연결")).toBeInTheDocument();
      expect(calls.filter((call) => call.method === "miniapp.skills.lifecycle")).toHaveLength(0);
    });

    it("requests a global feed without a skillName filter", async () => {
      await openPropus();
      await waitFor(() => expect(calls.some((call) => call.method === "miniapp.skills.lifecycle")).toBe(true));
      expect(calls.find((call) => call.method === "miniapp.skills.lifecycle")?.params).toEqual({ limit: 60 });
    });

    it("renders doctrine, evidence filters, quality gates and activity counts", async () => {
      await openPropus();
      expect(await screen.findByText("Propus")).toBeInTheDocument();
      expect(screen.getByText("propus-v3 · 논문 2개 · 보류 1개 · 게이트 2개")).toBeInTheDocument();
      expect(screen.getByText(/최근 11건 · 생성 4 · 진화 5 · 리뷰 2/)).toBeInTheDocument();
      expect(screen.getByText("검증 필요")).toBeInTheDocument();
      expect(screen.getByText("회귀 테스트를 확인하세요.")).toBeInTheDocument();
    });

    it("when prioritizes attention count over a nominal lifecycle state", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () =>
          reply({
            ...lifecycle,
            summary: { ...lifecycle.summary, state: "steady", attention: 2, attentionCue: "롤백 원인을 검토하세요." },
          }),
      });
      await openPropus();
      expect(await screen.findByText("주의 2건 · 기각/롤백 포함")).toBeInTheDocument();
      expect(screen.getByText("롤백 원인을 검토하세요.")).toBeInTheDocument();
      expect(screen.queryByText("정상 관찰 중")).not.toBeInTheDocument();
    });

    it.each([
      ["idle", "아직 관찰 대기 중"],
      ["steady", "정상 관찰 중"],
      ["has_backlog", "개선 후보 대기"],
      ["needs_validation", "검증 필요"],
      ["needs_evolution", "진화 검토 필요"],
      ["needs_review", "리뷰 필요"],
      ["attention", "주의 필요"],
      ["future-state", "정상 관찰 중"],
    ])("when maps summary state %s", async (state, label) => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () => reply({ ...lifecycle, summary: { system: "Propus", state } }),
      });
      await openPropus();
      expect(await screen.findByText(label)).toBeInTheDocument();
    });

    it("renders an empty feed separately from a failed feed", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () => reply({ events: [], count: 0 }),
      });
      await openPropus();
      expect(await screen.findByText("아직 Propus 로그가 없습니다.")).toBeInTheDocument();
    });

    it("renders a feed transport failure and removes the loading placeholder", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () => Promise.reject(new Error("propus offline")),
      });
      await openPropus();
      expect(await screen.findByText("Propus 로그를 불러오지 못했습니다: propus offline")).toBeInTheDocument();
      expect(screen.queryByText("불러오는 중…")).not.toBeInTheDocument();
    });

    it.each([
      ["genesis", "생성"],
      ["evolved", "진화"],
      ["evolve_rejected", "기각"],
      ["evolve_rolled_back", "롤백"],
      ["future-event", "리뷰"],
    ])("when maps lifecycle event %s to %s", async (type, label) => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () => reply({ events: [{ type, skillName: `skill-${type}`, detail: type }] }),
      });
      await openPropus();
      expect(await screen.findByText(label)).toBeInTheDocument();
    });

    it.each([
      ["no-op", "판정: 변경 없음"],
      ["evolve", "판정: 기존 스킬 진화"],
      ["genesis", "판정: 새 스킬 생성(자동)"],
      ["create", "판정: 새 스킬 생성(수동)"],
      ["manual-review", "판정: manual-review"],
    ])("displays route %s only after an event is expanded", async (route, verdict) => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () =>
          reply({ events: [{ type: "evolved", skillName: "route-skill", route, detail: "세부 정보" }] }),
      });
      await openPropus();
      expect(screen.queryByText(verdict)).not.toBeInTheDocument();
      await userEvent.click(await screen.findByText("세부 정보"));
      expect(screen.getByText(verdict)).toBeInTheDocument();
    });

    it("displays evidence and a skill navigation action only in expanded state", async () => {
      await openPropus();
      expect(screen.queryByText(/근거: 반복된 계약 검토 요청/)).not.toBeInTheDocument();
      await userEvent.click(await screen.findByText(/계약 위험 분석 스킬 생성/));
      expect(screen.getByText("근거: 반복된 계약 검토 요청 5건")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "스킬 보기 →" })).toBeInTheDocument();
    });

    it("when opens the event skill detail and switches back to the catalog", async () => {
      await openPropus();
      await userEvent.click(await screen.findByText(/계약 위험 분석 스킬 생성/));
      await userEvent.click(screen.getByRole("button", { name: "스킬 보기 →" }));
      expect(screen.getByRole("button", { name: "스킬 목록" })).toHaveStyle("color: var(--accent)");
      expect(await screen.findByRole("dialog", { name: "contract-review" })).toBeInTheDocument();
      await waitFor(() =>
        expect(calls).toContainEqual({ method: "miniapp.skills.detail", params: { name: "contract-review" } }),
      );
    });

    it("does not offer navigation for an event without a skill name", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.skills.lifecycle": () => reply({ events: [{ type: "evolved", detail: "orphan event" }] }),
      });
      await openPropus();
      expect(await screen.findByText("(스킬 미지정)")).toBeInTheDocument();
      await userEvent.click(screen.getByText("orphan event"));
      expect(screen.queryByRole("button", { name: "스킬 보기 →" })).not.toBeInTheDocument();
    });
  });
});
