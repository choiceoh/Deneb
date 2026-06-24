import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FILES_RPC } from "@/resources";
import { cachedRpcStorageKey, rpcCacheKey } from "@/rpcCache";
import type { FileEntry } from "@/types";
import { renderWithProviders } from "@/test/util";
import { FilesPane } from "./FilesPane";

const rootEntries: FileEntry[] = [{ tag: "folder", name: "projects", pathDisplay: "projects" }];
const projectEntries: FileEntry[] = [
  {
    tag: "file",
    name: "quarter-review.pdf",
    pathDisplay: "projects/quarter-review.pdf",
    size: 245_760,
    serverModified: "2026-06-17T09:00:00Z",
  },
];

beforeEach(() => {
  localStorage.clear();
  if (!globalThis.crypto?.randomUUID) {
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const { method, params } = JSON.parse(String(init?.body ?? "{}")) as {
        method: string;
        params: Record<string, unknown>;
      };
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
  it("hydrates the root folder from cache before the gateway refresh finishes", () => {
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

  it("lists folders and drills into a selected folder", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await userEvent.click((await screen.findAllByText("projects"))[0]);

    expect(await screen.findByText("quarter-review.pdf")).toBeInTheDocument();
    expect(screen.getByLabelText("파일 경로")).toHaveValue("projects");
  });

  it("searches files and shows a share link", async () => {
    renderWithProviders(<FilesPane />, { connected: true });

    await screen.findAllByText("projects");
    await userEvent.type(screen.getByPlaceholderText("파일 검색..."), "quarter");
    await userEvent.click(screen.getByRole("button", { name: "검색" }));
    expect(await screen.findByText("quarter-review.pdf")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "공유" }));

    expect(await screen.findByText(/https:\/\/files.example/)).toBeInTheDocument();
  });
});
