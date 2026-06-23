import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { SearchPane } from "./SearchPane";

// search.all is query-driven (callRpc → fetch); stub it with the gateway's
// fanned-out { wiki, diary, people } shape.
beforeEach(() => {
  if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        ({
          ok: true,
          json: async () => ({
            ok: true,
            payload: {
              wiki: [{ path: "projects/andromeda", title: "Andromeda 설계 노트", snippet: "3분할 워크스테이션" }],
              diary: [],
              people: [],
            },
          }),
        }) as unknown as Response,
    ),
  );
});
afterEach(() => vi.unstubAllGlobals());

describe("SearchPane", () => {
  it("renders results with a path as clickable (openable) hits", async () => {
    renderWithProviders(<SearchPane />, { connected: true });
    await userEvent.type(screen.getByPlaceholderText(/통합 검색/), "설계{enter}");

    const hit = await screen.findByRole("button", { name: /Andromeda 설계 노트/ });
    expect(hit).toHaveAttribute("title", "페이지 열기");
    // Clicking routes to the wiki page via openWiki — should not throw.
    await userEvent.click(hit);
  });
});
