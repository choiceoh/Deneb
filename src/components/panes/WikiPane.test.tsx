import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as fx from "@/mocks/fixtures";
import { renderWithProviders } from "@/test/util";
import { WikiPane } from "./WikiPane";

let writeParams: Record<string, unknown> | null;
let createParams: Record<string, unknown> | null;
let moveParams: Record<string, unknown> | null;

beforeEach(() => {
  localStorage.clear();
  writeParams = null;
  createParams = null;
  moveParams = null;
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
        case "miniapp.memory.categories":
          return reply({ categories: fx.wikiCategories, totalPages: fx.pages.length });
        case "miniapp.memory.list_in_category":
          return reply({
            category: params.category,
            pages: fx.pages.filter((p) => String(p.path ?? "").startsWith(`${params.category}/`)),
            total: 1,
          });
        case "miniapp.memory.diary_recent":
          return reply({ entries: fx.diaryEntries });
        case "miniapp.memory.search":
          return reply({ results: fx.pages });
        case "miniapp.memory.get_page":
          return reply({ path: params.path, title: "Andromeda 설계 노트", body: "# 설계\n\n본문 내용입니다." });
        case "miniapp.memory.write_page":
          writeParams = params;
          return reply({ path: params.path, body: params.body });
        case "miniapp.memory.create_page":
          createParams = params;
          return reply({ path: "projects/new-page.md", title: params.title, body: params.body ?? "" });
        case "miniapp.memory.move_page":
          moveParams = params;
          return reply({ ok: true, from: params.from, to: params.to });
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

describe("WikiPane", () => {
  it("unwraps search { results }, opens the body, and saves under `body`", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.type(screen.getByPlaceholderText("위키 검색..."), "설계{enter}");
    const hit = await screen.findByRole("button", { name: /Andromeda 설계 노트/ });
    await userEvent.click(hit);
    const editor = await screen.findByDisplayValue(/본문 내용입니다/);
    expect(screen.getByText("저장됨")).toBeInTheDocument();

    await userEvent.type(editor, " 추가됨");
    expect(screen.getByText("수정됨")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "저장" }));
    expect(await screen.findByText("저장됨")).toBeInTheDocument();
    expect(writeParams).toMatchObject({ path: "projects/andromeda" });
    expect(String(writeParams?.body)).toContain("본문 내용입니다");
    expect(writeParams).not.toHaveProperty("content");
  });

  it("creates a new page through title/category and opens it in the editor", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.click(screen.getByRole("button", { name: /새 페이지/ }));
    await userEvent.type(screen.getByPlaceholderText(/Andromeda 개선 노트/), "새 페이지");
    await userEvent.type(screen.getByPlaceholderText(/projects/), "projects");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));

    expect(await screen.findByRole("heading", { name: "projects/new-page.md" })).toBeInTheDocument();
    expect(createParams).toMatchObject({ title: "새 페이지", category: "projects" });
    expect(createParams).not.toHaveProperty("path");
  });

  it("opens on category browse and can switch to recent diary", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: /projects/ }));
    expect(await screen.findByRole("button", { name: /Andromeda 설계 노트/ })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "최근 일지" }));
    expect(await screen.findByRole("button", { name: /2026-06-17/ })).toBeInTheDocument();
  });

  it("moves the selected page by clicking a destination category", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.type(screen.getByPlaceholderText("위키 검색..."), "설계{enter}");
    await userEvent.click(await screen.findByRole("button", { name: /Andromeda 설계 노트/ }));
    await userEvent.click(await screen.findByRole("button", { name: "이동" }));

    // Pick the destination folder by clicking it; the page keeps its name.
    const dialog = screen.getByRole("dialog", { name: "페이지 이동" });
    await userEvent.click(within(dialog).getByRole("button", { name: "team" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "이동" }));

    expect(await screen.findByText("이동됨")).toBeInTheDocument();
    expect(moveParams).toMatchObject({ from: "projects/andromeda", to: "team/andromeda" });
  });

  it("guards unsaved edits before opening another wiki page", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.type(screen.getByPlaceholderText("위키 검색..."), "설계{enter}");
    await userEvent.click(await screen.findByRole("button", { name: /Andromeda 설계 노트/ }));
    const editor = await screen.findByDisplayValue(/본문 내용입니다/);
    await userEvent.type(editor, " 임시 수정");

    await userEvent.click(screen.getByRole("button", { name: /팀 온보딩 가이드/ }));
    const dialog = await screen.findByRole("dialog", { name: "저장하지 않은 변경" });
    expect(dialog).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "projects/andromeda" })).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "계속 편집" }));
    expect(screen.queryByRole("dialog", { name: "저장하지 않은 변경" })).not.toBeInTheDocument();
    expect(screen.getByDisplayValue(/임시 수정/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /팀 온보딩 가이드/ }));
    await userEvent.click(
      within(await screen.findByRole("dialog", { name: "저장하지 않은 변경" })).getByRole("button", {
        name: "버리고 열기",
      }),
    );

    expect(await screen.findByRole("heading", { name: "team/onboarding" })).toBeInTheDocument();
    expect(screen.queryByDisplayValue(/임시 수정/)).not.toBeInTheDocument();
  });

  it("toggles a Markdown preview of the page body", async () => {
    renderWithProviders(<WikiPane />, { connected: true });

    await userEvent.type(screen.getByPlaceholderText("위키 검색..."), "설계{enter}");
    await userEvent.click(await screen.findByRole("button", { name: /Andromeda 설계 노트/ }));
    await screen.findByDisplayValue(/본문 내용입니다/);

    await userEvent.click(screen.getByRole("button", { name: "미리보기" }));
    const preview = screen.getByLabelText("위키 미리보기");
    expect(within(preview).getByRole("heading", { name: "설계" })).toBeInTheDocument();
    expect(within(preview).getByText(/본문 내용입니다/)).toBeInTheDocument();
  });
});
