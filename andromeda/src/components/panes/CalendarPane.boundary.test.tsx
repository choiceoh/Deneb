import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@refinedev/core";
import type { CalEvent } from "@/types";
import { monthLabel } from "@/format";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { CalendarPane } from "./CalendarPane";

type RpcCall = { method: string; params: Record<string, unknown> };
type MutationCall = { kind: "create" | "update" | "delete"; resource: string; id?: string | number; values?: unknown };

const juneEvents: CalEvent[] = [
  {
    id: "timed",
    summary: "정기 회의",
    description: "주간 지표를 검토합니다.",
    start: "2026-06-18T01:00:00Z",
    end: "2026-06-18T02:30:00Z",
    location: "회의실 A",
    local: true,
    category: "mine",
  },
  {
    id: "deadline",
    summary: "입찰 마감",
    start: { date: "2026-06-22" },
    end: { date: "2026-06-23" },
    allDay: true,
    local: true,
    category: "deadline",
  },
  {
    id: "external",
    summary: "외부 협의",
    start: "2026-06-25T05:00:00Z",
    end: "2026-06-25T06:00:00Z",
    location: "Google Meet",
    category: "others",
  },
];

function response(payload: unknown, ok = true): Response {
  return new Response(JSON.stringify(ok ? { ok: true, payload } : { ok: false, error: String(payload) }), {
    status: ok ? 200 : 500,
    headers: { "Content-Type": "application/json" },
  });
}

function installFetch(
  calls: RpcCall[],
  handlers: Partial<Record<string, (params: Record<string, unknown>) => Response | Promise<Response>>> = {},
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/chat/stream")) {
        const body = 'event: done\ndata: {"text":"일정 분석"}\n\n';
        return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
      }
      const request = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      calls.push({ method: request.method, params: request.params ?? {} });
      if (handlers[request.method]) return handlers[request.method]!(request.params ?? {});
      if (request.method === "miniapp.calendar.proposals.list") return response({ proposals: [] });
      return response({ ok: true });
    }),
  );
}

function provider(
  events: CalEvent[] = juneEvents,
  mutations: MutationCall[] = [],
  ranges: Record<string, unknown>[] = [],
): DataProvider {
  const base = fakeProvider({ "calendar-range": events });
  return {
    ...base,
    getList: async (args) => {
      if (args.resource === "calendar-range") ranges.push((args.meta as Record<string, unknown>) ?? {});
      return base.getList(args);
    },
    create: async (args) => {
      mutations.push({ kind: "create", resource: args.resource, values: args.variables });
      return { data: { id: "created", ...(args.variables as object) } as any };
    },
    update: async (args) => {
      mutations.push({ kind: "update", resource: args.resource, id: args.id, values: args.variables });
      return { data: { id: args.id, ...(args.variables as object) } as any };
    },
    deleteOne: async (args) => {
      mutations.push({ kind: "delete", resource: args.resource, id: args.id });
      return { data: { id: args.id } as any };
    },
  };
}

async function openCreate() {
  if (!screen.queryByRole("button", { name: "새 일정" })) {
    renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([]) });
  }
  await userEvent.click(screen.getByRole("button", { name: "새 일정" }));
  return screen.findByRole("dialog", { name: "새 일정" });
}

describe("CalendarPane boundary behavior", () => {
  let rpc: RpcCall[];

  beforeEach(() => {
    localStorage.clear();
    rpc = [];
    installFetch(rpc);
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-06-15T00:15:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  describe("month navigation and agenda scope", () => {
    it("when queries a grid-covering range rather than only the numbered month", async () => {
      const ranges: Record<string, unknown>[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents, [], ranges) });
      await screen.findByText("정기 회의");
      expect(ranges.length).toBeGreaterThan(0);
      const rpcParams = ranges[0].rpcParams as Record<string, string>;
      expect(new Date(rpcParams.from).getTime()).toBeLessThan(new Date("2026-06-01T00:00:00Z").getTime());
      expect(new Date(rpcParams.to).getTime()).toBeGreaterThan(new Date("2026-06-30T00:00:00Z").getTime());
    });

    it("when steps from January backward into the previous year", async () => {
      vi.setSystemTime(new Date("2026-01-15T00:00:00Z"));
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([]) });
      expect(await screen.findByText(monthLabel(2026, 0))).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "이전 달" }));
      expect(screen.getByText(monthLabel(2025, 11))).toBeInTheDocument();
    });

    it("when steps from December forward into the next year", async () => {
      vi.setSystemTime(new Date("2026-12-15T00:00:00Z"));
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([]) });
      expect(await screen.findByText(monthLabel(2026, 11))).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "다음 달" }));
      expect(screen.getByText(monthLabel(2027, 0))).toBeInTheDocument();
    });

    it("returns from another month to the frozen current month", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([]) });
      await userEvent.click(screen.getByRole("button", { name: "다음 달" }));
      expect(screen.getByText(monthLabel(2026, 6))).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "오늘" }));
      expect(screen.getByText(monthLabel(2026, 5))).toBeInTheDocument();
    });

    it("clears a selected day when changing month", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      await userEvent.click(await screen.findByRole("button", { name: /6월 18일, 일정 1건/ }));
      expect(screen.getByText("6월 18일 일정")).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "다음 달" }));
      expect(screen.queryByText("6월 18일 일정")).not.toBeInTheDocument();
      expect(screen.getByText(`${monthLabel(2026, 6)} 일정`)).toBeInTheDocument();
    });

    it("when toggles the same day back to month scope", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const day = await screen.findByRole("button", { name: /6월 18일, 일정 1건/ });
      await userEvent.click(day);
      expect(screen.getByText("6월 18일 일정")).toBeInTheDocument();
      await userEvent.click(day);
      expect(screen.getByText(`${monthLabel(2026, 5)} 일정`)).toBeInTheDocument();
    });

    it("when places a multi-day all-day event on each covered day but excludes the exclusive end", async () => {
      const span = [
        {
          id: "span",
          summary: "사흘 워크숍",
          start: { date: "2026-06-20" },
          end: { date: "2026-06-23" },
          allDay: true,
        },
      ];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(span) });
      expect(await screen.findByRole("button", { name: /6월 20일, 일정 1건/ })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /6월 21일, 일정 1건/ })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /6월 22일, 일정 1건/ })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /^6월 23일$/ })).toBeInTheDocument();
    });

    it("without leak an adjacent-month event into the month agenda", async () => {
      const spill = [
        ...juneEvents,
        { id: "july", summary: "7월 시작", start: "2026-07-01T01:00:00Z", end: "2026-07-01T02:00:00Z" },
      ];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(spill) });
      await screen.findByText("정기 회의");
      expect(screen.queryByText("7월 시작")).not.toBeInTheDocument();
    });

    it("preserves historical events in a non-current month agenda", async () => {
      const may = [{ id: "past", summary: "5월 회고", start: "2026-05-02T01:00:00Z", end: "2026-05-02T02:00:00Z" }];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(may) });
      await userEvent.click(screen.getByRole("button", { name: "이전 달" }));
      expect(await screen.findByText("5월 회고")).toBeInTheDocument();
    });
  });

  describe("agenda keyboard and category semantics", () => {
    it.each([
      ["정기 회의", "mine"],
      ["입찰 마감", "deadline"],
      ["외부 협의", "others"],
    ])("renders %s with %s category tint", async (title, category) => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const row = (await screen.findByText(title)).closest(".cal-agenda-item")!;
      expect(row.querySelector(".cal-agenda-dot")).toHaveClass(category);
    });

    it("selects and deselects an agenda row with Enter", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const row = (await screen.findByText("정기 회의")).closest('[role="button"]') as HTMLElement;
      fireEvent.keyDown(row, { key: "Enter" });
      expect(row).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByRole("heading", { name: "일정 편집" })).toBeInTheDocument();
      fireEvent.keyDown(row, { key: "Enter" });
      expect(screen.queryByRole("heading", { name: "일정 편집" })).not.toBeInTheDocument();
    });

    it("selects a row with Space and prevents page scrolling", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const row = (await screen.findByText("입찰 마감")).closest('[role="button"]') as HTMLElement;
      const event = new KeyboardEvent("keydown", { key: " ", bubbles: true, cancelable: true });
      row.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(true);
      await waitFor(() => expect(row).toHaveAttribute("aria-pressed", "true"));
    });

    it("ignores unrelated keys on an agenda row", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const row = (await screen.findByText("정기 회의")).closest('[role="button"]') as HTMLElement;
      fireEvent.keyDown(row, { key: "ArrowDown" });
      expect(row).toHaveAttribute("aria-pressed", "false");
      expect(screen.queryByRole("heading", { name: "일정 편집" })).not.toBeInTheDocument();
    });

    it("keeps delete click from also opening the event editor", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents, mutations) });
      const deadline = (await screen.findByText("입찰 마감")).closest(".cal-agenda-item") as HTMLElement;
      await userEvent.click(within(deadline).getByRole("button", { name: "삭제" }));
      await waitFor(() => expect(rpc.some((call) => call.method === "miniapp.calendar.delete")).toBe(true));
      expect(screen.queryByRole("heading", { name: "일정 편집" })).not.toBeInTheDocument();
    });

    it("does not render destructive controls for externally sourced events", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const external = (await screen.findByText("외부 협의")).closest(".cal-agenda-item") as HTMLElement;
      expect(within(external).queryByRole("button", { name: "삭제" })).not.toBeInTheDocument();
    });
  });

  describe("new event form constraints", () => {
    it("when prefills the next full hour and a one-hour end", async () => {
      const dialog = await openCreate();
      expect(within(dialog).getByLabelText("시작 날짜")).toHaveValue("2026-06-15");
      expect(within(dialog).getByLabelText("시작 시간")).toHaveValue("10:00");
      expect(within(dialog).getByLabelText("종료 날짜")).toHaveValue("2026-06-15");
      expect(within(dialog).getByLabelText("종료 시간")).toHaveValue("11:00");
    });

    it("when prefills a clicked day at the working-hour default", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      await userEvent.click(await screen.findByRole("button", { name: /^6월 29일$/ }));
      const dialog = await openCreate();
      expect(within(dialog).getByLabelText("시작 날짜")).toHaveValue("2026-06-29");
      expect(within(dialog).getByLabelText("시작 시간")).toHaveValue("09:00");
    });

    it("validates the title before issuing a create mutation", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([], mutations) });
      const dialog = await openCreate();
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
      expect(within(dialog).getByText("제목을 입력하세요")).toBeInTheDocument();
      expect(mutations).toHaveLength(0);
    });

    it("preserves the seeded start when a date input emits an empty change", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([], mutations) });
      const dialog = await openCreate();
      fireEvent.change(within(dialog).getByLabelText("시작 날짜"), { target: { value: "" } });
      expect(within(dialog).getByLabelText("시작 날짜")).toHaveValue("2026-06-15");
      expect(within(dialog).getByLabelText("시작 시간")).toHaveValue("10:00");
      expect(mutations).toHaveLength(0);
    });

    it.each([
      ["30분", "10:30"],
      ["1시간", "11:00"],
      ["2시간", "12:00"],
    ])("when applies the %s quick duration", async (label, end) => {
      const dialog = await openCreate();
      await userEvent.click(within(dialog).getByRole("button", { name: label }));
      expect(within(dialog).getByLabelText("종료 시간")).toHaveValue(end);
    });

    it("preserves duration when the timed start moves", async () => {
      const dialog = await openCreate();
      await userEvent.click(within(dialog).getByRole("button", { name: "2시간" }));
      fireEvent.change(within(dialog).getByLabelText("시작 시간"), { target: { value: "14:30" } });
      expect(within(dialog).getByLabelText("종료 시간")).toHaveValue("16:30");
    });

    it("switches all-day inputs without losing the selected dates", async () => {
      const dialog = await openCreate();
      fireEvent.change(within(dialog).getByLabelText("시작 날짜"), { target: { value: "2026-06-20" } });
      fireEvent.change(within(dialog).getByLabelText("종료 날짜"), { target: { value: "2026-06-21" } });
      await userEvent.click(within(dialog).getByLabelText("종일"));
      expect(within(dialog).queryByLabelText("시작 시간")).not.toBeInTheDocument();
      expect(within(dialog).getByLabelText("시작 날짜")).toHaveValue("2026-06-20");
      expect(within(dialog).getByLabelText("종료 날짜")).toHaveValue("2026-06-21");
    });

    it("when trims title, location and description before persistence", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([], mutations) });
      const dialog = await openCreate();
      fireEvent.change(within(dialog).getByLabelText("제목"), { target: { value: "  계획 회의  " } });
      fireEvent.change(within(dialog).getByLabelText("장소"), { target: { value: "  회의실 C  " } });
      fireEvent.change(within(dialog).getByLabelText("설명"), { target: { value: "  의사결정 안건  " } });
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
      await waitFor(() => expect(mutations).toHaveLength(1));
      expect(mutations[0].values).toMatchObject({
        summary: "계획 회의",
        location: "회의실 C",
        description: "의사결정 안건",
        allDay: false,
      });
    });

    it("omits blank optional fields from the payload", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider([], mutations) });
      const dialog = await openCreate();
      fireEvent.change(within(dialog).getByLabelText("제목"), { target: { value: "제목만" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
      await waitFor(() => expect(mutations).toHaveLength(1));
      expect(mutations[0].values).not.toHaveProperty("location");
      expect(mutations[0].values).not.toHaveProperty("description");
    });

    it("cancels without creating and restores the agenda", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents, mutations) });
      const dialog = await openCreate();
      await userEvent.type(within(dialog).getByLabelText("제목"), "버릴 일정");
      await userEvent.click(within(dialog).getByRole("button", { name: "취소" }));
      expect(screen.queryByRole("dialog", { name: "새 일정" })).not.toBeInTheDocument();
      expect(mutations).toHaveLength(0);
      expect(screen.getByText("정기 회의")).toBeInTheDocument();
    });
  });

  describe("existing event editing", () => {
    it("when hydrates every editable field from a local event", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      await userEvent.click(await screen.findByText("정기 회의"));
      const workspace = screen.getByRole("region", { name: "선택한 일정" });
      expect(within(workspace).getByLabelText("제목")).toHaveValue("정기 회의");
      expect(within(workspace).getByLabelText("장소")).toHaveValue("회의실 A");
      expect(within(workspace).getByLabelText("설명")).toHaveValue("주간 지표를 검토합니다.");
    });

    it("updates a local event by its wire id and keeps the workspace open", async () => {
      const mutations: MutationCall[] = [];
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents, mutations) });
      await userEvent.click(await screen.findByText("정기 회의"));
      const workspace = screen.getByRole("region", { name: "선택한 일정" });
      fireEvent.change(within(workspace).getByLabelText("제목"), { target: { value: "변경 회의" } });
      await userEvent.click(within(workspace).getByRole("button", { name: "저장" }));
      await waitFor(() => expect(mutations.some((call) => call.kind === "update")).toBe(true));
      expect(mutations.find((call) => call.kind === "update")).toMatchObject({
        resource: "calendar",
        id: "timed",
        values: expect.objectContaining({ summary: "변경 회의" }),
      });
      expect(screen.getByRole("region", { name: "선택한 일정" })).toBeInTheDocument();
      expect(within(workspace).getByText("저장됨")).toBeInTheDocument();
    });

    it("closes a selected local event without deleting it", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      await userEvent.click(await screen.findByText("입찰 마감"));
      const workspace = screen.getByRole("region", { name: "선택한 일정" });
      const editor = workspace.querySelector(".calendar-workspace-panel") as HTMLElement;
      await userEvent.click(within(editor).getAllByRole("button", { name: "닫기" })[0]);
      expect(screen.queryByRole("region", { name: "선택한 일정" })).not.toBeInTheDocument();
      expect(screen.getByText("입찰 마감")).toBeInTheDocument();
    });

    it("renders external details but no mutable inputs", async () => {
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      await userEvent.click(await screen.findByText("외부 협의"));
      const workspace = screen.getByRole("region", { name: "선택한 일정" });
      expect(within(workspace).getByRole("heading", { name: "일정 상세" })).toBeInTheDocument();
      expect(workspace).toHaveTextContent("Google Meet");
      expect(within(workspace).queryByLabelText("제목")).not.toBeInTheDocument();
      expect(within(workspace).queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
    });
  });

  describe("proposal resilience", () => {
    it("hides the tray when proposal discovery fails for an older gateway", async () => {
      installFetch(rpc, {
        "miniapp.calendar.proposals.list": () => Promise.reject(new Error("method not found")),
      });
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      await screen.findByText("정기 회의");
      expect(screen.queryByRole("heading", { name: "일정 제안" })).not.toBeInTheDocument();
    });

    it("shows source context and safe fallbacks for proposal kinds", async () => {
      installFetch(rpc, {
        "miniapp.calendar.proposals.list": () =>
          response({
            proposals: [
              {
                id: "p",
                title: "마감 후보",
                start: "2026-06-30",
                allDay: true,
                kind: "deadline",
                sourceSubject: "입찰 공고",
                sourceFrom: "조달팀",
              },
            ],
          }),
      });
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const tray = await screen.findByRole("region", { name: "일정 제안" });
      expect(tray).toHaveTextContent("마감");
      expect(tray).toHaveTextContent("입찰 공고");
      expect(tray).toHaveTextContent("조달팀");
      expect(tray).toHaveTextContent("2026-06-30");
    });

    it("keeps a proposal available when accept fails and reports the error", async () => {
      installFetch(rpc, {
        "miniapp.calendar.proposals.list": () => response({ proposals: [{ id: "p", title: "실패 후보" }] }),
        "miniapp.calendar.proposals.accept": () => response("calendar write denied", false),
      });
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const tray = await screen.findByRole("region", { name: "일정 제안" });
      await userEvent.click(within(tray).getByRole("button", { name: "수락" }));
      expect(await within(tray).findByText(/HTTP 500/)).toBeInTheDocument();
      expect(within(tray).getByText("실패 후보")).toBeInTheDocument();
      expect(within(tray).getByRole("button", { name: "수락" })).toBeEnabled();
    });

    it("when disables both decisions while one proposal request is in flight", async () => {
      let resolve!: (response: Response) => void;
      installFetch(rpc, {
        "miniapp.calendar.proposals.list": () => response({ proposals: [{ id: "p", title: "대기 후보" }] }),
        "miniapp.calendar.proposals.accept": () => new Promise<Response>((done) => (resolve = done)),
      });
      renderWithProviders(<CalendarPane />, { connected: true, dataProvider: provider(juneEvents) });
      const tray = await screen.findByRole("region", { name: "일정 제안" });
      const accept = within(tray).getByRole("button", { name: "수락" });
      await userEvent.click(accept);
      expect(accept).toBeDisabled();
      expect(accept).toHaveTextContent("처리 중");
      expect(within(tray).getByRole("button", { name: "거절" })).toBeDisabled();
      expect(within(tray).getByText("일정에 추가하는 중…")).toBeInTheDocument();
      resolve(response({ ok: true }));
      await waitFor(() => expect(within(tray).getByRole("button", { name: "수락" })).toBeEnabled());
      expect(within(tray).getByText("일정에 추가됨")).toBeInTheDocument();
    });
  });
});
