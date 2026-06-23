import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
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
    renderWithProviders(<CalendarPane />, { connected: true, dataProvider: fakeProvider({ calendar: events }) });
    // weekday header row (한국어)
    expect(screen.getByText("일")).toBeInTheDocument();
    expect(screen.getByText("토")).toBeInTheDocument();
    // events show up (a chip in the grid + a row in the list → at least one match each)
    expect((await screen.findAllByText(/기획 리뷰/)).length).toBeGreaterThan(0);
    expect((await screen.findAllByText(/연차/)).length).toBeGreaterThan(0);
  });

  it("keeps the upcoming list with a delete action for local events", async () => {
    renderWithProviders(<CalendarPane />, { connected: true, dataProvider: fakeProvider({ calendar: events }) });
    expect(await screen.findByText("다가오는 일정")).toBeInTheDocument();
    // only the local event (연차) is deletable; the Google-sourced one is read-only
    expect(await screen.findByTitle("삭제")).toBeInTheDocument();
  });

  it("shows a connect notice and no calendar when disconnected", () => {
    renderWithProviders(<CalendarPane />, { connected: false });
    expect(screen.getByText(/게이트웨이에 연결하면/)).toBeInTheDocument();
    // weekday headers only exist when the grid renders (connected)
    expect(screen.queryByText("일")).not.toBeInTheDocument();
  });

  it("creates an event through the form", async () => {
    const calls: Record<string, unknown>[] = [];
    renderWithProviders(<CalendarPane />, { connected: true, dataProvider: capturing({ calendar: [] }, calls) });

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

  it("opens a read-only detail for Google (non-local) events", async () => {
    // A July event → appears only in the upcoming list (not the June grid), so the
    // title text is unambiguous to click.
    const july = [{ id: "g1", summary: "외부 회의", start: "2026-07-02T05:00:00Z", end: "2026-07-02T06:00:00Z" }];
    renderWithProviders(<CalendarPane />, { connected: true, dataProvider: fakeProvider({ calendar: july }) });

    await userEvent.click(await screen.findByText("외부 회의"));
    expect(await screen.findByText(/외부\(구글\) 일정은 여기서 수정할 수 없습니다/)).toBeInTheDocument();
    // No editable form → no 저장 button in the read-only detail.
    expect(screen.queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
  });
});
