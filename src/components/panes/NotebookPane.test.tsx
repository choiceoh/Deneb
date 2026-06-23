import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { NotebookPane } from "./NotebookPane";

beforeEach(() => {
  if (!globalThis.crypto?.randomUUID) {
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  }
  // Stand in for the Deneb gateway: notebook.list → summaries, notebook.get →
  // the deal's cited sources (the real miniapp.notebook.* wire shapes).
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const { method } = JSON.parse(String(init?.body ?? "{}")) as { method: string };
      const reply = (payload: unknown) =>
        ({ ok: true, json: async () => ({ ok: true, payload }) }) as unknown as Response;
      switch (method) {
        case "miniapp.notebook.list":
          return reply({ notebooks: [{ id: "ztt", name: "ZTT", sourceCount: 1, updated: 1782190313958 }] });
        case "miniapp.notebook.get":
          return reply({
            id: "ztt",
            name: "ZTT",
            dealRef: "프로젝트/거래/ztt.md",
            sources: [{ cite: "S1", kind: "note", title: "잔금 안내", text: "최종 5% 잔금 $401K, 마감 6/25." }],
          });
        default:
          return reply({});
      }
    }),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("NotebookPane", () => {
  it("lists deal notebooks and opens one to show its cited sources", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });
    // The notebook list shows immediately — click one to load its sources.
    await userEvent.click(await screen.findByRole("button", { name: /ZTT/ }));
    expect(await screen.findByText("잔금 안내")).toBeInTheDocument();
    expect(screen.getByText(/최종 5% 잔금/)).toBeInTheDocument();
  });
});
