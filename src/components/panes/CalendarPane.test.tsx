import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@refinedev/core";
import { CalendarPane } from "./CalendarPane";
import { fakeProvider, renderWithProviders } from "@/test/util";

// June 2026 events: one timed, one all-day local (deletable). Only Date is faked so
// the visible month defaults to June (and userEvent's setTimeout stays real).
const events = [
  { id: "e1", summary: "기획 리뷰", start: "2026-06-18T05:00:00Z", end: "2026-06-18T06:00:00Z" },
  { id: "e2", summary: "연차", start: { date: "2026-06-22" }, end: { date: "2026-06-23" }, allDay: true, local: true },
];

// fakeProvider + a create sink so tests pin the exact wire params the form sends
// (the calendar create shape is best-effort vs the live gateway).
function capturing(fixtures: Record<string, unknown[]>, sink: Record<string, unknown>[]): DataProvider {
  const base = fakeProvider(fixtures);
  return {
    ...base,
    create: async (a) => {
      sink.push({ ...a });
      return base.create(a);
    },
  };
}

describe("CalendarPane (일정 달력)", () => {
  beforeEach(() => {
    // useCachedList persists to localStorage with a 60s staleTime; with Date frozen
    // the cache always reads "fresh", so clear it per test to fetch each fixture anew.
    localStorage.clear();
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-06-15T09:00:00Z"));
  });
  afterEach(() => vi.useRealTimers());

  it("renders a month calendar with weekday headers and the month's events", async () => {
    renderWithProviders(<CalendarPane />, {
      connected: true,
      dataProvider: fakeProvider({ "calendar-range": events }),
    });
    // weekday header row (한국어)
    expect(screen.getByText("일")).toBeInTheDocument();
    expect(screen.getByText("토")).toBeInTheDocument();
    // events show up (a chip in the grid + a row in the list → at least one match each)
    expect((await screen.findAllByText(/기획 리뷰/)).length).toBeGreaterThan(0);
    expect((await screen.findAllByText(/연차/)).length).toBeGreaterThan(0);
  });

  it("keeps the visible-range list with a delete action for local events", async () => {
    renderWithProviders(<CalendarPane />, {
      connected: true,
      dataProvider: fakeProvider({ "calendar-range": events }),
    });
    // only the local event (연차) is deletable; the Google-sourced one is read-only
    expect(await screen.findByTitle("삭제")).toBeInTheDocument();
  });

  it("hides already-ended events from the current month's upcoming list", async () => {
    const mixed = [
      { id: "past", summary: "지난 회의", start: "2026-06-10T05:00:00Z", end: "2026-06-10T06:00:00Z" },
      { id: "soon", summary: "앞으로 회의", start: "2026-06-20T05:00:00Z", end: "2026-06-20T06:00:00Z" },
    ];
    renderWithProviders(<CalendarPane />, {
      connected: true,
      dataProvider: fakeProvider({ "calendar-range": mixed }),
    });
    const table = await screen.findByRole("table");
    expect(within(table).getByText("앞으로 회의")).toBeInTheDocument();
    // June 10 ended before the frozen "now" (June 15) → dropped from the list,
    // though it still appears as a chip in the grid above.
    expect(within(table).queryByText("지난 회의")).not.toBeInTheDocument();
  });

  it("shows a connect notice and no calendar when disconnected", () => {
    renderWithProviders(<CalendarPane />, { connected: false });
    expect(screen.getByText(/게이트웨이에 연결하면/)).toBeInTheDocument();
    // weekday headers only exist when the grid renders (connected)
    expect(screen.queryByText("일")).not.toBeInTheDocument();
  });

  it("creates an event through the form", async () => {
    const calls: Record<string, unknown>[] = [];
    renderWithProviders(<CalendarPane />, {
      connected: true,
      dataProvider: capturing({ "calendar-range": [] }, calls),
    });

    await userEvent.click(screen.getByRole("button", { name: "새 일정" }));
    await userEvent.type(await screen.findByLabelText("제목"), "신규 미팅");
    fireEvent.change(screen.getByLabelText("시작"), { target: { value: "2026-07-01T10:00" } });
    fireEvent.change(screen.getByLabelText("종료"), { target: { value: "2026-07-01T11:00" } });
    await userEvent.type(screen.getByLabelText("장소"), "회의실 B");
    await userEvent.click(screen.getByRole("button", { name: "저장" }));

    const created = calls.find((c) => c.resource === "calendar");
    expect(created).toBeTruthy();
    const v = created?.variables as Record<string, unknown>;
    expect(v.summary).toBe("신규 미팅");
    expect(v.location).toBe("회의실 B");
    expect(v.allDay).toBe(false);
    expect(String(v.start)).toContain("2026-07-01");
  });

  it("filters the list to a clicked day and clears back to the visible month", async () => {
    renderWithProviders(<CalendarPane />, {
      connected: true,
      dataProvider: fakeProvider({ "calendar-range": events }),
    });
    expect(await screen.findByText(/2026.*일정/)).toBeInTheDocument();

    // Click June 18 (holds 기획 리뷰) → the list filters to that day.
    await userEvent.click(screen.getByRole("button", { name: /6월 18일/ }));
    expect(screen.getByText("6월 18일 일정")).toBeInTheDocument();
    const table = screen.getByRole("table");
    expect(within(table).getByText("기획 리뷰")).toBeInTheDocument();
    expect(within(table).queryByText("연차")).not.toBeInTheDocument(); // 연차 is June 22, filtered out

    // Clear → back to the full visible-month list.
    await userEvent.click(screen.getByRole("button", { name: /← 월 전체/ }));
    expect(screen.getByText(/2026.*일정/)).toBeInTheDocument();
    expect(within(screen.getByRole("table")).getByText("연차")).toBeInTheDocument();
  });

  it("shows an empty notice when a day with no events is clicked", async () => {
    renderWithProviders(<CalendarPane />, {
      connected: true,
      dataProvider: fakeProvider({ "calendar-range": events }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /6월 5일/ }));
    expect(screen.getByText("이 날 일정이 없습니다.")).toBeInTheDocument();
  });

  it("opens a read-only detail for Google (non-local) events", async () => {
    // A July event can appear in June's trailing grid cells; click the list copy.
    const july = [{ id: "g1", summary: "외부 회의", start: "2026-07-02T05:00:00Z", end: "2026-07-02T06:00:00Z" }];
    renderWithProviders(<CalendarPane />, { connected: true, dataProvider: fakeProvider({ "calendar-range": july }) });

    const copies = await screen.findAllByText("외부 회의");
    await userEvent.click(copies[copies.length - 1]);
    expect(await screen.findByText(/외부\(구글\) 일정은 여기서 수정할 수 없습니다/)).toBeInTheDocument();
    // No editable form → no 저장 button in the read-only detail.
    expect(screen.queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
  });
});
