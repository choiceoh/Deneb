import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@/crud";
import { MailPane } from "./MailPane";
import { cachedListStorageKey, cachedOneStorageKey } from "@/cachedList";
import { MAIL_RPC } from "@/resources";
import { addDays, startOfDay } from "@/format";
import { useWorkspace } from "@/workspaceContext";
import { fakeProvider, renderWithProviders } from "@/test/util";

// MailPane now browses one day at a time, so its list cache is keyed per local day.
// Seed today's bucket so the cached-render tests match what the pane reads on mount.
const mailListCacheKey = cachedListStorageKey(`mail.${startOfDay()}`);

// Mirrors MailPane's private gmailDay(): the after:/before: token for a local day,
// so a fixture provider can serve day-bucketed lists the way Gmail would.
function gmailToken(dayMs: number): string {
  const d = new Date(dayMs);
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
}

// Deep-link trigger — stands in for the work feed / search "open this mail" path.
function OpenMail({ id }: { id: string }) {
  const { openPane } = useWorkspace();
  return <button onClick={() => openPane("mail", { id })}>딥링크 열기</button>;
}

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

  it("does not roll back a failed mark-read overlay after unmount", async () => {
    let rejectMarkRead: (error: Error) => void = () => {};
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
      if (body.method === MAIL_RPC.markRead) {
        return new Promise<Response>((_resolve, reject) => {
          rejectMarkRead = reject;
        });
      }
      return Promise.reject(new Error("offline test"));
    });
    vi.stubGlobal("fetch", fetchMock);

    const dataProvider = fakeProvider({
      mail: [{ id: "m1", subject: "늦게 실패한 읽음", from: "kim@corp.com", body: "본문", isUnread: true }],
    });
    const { unmount } = renderWithProviders(<MailPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByText("늦게 실패한 읽음"));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => {
          const body = JSON.parse(String((init as RequestInit | undefined)?.body ?? "{}")) as { method?: string };
          return body.method === MAIL_RPC.markRead;
        }),
      ).toBe(true),
    );

    unmount();
    const windowValue = globalThis.window;
    vi.stubGlobal("window", undefined);
    try {
      rejectMarkRead(new Error("offline test"));
      await Promise.resolve();
      await Promise.resolve();
    } finally {
      vi.stubGlobal("window", windowValue);
    }
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

  it("displays only the sender name in the list, dropping the address", async () => {
    const dataProvider = fakeProvider({
      mail: [{ id: "m1", subject: "이름만 표시", from: "김철수 <kim@corp.com>" }],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(await screen.findByText("김철수")).toBeInTheDocument();
    expect(screen.queryByText(/kim@corp\.com/)).not.toBeInTheDocument();
  });

  it("shows a download link for a non-previewable attachment", async () => {
    const dataProvider = fakeProvider({
      mail: [
        {
          id: "m1",
          subject: "첨부 메일",
          from: "a@b.com",
          body: "본문",
          attachmentCount: 1,
          // .xlsx has no in-app previewer, so it stays a plain download link
          // (previewable formats — PDF, images, and now HWP via the in-browser
          // text extractor — become in-app viewer buttons instead).
          attachments: [{ id: "att1", filename: "정산.xlsx", mimeType: "application/octet-stream", size: 2048 }],
        },
      ],
    });
    renderWithProviders(<MailPane />, { connected: true, dataProvider, cfg: { url: "http://gateway", token: "tok" } });

    await userEvent.click(await screen.findByText("첨부 메일"));
    const detail = screen.getByLabelText("메일 상세");
    const link = within(detail).getByRole("link", { name: /정산\.xlsx/ });

    expect(within(detail).getByText("첨부파일")).toBeInTheDocument();
    expect(link).toHaveAttribute("href", expect.stringContaining("/api/v1/miniapp/gmail/attachment"));
    expect(link).toHaveAttribute("href", expect.stringContaining("messageId=m1"));
    expect(link).toHaveAttribute("href", expect.stringContaining("attachmentId=att1"));
  });

  // Gmail buckets the day list by RECEIVED time while `date` carries the sender's
  // Date header — an automated mail received late yesterday can carry a header date
  // of today. Clicking its row on yesterday's list must open it in place, not bounce
  // the pager to today (where the received-time query doesn't even return it).
  it("opens a listed mail in place when its Date header falls on another day", async () => {
    const today = startOfDay();
    const yesterday = addDays(today, -1);
    const skewed = {
      id: "m1",
      subject: "코드 인증완료",
      from: "auth@service.example",
      body: "인증이 완료되었습니다.",
      date: new Date(today + 9 * 3_600_000).toISOString(), // header says today 09:00
    };
    const dataProvider: DataProvider = {
      ...fakeProvider(),
      // Received-time bucketing: the mail shows up only on yesterday's day query.
      getList: async ({ meta }) => {
        const q = String((meta?.rpcParams as { query?: string } | undefined)?.query ?? "");
        // `as any[]`: the dynamic RPC boundary, same as test/util's fakeProvider.
        const data = (q.includes(`after:${gmailToken(yesterday)} `) ? [skewed] : []) as any[];
        return { data, total: data.length };
      },
      getOne: async () => ({ data: skewed as any }),
    };
    renderWithProviders(<MailPane />, { connected: true, dataProvider });

    expect(await screen.findByText("이 날짜에는 메일이 없습니다.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
    await userEvent.click(await screen.findByText("코드 인증완료"));

    const detail = await screen.findByLabelText("메일 상세");
    expect(within(detail).getByText("코드 인증완료")).toBeInTheDocument();
    // The pager stayed on the day the user was browsing.
    expect(screen.getByText("어제")).toBeInTheDocument();
  });

  // The deep-link path has the same header-vs-received hazard: the pager jumps to
  // the mail's header day, but that day's received-time query may not list it. The
  // selection is then pinned as an extra top row so the mail still opens.
  it("pins a deep-linked mail above the list when its header day doesn't list it", async () => {
    const yesterday = addDays(startOfDay(), -1);
    const orphan = {
      id: "m9",
      subject: "버킷 밖 메일",
      from: "robot@svc.example",
      body: "본문",
      date: new Date(yesterday + 10 * 3_600_000).toISOString(),
    };
    const dataProvider: DataProvider = {
      ...fakeProvider(),
      getList: async () => ({ data: [], total: 0 }),
      getOne: async () => ({ data: orphan as any }),
    };
    renderWithProviders(
      <>
        <OpenMail id="m9" />
        <MailPane />
      </>,
      { connected: true, dataProvider },
    );

    await userEvent.click(screen.getByRole("button", { name: "딥링크 열기" }));

    const detail = await screen.findByLabelText("메일 상세");
    expect(within(detail).getByText("버킷 밖 메일")).toBeInTheDocument();
    // The pager followed the mail's header day…
    expect(await screen.findByText("어제")).toBeInTheDocument();
    // …and the pinned row replaces the empty-day notice.
    expect(screen.queryByText("이 날짜에는 메일이 없습니다.")).not.toBeInTheDocument();
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
