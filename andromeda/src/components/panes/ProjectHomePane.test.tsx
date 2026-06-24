import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
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
      const { method } = JSON.parse(String(init?.body ?? "{}")) as { method: string };
      const payload =
        method === "miniapp.notebook.list"
          ? {
              notebooks: [
                {
                  id: "nb-andromeda",
                  name: "Andromeda 설계 노트북",
                  dealRef: "projects/andromeda",
                  sourceCount: 3,
                  updated: 1782190313958,
                },
              ],
            }
          : {};
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
    },
    { id: "m2", subject: "게이트웨이 릴레이 확인", snippet: "deneb RPC 릴레이", date: "2026-06-22T09:00:00Z" },
  ],
  calendar: [
    {
      id: "e1",
      summary: "Andromeda 홈 리뷰",
      description: "프로젝트 홈 화면 점검",
      start: "2026-06-24T05:00:00Z",
      end: "2026-06-24T06:00:00Z",
    },
  ],
  todo: [{ id: "t1", title: "Andromeda 관련 메일 정리", note: "프로젝트 홈 검수", due: "2026-06-25T00:00:00Z" }],
  workfeed: [
    {
      id: "w1",
      source: "review",
      title: "Andromeda 홈 피드백",
      body: "관련 데이터 묶음 확인",
      createdAtMs: 1782190313958,
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
    expect(await screen.findByText("Andromeda 설계 노트북")).toBeInTheDocument();
  });

  it("switches the home contents when another project is selected", async () => {
    renderWithProviders(<ProjectHomePane />, { connected: true, dataProvider: provider });

    await userEvent.click(await screen.findByRole("button", { name: /데네브 게이트웨이/ }));

    expect(screen.getByRole("heading", { name: "데네브 게이트웨이" })).toBeInTheDocument();
    expect(screen.getByText("게이트웨이 릴레이 확인")).toBeInTheDocument();
    expect(screen.queryByText("Andromeda 관련 메일 정리")).not.toBeInTheDocument();
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
