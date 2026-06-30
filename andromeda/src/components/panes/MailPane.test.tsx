import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MailPane } from "./MailPane";
import { cachedListStorageKey, cachedOneStorageKey } from "@/cachedList";
import { MAIL_RPC } from "@/resources";
import { startOfDay } from "@/format";
import { fakeProvider, renderWithProviders } from "@/test/util";

// MailPane now browses one day at a time, so its list cache is keyed per local day.
// Seed today's bucket so the cached-render tests match what the pane reads on mount.
const mailListCacheKey = cachedListStorageKey(`mail.${startOfDay()}`);

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
      mailListCacheKey,
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
      mailListCacheKey,
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
    // The body lives behind the 본문 tab now (분석 is the default view).
    await userEvent.click(within(detail).getByRole("button", { name: "본문" }));
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
    // The snippet stands in for the body — behind the 본문 tab.
    await userEvent.click(within(detail).getByRole("button", { name: "본문" }));
    expect(within(detail).getByText("상세 본문 대신 스니펫을 표시합니다.")).toBeInTheDocument();
  });

  it("keeps the read overlay after remount while the cached list is still unread", () => {
    localStorage.setItem(
      mailListCacheKey,
      JSON.stringify({
        data: [{ id: "m1", subject: "이미 읽은 메일", from: "kim@corp.com", isUnread: true }],
        total: 1,
        savedAt: Date.now(),
      }),
    );
    localStorage.setItem("andromeda.mail.locallyReadIds", JSON.stringify([{ id: "m1", at: Date.now() }]));
    const dataProvider = {
      ...fakeProvider(),
      getList: async () => new Promise<never>(() => {}),
    };

    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(screen.getByText("이미 읽은 메일")).toBeInTheDocument();
    expect(screen.queryByText("●")).not.toBeInTheDocument();
  });

  it("expires the read overlay so a later unread state can surface", () => {
    localStorage.setItem(
      mailListCacheKey,
      JSON.stringify({
        data: [{ id: "m1", subject: "다시 안읽은 메일", from: "kim@corp.com", isUnread: true }],
        total: 1,
        savedAt: Date.now(),
      }),
    );
    localStorage.setItem("andromeda.mail.locallyReadIds", JSON.stringify([{ id: "m1", at: Date.now() - 10 * 60_000 }]));
    const dataProvider = {
      ...fakeProvider(),
      getList: async () => new Promise<never>(() => {}),
    };

    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(screen.getByText("다시 안읽은 메일")).toBeInTheDocument();
    expect(screen.getByText("●")).toBeInTheDocument();
  });

  it("marks an unread message read when opened and clears the unread dot optimistically", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
      if (body.method === MAIL_RPC.markRead) {
        return new Response(JSON.stringify({ ok: true, payload: { ok: true } }), {
          headers: { "Content-Type": "application/json" },
        });
      }
      return Promise.reject(new Error("offline test"));
    });
    vi.stubGlobal("fetch", fetchMock);

    const dataProvider = fakeProvider({
      mail: [{ id: "m1", subject: "안읽은 메일", from: "kim@corp.com", body: "본문", isUnread: true }],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(await screen.findByText("●")).toBeInTheDocument();
    await userEvent.click(await screen.findByText("안읽은 메일"));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => {
          const body = JSON.parse(String((init as RequestInit | undefined)?.body ?? "{}")) as { method?: string };
          return body.method === MAIL_RPC.markRead;
        }),
      ).toBe(true),
    );
    const detail = screen.getByLabelText("메일 상세");
    await waitFor(() => expect(within(detail).queryByRole("button", { name: "읽음" })).not.toBeInTheDocument());
    expect(screen.queryByText("●")).not.toBeInTheDocument();
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
    // The body (Markdown) lives behind the 본문 tab now.
    await userEvent.click(within(detail).getByRole("button", { name: "본문" }));
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

  it("shows attachment download links in the expanded detail", async () => {
    const dataProvider = fakeProvider({
      mail: [
        {
          id: "m1",
          subject: "첨부 메일",
          from: "a@b.com",
          body: "본문",
          attachmentCount: 1,
          attachments: [{ id: "att1", filename: "quote.pdf", mimeType: "application/pdf", size: 2048 }],
        },
      ],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider, cfg: { url: "http://gateway", token: "tok" } });

    await userEvent.click(await screen.findByText("첨부 메일"));
    const detail = screen.getByLabelText("메일 상세");
    const link = within(detail).getByRole("link", { name: /quote.pdf/ });

    expect(within(detail).getByText("첨부파일")).toBeInTheDocument();
    expect(link).toHaveAttribute("href", expect.stringContaining("/api/v1/miniapp/gmail/attachment"));
    expect(link).toHaveAttribute("href", expect.stringContaining("messageId=m1"));
    expect(link).toHaveAttribute("href", expect.stringContaining("attachmentId=att1"));
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
