import { beforeEach, describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ApprovalsPane } from "./ApprovalsPane";
import { approvalDayMs } from "../../approvalBody";
import { fakeProvider, renderWithProviders } from "@/test/util";
import type { GroupwareApprovalRow } from "@/gen/miniappWire";

const today = new Date();
const ymd = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
const yesterdayDate = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
const ymdYesterday = `${yesterdayDate.getFullYear()}-${String(yesterdayDate.getMonth() + 1).padStart(2, "0")}-${String(yesterdayDate.getDate()).padStart(2, "0")}`;

const approvals: GroupwareApprovalRow[] = [
  {
    docId: "1",
    title: "오늘 구매 품의",
    drafter: "홍길동",
    date: ymd,
    status: "결재대기",
    folder: "pending",
    canAct: true,
  },
  {
    docId: "2",
    title: "어제 휴가",
    drafter: "김대리",
    date: ymdYesterday,
    status: "결재완료",
    folder: "done",
    canAct: false,
  },
  {
    docId: "3",
    title: "어제 미결 발주",
    drafter: "박과장",
    date: ymdYesterday,
    status: "미결",
    folder: "total",
    canAct: true,
  },
];

function renderApprovals(rows = approvals, connected = true) {
  return renderWithProviders(<ApprovalsPane />, {
    connected,
    dataProvider: fakeProvider({ approvals: rows }),
  });
}

beforeEach(() => localStorage.clear());

describe("approvalDayMs", () => {
  it("normalizes Amaranth date stamps", () => {
    expect(approvalDayMs("2026-07-16")).toBe(new Date(2026, 6, 16).getTime());
    expect(approvalDayMs("2026.07.16")).toBe(new Date(2026, 6, 16).getTime());
    expect(approvalDayMs("20260716")).toBe(new Date(2026, 6, 16).getTime());
    expect(approvalDayMs("")).toBeNull();
  });
});

describe("ApprovalsPane", () => {
  it("shows disconnected state before querying", () => {
    renderApprovals(approvals, false);
    expect(screen.getByText("미연결")).toBeInTheDocument();
  });

  it("day-pagers today's rows and hides yesterday until stepped", async () => {
    renderApprovals();
    expect(await screen.findByText("오늘 구매 품의")).toBeInTheDocument();
    expect(screen.queryByText("어제 휴가")).not.toBeInTheDocument();
    await userEvent.click(screen.getByLabelText("이전 날"));
    expect(await screen.findByText("어제 휴가")).toBeInTheDocument();
    expect(screen.queryByText("오늘 구매 품의")).not.toBeInTheDocument();
  });

  it("shows empty copy for a day with no docs", async () => {
    renderApprovals([]);
    expect(await screen.findByText("이 날짜에는 결재 문서가 없습니다.")).toBeInTheDocument();
  });

  it("offers approve/reject on a pending row", async () => {
    renderApprovals();
    await userEvent.click(await screen.findByText("오늘 구매 품의"));
    expect(screen.getByRole("button", { name: "승인" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "반려" })).toBeInTheDocument();
  });

  it("미결만 collects pending docs across days", async () => {
    renderApprovals();
    await screen.findByText("오늘 구매 품의");
    expect(screen.queryByText("어제 미결 발주")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /미결만/ }));
    expect(screen.getByText("오늘 구매 품의")).toBeInTheDocument();
    expect(screen.getByText("어제 미결 발주")).toBeInTheDocument();
    expect(screen.queryByText("어제 휴가")).not.toBeInTheDocument();
  });
});
