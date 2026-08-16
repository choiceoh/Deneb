import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@/crud";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { useAiFeed, useWorkspace } from "@/workspaceContext";
import { TodayPane } from "./TodayPane";

function WorkspaceState() {
  const { view, paneTarget } = useWorkspace();
  const { aiText } = useAiFeed();
  return (
    <div>
      <output data-testid="view">{view}</output>
      <output data-testid="target">
        {JSON.stringify({
          view: paneTarget?.view,
          id: paneTarget?.id,
          dayKey: paneTarget?.dayKey,
        })}
      </output>
      <output data-testid="ai-text">{aiText}</output>
    </div>
  );
}

function renderToday(fixtures: Record<string, any[]> = {}, connected = true) {
  return renderWithProviders(
    <>
      <TodayPane />
      <WorkspaceState />
    </>,
    { connected, dataProvider: fakeProvider(fixtures) },
  );
}

async function openEditor() {
  await userEvent.click(screen.getByRole("button", { name: "편집" }));
  return screen.getByText("표시할 섹션과 순서, 너비를 정하세요.").parentElement as HTMLElement;
}

function setDashboardState(key: "Order" | "Hidden" | "Wide", value: unknown) {
  localStorage.setItem(`andromeda.today${key}`, JSON.stringify(value));
}

describe("TodayPane boundary behavior", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date("2026-07-11T09:30:00+09:00"));
  });

  afterEach(() => {
    localStorage.clear();
    vi.useRealTimers();
  });

  describe("connection and empty states", () => {
    it("without offer editor controls while disconnected", () => {
      renderToday({}, false);
      expect(screen.getByText("미연결")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "편집" })).not.toBeInTheDocument();
      expect(screen.queryByText("다가오는 일정 없음")).not.toBeInTheDocument();
    });

    it("renders an empty message for every visible default section", async () => {
      renderToday();
      expect(await screen.findByText("다가오는 일정 없음")).toBeInTheDocument();
      expect(screen.getByText("메일 없음")).toBeInTheDocument();
      expect(screen.getByText("할일 없음")).toBeInTheDocument();
      expect(screen.getByText("피드 비어 있음")).toBeInTheDocument();
    });

    it("shows a dedicated state when the user hides all sections", async () => {
      setDashboardState("Order", [
        "timeline",
        "calendar",
        "approvals",
        "mail",
        "todo",
        "workfeed",
        "radar",
        "people",
        "crons",
        "market",
      ]);
      setDashboardState("Hidden", [
        "timeline",
        "calendar",
        "approvals",
        "mail",
        "todo",
        "workfeed",
        "radar",
        "people",
        "crons",
        "market",
      ]);
      renderToday();
      expect(await screen.findByText("표시할 섹션이 없습니다 — 편집에서 켜세요.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "편집" })).toBeEnabled();
    });

    it("lets the editor recover a dashboard whose sections are all hidden", async () => {
      setDashboardState("Order", [
        "timeline",
        "calendar",
        "approvals",
        "mail",
        "todo",
        "workfeed",
        "radar",
        "people",
        "crons",
        "market",
      ]);
      setDashboardState("Hidden", [
        "timeline",
        "calendar",
        "approvals",
        "mail",
        "todo",
        "workfeed",
        "radar",
        "people",
        "crons",
        "market",
      ]);
      renderToday({ todo: [{ id: "recover", title: "다시 표시할 할일", done: false }] });
      await openEditor();
      await userEvent.click(screen.getByRole("checkbox", { name: "할일" }));
      expect(await screen.findByText("다시 표시할 할일")).toBeInTheDocument();
    });

    it("surfaces a provider error inside only the affected section", async () => {
      const base = fakeProvider();
      const provider: DataProvider = {
        ...base,
        getList: async ({ resource }) => {
          if (resource === "mail") throw new Error("mail gateway unavailable");
          return { data: [], total: 0 };
        },
      };
      renderWithProviders(<TodayPane />, { connected: true, dataProvider: provider });
      expect(await screen.findByText(/mail gateway unavailable/)).toBeInTheDocument();
      expect(screen.getByText("다가오는 일정 없음")).toBeInTheDocument();
    });
  });

  describe("brief ordering and filtering", () => {
    it("puts unread mail before read mail while preserving the original order within each group", async () => {
      renderToday({
        mail: [
          { id: "read-1", subject: "읽은 첫 메일", from: "a@example.com", isUnread: false },
          { id: "unread-1", subject: "안 읽은 첫 메일", from: "b@example.com", isUnread: true },
          { id: "unread-2", subject: "안 읽은 둘째 메일", from: "c@example.com", isUnread: true },
          { id: "read-2", subject: "읽은 둘째 메일", from: "d@example.com", isUnread: false },
        ],
      });
      const mailCard = (await screen.findByRole("button", { name: "메일 열기" })).closest("section")!;
      expect(
        within(mailCard)
          .getAllByRole("button")
          .map((button) => button.textContent),
      ).toEqual([
        expect.stringContaining("메일"),
        expect.stringContaining("안 읽은 첫 메일"),
        expect.stringContaining("안 읽은 둘째 메일"),
        expect.stringContaining("읽은 첫 메일"),
        expect.stringContaining("읽은 둘째 메일"),
      ]);
    });

    it("uses the isUnread wire field instead of a legacy unread alias", async () => {
      renderToday({
        mail: [
          { id: "legacy", subject: "legacy unread only", from: "a@example.com", unread: true },
          { id: "wire", subject: "wire unread", from: "b@example.com", isUnread: true },
        ],
      });
      const rows = await screen.findAllByTitle("메일에서 열기");
      expect(rows[0]).toHaveTextContent("wire unread");
      expect(rows[1]).toHaveTextContent("legacy unread only");
    });

    it("when filters completed todos before counting and overflow calculation", async () => {
      renderToday({
        todo: [
          ...Array.from({ length: 6 }, (_, i) => ({ id: `done-${i}`, title: `완료 ${i}`, done: true })),
          { id: "open", title: "열린 할일", done: false },
        ],
      });
      expect(await screen.findByText("열린 할일")).toBeInTheDocument();
      expect(screen.queryByText(/완료 0/)).not.toBeInTheDocument();
      expect(screen.queryByText(/외 \d+건/)).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "할일 열기" })).toHaveTextContent("1");
    });

    it("orders open todos by parseable due date and sinks missing or invalid dates", async () => {
      renderToday({
        todo: [
          { id: "none", title: "기한 없음", done: false },
          { id: "late", title: "늦은 기한", done: false, due: "2026-07-20T00:00:00Z" },
          { id: "bad", title: "잘못된 기한", done: false, due: "not-a-date" },
          { id: "early", title: "빠른 기한", done: false, due: "2026-07-12T00:00:00Z" },
        ],
      });
      const rows = await screen.findAllByTitle("할일에서 열기");
      expect(rows.map((row) => row.textContent)).toEqual([
        expect.stringContaining("빠른 기한"),
        expect.stringContaining("늦은 기한"),
        expect.stringContaining("기한 없음"),
        expect.stringContaining("잘못된 기한"),
      ]);
    });

    it("marks only genuinely overdue parseable todos with the accent class", async () => {
      renderToday({
        todo: [
          { id: "past", title: "기한 지남", done: false, due: "2026-07-10T00:00:00Z" },
          { id: "future", title: "기한 남음", done: false, due: "2026-07-12T00:00:00Z" },
          { id: "bad", title: "기한 불명", done: false, due: "invalid" },
        ],
      });
      expect((await screen.findByText("기한 지남", { selector: ".today-row-title" })).closest("button")).toHaveClass(
        "accent",
      );
      expect(screen.getByText("기한 남음", { selector: ".today-row-title" }).closest("button")).not.toHaveClass(
        "accent",
      );
      expect(screen.getByText("기한 불명", { selector: ".today-row-title" }).closest("button")).not.toHaveClass(
        "accent",
      );
    });

    it.each([
      [6, 0],
      [7, 1],
      [11, 5],
    ])("when caps %i workfeed entries and reports %i hidden rows", async (count, hidden) => {
      renderToday({
        workfeed: Array.from({ length: count }, (_, i) => ({ id: `w-${i}`, title: `피드 ${i}`, source: "test" })),
      });
      await screen.findByText("피드 0");
      expect(screen.getAllByTitle("피드에서 열기")).toHaveLength(Math.min(count, 6));
      if (hidden) expect(screen.getByText(`…외 ${hidden}건`)).toBeInTheDocument();
      else expect(screen.queryByText(/…외/)).not.toBeInTheDocument();
    });
  });

  describe("navigation targets", () => {
    it("opens a mail with its stable message id", async () => {
      renderToday({ mail: [{ id: "m-42", subject: "계약 검토", from: "law@example.com" }] });
      await userEvent.click(await screen.findByRole("button", { name: /계약 검토/ }));
      expect(screen.getByTestId("view")).toHaveTextContent("mail");
      expect(screen.getByTestId("target")).toHaveTextContent('"id":"m-42"');
    });

    it("opens an all-day calendar event with a normalized day target", async () => {
      renderToday({
        calendar: [{ id: "all-day", summary: "휴가", start: { date: "2026-07-13" }, end: { date: "2026-07-14" } }],
      });
      await userEvent.click(await screen.findByRole("button", { name: /휴가/ }));
      expect(screen.getByTestId("target")).toHaveTextContent('"dayKey":"2026-7-13"');
    });

    it("when opens a timed calendar event using its local calendar day", async () => {
      renderToday({
        calendar: [
          {
            id: "timed",
            summary: "자정 전 회의",
            start: { dateTime: "2026-07-11T23:30:00+09:00" },
            end: { dateTime: "2026-07-12T00:10:00+09:00" },
          },
        ],
      });
      await userEvent.click(
        (await screen.findAllByTitle("일정에서 열기")).find((el) => el.textContent?.includes("자정 전 회의"))!,
      );
      expect(screen.getByTestId("target")).toHaveTextContent('"dayKey":"2026-7-11"');
    });

    it("opens a todo and workfeed item without inventing extra target fields", async () => {
      renderToday({
        todo: [{ id: "todo-1", title: "증빙 제출", done: false }],
        workfeed: [{ id: "feed-1", title: "질문 응답", source: "question" }],
      });
      await userEvent.click(await screen.findByRole("button", { name: /증빙 제출/ }));
      expect(screen.getByTestId("target")).toHaveTextContent('{"view":"todo","id":"todo-1"}');
      await userEvent.click(screen.getByRole("button", { name: /질문 응답/ }));
      expect(screen.getByTestId("target")).toHaveTextContent('{"view":"workfeed","id":"feed-1"}');
    });

    it.each([
      ["일정 열기", "calendar"],
      ["메일 열기", "mail"],
      ["할일 열기", "todo"],
      ["피드 열기", "workfeed"],
    ])("when opens the %s card through its header", async (name, view) => {
      renderToday();
      await userEvent.click(await screen.findByRole("button", { name }));
      expect(screen.getByTestId("view")).toHaveTextContent(view);
    });
  });

  describe("customization storage hygiene", () => {
    it("drops unknown keys and duplicates from a corrupted saved order", async () => {
      setDashboardState("Order", ["mail", "unknown", "mail", "todo"]);
      renderToday();
      const editor = await openEditor();
      const labels = within(editor)
        .getAllByRole("checkbox")
        .filter((box) => box.parentElement?.classList.contains("today-editor-label"))
        .map((box) => box.parentElement?.textContent);
      expect(labels).toEqual([
        "메일",
        "할일",
        "타임라인",
        "일정",
        "결재",
        "피드",
        "마감",
        "연락처",
        "크론",
        "시장",
      ]);
    });

    it("recovers from non-array order, hidden, and wide values", async () => {
      setDashboardState("Order", { mail: 1 });
      setDashboardState("Hidden", "mail");
      setDashboardState("Wide", 42);
      renderToday({ mail: [{ id: "m", subject: "복구 확인", from: "x@example.com" }] });
      expect(await screen.findByText("복구 확인")).toBeInTheDocument();
      const editor = await openEditor();
      expect(within(editor).getByRole("checkbox", { name: "메일" })).toBeChecked();
      // 타임라인 카드만 상시 wide — 저장된 wide 값(불량 42)은 무시된다.
      const wides = [...document.querySelectorAll(".today-card.wide")];
      expect(wides).toHaveLength(1);
      expect(wides[0].textContent).toContain("타임라인");
    });

    it("keeps newly introduced optional sections hidden for old saved orders", async () => {
      setDashboardState("Order", ["calendar", "mail", "todo", "workfeed"]);
      renderToday({ people: [{ email: "lead@example.com", name: "숨겨진 연락처" }] });
      expect(screen.queryByText("숨겨진 연락처")).not.toBeInTheDocument();
      const editor = await openEditor();
      expect(within(editor).getByRole("checkbox", { name: "연락처" })).not.toBeChecked();
    });

    it("restores a hidden section and persists a unique hidden list", async () => {
      setDashboardState("Order", [
        "timeline",
        "calendar",
        "approvals",
        "mail",
        "todo",
        "workfeed",
        "radar",
        "people",
        "crons",
        "market",
      ]);
      setDashboardState("Hidden", ["mail", "mail", "market"]);
      renderToday({ mail: [{ id: "m", subject: "복원한 메일", from: "x@example.com" }] });
      const editor = await openEditor();
      await userEvent.click(within(editor).getByRole("checkbox", { name: "메일" }));
      expect(await screen.findByText("복원한 메일")).toBeInTheDocument();
      expect(JSON.parse(localStorage.getItem("andromeda.todayHidden") ?? "[]")).toEqual(["market"]);
    });

    it("moves a section by one position without losing catalog members", async () => {
      renderToday();
      const editor = await openEditor();
      await userEvent.click(within(editor).getByRole("button", { name: "메일 위로" }));
      const stored = JSON.parse(localStorage.getItem("andromeda.todayOrder") ?? "[]") as string[];
      expect(stored.slice(0, 4)).toEqual(["timeline", "calendar", "mail", "approvals"]);
      expect(new Set(stored).size).toBe(10);
    });

    it("when disables moves beyond both order boundaries", async () => {
      renderToday();
      const editor = await openEditor();
      expect(within(editor).getByRole("button", { name: "타임라인 위로" })).toBeDisabled();
      expect(within(editor).getByRole("button", { name: "시장 아래로" })).toBeDisabled();
      expect(within(editor).getByRole("button", { name: "타임라인 아래로" })).toBeEnabled();
      expect(within(editor).getByRole("button", { name: "시장 위로" })).toBeEnabled();
    });

    it("saves wide cards independently from visibility", async () => {
      renderToday({ mail: [{ id: "m", subject: "넓은 메일", from: "x@example.com" }] });
      const editor = await openEditor();
      const mailRow = within(editor).getByRole("checkbox", { name: "메일" }).closest(".today-editor-row")!;
      await userEvent.click(within(mailRow as HTMLElement).getByRole("checkbox", { name: "넓게" }));
      expect(screen.getByText("넓은 메일").closest("section")).toHaveClass("wide");
      expect(JSON.parse(localStorage.getItem("andromeda.todayWide") ?? "[]")).toContain("mail");

      await userEvent.click(within(mailRow as HTMLElement).getByRole("checkbox", { name: "메일" }));
      expect(screen.queryByText("넓은 메일")).not.toBeInTheDocument();
      expect(JSON.parse(localStorage.getItem("andromeda.todayWide") ?? "[]")).toContain("mail");
    });

    it("closes the editor without changing the selected customization", async () => {
      renderToday();
      await openEditor();
      const editButton = screen.getByRole("button", { name: "완료" });
      expect(editButton).toHaveAttribute("aria-pressed", "true");
      await userEvent.click(editButton);
      expect(screen.queryByText("표시할 섹션과 순서, 너비를 정하세요.")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "편집" })).toHaveAttribute("aria-pressed", "false");
    });
  });

  describe("optional section semantics", () => {
    it("when uses person name and latest subject while falling back to email and wiki summary", async () => {
      setDashboardState("Order", ["people"]);
      setDashboardState("Hidden", []);
      renderToday({
        people: [
          { email: "lead@example.com", name: "김리드", lastSubject: "계약 검토" },
          { email: "unknown@example.com", wikiSummary: "외부 자문" },
        ],
      });
      expect(await screen.findByText("김리드")).toBeInTheDocument();
      expect(screen.getByText("계약 검토")).toBeInTheDocument();
      expect(screen.getByText("unknown@example.com")).toBeInTheDocument();
      expect(screen.getByText("외부 자문")).toBeInTheDocument();
    });

    it("uses stable fallbacks for nameless cron jobs", async () => {
      setDashboardState("Order", ["crons"]);
      setDashboardState("Hidden", []);
      renderToday({ crons: [{ id: "cron-1", schedule: "매일 08:00" }] });
      expect(await screen.findByText("(작업)")).toBeInTheDocument();
      expect(screen.getByText("매일 08:00")).toBeInTheDocument();
    });

    it.each([
      [101.25, 1.236, "101.25", "▲ 1.24%", "up"],
      [95, -0.004, "95", "▼ 0.00%", "down"],
      [0, 0, "0", "· 0.00%", "flat"],
    ])("formats market quote %i / %i", async (price, changePct, shownPrice, change, tone) => {
      setDashboardState("Order", ["market"]);
      setDashboardState("Hidden", []);
      renderToday({ market: [{ symbol: "TEST", label: "테스트 지수", price, changePct }] });
      const tile = (await screen.findByText("테스트 지수")).closest(".market-tile") as HTMLElement;
      expect(within(tile).getByText(shownPrice)).toBeInTheDocument();
      expect(within(tile).getByText(change)).toBeInTheDocument();
      expect(tile).toHaveClass(tone);
    });

    it("preserves market tiles non-interactive because no destination pane exists", async () => {
      setDashboardState("Order", ["market"]);
      setDashboardState("Hidden", []);
      renderToday({ market: [{ symbol: "USD", label: "원/달러", price: 1_400, changePct: 0.1 }] });
      expect(await screen.findByText("원/달러")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /시장/ })).not.toBeInTheDocument();
      expect(screen.getByTestId("view")).toHaveTextContent("today");
    });
  });

  describe("AI context parity", () => {
    it("when projects only visible sections and the same capped rows shown on screen", async () => {
      setDashboardState("Order", ["mail", "todo"]);
      setDashboardState("Hidden", ["todo"]);
      renderToday({
        mail: Array.from({ length: 8 }, (_, i) => ({
          id: `m-${i}`,
          subject: `메일 ${i}`,
          from: `sender${i}@example.com`,
        })),
        todo: [{ id: "secret", title: "숨긴 할일", done: false }],
      });
      await screen.findByText("메일 0");
      const ai = screen.getByTestId("ai-text");
      expect(ai).toHaveTextContent("[오늘 브리핑]");
      expect(ai).toHaveTextContent("[메일 8건]");
      expect(ai).toHaveTextContent("메일 5");
      expect(ai).not.toHaveTextContent("메일 6");
      expect(ai).toHaveTextContent("…외 2건");
      expect(ai).not.toHaveTextContent("숨긴 할일");
    });

    it("marks unread mail in the AI projection as well as visually", async () => {
      renderToday({ mail: [{ id: "u", subject: "긴급 승인", from: "boss@example.com", isUnread: true }] });
      await screen.findByText("긴급 승인");
      expect(screen.getByTestId("ai-text")).toHaveTextContent("● 긴급 승인");
      expect(document.querySelectorAll(".today-dot")).toHaveLength(1);
    });

    it("clears the AI projection when every dashboard section is hidden", async () => {
      setDashboardState("Hidden", [
        "timeline",
        "calendar",
        "approvals",
        "mail",
        "todo",
        "workfeed",
        "radar",
        "people",
        "crons",
        "market",
      ]);
      renderToday({ mail: [{ id: "m", subject: "숨긴 메일", from: "x@example.com" }] });
      expect(await screen.findByText(/표시할 섹션이 없습니다/)).toBeInTheDocument();
      // 섹션 본문은 비지만 KPI 지표 줄은 남는다 — 스트립이 화면에도 계속 떠 있다.
      expect(screen.getByTestId("ai-text").textContent).toContain("오늘 지표");
      expect(screen.getByTestId("ai-text").textContent).not.toContain("숨긴 메일");
    });
  });
});
