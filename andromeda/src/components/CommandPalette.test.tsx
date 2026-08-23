import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mocks = vi.hoisted(() => ({ callRpc: vi.fn() }));
vi.mock("@/gateway", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/gateway")>();
  return { ...actual, callRpc: mocks.callRpc };
});

import { CommandPalette } from "./CommandPalette";
import { workspaceValue, WorkspaceStub } from "@/test/workspace";

function renderPalette(overrides: Parameters<typeof workspaceValue>[0] = {}) {
  const value = workspaceValue(overrides);
  render(
    <WorkspaceStub value={value}>
      <CommandPalette />
    </WorkspaceStub>,
  );
  return value;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.callRpc.mockResolvedValue({ results: [] });
});

describe("CommandPalette", () => {
  it("lists pane navigation rows on open and runs one on click", async () => {
    const value = renderPalette();
    await userEvent.click(await screen.findByText("메일"));
    expect(value.setView).toHaveBeenCalledWith("mail");
    expect(value.setPaletteOpen).toHaveBeenCalledWith(false);
  });

  it("filters rows by query and splits a pane", async () => {
    const value = renderPalette();
    await userEvent.type(screen.getByRole("textbox", { name: "명령 입력" }), "일정 분할");
    await userEvent.click(await screen.findByText("일정 분할로 열기"));
    expect(value.splitPane).toHaveBeenCalledWith("calendar");
  });

  it("offers close rows only while split", async () => {
    renderPalette();
    expect(screen.queryByText(/분할 닫기/)).not.toBeInTheDocument();

    const value = renderPalette({ tiles: ["today", "mail"] });
    await userEvent.click(await screen.findByText("메일 분할 닫기"));
    expect(value.closePane).toHaveBeenCalledWith("mail");
  });

  it("saves the current split as a named layout from the query", async () => {
    const value = renderPalette({ tiles: ["mail", "calendar"] });
    await userEvent.type(screen.getByRole("textbox", { name: "명령 입력" }), "아침 루틴");
    await userEvent.click(await screen.findByText("현재 분할을 '아침 루틴' 레이아웃으로 저장"));
    expect(value.saveLayout).toHaveBeenCalledWith("아침 루틴");
  });

  it("applies and deletes saved layouts", async () => {
    const value = renderPalette({ layouts: [{ name: "결재 모드", views: ["approvals", "mail"] }] });
    await userEvent.click(await screen.findByText("레이아웃: 결재 모드"));
    expect(value.applyLayout).toHaveBeenCalledWith(["approvals", "mail"]);

    const again = renderPalette({ layouts: [{ name: "결재 모드", views: ["approvals", "mail"] }] });
    await userEvent.click((await screen.findAllByRole("button", { name: "삭제" })).at(-1)!);
    expect(again.deleteLayout).toHaveBeenCalledWith("결재 모드");
    expect(again.applyLayout).not.toHaveBeenCalled();
  });

  it("quick-opens a wiki hit from memory.search", async () => {
    mocks.callRpc.mockResolvedValue({ results: [{ path: "프로젝트/데네브.md", title: "데네브" }] });
    const value = renderPalette();
    await userEvent.type(screen.getByRole("textbox", { name: "명령 입력" }), "데네브");
    await waitFor(() => expect(mocks.callRpc).toHaveBeenCalled());
    await userEvent.click(await screen.findByText("데네브", { selector: ".cmdk-label" }));
    expect(value.openWiki).toHaveBeenCalledWith("프로젝트/데네브.md");
  });

  it("does not quick-open synthetic fact paths as wiki pages", async () => {
    mocks.callRpc.mockResolvedValue({
      results: [
        {
          path: "@facts/fact-123.md",
          title: "Synthetic fact",
          resultKind: "fact",
          readOnly: true,
          factId: "fact-123",
        },
      ],
    });
    const value = renderPalette();
    await userEvent.type(screen.getByRole("textbox", { name: "명령 입력" }), "alpha");
    await waitFor(() => expect(mocks.callRpc).toHaveBeenCalled());
    expect(screen.queryByText("Synthetic fact", { selector: ".cmdk-label" })).not.toBeInTheDocument();
    expect(value.openWiki).not.toHaveBeenCalled();
  });

  it("hands a query off to 통합 검색 and to Deneb", async () => {
    const value = renderPalette();
    await userEvent.type(screen.getByRole("textbox", { name: "명령 입력" }), "면허 대여");
    await userEvent.click(await screen.findByText("통합 검색: 면허 대여"));
    expect(value.openPane).toHaveBeenCalledWith("search", { query: "면허 대여" });

    const again = renderPalette();
    await userEvent.type(screen.getAllByRole("textbox", { name: "명령 입력" }).at(-1)!, "면허 대여");
    await userEvent.click((await screen.findAllByText("데네브에게 묻기: 면허 대여")).at(-1)!);
    expect(again.setAiCollapsed).toHaveBeenCalledWith(false);
    expect(again.askDeneb).toHaveBeenCalledWith("면허 대여");
  });

  it("모닝 브리핑 투어: opens today locally, uncollapses the AI panel, then asks Deneb", async () => {
    const value = renderPalette();
    await userEvent.type(screen.getByRole("textbox", { name: "명령 입력" }), "모닝");
    await userEvent.click(await screen.findByText("모닝 브리핑 투어"));
    // 즉시 반응: 1단계 레이아웃은 에이전트 왕복 없이 로컬에서 바로 연다.
    expect(value.applyLayout).toHaveBeenCalledWith(["today"]);
    // 브리핑이 접힌 패널 속으로 흘러가 "안 되는" 것처럼 보이지 않게 편다.
    expect(value.setAiCollapsed).toHaveBeenCalledWith(false);
    expect(value.askDeneb).toHaveBeenCalledWith(expect.stringContaining("모닝 브리핑 투어"));
  });

  it("closes on Escape and runs the active row on Enter", async () => {
    const value = renderPalette();
    const input = screen.getByRole("textbox", { name: "명령 입력" });
    await userEvent.type(input, "{Escape}");
    expect(value.setPaletteOpen).toHaveBeenCalledWith(false);

    const again = renderPalette();
    const box = screen.getAllByRole("textbox", { name: "명령 입력" }).at(-1)!;
    await userEvent.type(box, "위키");
    await userEvent.type(box, "{Enter}");
    expect(again.setView).toHaveBeenCalledWith("wiki");
  });
});
