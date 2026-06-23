import { afterEach, describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TodayPane } from "./TodayPane";
import { cachedListStorageKey } from "@/cachedList";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { useWorkspace } from "@/workspaceContext";

afterEach(() => {
  localStorage.clear();
});

function WorkspaceProbe() {
  const { paneTarget, view } = useWorkspace();
  return (
    <output data-testid="workspace-target">
      {view}:{paneTarget?.view ?? ""}:{paneTarget?.id ?? ""}:{paneTarget?.dayKey ?? ""}
    </output>
  );
}

describe("TodayPane (오늘 대시보드)", () => {
  it("aggregates each resource into a section card", async () => {
    const dataProvider = fakeProvider({
      calendar: [{ id: "c1", title: "스탠드업", start: { dateTime: "2026-06-18T09:00:00" } }],
      mail: [{ id: "m1", subject: "예산 검토", from: "kim@corp.com", unread: true }],
      todo: [
        { id: 1, title: "보고서 초안", done: false },
        { id: 2, title: "완료된 일", done: true },
      ],
      workfeed: [{ id: "w1", title: "일정 충돌 감지", kind: "alert" }],
    });
    renderWithProviders(<TodayPane />, { connected: true, dataProvider });

    expect(await screen.findByText(/스탠드업/)).toBeInTheDocument();
    expect(await screen.findByText(/예산 검토/)).toBeInTheDocument();
    expect(await screen.findByText(/보고서 초안/)).toBeInTheDocument();
    expect(await screen.findByText(/일정 충돌 감지/)).toBeInTheDocument();
    // Completed todos are filtered out of the open-todo card.
    expect(screen.queryByText(/완료된 일/)).not.toBeInTheDocument();
  });

  it("caps a section at six rows and notes the overflow", async () => {
    const many = Array.from({ length: 9 }, (_, i) => ({ id: i, title: `할일 ${i}`, done: false }));
    renderWithProviders(<TodayPane />, { connected: true, dataProvider: fakeProvider({ todo: many }) });
    // 9 open todos, capped at 6 → an "외 3건" overflow note.
    expect(await screen.findByText(/외 3건/)).toBeInTheDocument();
  });

  it("opens individual briefing rows with pane targets", async () => {
    const dataProvider = fakeProvider({
      calendar: [{ id: "c1", title: "스탠드업", start: { dateTime: "2026-06-18T09:00:00" } }],
      mail: [{ id: "m1", subject: "예산 검토", from: "kim@corp.com" }],
      todo: [{ id: "t1", title: "보고서 초안", done: false }],
      workfeed: [{ id: "w1", title: "일정 충돌 감지", source: "alert" }],
    });
    renderWithProviders(
      <>
        <TodayPane />
        <WorkspaceProbe />
      </>,
      { connected: true, dataProvider },
    );

    await userEvent.click(await screen.findByRole("button", { name: /예산 검토/ }));
    expect(screen.getByTestId("workspace-target")).toHaveTextContent("mail:mail:m1:");

    await userEvent.click(screen.getByRole("button", { name: /스탠드업/ }));
    expect(screen.getByTestId("workspace-target")).toHaveTextContent("calendar:calendar:c1:2026-6-18");

    await userEvent.click(screen.getByRole("button", { name: /보고서 초안/ }));
    expect(screen.getByTestId("workspace-target")).toHaveTextContent("todo:todo:t1:");
  });

  it("renders cached todo and workfeed sections while the gateway refresh is still pending", () => {
    localStorage.setItem(
      cachedListStorageKey("todo"),
      JSON.stringify({
        data: [{ id: "cached-todo", title: "캐시된 할일", done: false }],
        total: 1,
        savedAt: Date.now() - 120_000,
      }),
    );
    localStorage.setItem(
      cachedListStorageKey("workfeed"),
      JSON.stringify({
        data: [{ id: "cached-work", title: "캐시된 작업피드", source: "cache" }],
        total: 1,
        savedAt: Date.now() - 120_000,
      }),
    );
    const dataProvider = {
      ...fakeProvider(),
      getList: async () => new Promise<never>(() => {}),
    };

    renderWithProviders(<TodayPane />, { connected: true, dataProvider });

    expect(screen.getByText(/캐시된 할일/)).toBeInTheDocument();
    expect(screen.getByText(/캐시된 작업피드/)).toBeInTheDocument();
  });

  it("shows a single connect notice when disconnected", () => {
    renderWithProviders(<TodayPane />, { connected: false });
    expect(screen.getAllByText(/미연결/)).toHaveLength(1);
  });

  it("hides a section via the inline editor and persists the choice", async () => {
    const dataProvider = fakeProvider({
      calendar: [{ id: "c1", title: "스탠드업", start: { dateTime: "2026-06-18T09:00:00" } }],
      mail: [{ id: "m1", subject: "예산 검토", from: "kim@corp.com" }],
    });
    renderWithProviders(<TodayPane />, { connected: true, dataProvider });
    expect(await screen.findByText(/예산 검토/)).toBeInTheDocument(); // mail section visible by default

    // Open the inline editor and uncheck 메일.
    await userEvent.click(screen.getByRole("button", { name: "편집" }));
    const mailToggle = screen.getByRole("checkbox", { name: "메일" });
    expect(mailToggle).toBeChecked();
    await userEvent.click(mailToggle);

    // The 메일 section drops out of the dashboard, and the choice persists.
    expect(screen.queryByText(/예산 검토/)).not.toBeInTheDocument();
    expect(JSON.parse(localStorage.getItem("andromeda.todayHidden") ?? "[]")).toContain("mail");
  });
});
