import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { NotebookPane } from "./NotebookPane";

// Stateful stand-in for the Deneb gateway notebook surface: create assigns an id,
// add_source pins to an in-test list, and get reflects what's been added so the
// write round-trip (create → add_source → get) shows up in the UI.
let added: { cite: string; kind: string; title: string; text: string }[];
let createdName: string;

beforeEach(() => {
  added = [];
  createdName = "";
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
        case "miniapp.notebook.list":
          return reply({ notebooks: [{ id: "ztt", name: "ZTT", sourceCount: 1, updated: 1782190313958 }] });
        case "miniapp.notebook.create":
          createdName = String(params.name);
          return reply({ id: "nb-new", name: createdName, sourceCount: 0, updated: 2 });
        case "miniapp.notebook.add_source": {
          const s = {
            cite: `S${added.length + 1}`,
            kind: "note",
            title: String(params.title ?? ""),
            text: String(params.text ?? ""),
          };
          added.push(s);
          return reply(s);
        }
        case "miniapp.notebook.get":
          if (params.id === "ztt")
            return reply({
              id: "ztt",
              name: "ZTT",
              dealRef: "프로젝트/거래/ztt.md",
              sources: [{ cite: "S1", kind: "note", title: "잔금 안내", text: "최종 5% 잔금 $401K, 마감 6/25." }],
            });
          return reply({ id: params.id, name: createdName || String(params.id), sources: added });
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

describe("NotebookPane", () => {
  it("lists deal notebooks and opens one to show its cited sources", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });
    // The notebook list shows immediately — click one to load its sources.
    await userEvent.click(await screen.findByRole("button", { name: /ZTT/ }));
    expect(await screen.findByText("잔금 안내")).toBeInTheDocument();
    expect(screen.getByText(/최종 5% 잔금/)).toBeInTheDocument();
  });

  it("creates a notebook and pins a citation source", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    // + 노트북 → create form → the new notebook opens.
    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "신규 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "신규 딜" })).toBeInTheDocument();

    // + 인용자료 → pin a pasted note → it renders as a source card.
    await userEvent.click(screen.getByRole("button", { name: "인용자료 추가" }));
    await userEvent.type(screen.getByLabelText("내용"), "잔금 6/25 마감.");
    await userEvent.click(screen.getByRole("button", { name: "추가" }));
    expect(await screen.findByText(/잔금 6\/25/)).toBeInTheDocument();
  });
});
