import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MailPane } from "./MailPane";
import { cachedListStorageKey, cachedOneStorageKey } from "@/cachedList";
import { fakeProvider, renderWithProviders } from "@/test/util";

beforeEach(() => {
  // The detail's enrichment cards (분석·발신자) call gateway RPCs on open; keep
  // these fixture-driven tests offline so the cards degrade instead of hitting
  // the network. The data provider is injected, so this only stubs callRpc.
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline test"))),
  );
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
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
    // 목록은 제목만 — 메일 초입부 한 줄(스니펫)은 표시하지 않는다.
    expect(screen.queryByText("먼저 보이는 내용")).not.toBeInTheDocument();
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
    expect(detail.closest("tr")?.className).toContain("dgrid-expanded-row");
    expect(detail.closest(".mail-split")).toBeNull();
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

  it("renders the message body as Markdown (links become anchors)", async () => {
    const dataProvider = fakeProvider({
      mail: [
        {
          id: "m1",
          subject: "링크 메일",
          from: "a@b.com",
          body: "## 안내\n\n자세한 내용은 [문서](https://example.com) 참고.",
        },
      ],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByText("링크 메일"));
    const detail = screen.getByLabelText("메일 상세");
    expect(within(detail).getByRole("heading", { name: "안내" })).toBeInTheDocument();
    const link = within(detail).getByRole("link", { name: "문서" });
    expect(link).toHaveAttribute("href", "https://example.com");
  });

  it("shows only the sender name in the list, dropping the address", async () => {
    const dataProvider = fakeProvider({
      mail: [{ id: "m1", subject: "이름만 표시", from: "김철수 <kim@corp.com>" }],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(await screen.findByText("김철수")).toBeInTheDocument();
    expect(screen.queryByText(/kim@corp\.com/)).not.toBeInTheDocument();
  });

  it("shows no inline action buttons on rows; delete lives in the detail view", async () => {
    const dataProvider = fakeProvider({
      mail: [{ id: "m1", subject: "정리 대상", from: "kim@corp.com", isUnread: true }],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(await screen.findByText("정리 대상")).toBeInTheDocument();
    // The list carries no per-row actions — not even delete.
    expect(screen.queryByRole("button", { name: "삭제" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "읽음" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "보관" })).not.toBeInTheDocument();

    // Opening the message surfaces delete (and the other actions) in the detail.
    await userEvent.click(screen.getByText("정리 대상"));
    const detail = screen.getByLabelText("메일 상세");
    expect(within(detail).getByRole("button", { name: "삭제" })).toBeInTheDocument();
  });
});
