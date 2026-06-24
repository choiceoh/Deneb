import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { NotebookPane } from "./NotebookPane";

// Stateful stand-in for the Deneb gateway notebook surface: create assigns an id,
// add_source pins to an in-test list, and get reflects what's been added so the
// write round-trip (create → add_source → get) shows up in the UI.
let added: { cite: string; kind: string; title: string; text: string; ref: string }[];
let createdName: string;
let notebookRows: { id: string; name: string; sourceCount: number; updated: number }[];

beforeEach(() => {
  added = [];
  createdName = "";
  notebookRows = [{ id: "ztt", name: "ZTT", sourceCount: 1, updated: 1782190313958 }];
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
          return reply({ notebooks: notebookRows });
        case "miniapp.notebook.create":
          createdName = String(params.name);
          notebookRows = [{ id: "nb-new", name: createdName, sourceCount: 0, updated: 2 }, ...notebookRows];
          return reply({ id: "nb-new", name: createdName, sourceCount: 0, updated: 2 });
        case "miniapp.notebook.delete":
          notebookRows = notebookRows.filter((notebook) => notebook.id !== params.id);
          return reply({ deleted: true, id: params.id });
        case "miniapp.notebook.add_source": {
          const s = {
            cite: `S${added.length + 1}`,
            kind: String(params.kind ?? "note"),
            title: String(params.title ?? ""),
            text: String(params.text ?? ""),
            ref: String(params.ref ?? ""),
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

  it("pins a wiki page as a source — expands the supported source kinds", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "위키 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "위키 딜" })).toBeInTheDocument();

    // + 인용자료 → switch the kind to 위키 → a path field replaces the note textarea.
    await userEvent.click(screen.getByRole("button", { name: "인용자료 추가" }));
    await userEvent.click(screen.getByRole("button", { name: "위키 페이지" }));
    await userEvent.type(screen.getByLabelText("제목 (선택)"), "탑솔라");
    await userEvent.type(screen.getByLabelText("위키 경로"), "프로젝트/topsolar.md");
    await userEvent.click(screen.getByRole("button", { name: "추가" }));

    // add_source carried kind=wiki + ref (a wiki page), not a pasted note.
    expect(added.at(-1)).toMatchObject({ kind: "wiki", ref: "프로젝트/topsolar.md", title: "탑솔라" });
    expect(await screen.findByText("탑솔라")).toBeInTheDocument();
  });

  it("deletes the open notebook after confirmation", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: /ZTT/ }));
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "노트북 삭제" }));
    expect(screen.getByRole("dialog", { name: "노트북 삭제" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "삭제" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: "ZTT" })).not.toBeInTheDocument());
    expect(await screen.findByText(/노트북이 없습니다/)).toBeInTheDocument();
  });
});
