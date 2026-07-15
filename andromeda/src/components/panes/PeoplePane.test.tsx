import { beforeEach, describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { PeoplePane } from "./PeoplePane";
import { fakeProvider, renderWithProviders } from "@/test/util";
import type { Person } from "@/types";

const people: Person[] = [
  {
    id: "person-1",
    name: "김데네브",
    email: "deneb@example.com",
    messageCount: 12,
    lastSeen: "2025-07-10T12:00:00+09:00",
    lastSubject: "설계 검토",
    wikiPath: "people/kim-deneb.md",
    wikiSummary: "프로젝트 설계 담당",
  },
  {
    id: "person-2",
    email: "anonymous@example.com",
    messageCount: 0,
    wikiSummary: "이름 미확인 연락처",
  },
];

function renderPeople(rows = people, connected = true) {
  return renderWithProviders(<PeoplePane />, {
    connected,
    dataProvider: fakeProvider({ people: rows }),
  });
}

beforeEach(() => localStorage.clear());

describe("PeoplePane list", () => {
  it("displays disconnected state before querying rows", () => {
    renderPeople(people, false);
    expect(screen.getByText("미연결")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("shows a clear empty state", async () => {
    renderPeople([]);
    expect(await screen.findByText("연락처가 없습니다.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders the merged Gmail and wiki columns", async () => {
    renderPeople();
    const table = await screen.findByRole("table");

    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((cell) => cell.textContent),
    ).toEqual(["이름", "이메일", "메일", "최근 제목 / 메모", "최근"]);
    expect(within(table).getByText("김데네브")).toBeInTheDocument();
    expect(within(table).getByText("deneb@example.com")).toBeInTheDocument();
    expect(within(table).getByText("12")).toBeInTheDocument();
    expect(within(table).getByText("설계 검토")).toBeInTheDocument();
  });

  it("when falls back for a wiki-only or unnamed contact", async () => {
    renderPeople();
    const table = await screen.findByRole("table");
    const row = within(table).getByText("anonymous@example.com").closest("tr")!;

    expect(within(row).getByText("—")).toBeInTheDocument();
    expect(within(row).getByText("이름 미확인 연락처")).toBeInTheDocument();
    expect(within(row).queryByText("0")).not.toBeInTheDocument();
  });

  it("when makes each data row keyboard-accessible", async () => {
    renderPeople();
    const table = await screen.findByRole("table");
    const row = within(table).getByText("김데네브").closest("tr")!;
    expect(row).toHaveClass("clickable");
    expect(row).toHaveAttribute("tabindex", "0");

    row.focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByRole("dialog", { name: "김데네브" })).toBeInTheDocument();
  });

  it("when opens a detail card by click", async () => {
    renderPeople();
    await userEvent.click(await screen.findByText("김데네브"));

    const dialog = screen.getByRole("dialog", { name: "김데네브" });
    expect(within(dialog).getByText("deneb@example.com")).toBeInTheDocument();
    expect(within(dialog).getByText("12건")).toBeInTheDocument();
    expect(within(dialog).getByText("설계 검토")).toBeInTheDocument();
    expect(within(dialog).getByText("프로젝트 설계 담당")).toBeInTheDocument();
  });

  it("uses email as the detail title when name is absent", async () => {
    renderPeople();
    await userEvent.click(await screen.findByText("anonymous@example.com"));
    expect(screen.getByRole("dialog", { name: "anonymous@example.com" })).toBeInTheDocument();
  });

  it("closes the detail card", async () => {
    renderPeople();
    await userEvent.click(await screen.findByText("김데네브"));
    const dialog = screen.getByRole("dialog", { name: "김데네브" });
    await userEvent.click(within(dialog).getAllByRole("button", { name: "닫기" })[1]);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("offers a wiki deep link only when the contact has one", async () => {
    renderPeople();
    await userEvent.click(await screen.findByText("김데네브"));
    expect(screen.getByRole("button", { name: "위키 열기" })).toBeInTheDocument();
    const dialog = screen.getByRole("dialog", { name: "김데네브" });
    await userEvent.click(within(dialog).getAllByRole("button", { name: "닫기" })[1]);

    await userEvent.click(screen.getByText("anonymous@example.com"));
    expect(screen.queryByRole("button", { name: "위키 열기" })).not.toBeInTheDocument();
  });

  it("navigates to a linked wiki page and closes the card", async () => {
    renderPeople();
    await userEvent.click(await screen.findByText("김데네브"));
    await userEvent.click(screen.getByRole("button", { name: "위키 열기" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("when uses the latest subject before a wiki summary in the compact row", async () => {
    renderPeople();
    const row = (await screen.findByText("김데네브")).closest("tr")!;
    expect(within(row).getByText("설계 검토")).toBeInTheDocument();
    expect(within(row).queryByText("프로젝트 설계 담당")).not.toBeInTheDocument();
  });
});
