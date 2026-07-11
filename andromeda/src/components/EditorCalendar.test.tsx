import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ErrorBoundary } from "./ErrorBoundary";
import { MarkdownEditor } from "./MarkdownEditor";
import { MonthGrid } from "./MonthGrid";
import { dayKey, monthLabel } from "@/format";
import type { CalEvent } from "@/types";

describe("MarkdownEditor", () => {
  it("renders an editable textarea by default", () => {
    render(<MarkdownEditor value="초안" onChange={() => {}} preview={false} ariaLabel="문서 본문" />);

    const editor = screen.getByRole("textbox");
    expect(editor).toHaveValue("초안");
    expect(editor).toHaveClass("field");
    expect(editor).toHaveStyle({ width: "100%", height: "70vh", resize: "none" });
  });

  it("reports each user edit as the complete next value", async () => {
    const onChange = vi.fn();
    render(<MarkdownEditor value="" onChange={onChange} preview={false} />);

    await userEvent.type(screen.getByRole("textbox"), "문서");

    expect(onChange.mock.calls.map(([value]) => value)).toEqual(["문", "서"]);
  });

  it("forwards disabled and placeholder state", () => {
    render(
      <MarkdownEditor value="" onChange={() => {}} preview={false} disabled placeholder="Markdown을 입력하세요" />,
    );

    expect(screen.getByRole("textbox")).toBeDisabled();
    expect(screen.getByPlaceholderText("Markdown을 입력하세요")).toBeInTheDocument();
  });

  it("uses flex-fill sizing for embedded wiki editors", () => {
    render(<MarkdownEditor value="wiki" onChange={() => {}} preview={false} fill />);

    expect(screen.getByRole("textbox")).toHaveStyle({ flex: "1", minHeight: "0" });
    expect(screen.getByRole("textbox")).not.toHaveStyle({ height: "70vh" });
  });

  it("renders Markdown instead of a textarea in preview mode", () => {
    const { container } = render(
      <MarkdownEditor value={"# 제목\n\n**강조**"} onChange={() => {}} preview ariaLabel="문서 미리보기" />,
    );

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "제목" })).toBeInTheDocument();
    expect(screen.getByText("강조").tagName).toBe("STRONG");
    expect(container.querySelector(".md-surface")).toHaveAttribute("aria-label", "문서 미리보기");
  });

  it("keeps fixed preview sizing outside a fill container", () => {
    const { container } = render(<MarkdownEditor value="preview" onChange={() => {}} preview />);
    expect(container.querySelector(".md-surface")).toHaveStyle({ width: "100%", height: "70vh", overflow: "auto" });
  });

  it("keeps fill preview sizing inside a flex container", () => {
    const { container } = render(<MarkdownEditor value="preview" onChange={() => {}} preview fill />);
    expect(container.querySelector(".md-surface")).toHaveStyle({ flex: "1", minHeight: "0", overflow: "auto" });
  });
});

function event(overrides: Partial<CalEvent> = {}): CalEvent {
  return {
    id: "event-1",
    summary: "일정",
    start: "2025-06-12T09:00:00+09:00",
    end: "2025-06-12T10:00:00+09:00",
    ...overrides,
  };
}

function renderMonth(overrides: Partial<React.ComponentProps<typeof MonthGrid>> = {}) {
  const props: React.ComponentProps<typeof MonthGrid> = {
    year: 2025,
    month0: 5,
    eventsByDay: new Map(),
    todayKey: dayKey(new Date(2025, 5, 11)),
    selectedKey: null,
    onSelectDay: vi.fn(),
    onPrev: vi.fn(),
    onNext: vi.fn(),
    onToday: vi.fn(),
    ...overrides,
  };
  const view = render(<MonthGrid {...props} />);
  return { props, ...view };
}

describe("MonthGrid", () => {
  it("renders a Sunday-first weekday header", () => {
    const { container } = renderMonth();
    expect(Array.from(container.querySelectorAll(".cal-dow"), (node) => node.textContent)).toEqual([
      "일",
      "월",
      "화",
      "수",
      "목",
      "금",
      "토",
    ]);
    expect(container.querySelector(".cal-dow")).toHaveStyle({ color: "var(--due)" });
  });

  it("labels the current month and all in-month days", () => {
    renderMonth();

    expect(screen.getByText(monthLabel(2025, 5))).toBeInTheDocument();
    const dayButtons = screen.getAllByRole("button").filter((button) => button.classList.contains("cal-cell"));
    expect(dayButtons).toHaveLength(30);
    expect(screen.getByRole("button", { name: "6월 1일" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "6월 30일" })).toBeInTheDocument();
  });

  it("renders context days as inert cells", () => {
    const { container } = renderMonth({ year: 2025, month0: 6 });
    const out = container.querySelectorAll(".cal-cell.out");
    expect(out.length).toBeGreaterThan(0);
    for (const cell of out) {
      expect(cell).not.toHaveAttribute("role");
      expect(cell).not.toHaveAttribute("tabindex");
      expect(cell).not.toHaveAttribute("aria-label");
    }
  });

  it("marks today without implicitly selecting it", () => {
    const { container } = renderMonth({ todayKey: dayKey(new Date(2025, 5, 11)) });
    const today = Array.from(container.querySelectorAll(".cal-cell")).find(
      (cell) => cell.getAttribute("aria-label") === "6월 11일",
    );
    expect(today).toHaveClass("cal-today");
    expect(today).not.toHaveClass("cal-selected");
    expect(today).toHaveAttribute("aria-pressed", "false");
  });

  it("marks the selected in-month day accessibly", () => {
    renderMonth({ selectedKey: dayKey(new Date(2025, 5, 19)) });
    const selected = screen.getByRole("button", { name: "6월 19일" });
    expect(selected).toHaveClass("cal-selected");
    expect(selected).toHaveAttribute("aria-pressed", "true");
  });

  it("selects a day with click, Enter, and Space", async () => {
    const onSelectDay = vi.fn();
    renderMonth({ onSelectDay });
    const target = screen.getByRole("button", { name: "6월 18일" });

    await userEvent.click(target);
    target.focus();
    await userEvent.keyboard("{Enter}");
    await userEvent.keyboard(" ");

    expect(onSelectDay).toHaveBeenCalledTimes(3);
    const expected = dayKey(new Date(2025, 5, 18));
    expect(onSelectDay).toHaveBeenNthCalledWith(1, expected);
    expect(onSelectDay).toHaveBeenNthCalledWith(2, expected);
    expect(onSelectDay).toHaveBeenNthCalledWith(3, expected);
  });

  it("ignores unrelated keys on a selectable day", async () => {
    const onSelectDay = vi.fn();
    renderMonth({ onSelectDay });
    screen.getByRole("button", { name: "6월 18일" }).focus();

    await userEvent.keyboard("{ArrowRight}a{Escape}");

    expect(onSelectDay).not.toHaveBeenCalled();
  });

  it("handles month and today navigation", async () => {
    const { props } = renderMonth();

    await userEvent.click(screen.getByRole("button", { name: "이전 달" }));
    await userEvent.click(screen.getByRole("button", { name: "다음 달" }));
    await userEvent.click(screen.getByRole("button", { name: "오늘" }));

    expect(props.onPrev).toHaveBeenCalledTimes(1);
    expect(props.onNext).toHaveBeenCalledTimes(1);
    expect(props.onToday).toHaveBeenCalledTimes(1);
  });

  it("announces the number of events on a day", () => {
    const eventsByDay = new Map([[dayKey(new Date(2025, 5, 12)), [event(), event({ id: "event-2" })]]]);
    renderMonth({ eventsByDay });

    const day = screen.getByRole("button", { name: "6월 12일, 일정 2건" });
    expect(day.querySelector(".cal-markers")).toHaveAttribute("title", "일정 2건");
    expect(day.querySelectorAll(".cal-marker")).toHaveLength(2);
  });

  it("caps visible markers at three and adds an overflow marker", () => {
    const eventsByDay = new Map([
      [
        dayKey(new Date(2025, 5, 12)),
        [event(), event({ id: "event-2" }), event({ id: "event-3" }), event({ id: "event-4" })],
      ],
    ]);
    renderMonth({ eventsByDay });

    const day = screen.getByRole("button", { name: "6월 12일, 일정 4건" });
    expect(day.querySelectorAll(".cal-marker")).toHaveLength(4);
    expect(day.querySelectorAll(".cal-marker.overflow")).toHaveLength(1);
  });

  it.each([
    [event(), "cal-marker dot mine"],
    [event({ category: "others" }), "cal-marker dot others"],
    [event({ category: "deadline" }), "cal-marker dot deadline"],
    [event({ allDay: true, start: { date: "2025-06-12" }, end: { date: "2025-06-13" } }), "cal-marker line mine"],
    [event({ start: "2025-06-12T09:00:00+09:00", end: "2025-06-14T10:00:00+09:00" }), "cal-marker line mine"],
  ])("assigns semantic marker classes", (calendarEvent, expected) => {
    const eventsByDay = new Map([[dayKey(new Date(2025, 5, 12)), [calendarEvent]]]);
    const { container } = renderMonth({ eventsByDay });
    expect(container.querySelector(".cal-markers .cal-marker")).toHaveClass(...expected.split(" "));
  });

  it("does not make an out-of-month context day selectable even if it has events", () => {
    const context = new Date(2025, 6, 1);
    const eventsByDay = new Map([[dayKey(context), [event({ start: "2025-07-01T09:00:00+09:00" })]]]);
    const onSelectDay = vi.fn();
    const { container } = renderMonth({ eventsByDay, onSelectDay });
    const out = Array.from(container.querySelectorAll(".cal-cell.out")).find(
      (cell) => cell.querySelector(".cal-daynum")?.textContent === "1",
    );
    expect(out).not.toHaveAttribute("role");
    expect(out?.querySelector(".cal-marker")).toBeInTheDocument();
  });
});

describe("ErrorBoundary", () => {
  function Broken({ message = "render exploded" }: { message?: string }): never {
    throw new Error(message);
  }

  it("passes healthy children through unchanged", () => {
    render(
      <ErrorBoundary>
        <button>작동 중</button>
      </ErrorBoundary>,
    );
    expect(screen.getByRole("button", { name: "작동 중" })).toBeInTheDocument();
  });

  it("replaces a crashing subtree with a readable recovery surface", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <ErrorBoundary>
        <Broken message="pane failed" />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("heading", { name: "문제가 발생했습니다" })).toBeInTheDocument();
    expect(screen.getByText("pane failed")).toBeInTheDocument();
    expect(screen.getByText(/Error: pane failed/).tagName).toBe("PRE");
    expect(screen.getByRole("button", { name: "다시 시도" })).toBeInTheDocument();
    consoleError.mockRestore();
  });

  it("derives error state deterministically", () => {
    const error = new TypeError("bad pane");
    expect(ErrorBoundary.getDerivedStateFromError(error)).toEqual({ error });
  });

  it("logs the captured error and component stack", () => {
    const boundary = new ErrorBoundary({ children: null });
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    boundary.componentDidCatch(new Error("broken"), { componentStack: "\n at Broken" });

    expect(consoleError).toHaveBeenCalledWith("[andromeda:ui]", "render error:", "broken", "\n at Broken");
  });

  it("tolerates a missing component stack while logging", () => {
    const boundary = new ErrorBoundary({ children: null });
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    boundary.componentDidCatch(new Error("broken"), {});

    expect(consoleError).toHaveBeenCalledWith("[andromeda:ui]", "render error:", "broken", "");
  });
});
