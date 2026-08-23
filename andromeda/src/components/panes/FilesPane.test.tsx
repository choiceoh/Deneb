import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FILES_RPC } from "@/resources";
import { cachedRpcStorageKey, rpcCacheKey } from "@/rpcCache";
import { maxTextPreviewBytes } from "@/components/fileView";
import type { FileEntry } from "@/types";
import { renderWithProviders } from "@/test/util";
import { useWorkspace } from "@/workspaceContext";
import { FilesPane } from "./FilesPane";

const rootEntries: FileEntry[] = [{ tag: "folder", name: "projects", pathDisplay: "projects" }];
let rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
let downloadCalls = 0;

const projectEntries: FileEntry[] = [
  {
    tag: "file",
    name: "quarter-review.pdf",
    pathDisplay: "projects/quarter-review.pdf",
    size: 245_760,
    serverModified: "2026-06-17T09:00:00Z",
  },
  {
    tag: "file",
    name: "notes.md",
    pathDisplay: "projects/notes.md",
    size: 42,
    serverModified: "2026-06-18T09:00:00Z",
  },
];

function FileTargetOpener() {
  const { openPane } = useWorkspace();
  return <button onClick={() => openPane("files", { path: "projects/notes.md" })}>검색 파일 열기</button>;
}

function OversizedFileTargetOpener() {
  const { openPane } = useWorkspace();
  return (
    <button onClick={() => openPane("files", { path: "projects/oversized.md", size: maxTextPreviewBytes + 1 })}>
      큰 검색 파일 열기
    </button>
  );
}

beforeEach(() => {
  localStorage.clear();
  rpcCalls = [];
  downloadCalls = 0;
  if (!globalThis.crypto?.randomUUID) {
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      // The file viewer downloads bytes via GET /api/v1/files/download (no JSON
      // body) — serve markdown as a Blob. jsdom's Blob lacks .text(), so back
      // it with an explicit body the viewer can read.
      if (typeof url === "string" && url.includes("/api/v1/files/download")) {
        downloadCalls += 1;
        const body = "# 메모\n원본 내용";
        const blob = { size: body.length, type: "text/markdown", text: async () => body } as unknown as Blob;
        return { ok: true, blob: async () => blob } as unknown as Response;
      }
      const { method, params } = JSON.parse(String(init?.body ?? "{}")) as {
        method: string;
        params: Record<string, unknown>;
      };
      rpcCalls.push({ method, params });
      const reply = (payload: unknown) =>
        ({ ok: true, json: async () => ({ ok: true, payload }) }) as unknown as Response;
      switch (method) {
        case "miniapp.files.list":
          return reply({ entries: params.path === "projects" ? projectEntries : rootEntries, path: params.path ?? "" });
        case "miniapp.files.search":
          return reply({ entries: projectEntries });
        case "miniapp.files.share":
          return reply({ url: `https://files.example/${encodeURIComponent(String(params.path))}` });
        default:
          return reply({});
      }
    }),
  );
});

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("FilesPane", () => {
  it("opens the exact file carried by a unified-search deep link", async () => {
    renderWithProviders(
      <>
        <FileTargetOpener />
        <FilesPane />
      </>,
      { connected: true },
    );

    await userEvent.click(screen.getByRole("button", { name: "검색 파일 열기" }));
    expect(await screen.findByRole("dialog", { name: "notes.md" })).toBeInTheDocument();
    expect(screen.getByText("원본 내용")).toBeInTheDocument();
  });

  it("refuses a known oversized unified-search target before starting its download", async () => {
    renderWithProviders(
      <>
        <OversizedFileTargetOpener />
        <FilesPane />
      </>,
      { connected: true },
    );

    await userEvent.click(screen.getByRole("button", { name: "큰 검색 파일 열기" }));
    expect(await screen.findByRole("dialog", { name: "oversized.md" })).toBeInTheDocument();
    expect(downloadCalls).toBe(0);
  });

  it("when hydrates the root folder from cache before the gateway refresh finishes", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );
    localStorage.setItem(
      cachedRpcStorageKey("files", rpcCacheKey(FILES_RPC.list, { path: "", limit: 300 })),
      JSON.stringify({
        data: { entries: [{ tag: "file", name: "cached-contract.pdf", pathDisplay: "cached-contract.pdf" }], path: "" },
        savedAt: Date.now(),
      }),
    );

    renderWithProviders(<FilesPane />, { connected: true });

    expect(screen.getAllByText("cached-contract.pdf")[0]).toBeInTheDocument();
  });

  it("when lists folders and drills into a selected folder", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await userEvent.click((await screen.findAllByText("projects"))[0]);

    expect(await screen.findByText("quarter-review.pdf")).toBeInTheDocument();
    expect(screen.getByLabelText("파일 경로")).toHaveValue("projects");
  });

  it("uploads files with an inferred MIME type when File.type is empty", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await screen.findAllByText("projects");
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, new File(["fake pdf"], "contract.pdf", { type: "" }));

    await screen.findByText("업로드됨");
    const upload = rpcCalls.find((c) => c.method === FILES_RPC.upload);
    expect(upload?.params).toMatchObject({
      path: "contract.pdf",
      mimeType: "application/pdf",
      dataBase64: "ZmFrZSBwZGY=",
    });
  });

  it("searches files and displays a share link", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await screen.findAllByText("projects");
    await userEvent.type(screen.getByPlaceholderText("파일 검색..."), "quarter");
    await userEvent.click(screen.getByRole("button", { name: "검색" }));
    expect(await screen.findByText("quarter-review.pdf")).toBeInTheDocument();

    await userEvent.click(screen.getAllByRole("button", { name: "공유" })[0]);

    expect(await screen.findByText(/https:\/\/files.example/)).toBeInTheDocument();
  });

  it("opens a file in the preview popup and saves edits with overwrite", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await userEvent.click((await screen.findAllByText("projects"))[0]);
    // Clicking a file row pops up the preview modal (the markdown body loads).
    await userEvent.click(await screen.findByText("notes.md"));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: /notes\.md/ })).toBeInTheDocument();

    // Wait for the blob to load (mode buttons appear once ready), then edit.
    await userEvent.click(await within(dialog).findByRole("button", { name: "편집" }, { timeout: 3000 }));
    const editor = (await within(dialog).findByDisplayValue(/원본 내용/)) as HTMLTextAreaElement;
    await userEvent.type(editor, "추가");
    expect(within(dialog).getByText("수정됨")).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));
    // Save round-trips through the upload RPC with overwrite=true (replace in
    // place — the default autorenames and would fork the file per save).
    await vi.waitFor(() => {
      const save = rpcCalls.find((c) => c.method === FILES_RPC.upload);
      expect(save?.params).toMatchObject({ path: "projects/notes.md", overwrite: true });
      expect(String(save?.params.dataBase64 ?? "")).not.toBe("");
    });
    // The viewer bar's dirty badge flips back to 저장됨.
    expect(document.querySelector(".wiki-save-state")?.textContent).toBe("저장됨");

    // Closing the popup (now clean) dismisses it — no unsaved-changes guard.
    await userEvent.click(within(dialog).getByRole("button", { name: "닫기" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("guards the preview popup against discarding unsaved edits", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await userEvent.click((await screen.findAllByText("projects"))[0]);
    await userEvent.click(await screen.findByText("notes.md"));
    const dialog = await screen.findByRole("dialog");

    // Edit without saving, then try to close: the dirty guard intercepts.
    await userEvent.click(await within(dialog).findByRole("button", { name: "편집" }, { timeout: 3000 }));
    await userEvent.type(await within(dialog).findByDisplayValue(/원본 내용/), "추가");
    await userEvent.click(within(dialog).getByRole("button", { name: "닫기" }));

    // The preview stays open behind a confirm; 계속 편집 dismisses the guard only.
    expect(await screen.findByText(/저장하지 않은 변경이 있습니다/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "계속 편집" }));
    expect(within(await screen.findByRole("dialog")).getByRole("heading", { name: /notes\.md/ })).toBeInTheDocument();

    // 버리고 닫기 discards and closes.
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "닫기" }));
    await userEvent.click(await screen.findByRole("button", { name: "버리고 닫기" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
