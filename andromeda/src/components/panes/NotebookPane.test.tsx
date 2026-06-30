import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { NotebookPane } from "./NotebookPane";

// Stateful stand-in for the Deneb gateway notebook surface: create assigns an id,
// add_source/remove_source mutate in-test source lists, and get reflects the
// latest notebook so write round-trips show up in the UI.
let added: { cite: string; kind: string; title: string; text: string; ref: string }[];
let zttSources: { cite: string; kind: string; title: string; text: string; ref: string }[];
let createdName: string;
let notebookRows: { id: string; name: string; sourceCount: number; updated: number }[];

beforeEach(() => {
  added = [];
  zttSources = [{ cite: "S1", kind: "note", title: "잔금 안내", text: "최종 5% 잔금 $401K, 마감 6/25.", ref: "" }];
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
          notebookRows = notebookRows.map((notebook) =>
            notebook.id === params.id ? { ...notebook, sourceCount: added.length, updated: 3 } : notebook,
          );
          return reply(s);
        }
        case "miniapp.notebook.remove_source": {
          const cite = String(params.cite ?? "");
          if (params.id === "ztt") {
            zttSources = zttSources.filter((source) => source.cite !== cite);
            notebookRows = notebookRows.map((notebook) =>
              notebook.id === "ztt" ? { ...notebook, sourceCount: zttSources.length, updated: 4 } : notebook,
            );
            return reply({
              id: "ztt",
              name: "ZTT",
              dealRef: "프로젝트/거래/ztt.md",
              sources: zttSources,
              updated: 4,
            });
          }
          added = added.filter((source) => source.cite !== cite);
          notebookRows = notebookRows.map((notebook) =>
            notebook.id === params.id ? { ...notebook, sourceCount: added.length, updated: 4 } : notebook,
          );
          return reply({ id: params.id, name: createdName || String(params.id), sources: added, updated: 4 });
        }
        case "miniapp.notebook.get":
          if (params.id === "ztt")
            return reply({
              id: "ztt",
              name: "ZTT",
              dealRef: "프로젝트/거래/ztt.md",
              sources: zttSources,
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
  it("auto-opens the latest notebook and shows the first source in the detail pane", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });
    // Picking is once-per-task → the freshest notebook (ZTT) auto-opens, no manual click.
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();
    // Master-detail: the first source auto-selects, so its content shows on the right.
    const detail = await screen.findByRole("group", { name: "자료 내용" });
    expect(within(detail).getByText(/최종 5% 잔금/)).toBeInTheDocument();
  });

  it("creates a notebook and pins a citation source", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    // + 노트북 → create form → the new notebook opens.
    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "신규 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "신규 딜" })).toBeInTheDocument();

    // + 인용자료 → pin a pasted note → it renders as a source card.
    await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
    await userEvent.type(screen.getByLabelText("내용"), "잔금 6/25 마감.");
    await userEvent.click(screen.getByRole("button", { name: "추가" }));
    // The pinned note appears (titleless note → a text snippet stands in as its title);
    // it shows in both the list row and the auto-selected detail pane.
    expect((await screen.findAllByText(/잔금 6\/25/)).length).toBeGreaterThan(0);
  });

  it("pins a wiki page as a source — expands the supported source kinds", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "위키 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "위키 딜" })).toBeInTheDocument();

    // + 인용자료 → switch the kind to 위키 → a path field replaces the note textarea.
    await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
    await userEvent.click(screen.getByRole("button", { name: "위키" }));
    await userEvent.type(screen.getByLabelText("제목 (선택)"), "탑솔라");
    await userEvent.type(screen.getByLabelText("위키 경로"), "프로젝트/topsolar.md");
    await userEvent.click(screen.getByRole("button", { name: "추가" }));

    // add_source carried kind=wiki + ref (a wiki page), not a pasted note.
    expect(added.at(-1)).toMatchObject({ kind: "wiki", ref: "프로젝트/topsolar.md", title: "탑솔라" });
    expect((await screen.findAllByText("탑솔라")).length).toBeGreaterThan(0);
  });

  it("pins gateway-ingested source kinds by ref", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "파일 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "파일 딜" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
    await userEvent.click(screen.getByRole("button", { name: "파일" }));
    await userEvent.type(screen.getByLabelText("제목 (선택)"), "계약서");
    await userEvent.type(screen.getByLabelText("파일 경로"), "contracts/topsolar.pdf");
    await userEvent.click(screen.getByRole("button", { name: "추가" }));

    expect(added.at(-1)).toMatchObject({ kind: "file", ref: "contracts/topsolar.pdf", title: "계약서" });
    expect((await screen.findAllByText("계약서")).length).toBeGreaterThan(0);
  });

  it("removes a source from the open notebook", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    // ZTT auto-opens; its source row carries the delete control.
    await userEvent.click(await screen.findByRole("button", { name: "인용자료 삭제 S1" }));
    expect(screen.getByRole("dialog", { name: "인용자료 삭제" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "삭제" }));

    await waitFor(() => expect(screen.queryByText("잔금 안내")).not.toBeInTheDocument());
    expect(await screen.findByText(/아직 자료가 없습니다/)).toBeInTheDocument();
    expect(notebookRows[0].sourceCount).toBe(0);
  });

  it("deletes the open notebook after confirmation", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    // ZTT auto-opens.
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "노트북 삭제" }));
    expect(screen.getByRole("dialog", { name: "노트북 삭제" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "삭제" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: "ZTT" })).not.toBeInTheDocument());
    expect(await screen.findByText(/노트북이 없습니다/)).toBeInTheDocument();
  });
});
