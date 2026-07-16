import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ProjectLinkedOut } from "@/types";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { useAiFeed, useWorkspace } from "@/workspaceContext";
import { ProjectHomePane } from "./ProjectHomePane";

type RpcCall = { method: string; params: Record<string, unknown> };

const projects = [
  {
    project: "Alpha",
    headline: "Alpha 출시 준비",
    bullets: ["최종 계약 검토", "출시 일정 확정"],
    due: "7월 20일",
    updatedAtMs: 3_000,
    path: "projects/alpha",
  },
  {
    project: "Beta",
    headline: "Beta 안정화",
    bullets: ["런타임 관찰"],
    updatedAtMs: 2_000,
    path: "projects/beta",
  },
  {
    project: "Pathless",
    headline: "아직 위키 없음",
    updatedAtMs: 1_000,
  },
];

const fixtures: Record<string, any[]> = {
  progress: projects,
  mail: [
    { id: "mail-old", subject: "Alpha 오래된 메일", from: "old@example.com", date: "2026-07-01T00:00:00Z" },
    {
      id: "mail-new",
      subject: "Alpha 최신 메일",
      from: "new@example.com",
      date: "2026-07-10T00:00:00Z",
      isUnread: true,
    },
    { id: "mail-beta", subject: "Beta 전용 메일", date: "2026-07-09T00:00:00Z" },
    {
      id: "mention-only",
      subject: "Alpha라는 단어만 포함",
      body: "명시적 프로젝트 참조가 없는 일반 안내",
      date: "2026-07-11T00:00:00Z",
    },
  ],
  calendar: [
    { id: "event-late", summary: "Alpha 후속 회의", start: "2026-07-20T09:00:00Z" },
    { id: "event-early", summary: "Alpha 킥오프", start: "2026-07-12T09:00:00Z", location: "회의실 A" },
    { id: "event-beta", summary: "Beta 회의", start: "2026-07-13T09:00:00Z" },
  ],
  todo: [
    { id: "todo-late", title: "Alpha 늦은 할일", due: "2026-07-19T00:00:00Z", done: false },
    { id: "todo-early", title: "Alpha 빠른 할일", due: "2026-07-13T00:00:00Z", done: false },
    { id: "todo-done", title: "Alpha 완료 할일", done: true },
    { id: "todo-beta", title: "Beta 할일", done: false },
  ],
  workfeed: [
    { id: "feed-old", title: "Alpha 이전 피드", source: "review", createdAtMs: 100 },
    { id: "feed-new", title: "Alpha 최신 피드", source: "alert", createdAtMs: 200 },
    { id: "feed-beta", title: "Beta 피드", createdAtMs: 300 },
  ],
};

const linkedByPath: Record<string, ProjectLinkedOut> = {
  "projects/alpha": {
    mail: ["mail-old", "mail-new"],
    calendar: ["event-late", "event-early"],
    todo: ["todo-late", "todo-early", "todo-done"],
    workfeed: ["feed-old", "feed-new"],
    notebook: ["nb-old", "nb-new"],
  },
  "projects/beta": {
    mail: ["mail-beta"],
    calendar: ["event-beta"],
    todo: ["todo-beta"],
    workfeed: ["feed-beta"],
    notebook: [],
  },
};

function reply(payload: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => (ok ? { ok: true, payload } : { ok: false, error: String(payload) }),
  } as Response;
}

function installGateway(
  calls: RpcCall[],
  linked: (path: string) => Response | Promise<Response> = (path) => reply(linkedByPath[path] ?? {}),
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const request = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      calls.push({ method: request.method, params: request.params ?? {} });
      if (request.method === "miniapp.notebook.list") {
        return reply({
          notebooks: [
            { id: "nb-old", name: "Alpha 이전 노트북", sourceCount: 1, updated: 100 },
            { id: "nb-new", name: "Alpha 최신 노트북", sourceCount: 4, updated: 200, dealRef: "projects/alpha" },
            { id: "nb-unlinked", name: "이름만 Alpha 노트북", sourceCount: 2, updated: 300 },
          ],
        });
      }
      if (request.method === "miniapp.project.linked") return linked(String(request.params?.path ?? ""));
      return reply({});
    }),
  );
}

function WorkspaceProbe() {
  const { view, paneTarget, wikiTarget } = useWorkspace();
  const { aiText } = useAiFeed();
  return (
    <div>
      <output data-testid="view">{view}</output>
      <output data-testid="target">{JSON.stringify(paneTarget)}</output>
      <output data-testid="wiki">{wikiTarget}</output>
      <output data-testid="ai">{aiText}</output>
    </div>
  );
}

function renderHome(data = fixtures) {
  return renderWithProviders(
    <>
      <ProjectHomePane />
      <WorkspaceProbe />
    </>,
    { connected: true, dataProvider: fakeProvider(data) },
  );
}

describe("ProjectHomePane boundary behavior", () => {
  let calls: RpcCall[];

  beforeEach(() => {
    localStorage.clear();
    calls = [];
    installGateway(calls);
    if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "project-home-test" });
  });

  afterEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  describe("source-of-truth linkage", () => {
    it("when requests linked ids for the freshest project's exact wiki path", async () => {
      renderHome();
      await screen.findByRole("heading", { name: "Alpha" });
      await waitFor(() => expect(calls.some((call) => call.method === "miniapp.project.linked")).toBe(true));
      expect(calls.find((call) => call.method === "miniapp.project.linked")?.params).toEqual({
        path: "projects/alpha",
      });
    });

    it("includes only ids returned by the gateway and rejects name-overlap false positives", async () => {
      renderHome();
      expect(await screen.findByText("Alpha 최신 메일")).toBeInTheDocument();
      expect(screen.getByText("Alpha 오래된 메일")).toBeInTheDocument();
      expect(screen.queryByText("Alpha라는 단어만 포함")).not.toBeInTheDocument();
      expect(screen.queryByText("이름만 Alpha 노트북")).not.toBeInTheDocument();
      expect(screen.queryByText("Beta 전용 메일")).not.toBeInTheDocument();
    });

    it("clears the previous linked set immediately while a switched project is resolving", async () => {
      let resolveBeta!: (response: Response) => void;
      installGateway(calls, (path) =>
        path === "projects/beta"
          ? new Promise<Response>((done) => (resolveBeta = done))
          : reply(linkedByPath[path] ?? {}),
      );
      renderHome();
      await screen.findByText("Alpha 최신 메일");
      await userEvent.click(screen.getByRole("button", { name: /Beta/ }));
      expect(screen.queryByText("Alpha 최신 메일")).not.toBeInTheDocument();
      expect(screen.getByText("연결된 메일 없음")).toBeInTheDocument();
      resolveBeta(reply(linkedByPath["projects/beta"]));
      expect(await screen.findByText("Beta 전용 메일")).toBeInTheDocument();
    });

    it("does not issue linked lookup for a project without a canonical path", async () => {
      renderHome();
      await userEvent.click(await screen.findByRole("button", { name: /Pathless/ }));
      expect(screen.getByRole("heading", { name: "Pathless" })).toBeInTheDocument();
      expect(calls.filter((call) => call.method === "miniapp.project.linked")).toHaveLength(1);
      expect(calls.filter((call) => call.method === "miniapp.project.linked").at(-1)?.params.path).toBe(
        "projects/alpha",
      );
      expect(screen.queryByRole("button", { name: "위키 열기" })).not.toBeInTheDocument();
    });

    it("degrades to empty linked sections when the gateway returns no membership", async () => {
      installGateway(calls, () => reply({}));
      renderHome();
      expect(await screen.findByText("연결된 메일 없음")).toBeInTheDocument();
      expect(screen.getByText("연결된 일정 없음")).toBeInTheDocument();
      expect(screen.getByText("연결된 할일 없음")).toBeInTheDocument();
      expect(screen.getByText("연결된 피드 없음")).toBeInTheDocument();
      expect(await screen.findByText("연결된 노트북 없음")).toBeInTheDocument();
    });
  });

  describe("sorting, filtering and caps", () => {
    it("sorts related mail newest first and marks unread mail", async () => {
      renderHome();
      const section = (await screen.findByText("관련 메일")).closest("section")!;
      const rows = await within(section).findAllByRole("button");
      expect(rows[0]).toHaveTextContent("Alpha 최신 메일");
      expect(rows[1]).toHaveTextContent("Alpha 오래된 메일");
      expect(rows[0]).toHaveTextContent("미열람");
    });

    it("sorts events by start time and carries location as body context", async () => {
      renderHome();
      const section = (await screen.findByText("관련 일정")).closest("section")!;
      const rows = await within(section).findAllByRole("button");
      expect(rows[0]).toHaveTextContent("Alpha 킥오프");
      expect(rows[0]).toHaveTextContent("회의실 A");
      expect(rows[1]).toHaveTextContent("Alpha 후속 회의");
    });

    it("when filters completed todos before sorting by due date", async () => {
      renderHome();
      const section = (await screen.findByText("관련 할일")).closest("section")!;
      const rows = await within(section).findAllByRole("button");
      expect(rows[0]).toHaveTextContent("Alpha 빠른 할일");
      expect(rows[1]).toHaveTextContent("Alpha 늦은 할일");
      expect(within(section).queryByText("Alpha 완료 할일")).not.toBeInTheDocument();
    });

    it("when sorts workfeed and notebooks newest first", async () => {
      renderHome();
      const feed = (await screen.findByText("관련 피드")).closest("section")!;
      expect((await within(feed).findAllByRole("button"))[0]).toHaveTextContent("Alpha 최신 피드");
      const notebooks = screen.getByText("연결 노트북").closest("section")!;
      expect((await within(notebooks).findAllByRole("button"))[0]).toHaveTextContent("Alpha 최신 노트북");
    });

    it("when caps every related section at five rows after sorting", async () => {
      const manyMail = Array.from({ length: 8 }, (_, index) => ({
        id: `mail-${index}`,
        subject: `메일 ${index}`,
        date: new Date(2026, 6, index + 1).toISOString(),
      }));
      installGateway(calls, () => reply({ mail: manyMail.map((mail) => mail.id) }));
      renderHome({ ...fixtures, mail: manyMail });
      const section = (await screen.findByText("관련 메일")).closest("section")!;
      expect(await within(section).findAllByRole("button")).toHaveLength(5);
      expect(within(section).getByText("5")).toHaveClass("project-home-count");
      expect(within(section).getAllByRole("button")[0]).toHaveTextContent("메일 7");
    });

    it("sorts project catalog newest first and sinks missing timestamps", async () => {
      renderHome({
        ...fixtures,
        progress: [
          { project: "No timestamp", path: "projects/none" },
          { project: "Newest", path: "projects/new", updatedAtMs: 9_000 },
          { project: "Old", path: "projects/old", updatedAtMs: 1_000 },
        ],
      });
      const list = await screen.findByRole("complementary", { name: "프로젝트 목록" });
      expect((await within(list).findAllByRole("button")).map((button) => button.textContent)).toEqual([
        expect.stringContaining("Newest"),
        expect.stringContaining("Old"),
        expect.stringContaining("No timestamp"),
      ]);
    });
  });

  describe("status, focus and empty states", () => {
    it("renders digest headline, bullets, due, update and canonical path", async () => {
      renderHome();
      expect(await screen.findByRole("heading", { name: "Alpha" })).toBeInTheDocument();
      expect(screen.getAllByText("Alpha 출시 준비").length).toBeGreaterThan(0);
      expect(screen.getByText("최종 계약 검토")).toBeInTheDocument();
      expect(screen.getByText("출시 일정 확정")).toBeInTheDocument();
      expect(screen.getAllByText("마감 7월 20일").length).toBeGreaterThan(0);
      expect(screen.getAllByText("projects/alpha").length).toBeGreaterThan(0);
    });

    it("builds focus cues from due and non-empty related sections only", async () => {
      renderHome();
      const focus = await screen.findByRole("region", { name: "지금 볼 것" });
      expect(within(focus).getByText("마감 7월 20일")).toBeInTheDocument();
      expect(within(focus).getByText("메일 2")).toBeInTheDocument();
      expect(within(focus).getByText("일정 2")).toBeInTheDocument();
      expect(within(focus).getByText("할일 2")).toBeInTheDocument();
      expect(within(focus).getByText("피드 2")).toBeInTheDocument();
      expect(within(focus).getByText("연결 노트북 2")).toBeInTheDocument();
    });

    it("shows status and focus empty states for a sparse project", async () => {
      installGateway(calls, () => reply({}));
      renderHome({ ...fixtures, progress: [{ project: "Sparse", path: "projects/sparse" }] });
      expect(await screen.findByText("상태 항목 없음")).toBeInTheDocument();
      expect(screen.getByText("연결된 항목 없음")).toBeInTheDocument();
    });

    it("shows project-level empty state when no digests exist", async () => {
      renderHome({ ...fixtures, progress: [] });
      expect(await screen.findByText("진행 중인 프로젝트가 없습니다.")).toBeInTheDocument();
      expect(screen.queryByLabelText("프로젝트 목록")).not.toBeInTheDocument();
    });
  });

  describe("navigation and AI parity", () => {
    it.each([
      ["Alpha 최신 메일", "mail", "mail-new"],
      ["Alpha 킥오프", "calendar", "event-early"],
      ["Alpha 빠른 할일", "todo", "todo-early"],
      ["Alpha 최신 피드", "workfeed", "feed-new"],
      ["Alpha 최신 노트북", "notebook", "nb-new"],
    ])("opens %s with %s/%s target", async (title, view, id) => {
      renderHome();
      await userEvent.click(await screen.findByRole("button", { name: new RegExp(title) }));
      expect(screen.getByTestId("view")).toHaveTextContent(view);
      expect(screen.getByTestId("target")).toHaveTextContent(`"id":"${id}"`);
    });

    it("when opens the selected canonical wiki path", async () => {
      renderHome();
      await userEvent.click(await screen.findByRole("button", { name: "위키 열기" }));
      expect(screen.getByTestId("view")).toHaveTextContent("wiki");
      expect(screen.getByTestId("wiki")).toHaveTextContent("projects/alpha");
    });

    it("when projects the same selected digest and explicit linked rows into AI context", async () => {
      renderHome();
      await screen.findByText("Alpha 최신 메일");
      const ai = screen.getByTestId("ai");
      // The linked-row sections reach the AI projection one effect flush after
      // the grid renders them — wait on the projection itself, not the grid.
      await waitFor(() => expect(ai).toHaveTextContent("[관련 메일 2건]"));
      expect(ai).toHaveTextContent("[프로젝트 홈] Alpha");
      expect(ai).toHaveTextContent("Alpha 출시 준비");
      expect(ai).toHaveTextContent("마감: 7월 20일");
      expect(ai).toHaveTextContent("위키: projects/alpha");
      expect(ai).toHaveTextContent("Alpha 최신 메일");
      expect(ai).not.toHaveTextContent("Alpha라는 단어만 포함");
    });

    it("updates AI context when project selection changes", async () => {
      renderHome();
      await screen.findByText("Alpha 최신 메일");
      await userEvent.click(screen.getByRole("button", { name: /Beta/ }));
      await screen.findByText("Beta 전용 메일");
      const ai = screen.getByTestId("ai");
      expect(ai).toHaveTextContent("[프로젝트 홈] Beta");
      expect(ai).toHaveTextContent("Beta 안정화");
      expect(ai).not.toHaveTextContent("Alpha 최신 메일");
    });
  });
});
