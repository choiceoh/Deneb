import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { fakeProvider, renderWithProviders } from "@/test/util";
import { useWorkspace } from "@/workspaceContext";
import { ProjectHomePane } from "./ProjectHomePane";

beforeEach(() => {
  localStorage.clear();
  if (!globalThis.crypto?.randomUUID) {
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const { method, params } = JSON.parse(String(init?.body ?? "{}")) as {
        method: string;
        params?: { path?: string };
      };
      let payload: unknown = {};
      if (method === "miniapp.notebook.list") {
        payload = {
          notebooks: [
            {
              id: "nb-andromeda",
              name: "Andromeda 설계 노트북",
              dealRef: "projects/andromeda",
              sourceCount: 3,
              updated: 1782190313958,
            },
          ],
        };
      } else if (method === "miniapp.project.linked") {
        // The gateway resolves linkage now; this stubs its per-project ID sets so
        // the pane's job under test is purely "filter the cached lists by these IDs".
        payload =
          params?.path === "projects/deneb"
            ? { mail: ["m2"], calendar: [], todo: [], workfeed: [], notebook: [] }
            : { mail: ["m1"], calendar: ["e1"], todo: ["t1"], workfeed: ["w1"], notebook: ["nb-andromeda"] };
      }
      return { ok: true, json: async () => ({ ok: true, payload }) } as unknown as Response;
    }),
  );
});

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

function WorkspaceProbe() {
  const { view, wikiTarget } = useWorkspace();
  return <output data-testid="workspace-target">{`${view}:${wikiTarget ?? ""}`}</output>;
}

const provider = fakeProvider({
  progress: [
    {
      project: "Andromeda 워크스테이션",
      headline: "프로젝트 홈 정리 중",
      bullets: ["메일과 일정 연결", "노트북 자료 확인"],
      due: "이번 주",
      updatedAtMs: 1782180000000,
      path: "projects/andromeda",
    },
    {
      project: "데네브 게이트웨이",
      headline: "RPC 안정화",
      bullets: ["릴레이 확인"],
      path: "projects/deneb",
    },
  ],
  mail: [
    {
      id: "m1",
      subject: "Andromeda 홈 리뷰",
      from: "기획 <lead@example.com>",
      snippet: "프로젝트 홈의 관련 항목 묶음을 확인합니다.",
      date: "2026-06-23T09:00:00Z",
      isUnread: true,
      relatedProjects: [{ path: "projects/andromeda", title: "Andromeda 워크스테이션" }],
    },
    {
      id: "m2",
      subject: "게이트웨이 릴레이 확인",
      snippet: "deneb RPC 릴레이",
      date: "2026-06-22T09:00:00Z",
      relatedProjects: [{ path: "projects/deneb", title: "데네브 게이트웨이" }],
    },
    {
      id: "m3",
      subject: "Andromeda 이름만 들어간 메일",
      snippet: "프로젝트 참조는 없는 공용 장비 점검",
      date: "2026-06-21T09:00:00Z",
    },
  ],
  calendar: [
    {
      id: "e1",
      summary: "Andromeda 홈 리뷰",
      description: "프로젝트 홈 화면 점검",
      start: "2026-06-24T05:00:00Z",
      end: "2026-06-24T06:00:00Z",
      projectPath: "projects/andromeda",
    },
  ],
  todo: [
    {
      id: "t1",
      title: "Andromeda 관련 메일 정리",
      note: "프로젝트 홈 검수",
      due: "2026-06-25T00:00:00Z",
      projectPath: "projects/andromeda",
    },
  ],
  workfeed: [
    {
      id: "w1",
      source: "review",
      refId: "projects/andromeda",
      title: "Andromeda 홈 피드백",
      body: "관련 데이터 묶음 확인",
      createdAtMs: 1782190313958,
    },
    {
      id: "w2",
      source: "notice",
      title: "Andromeda 일반 점검",
      body: "공용 장비 알림",
      createdAtMs: 1782180313958,
    },
  ],
});

describe("ProjectHomePane", () => {
  it("gathers related work for the selected project", async () => {
    renderWithProviders(<ProjectHomePane />, { connected: true, dataProvider: provider });

    expect(await screen.findByRole("heading", { name: "프로젝트 홈" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Andromeda 워크스테이션" })).toBeInTheDocument();
    expect(screen.getAllByText("프로젝트 홈 정리 중").length).toBeGreaterThan(0);
    expect(screen.getByText("메일과 일정 연결")).toBeInTheDocument();
    expect(screen.getAllByText("Andromeda 홈 리뷰").length).toBeGreaterThan(0);
    expect(screen.getByText("Andromeda 관련 메일 정리")).toBeInTheDocument();
    expect(screen.getByText("Andromeda 홈 피드백")).toBeInTheDocument();
    expect(screen.queryByText("Andromeda 이름만 들어간 메일")).not.toBeInTheDocument();
    expect(screen.queryByText("Andromeda 일반 점검")).not.toBeInTheDocument();
    expect(await screen.findByText("Andromeda 설계 노트북")).toBeInTheDocument();
  });

  it("switches the home contents when another project is selected", async () => {
    renderWithProviders(<ProjectHomePane />, { connected: true, dataProvider: provider });

    await userEvent.click(await screen.findByRole("button", { name: /데네브 게이트웨이/ }));

    expect(screen.getByRole("heading", { name: "데네브 게이트웨이" })).toBeInTheDocument();
    expect(screen.getByText("게이트웨이 릴레이 확인")).toBeInTheDocument();
    expect(screen.queryByText("Andromeda 관련 메일 정리")).not.toBeInTheDocument();
  });

  it("orders the project list by most-recently-updated (newest first)", async () => {
    const p = fakeProvider({
      progress: [
        { project: "오래된 프로젝트", updatedAtMs: 1000, path: "projects/old" },
        { project: "최신 프로젝트", updatedAtMs: 9000, path: "projects/new" },
        { project: "중간 프로젝트", updatedAtMs: 5000, path: "projects/mid" },
        { project: "시간없는 프로젝트", path: "projects/none" }, // no updatedAtMs → sinks to bottom
      ],
    });
    renderWithProviders(<ProjectHomePane />, { connected: true, dataProvider: p });
    await screen.findByRole("heading", { name: "프로젝트 홈" });

    const list = screen.getByLabelText("프로젝트 목록");
    const names = within(list)
      .getAllByRole("button")
      .map((b) => b.querySelector(".project-home-project-name")?.textContent);
    expect(names).toEqual(["최신 프로젝트", "중간 프로젝트", "오래된 프로젝트", "시간없는 프로젝트"]);
  });

  it("opens the selected project's wiki page", async () => {
    renderWithProviders(
      <>
        <ProjectHomePane />
        <WorkspaceProbe />
      </>,
      { connected: true, dataProvider: provider },
    );

    await userEvent.click(await screen.findByRole("button", { name: "위키 열기" }));
    expect(screen.getByTestId("workspace-target")).toHaveTextContent("wiki:projects/andromeda");
  });
});
