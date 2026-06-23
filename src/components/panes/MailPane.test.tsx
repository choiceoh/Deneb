import { afterEach, describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MailPane } from "./MailPane";
import { cachedListStorageKey, cachedOneStorageKey } from "@/cachedList";
import { fakeProvider, renderWithProviders } from "@/test/util";

afterEach(() => {
  localStorage.clear();
});

describe("MailPane", () => {
  it("renders the cached mail list immediately while the gateway refresh is still pending", () => {
    localStorage.setItem(
      cachedListStorageKey("mail"),
      JSON.stringify({
        data: [{ id: "cached-1", subject: "캐시된 메일", from: "cache@corp.com", snippet: "먼저 보이는 내용" }],
        total: 1,
        savedAt: Date.now() - 120_000,
      }),
    );
    const dataProvider = {
      ...fakeProvider(),
      getList: async () => new Promise<never>(() => {}),
    };

    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(screen.getByText("캐시된 메일")).toBeInTheDocument();
    expect(screen.getByText("먼저 보이는 내용")).toBeInTheDocument();
  });

  it("renders a cached mail body immediately while the detail refresh is still pending", async () => {
    localStorage.setItem(
      cachedListStorageKey("mail"),
      JSON.stringify({
        data: [{ id: "cached-1", subject: "캐시 본문 메일", from: "cache@corp.com", snippet: "목록 스니펫" }],
        total: 1,
        savedAt: Date.now(),
      }),
    );
    localStorage.setItem(
      cachedOneStorageKey("mail", "cached-1"),
      JSON.stringify({
        data: {
          id: "cached-1",
          subject: "캐시 본문 메일",
          from: "cache@corp.com",
          body: "캐시된 상세 본문입니다.",
        },
        savedAt: Date.now() - 600_000,
      }),
    );
    const dataProvider = {
      ...fakeProvider(),
      getOne: async () => new Promise<never>(() => {}),
    };

    renderWithProviders(<MailPane />, { connected: true, dataProvider });
    await userEvent.click(screen.getByText("캐시 본문 메일"));

    const detail = screen.getByLabelText("메일 상세");
    expect(await within(detail).findByText("캐시된 상세 본문입니다.")).toBeInTheDocument();
  });

  it("opens a selected message and falls back to the snippet when no body is available", async () => {
    const dataProvider = fakeProvider({
      mail: [
        {
          id: "m1",
          subject: "본문 없는 메일",
          from: "kim@corp.com",
          snippet: "상세 본문 대신 스니펫을 표시합니다.",
        },
      ],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByText("본문 없는 메일"));

    const detail = screen.getByLabelText("메일 상세");
    expect(within(detail).getByText("본문 없는 메일")).toBeInTheDocument();
    expect(within(detail).getByText("상세 본문 대신 스니펫을 표시합니다.")).toBeInTheDocument();
  });

  it("keeps only the inline delete action on each row", async () => {
    const dataProvider = fakeProvider({
      mail: [{ id: "m1", subject: "정리 대상", from: "kim@corp.com", isUnread: true }],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(await screen.findByText("정리 대상")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "읽음" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "보관" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "삭제" })).toBeInTheDocument();
  });
});
