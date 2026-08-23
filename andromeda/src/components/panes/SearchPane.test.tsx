import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { useAiFeed, useWorkspace } from "@/workspaceContext";
import { SearchPane } from "./SearchPane";

let payload: unknown;
let factPageBody: string;
let factPageFails: boolean;
let getPageCalls: number;
let lastGetPagePath: string;

function WorkspaceProbe() {
  const { paneTarget } = useWorkspace();
  const { aiText } = useAiFeed();
  return (
    <>
      <output data-testid="workspace-target">
        {`${paneTarget?.view ?? ""}:${paneTarget?.id ?? ""}:${paneTarget?.path ?? ""}:${paneTarget?.size ?? ""}`}
      </output>
      <output data-testid="ai-text">{aiText}</output>
    </>
  );
}

function factPayload(path = "@facts/fact-123.md") {
  return {
    wiki: [
      {
        path,
        snippet: "project:alpha owner is A (stale cache)",
        resultKind: "fact",
        readOnly: true,
        factId: "fact-123",
        subjectId: "project:alpha",
      },
    ],
    diary: [],
    people: [],
    files: [],
    mail: [],
    sources: { wiki: "ok", diary: "ok", people: "ok", files: "ok", mail: "ok" },
  };
}

beforeEach(() => {
  localStorage.clear();
  payload = {
    wiki: [{ path: "projects/andromeda", title: "Andromeda 설계 노트", snippet: "3분할 워크스테이션" }],
    diary: [],
    people: [],
    files: [
      {
        path: "contracts/renewal.md",
        name: "renewal.md",
        snippet: "자동 갱신 조항은 30일 전에 통지해야 합니다.",
        score: 0.91,
        size: 245_760,
        startLine: 42,
        endLine: 47,
        kind: "markdown",
        heading: "해지 및 갱신",
      },
    ],
    mail: [
      {
        id: "mail-7",
        threadId: "thread-2",
        from: "법무팀 <legal@example.com>",
        subject: "계약 갱신 검토",
        date: "2026-08-22T09:10:00+09:00",
        snippet: "갱신 조건을 이번 주까지 확인해 주세요.",
        mailbox: "INBOX",
      },
    ],
    sources: { wiki: "ok", diary: "ok", people: "ok", files: "ok", mail: "ok" },
  };
  factPageBody = "# Current fact\n\nproject:alpha owner is B (current)";
  factPageFails = false;
  getPageCalls = 0;
  lastGetPagePath = "";
  if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const { method, params } = JSON.parse(String(init?.body ?? "{}")) as {
        method?: string;
        params?: { path?: string };
      };
      if (method === "miniapp.memory.get_page") {
        getPageCalls += 1;
        lastGetPagePath = params?.path ?? "";
        if (factPageFails) {
          return {
            ok: true,
            json: async () => ({ ok: false, error: { code: "NOT_FOUND", message: "fact no longer current" } }),
          } as unknown as Response;
        }
        return {
          ok: true,
          json: async () => ({ ok: true, payload: { body: factPageBody } }),
        } as unknown as Response;
      }
      return {
        ok: true,
        json: async () => ({ ok: true, payload }),
      } as unknown as Response;
    }),
  );
});

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("SearchPane", () => {
  it("groups grounded hits, shows locators, and preserves result navigation", async () => {
    renderWithProviders(
      <>
        <SearchPane />
        <WorkspaceProbe />
      </>,
      { connected: true },
    );

    expect(screen.getByText("통합 검색")).toBeInTheDocument();
    await userEvent.type(screen.getByPlaceholderText(/검색/), "계약{enter}");

    const wiki = await screen.findByRole("region", { name: "위키" });
    const files = screen.getByRole("region", { name: "파일" });
    const mail = screen.getByRole("region", { name: "메일" });
    expect(within(wiki).getByRole("button", { name: /Andromeda 설계 노트/ })).toHaveAttribute("title", "페이지 열기");
    expect(within(files).getByText("contracts/renewal.md:42–47")).toBeInTheDocument();
    expect(within(files).getByText(/자동 갱신 조항은 30일 전에/)).toBeInTheDocument();
    expect(within(mail).getByText("계약 갱신 검토")).toBeInTheDocument();
    expect(within(mail).getByText(/법무팀 <legal@example.com>/)).toBeInTheDocument();
    expect(within(mail).getByText(/갱신 조건을 이번 주까지/)).toBeInTheDocument();
    expect(screen.queryByText("통합 검색")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("ai-text")).toHaveTextContent("[파일 · 1건]"));
    expect(screen.getByTestId("ai-text")).toHaveTextContent("[메일 · 1건]");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("Andromeda 설계 노트");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("contracts/renewal.md");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("자동 갱신 조항은 30일 전에");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("계약 갱신 검토");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("legal@example.com");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("갱신 조건을 이번 주까지");
    expect(
      Array.from({ length: localStorage.length }, (_, index) => localStorage.key(index)).filter((key) =>
        key?.startsWith("andromeda.rpcCache.search."),
      ),
    ).toEqual([]);
    const persistedStorage = Array.from({ length: localStorage.length }, (_, index) => {
      const key = localStorage.key(index);
      return key ? `${key}:${localStorage.getItem(key)}` : "";
    }).join("\n");
    expect(persistedStorage).not.toContain("계약");
    expect(persistedStorage).not.toContain("자동 갱신 조항은 30일 전에");
    expect(persistedStorage).not.toContain("갱신 조건을 이번 주까지");

    await userEvent.click(within(files).getByRole("button", { name: /renewal\.md/ }));
    expect(screen.getByTestId("workspace-target")).toHaveTextContent("files::contracts/renewal.md:245760");
    await userEvent.click(within(mail).getByRole("button", { name: /계약 갱신 검토/ }));
    expect(screen.getByTestId("workspace-target")).toHaveTextContent("mail:mail-7");
    await userEvent.click(within(wiki).getByRole("button", { name: /Andromeda 설계 노트/ }));
  });

  it("distinguishes an empty successful source from unavailable, error, and timeout", async () => {
    payload = {
      wiki: [],
      diary: [{ file: "diary/2026-08-23.md", header: "부분 일기 결과", content: "가용한 일부 검색 결과" }],
      people: [],
      files: [],
      mail: [],
      sources: { wiki: "ok", diary: "partial", people: "error", files: "unavailable", mail: "timeout" },
    };
    renderWithProviders(<SearchPane />, { connected: true });
    await userEvent.type(screen.getByPlaceholderText(/검색/), "없는 항목{enter}");

    expect(within(await screen.findByRole("region", { name: "위키" })).getByText("결과 없음")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "파일" })).getByText("검색 소스를 사용할 수 없습니다."),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "일기" })).getByText("일부 검색 결과만 표시합니다."),
    ).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "일기" })).getByText("부분 일기 결과")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "인물" })).getByText("검색 중 오류가 발생했습니다."),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "메일" })).getByText("검색 시간이 초과되었습니다."),
    ).toBeInTheDocument();
    expect(screen.getByText("일부 검색 소스를 확인하지 못했습니다 · 4곳")).toBeInTheDocument();
  });

  it("bounds the submitted query to 500 Unicode code points", async () => {
    renderWithProviders(<SearchPane />, { connected: true });
    const input = screen.getByPlaceholderText(/검색/);
    fireEvent.change(input, { target: { value: "😀".repeat(501) } });
    expect(input).toHaveValue("😀".repeat(500));
    fireEvent.keyDown(input, { key: "Enter" });
    await screen.findByRole("region", { name: "위키" });

    const fetchMock = vi.mocked(fetch);
    const request = JSON.parse(String(fetchMock.mock.calls.at(-1)?.[1]?.body ?? "{}")) as {
      params?: { query?: string };
    };
    expect(request.params?.query).toBe("😀".repeat(500));
  });

  it("invalidates a pending search when its query is edited and ignores the late response", async () => {
    let resolveFirst!: (response: Response) => void;
    let markJsonRead!: () => void;
    const jsonRead = new Promise<void>((resolve) => {
      markJsonRead = resolve;
    });
    const firstResponse = new Promise<Response>((resolve) => {
      resolveFirst = resolve;
    });
    const fetchMock = vi.fn(() => firstResponse);
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(
      <>
        <SearchPane />
        <WorkspaceProbe />
      </>,
      { connected: true },
    );
    const input = screen.getByPlaceholderText(/검색/);

    await userEvent.type(input, "A 질의{enter}");
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(input, { key: "Enter" });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await userEvent.clear(input);
    await userEvent.type(input, "B 질의");
    expect(screen.queryByText("검색 중…")).not.toBeInTheDocument();
    expect(screen.getByTestId("ai-text")).toBeEmptyDOMElement();

    await act(async () => {
      resolveFirst({
        ok: true,
        json: async () => {
          markJsonRead();
          return {
            ok: true,
            payload: {
              wiki: [{ path: "late-a", title: "늦게 도착한 A 결과", snippet: "A 근거" }],
              diary: [],
              people: [],
              files: [],
              mail: [],
              sources: { wiki: "ok", diary: "ok", people: "ok", files: "ok", mail: "ok" },
            },
          };
        },
      } as Response);
      await jsonRead;
    });

    expect(input).toHaveValue("B 질의");
    expect(screen.queryByText("늦게 도착한 A 결과")).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "위키" })).not.toBeInTheDocument();
    expect(screen.getByTestId("ai-text")).toBeEmptyDOMElement();
  });

  it("revalidates fact hits uncached, hides cached snippets, and keeps payloads out of AI context", async () => {
    payload = factPayload();
    renderWithProviders(
      <>
        <SearchPane />
        <WorkspaceProbe />
      </>,
      { connected: true },
    );

    await userEvent.type(screen.getByPlaceholderText(/검색/), "alpha{enter}");
    const hit = await screen.findByRole("button", { name: /project:alpha/ });
    expect(hit).toHaveAttribute("title", "읽기 전용 사실 열기");
    expect(hit).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByText("클릭해 현재 사실 확인")).toBeInTheDocument();
    expect(screen.queryByText(/owner is A/)).not.toBeInTheDocument();

    await userEvent.click(hit);
    expect(await screen.findByText(/owner is B \(current\)/)).toBeInTheDocument();
    expect(hit).toHaveAttribute("aria-expanded", "true");
    expect(getPageCalls).toBe(1);
    expect(lastGetPagePath).toBe("@facts/fact-123.md");
    expect(screen.getByText(/읽기 전용 사실 · project:alpha · fact-123/)).toBeInTheDocument();
    expect(screen.getByTestId("ai-text")).toHaveTextContent("[위키 · 1건]");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("project:alpha");
    expect(screen.getByTestId("ai-text")).not.toHaveTextContent("owner is B");

    await userEvent.click(hit);
    expect(screen.queryByText(/owner is B \(current\)/)).not.toBeInTheDocument();
    await userEvent.click(hit);
    expect(await screen.findByText(/owner is B \(current\)/)).toBeInTheDocument();
    expect(getPageCalls).toBe(2);

    const persistedStorage = Array.from({ length: localStorage.length }, (_, index) => {
      const key = localStorage.key(index);
      return key ? `${key}:${localStorage.getItem(key)}` : "";
    }).join("\n");
    expect(persistedStorage).not.toContain("owner is A");
    expect(persistedStorage).not.toContain("owner is B");
  });

  it("rejects a superseded fact ref and asks the user to search again", async () => {
    payload = factPayload("@facts/fact-old.md");
    factPageFails = true;
    renderWithProviders(<SearchPane />, { connected: true });

    await userEvent.type(screen.getByPlaceholderText(/검색/), "alpha{enter}");
    await userEvent.click(await screen.findByRole("button", { name: /project:alpha/ }));

    expect(await screen.findByText("사실이 변경되었습니다. 다시 검색하세요.")).toBeInTheDocument();
    expect(screen.queryByText(/owner is A/)).not.toBeInTheDocument();
    expect(getPageCalls).toBe(1);
    expect(lastGetPagePath).toBe("@facts/fact-old.md");
  });

  it("discards a late fact detail after a new search generation", async () => {
    payload = factPayload();
    let resolveDetail!: (response: Response) => void;
    let markDetailJsonRead!: () => void;
    const detailJsonRead = new Promise<void>((resolve) => {
      markDetailJsonRead = resolve;
    });
    const detailResponse = new Promise<Response>((resolve) => {
      resolveDetail = resolve;
    });
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      const { method } = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
      if (method === "miniapp.memory.get_page") return detailResponse;
      return {
        ok: true,
        json: async () => ({ ok: true, payload }),
      } as unknown as Response;
    });
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(<SearchPane />, { connected: true });
    const input = screen.getByPlaceholderText(/검색/);

    await userEvent.type(input, "alpha{enter}");
    await userEvent.click(await screen.findByRole("button", { name: /project:alpha/ }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    await userEvent.clear(input);
    await userEvent.type(input, "beta{enter}");
    const currentHit = await screen.findByRole("button", { name: /project:alpha/ });
    expect(currentHit).toHaveAttribute("aria-expanded", "false");
    expect(within(currentHit).getByText("클릭해 현재 사실 확인")).toBeInTheDocument();

    await act(async () => {
      resolveDetail({
        ok: true,
        json: async () => {
          markDetailJsonRead();
          return { ok: true, payload: { body: "late alpha fact must stay hidden" } };
        },
      } as Response);
      await detailJsonRead;
      await Promise.resolve();
    });

    expect(screen.queryByText("late alpha fact must stay hidden")).not.toBeInTheDocument();
    expect(currentHit).toHaveAttribute("aria-expanded", "false");
    expect(within(currentHit).getByText("클릭해 현재 사실 확인")).toBeInTheDocument();
    expect(screen.queryByText("현재 사실 확인 중...")).not.toBeInTheDocument();
  });
});
