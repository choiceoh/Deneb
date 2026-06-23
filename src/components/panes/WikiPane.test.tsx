import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as fx from "@/mocks/fixtures";
import { renderWithProviders } from "@/test/util";
import { WikiPane } from "./WikiPane";

// Params of the last write_page / create_page RPC, captured for field assertions.
let writeParams: Record<string, unknown> | null;
let createParams: Record<string, unknown> | null;

beforeEach(() => {
  writeParams = null;
  createParams = null;
  if (!globalThis.crypto?.randomUUID) {
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  }
  // Stand in for the Deneb gateway, replying with the REAL envelope + payload
  // shapes (search → { results }, get_page/write_page → { body }) so this test
  // pins the field-name contract the wiki pane regressed on.
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
        case "miniapp.memory.search":
          return reply({ results: fx.pages });
        case "miniapp.memory.get_page":
          return reply({ path: params.path, title: "Andromeda 설계 노트", body: "# 설계\n\n본문 내용입니다." });
        case "miniapp.memory.write_page":
          writeParams = params;
          return reply({ path: params.path, body: params.body });
        case "miniapp.memory.create_page":
          createParams = params;
          return reply({ path: params.path, title: params.path, body: "" });
        default:
          return reply({});
      }
    }),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("WikiPane", () => {
  it("unwraps search { results }, opens the body, and saves under `body`", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    // Search → results render. Regression: it read `.pages`, so { results } left
    // the list empty and the pane only ever showed "결과 없음".
    await userEvent.type(screen.getByPlaceholderText("위키 검색…"), "설계{enter}");
    const hit = await screen.findByRole("button", { name: /Andromeda 설계 노트/ });

    // Open → editor shows the page body. Regression: it read `.content`, so the
    // editor opened blank against a gateway that returns `body`.
    await userEvent.click(hit);
    const editor = await screen.findByDisplayValue(/본문 내용입니다/);

    // Edit + save → write_page must carry the text under `body`. Regression:
    // sending it as `content` left body empty server-side, clobbering the page.
    await userEvent.type(editor, " 추가됨");
    await userEvent.click(screen.getByRole("button", { name: "저장" }));
    expect(await screen.findByText("저장됨")).toBeInTheDocument();
    expect(writeParams).toMatchObject({ path: "projects/andromeda" });
    expect(String(writeParams?.body)).toContain("본문 내용입니다");
    expect(writeParams).not.toHaveProperty("content");
  });

  it("creates a new page and opens it in the editor", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.click(screen.getByRole("button", { name: /새 페이지/ }));
    await userEvent.type(screen.getByPlaceholderText(/projects\/andromeda/), "projects/new");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));

    // create_page carries the path; then the editor opens that page (h2 shows it).
    expect(await screen.findByRole("heading", { name: "projects/new" })).toBeInTheDocument();
    expect(createParams).toMatchObject({ path: "projects/new" });
  });
});
