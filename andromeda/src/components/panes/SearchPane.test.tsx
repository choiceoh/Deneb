import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { SearchPane } from "./SearchPane";

// search.all is query-driven (callRpc → fetch); stub it with the gateway's
// fanned-out { wiki, diary, people } shape.
beforeEach(() => {
  localStorage.clear();
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
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("SearchPane", () => {
  it("centers the box until a search, then rises and shows clickable hits", async () => {
    renderWithProviders(<SearchPane />, { connected: true });

    // Centered (pre-search) state: the big hero title is shown, no results yet.
    expect(screen.getByText("통합 검색")).toBeInTheDocument();

    await userEvent.type(screen.getByPlaceholderText(/검색/), "설계{enter}");

    const hit = await screen.findByRole("button", { name: /Andromeda 설계 노트/ });
    expect(hit).toHaveAttribute("title", "페이지 열기");
    // After searching, the centered hero title is gone (the box rose to the top).
    expect(screen.queryByText("통합 검색")).not.toBeInTheDocument();
    // Clicking routes to the wiki page via openWiki — should not throw.
    await userEvent.click(hit);
  });
});
