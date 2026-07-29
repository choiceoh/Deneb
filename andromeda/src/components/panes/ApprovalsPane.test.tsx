import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ApprovalsPane } from "./ApprovalsPane";
import { approvalDayMs } from "../../approvalBody";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { WorkspaceStub, workspaceValue } from "@/test/workspace";
import { DataProviderScope } from "@/crud";
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

  const reviewRows: GroupwareApprovalRow[] = [
    {
      docId: "11",
      title: "일반 경비 지출 품의",
      drafter: "홍길동",
      date: ymd,
      status: "미결",
      folder: "pending",
      canAct: true,
    },
    {
      docId: "12",
      title: "완도 관산포 기자재 발주 품의",
      drafter: "홍길동",
      date: ymd,
      status: "미결",
      folder: "pending",
      canAct: true,
    },
  ];

  function renderReview(value: Partial<ReturnType<typeof workspaceValue>>) {
    return render(
      <DataProviderScope
        dataProvider={fakeProvider({
          approvals: reviewRows,
          progress: [{ project: "완도 관산포", path: "프로젝트/완도 관산포/대표.md" }],
          people: [{ name: "홍길동", wikiPath: "인물/홍길동.md" }],
        })}
      >
        <WorkspaceStub value={workspaceValue(value)}>
          <ApprovalsPane />
        </WorkspaceStub>
      </DataProviderScope>,
    );
  }

  // 결재 행을 여는 것만으로 위키 타일이 끼어들면 안 된다 — splitWiki는 새 타일에
  // 포커스를 주고 3타일이 차 있으면 기존 타일을 밀어내, "결재 내용이 아니라
  // 프로젝트 정보"가 뜬 것으로 읽혔다. 근거는 버튼 한 개로만 제시한다.
  it("never steals the pane on open — the wiki hit is an explicit button", async () => {
    const splitWiki = vi.fn();
    renderReview({ splitWiki });

    await userEvent.click(await screen.findByText("완도 관산포 기자재 발주 품의"));
    expect(splitWiki).not.toHaveBeenCalled();

    const openWiki = await screen.findByRole("button", { name: /근거 위키 · 완도 관산포/ });
    await userEvent.click(openWiki);
    expect(splitWiki).toHaveBeenCalledWith("프로젝트/완도 관산포/대표.md");
  });

  // 검토 모드는 문서 "제목"에서만 근거를 찾는다 — 기안자 이름으로 기안자의
  // 인물 위키를 제시하는 것(결재 내용 대신 사람 정보로 읽힘)은 회귀.
  it("matches the title's project, never the drafter's person wiki", async () => {
    renderReview({});
    // 기안자(홍길동)만 걸리는 문서 — 근거 위키 버튼 자체가 없다.
    await userEvent.click(await screen.findByText("일반 경비 지출 품의"));
    await screen.findByRole("button", { name: "닫기" });
    expect(screen.queryByRole("button", { name: /근거 위키/ })).not.toBeInTheDocument();
  });

  // 컨텍스트 팔로우 모드(명시 opt-in)에서만 자동 배치가 살아난다.
  it("auto-splits the wiki only when 컨텍스트 팔로우 모드 is on", async () => {
    const splitWiki = vi.fn();
    renderReview({ splitWiki, followMode: true });

    await userEvent.click(await screen.findByText("완도 관산포 기자재 발주 품의"));
    await waitFor(() => expect(splitWiki).toHaveBeenCalledWith("프로젝트/완도 관산포/대표.md"));
    expect(splitWiki).toHaveBeenCalledTimes(1);
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
