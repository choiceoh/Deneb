import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { FileEntry } from "@/types";
import { FILES_RPC } from "@/resources";
import { renderWithProviders } from "@/test/util";
import { FilesPane } from "./FilesPane";

type RpcCall = { method: string; params: Record<string, unknown> };

const root: FileEntry[] = [
  { tag: "folder", name: "projects", pathDisplay: "projects" },
  { tag: "dir", name: "archive", pathLower: "archive" },
  { tag: "file", name: "readme.md", pathDisplay: "readme.md", size: 128, serverModified: "2026-07-10T00:00:00Z" },
];

const projects: FileEntry[] = [
  { tag: "folder", name: "nested", pathDisplay: "projects/nested" },
  { tag: "file", name: "contract.pdf", pathDisplay: "projects/contract.pdf", size: 2_048 },
  { tag: "file", name: "notes.md", pathDisplay: "projects/notes.md", size: 35 },
  { tag: "file", name: "zero.txt", pathDisplay: "projects/zero.txt", size: 0 },
];

function rpcReply(payload: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => (ok ? { ok: true, payload } : { ok: false, error: String(payload) }),
  } as Response;
}

function installGateway(
  calls: RpcCall[],
  handlers: Partial<Record<string, (params: Record<string, unknown>) => Response | Promise<Response>>> = {},
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/api/v1/files/download")) {
        const text = "# 문서\n\n저장 전 본문";
        const blob = { type: "text/markdown", size: text.length, text: async () => text } as Blob;
        return { ok: true, status: 200, blob: async () => blob } as Response;
      }
      const request = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      const method = request.method ?? "";
      const params = request.params ?? {};
      calls.push({ method, params });
      if (handlers[method]) return handlers[method]!(params);
      switch (method) {
        case FILES_RPC.list: {
          const path = String(params.path ?? "");
          return rpcReply({ path, entries: path === "projects" ? projects : path === "projects/nested" ? [] : root });
        }
        case FILES_RPC.search:
          return rpcReply({ entries: projects.filter((entry) => entry.tag === "file") });
        case FILES_RPC.share:
          return rpcReply({ url: `https://share.example/${encodeURIComponent(String(params.path))}` });
        default:
          return rpcReply({ ok: true });
      }
    }),
  );
}

function renderFiles(connected = true, active = true) {
  return renderWithProviders(<FilesPane active={active} />, {
    connected,
    cfg: connected ? { url: "http://test", token: "tok" } : { url: "", token: "" },
  });
}

async function enterProjects() {
  await userEvent.click((await screen.findAllByText("projects"))[0]);
  return screen.findByText("contract.pdf");
}

function lastCall(calls: RpcCall[], method: string) {
  return calls.filter((call) => call.method === method).at(-1);
}

describe("FilesPane boundary behavior", () => {
  let calls: RpcCall[];

  beforeEach(() => {
    localStorage.clear();
    calls = [];
    installGateway(calls);
    if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "files-test" });
  });

  afterEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  describe("connection and path navigation", () => {
    it("disables every network mutation while disconnected", () => {
      renderFiles(false);
      expect(screen.getByRole("button", { name: "상위" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "새로고침" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "새 폴더" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "업로드" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "검색" })).toBeDisabled();
      expect(screen.getByPlaceholderText("파일 검색...")).toBeDisabled();
      expect(calls).toHaveLength(0);
    });

    it("lists root with the bounded list contract", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      expect(calls[0]).toEqual({ method: FILES_RPC.list, params: { path: "", limit: 300 } });
      expect(screen.getByLabelText("파일 경로")).toHaveValue("/");
    });

    it("treats both folder and dir tags as navigable directories", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      expect(screen.getAllByText("폴더")).toHaveLength(2);
      await userEvent.click(screen.getAllByText("archive")[0]);
      expect(lastCall(calls, FILES_RPC.list)?.params).toMatchObject({ path: "archive" });
    });

    it("navigates down and then back to its canonical parent", async () => {
      renderFiles();
      await enterProjects();
      expect(screen.getByLabelText("파일 경로")).toHaveValue("projects");
      await userEvent.click(screen.getByRole("button", { name: "상위" }));
      await waitFor(() => expect(screen.getByLabelText("파일 경로")).toHaveValue("/"));
      expect(lastCall(calls, FILES_RPC.list)?.params.path).toBe("");
    });

    it("disables parent navigation at root", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      expect(screen.getByRole("button", { name: "상위" })).toBeDisabled();
    });

    it("loads a typed path when Enter is pressed", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      const path = screen.getByLabelText("파일 경로");
      fireEvent.change(path, { target: { value: "projects" } });
      fireEvent.keyDown(path, { key: "Enter" });
      expect(await screen.findByText("contract.pdf")).toBeInTheDocument();
      expect(lastCall(calls, FILES_RPC.list)?.params).toEqual({ path: "projects", limit: 300 });
    });

    it("does not navigate on unrelated path keys", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      const before = calls.filter((call) => call.method === FILES_RPC.list).length;
      fireEvent.keyDown(screen.getByLabelText("파일 경로"), { key: "Tab" });
      expect(calls.filter((call) => call.method === FILES_RPC.list)).toHaveLength(before);
    });

    it("shows a true empty-folder state after entering an empty directory", async () => {
      renderFiles();
      await enterProjects();
      await userEvent.click((await screen.findAllByText("nested"))[0]);
      expect(await screen.findByText("파일이 없습니다.")).toBeInTheDocument();
      expect(screen.getByLabelText("파일 경로")).toHaveValue("projects/nested");
    });

    it("renders size only for files and leaves unknown or zero sizes blank", async () => {
      renderFiles();
      await enterProjects();
      const contract = screen.getByText("contract.pdf").closest("tr")!;
      expect(within(contract).getByText("2.0 KB")).toBeInTheDocument();
      const zero = screen.getByText("zero.txt").closest("tr")!;
      expect(within(zero).getAllByRole("cell")[2]).toHaveTextContent("");
      const nested = screen.getAllByText("nested")[0].closest("tr")!;
      expect(within(nested).getAllByRole("cell")[2]).toHaveTextContent("");
    });
  });

  describe("search state and parameters", () => {
    it("sends trimmed query plus explicit content and semantic flags", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      await userEvent.type(screen.getByPlaceholderText("파일 검색..."), "  contract  ");
      await userEvent.click(screen.getByRole("checkbox", { name: "내용" }));
      await userEvent.click(screen.getByRole("checkbox", { name: "의미" }));
      await userEvent.click(screen.getByRole("button", { name: "검색" }));
      await screen.findByText("contract.pdf");
      expect(lastCall(calls, FILES_RPC.search)?.params).toEqual({
        query: "contract",
        content: true,
        semantic: true,
        max: 80,
      });
    });

    it("runs search from Enter and ignores unrelated keys", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      const query = screen.getByPlaceholderText("파일 검색...");
      await userEvent.type(query, "notes");
      fireEvent.keyDown(query, { key: "ArrowDown" });
      expect(calls.filter((call) => call.method === FILES_RPC.search)).toHaveLength(0);
      fireEvent.keyDown(query, { key: "Enter" });
      await waitFor(() => expect(calls.filter((call) => call.method === FILES_RPC.search)).toHaveLength(1));
    });

    it("treats an empty search as refresh of the current folder", async () => {
      renderFiles();
      await enterProjects();
      const before = calls.filter((call) => call.method === FILES_RPC.list).length;
      await userEvent.click(screen.getByRole("button", { name: "검색" }));
      await waitFor(() =>
        expect(calls.filter((call) => call.method === FILES_RPC.list).length).toBeGreaterThan(before),
      );
      expect(lastCall(calls, FILES_RPC.list)?.params.path).toBe("projects");
      expect(calls.filter((call) => call.method === FILES_RPC.search)).toHaveLength(0);
    });

    it("marks search mode, locks path editing, and offers a folder return", async () => {
      renderFiles();
      await userEvent.type(await screen.findByPlaceholderText("파일 검색..."), "contract");
      await userEvent.click(screen.getByRole("button", { name: "검색" }));
      expect(await screen.findByLabelText("파일 경로")).toHaveValue("검색 결과");
      expect(screen.getByLabelText("파일 경로")).toBeDisabled();
      expect(screen.getByRole("button", { name: "새 폴더" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "폴더로" })).toBeInTheDocument();
    });

    it("returns from search to the same folder path", async () => {
      renderFiles();
      await enterProjects();
      await userEvent.type(screen.getByPlaceholderText("파일 검색..."), "contract");
      await userEvent.click(screen.getByRole("button", { name: "검색" }));
      await userEvent.click(await screen.findByRole("button", { name: "폴더로" }));
      expect(await screen.findByLabelText("파일 경로")).toHaveValue("projects");
      expect(lastCall(calls, FILES_RPC.list)?.params.path).toBe("projects");
    });

    it("reports empty search separately from an empty folder", async () => {
      installGateway(calls, { [FILES_RPC.search]: () => rpcReply({ entries: [] }) });
      renderFiles();
      await userEvent.type(await screen.findByPlaceholderText("파일 검색..."), "missing");
      await userEvent.click(screen.getByRole("button", { name: "검색" }));
      expect(await screen.findByText("검색 결과 없음")).toBeInTheDocument();
      expect(screen.getByText("파일이 없습니다.")).toBeInTheDocument();
    });
  });

  describe("file operations", () => {
    it("does not offer share for folders but offers move and delete", async () => {
      renderFiles();
      const row = (await screen.findAllByText("projects"))[0].closest("tr")!;
      expect(within(row).queryByRole("button", { name: "공유" })).not.toBeInTheDocument();
      expect(within(row).getByRole("button", { name: "이동" })).toBeInTheDocument();
      expect(within(row).getByRole("button", { name: "삭제" })).toBeInTheDocument();
    });

    it("shares a file by its canonical path and selects its row", async () => {
      renderFiles();
      await enterProjects();
      const row = screen.getByText("contract.pdf").closest("tr")!;
      await userEvent.click(within(row).getByRole("button", { name: "공유" }));
      expect(await screen.findByRole("link", { name: /share\.example/ })).toHaveAttribute(
        "href",
        "https://share.example/projects%2Fcontract.pdf",
      );
      expect(lastCall(calls, FILES_RPC.share)?.params).toEqual({ path: "projects/contract.pdf" });
      expect(row).toHaveClass("selected");
    });

    it("does not render an empty link when share returns no URL", async () => {
      installGateway(calls, { [FILES_RPC.share]: () => rpcReply({}) });
      renderFiles();
      await enterProjects();
      const row = screen.getByText("contract.pdf").closest("tr")!;
      await userEvent.click(within(row).getByRole("button", { name: "공유" }));
      expect(await screen.findByText("공유 링크 없음")).toBeInTheDocument();
      expect(screen.queryByRole("link")).not.toBeInTheDocument();
    });

    it("creates a trimmed folder under the current path", async () => {
      renderFiles();
      await enterProjects();
      await userEvent.click(screen.getByRole("button", { name: "새 폴더" }));
      const dialog = screen.getByRole("dialog", { name: "새 폴더" });
      fireEvent.change(within(dialog).getByLabelText("폴더 이름"), { target: { value: "  2027 계약  " } });
      await userEvent.click(within(dialog).getByRole("button", { name: "생성" }));
      await waitFor(() => expect(lastCall(calls, FILES_RPC.mkdir)?.params).toEqual({ path: "projects/2027 계약" }));
      expect(await screen.findByText("폴더 생성됨")).toBeInTheDocument();
      expect(screen.queryByRole("dialog", { name: "새 폴더" })).not.toBeInTheDocument();
    });

    it("keeps create modal open when mkdir fails", async () => {
      installGateway(calls, { [FILES_RPC.mkdir]: () => rpcReply("permission denied", false) });
      renderFiles();
      await screen.findAllByText("projects");
      await userEvent.click(screen.getByRole("button", { name: "새 폴더" }));
      const dialog = screen.getByRole("dialog", { name: "새 폴더" });
      await userEvent.type(within(dialog).getByLabelText("폴더 이름"), "blocked");
      await userEvent.click(within(dialog).getByRole("button", { name: "생성" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(dialog).toBeInTheDocument();
    });

    it("moves a file with exact source and trimmed destination", async () => {
      renderFiles();
      await enterProjects();
      const row = screen.getByText("contract.pdf").closest("tr")!;
      await userEvent.click(within(row).getByRole("button", { name: "이동" }));
      const dialog = screen.getByRole("dialog", { name: "파일 이동" });
      expect(within(dialog).getByLabelText("대상 경로")).toHaveValue("projects/contract.pdf");
      fireEvent.change(within(dialog).getByLabelText("대상 경로"), {
        target: { value: "  archive/contract.pdf  " },
      });
      await userEvent.click(within(dialog).getByRole("button", { name: "이동" }));
      await waitFor(() =>
        expect(lastCall(calls, FILES_RPC.move)?.params).toEqual({
          src: "projects/contract.pdf",
          dst: "archive/contract.pdf",
        }),
      );
      expect(await screen.findByText("이동됨")).toBeInTheDocument();
    });

    it("requires destructive confirmation before deleting", async () => {
      renderFiles();
      await enterProjects();
      const row = screen.getByText("contract.pdf").closest("tr")!;
      await userEvent.click(within(row).getByRole("button", { name: "삭제" }));
      const dialog = screen.getByRole("dialog", { name: "파일 삭제" });
      expect(dialog).toHaveTextContent("projects/contract.pdf");
      expect(calls.filter((call) => call.method === FILES_RPC.delete)).toHaveLength(0);
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      await waitFor(() => expect(lastCall(calls, FILES_RPC.delete)?.params).toEqual({ path: "projects/contract.pdf" }));
      expect(await screen.findByText("삭제됨")).toBeInTheDocument();
    });

    it.each([
      ["report.pdf", "", "application/pdf"],
      ["notes.md", "", "text/markdown"],
      ["photo.png", "image/custom", "image/custom"],
      ["unknown.bin", "", "application/octet-stream"],
    ])("uploads %s with MIME %s => %s", async (name, type, expected) => {
      renderFiles();
      await screen.findAllByText("projects");
      const input = document.querySelector('input[type="file"]') as HTMLInputElement;
      await userEvent.upload(input, new File(["abc"], name, { type }));
      await waitFor(() => expect(lastCall(calls, FILES_RPC.upload)).toBeDefined());
      expect(lastCall(calls, FILES_RPC.upload)?.params).toMatchObject({
        path: name,
        mimeType: expected,
        dataBase64: "YWJj",
      });
    });

    it("clears the native file input after selection so the same file can be reselected", async () => {
      renderFiles();
      await screen.findAllByText("projects");
      const input = document.querySelector('input[type="file"]') as HTMLInputElement;
      await userEvent.upload(input, new File(["abc"], "again.txt", { type: "text/plain" }));
      await waitFor(() => expect(lastCall(calls, FILES_RPC.upload)).toBeDefined());
      expect(input.value).toBe("");
    });
  });

  describe("preview edit safety", () => {
    async function openNotes() {
      renderFiles();
      await enterProjects();
      await userEvent.click(screen.getByText("notes.md"));
      return screen.findByRole("dialog", { name: "notes.md" });
    }

    it("builds a token-authenticated download URL for the exact file path", async () => {
      const dialog = await openNotes();
      await within(dialog).findByRole("button", { name: "편집" });
      const download = within(dialog).getByRole("link", { name: /다운로드/ });
      expect(download.getAttribute("href")).toContain("projects%2Fnotes.md");
    });

    it("opens clean, switches to edit, and tracks dirty state", async () => {
      const dialog = await openNotes();
      expect(await within(dialog).findByText("저장됨")).toBeInTheDocument();
      await userEvent.click(within(dialog).getByRole("button", { name: "편집" }));
      const editor = await within(dialog).findByDisplayValue(/저장 전 본문/);
      await userEvent.type(editor, "\n추가 내용");
      expect(within(dialog).getByText("수정됨")).toBeInTheDocument();
      expect(within(dialog).getByRole("button", { name: "저장" })).toBeEnabled();
    });

    it("overwrites the same path with UTF-8 text encoded to base64", async () => {
      const dialog = await openNotes();
      await userEvent.click(await within(dialog).findByRole("button", { name: "편집" }));
      const editor = await within(dialog).findByDisplayValue(/저장 전 본문/);
      fireEvent.change(editor, { target: { value: "한글 저장" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
      await waitFor(() => expect(lastCall(calls, FILES_RPC.upload)?.params.overwrite).toBe(true));
      expect(lastCall(calls, FILES_RPC.upload)?.params).toMatchObject({
        path: "projects/notes.md",
        mimeType: "text/markdown",
        dataBase64: "7ZWc6riAIOyggOyepQ==",
      });
      expect(await within(dialog).findByText("저장됨")).toBeInTheDocument();
    });

    it("preserves dirty content when overwrite fails", async () => {
      installGateway(calls, {
        [FILES_RPC.upload]: (params) => (params.overwrite ? rpcReply("disk full", false) : rpcReply({ ok: true })),
      });
      const dialog = await openNotes();
      await userEvent.click(await within(dialog).findByRole("button", { name: "편집" }));
      const editor = await within(dialog).findByDisplayValue(/저장 전 본문/);
      fireEvent.change(editor, { target: { value: "보존할 편집" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(within(dialog).getByDisplayValue("보존할 편집")).toBeInTheDocument();
      expect(within(dialog).getByText("수정됨")).toBeInTheDocument();
    });

    it("does not prompt when closing a clean preview", async () => {
      const dialog = await openNotes();
      await within(dialog).findByRole("button", { name: "편집" });
      await userEvent.click(within(dialog).getByRole("button", { name: "닫기" }));
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(screen.queryByText(/저장하지 않은 변경이 있습니다/)).not.toBeInTheDocument();
    });

    it("keeps dirty preview open when the close confirmation is cancelled", async () => {
      const dialog = await openNotes();
      await userEvent.click(await within(dialog).findByRole("button", { name: "편집" }));
      await userEvent.type(await within(dialog).findByDisplayValue(/저장 전 본문/), " 추가");
      await userEvent.click(within(dialog).getByRole("button", { name: "닫기" }));
      const confirm = screen.getByRole("dialog", { name: "저장하지 않은 변경" });
      expect(confirm).toHaveTextContent("projects/notes.md에 저장하지 않은 변경이 있습니다.");
      await userEvent.click(within(confirm).getByRole("button", { name: "계속 편집" }));
      expect(screen.queryByRole("dialog", { name: "저장하지 않은 변경" })).not.toBeInTheDocument();
      expect(screen.getByRole("dialog", { name: "notes.md" })).toBeInTheDocument();
    });

    it("discards dirty preview only after explicit confirmation", async () => {
      const dialog = await openNotes();
      await userEvent.click(await within(dialog).findByRole("button", { name: "편집" }));
      await userEvent.type(await within(dialog).findByDisplayValue(/저장 전 본문/), " 추가");
      await userEvent.click(within(dialog).getByRole("button", { name: "닫기" }));
      await userEvent.click(
        within(screen.getByRole("dialog", { name: "저장하지 않은 변경" })).getByRole("button", {
          name: "버리고 닫기",
        }),
      );
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(screen.getByText("notes.md")).toBeInTheDocument();
    });
  });
});
