import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { useWorkspace } from "@/workspaceContext";
import { NotebookPane } from "./NotebookPane";

// Stateful stand-in for the Deneb gateway notebook surface: create assigns an id,
// add_source/remove_source mutate in-test source lists, and get reflects the
// latest notebook so write round-trips show up in the UI.
let added: { cite: string; kind: string; title: string; text: string; ref: string }[];
let zttSources: { cite: string; kind: string; title: string; text: string; ref: string }[];
let uploaded: { filename: string; title: string; mimeType: string; dataBase64: string }[];
let createdName: string;
let notebookRows: { id: string; name: string; sourceCount: number; updated: number }[];

beforeEach(() => {
  added = [];
  uploaded = [];
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
        case "miniapp.notebook.add_file": {
          uploaded.push({
            filename: String(params.filename ?? ""),
            title: String(params.title ?? ""),
            mimeType: String(params.mimeType ?? ""),
            dataBase64: String(params.dataBase64 ?? ""),
          });
          // The gateway would extract text server-side; the in-test stub pins a file
          // source titled by the (optional) title, defaulting to the filename.
          const s = {
            cite: `S${added.length + 1}`,
            kind: "file",
            title: String(params.title || params.filename || ""),
            text: "추출된 본문",
            ref: String(params.filename ?? ""),
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
  it("when auto-opens the latest notebook; a source chip expands into the preview on click", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });
    // Picking is once-per-task → the freshest notebook (ZTT) auto-opens, no manual click.
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();
    // The sources read as a light chip strip — no content preview until asked.
    expect(screen.getByText("잔금 안내")).toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "자료 내용" })).not.toBeInTheDocument();
    // Click the chip → its full content expands below; close (×) folds it again.
    await userEvent.click(screen.getByRole("button", { name: /잔금 안내/ }));
    const detail = await screen.findByRole("group", { name: "자료 내용" });
    expect(within(detail).getByText(/최종 5% 잔금/)).toBeInTheDocument();
    await userEvent.click(within(detail).getByRole("button", { name: "미리보기 닫기" }));
    expect(screen.queryByRole("group", { name: "자료 내용" })).not.toBeInTheDocument();
  });

  it("cycles the materials area height — 기본 → 확대 → 접힘 → 기본 — and the choice saves", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();
    // Open a chip preview first — cycling the height must not lose it.
    await userEvent.click(screen.getByRole("button", { name: /잔금 안내/ }));
    expect(await screen.findByRole("group", { name: "자료 내용" })).toBeInTheDocument();

    // 기본 → 확대: body stays (still visible); the button now offers 접기.
    await userEvent.click(screen.getByRole("button", { name: "자료 영역 확대" }));
    expect(screen.getByRole("list", { name: "자료 목록" })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "자료 내용" })).toBeInTheDocument();
    expect(localStorage.getItem("andromeda.notebook.top")).toBe('"expanded"');

    // 확대 → 접힘: strip + preview disappear; only the one-line bar remains.
    await userEvent.click(screen.getByRole("button", { name: "자료 영역 접기" }));
    expect(screen.queryByRole("list", { name: "자료 목록" })).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "자료 내용" })).not.toBeInTheDocument();
    expect(localStorage.getItem("andromeda.notebook.top")).toBe('"folded"');

    // 접힘 → 기본: the exact view (open preview included) comes back.
    await userEvent.click(screen.getByRole("button", { name: "자료 영역 펼치기" }));
    expect(screen.getByRole("list", { name: "자료 목록" })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "자료 내용" })).toBeInTheDocument();
    expect(localStorage.getItem("andromeda.notebook.top")).toBe('"default"');
  });

  it("starts folded when the persisted state says so — new key and legacy migration", async () => {
    // New 3-state key.
    localStorage.setItem("andromeda.notebook.top", '"folded"');
    const { unmount } = renderWithProviders(<NotebookPane />, { connected: true });
    // The bar still works (notebook auto-opens into it); the body stays folded.
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "자료 목록" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "자료 영역 펼치기" })).toBeInTheDocument();
    unmount();

    // Legacy boolean flag (from before the 3-state height) migrates to 접힘.
    localStorage.removeItem("andromeda.notebook.top");
    localStorage.setItem("andromeda.notebook.folded", "true");
    renderWithProviders(<NotebookPane />, { connected: true });
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "자료 목록" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "자료 영역 펼치기" })).toBeInTheDocument();
  });

  it("when registers a note sink while a notebook is open — saving an AI answer pins a note source", async () => {
    // Consume the workspace channel the way AIPanel does: while NotebookPane has a
    // notebook open it registers a sink; feeding it an answer pins a kind=note source.
    let sink: ((text: string) => void) | null = null;
    function SinkProbe() {
      sink = useWorkspace().noteSink;
      return null;
    }
    renderWithProviders(
      <>
        <NotebookPane />
        <SinkProbe />
      </>,
      { connected: true },
    );
    expect(await screen.findByRole("heading", { name: "ZTT" })).toBeInTheDocument();
    await waitFor(() => expect(sink).not.toBeNull());

    sink!("잔금 일정 요약: 최종 5%는 6/25 마감.");
    // The saved answer lands as a cited note source and shows up as a new chip
    // (titleless note → first-line snippet stands in as the title).
    await waitFor(() =>
      expect(added.at(-1)).toMatchObject({ kind: "note", text: expect.stringContaining("잔금 일정 요약") }),
    );
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

  it("when pins a wiki page as a source — expands the supported source kinds", async () => {
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

  it("when attaches a picked document — uploads its bytes for server-side extraction, no path typed", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "파일 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "파일 딜" })).toBeInTheDocument();

    // + 인용자료 → 파일 kind → a native file picker replaces the path text field.
    await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
    await userEvent.click(screen.getByRole("button", { name: "파일" }));
    await userEvent.type(screen.getByLabelText("제목 (선택)"), "계약서");
    expect(screen.queryByLabelText("파일 경로")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "파일 선택" })).toBeInTheDocument();

    // Pick a document → its name/size shows and 추가 enables (no path to type).
    const file = new File(["계약서 본문 내용"], "topsolar.pdf", { type: "application/pdf" });
    await userEvent.upload(document.querySelector('input[type="file"]') as HTMLInputElement, file);
    expect(await screen.findByText(/topsolar\.pdf/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "추가" }));

    // add_file carried the raw bytes + filename + mime — NOT a typed path.
    const last = uploaded.at(-1);
    expect(last).toMatchObject({ filename: "topsolar.pdf", title: "계약서", mimeType: "application/pdf" });
    expect(last?.dataBase64.length).toBeGreaterThan(0);
    // The pinned file source shows up after the reload.
    expect((await screen.findAllByText("계약서")).length).toBeGreaterThan(0);
  });

  it("attaches multiple picked files at once — one add_file per file", async () => {
    renderWithProviders(<NotebookPane />, { connected: true });

    await userEvent.click(await screen.findByRole("button", { name: "새 노트북" }));
    await userEvent.type(screen.getByLabelText("이름"), "파일 딜");
    await userEvent.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "파일 딜" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
    await userEvent.click(screen.getByRole("button", { name: "파일" }));

    // Two files of different classes: a document and an image (routes to OCR server-side).
    const doc = new File(["견적 본문"], "quote.pdf", { type: "application/pdf" });
    const img = new File(["사진 바이트"], "scan.png", { type: "image/png" });
    await userEvent.upload(document.querySelector('input[type="file"]') as HTMLInputElement, [doc, img]);
    expect(await screen.findByText(/quote\.pdf/)).toBeInTheDocument();
    expect(screen.getByText(/scan\.png/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "추가" }));

    // One add_file call per file; a shared title is dropped when there are several.
    await waitFor(() => expect(uploaded).toHaveLength(2));
    expect(uploaded.map((u) => u.filename).sort()).toEqual(["quote.pdf", "scan.png"]);
    expect(uploaded.every((u) => u.title === "")).toBe(true);
  });

  it("rejects an unsupported file with an in-dialog notice and no upload", async () => {
    // applyAccept:false simulates a user who bypasses the dialog's format filter
    // (some OSes allow "All files") — the client-side guard must still refuse it.
    const user = userEvent.setup({ applyAccept: false });
    renderWithProviders(<NotebookPane />, { connected: true });

    await user.click(await screen.findByRole("button", { name: "새 노트북" }));
    await user.type(screen.getByLabelText("이름"), "파일 딜");
    await user.click(screen.getByRole("button", { name: "생성" }));
    expect(await screen.findByRole("heading", { name: "파일 딜" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "자료 추가" }));
    await user.click(screen.getByRole("button", { name: "파일" }));

    const bad = new File(["MZ..."], "installer.exe", { type: "application/octet-stream" });
    await user.upload(document.querySelector('input[type="file"]') as HTMLInputElement, bad);

    expect(await screen.findByText(/지원하지 않는 형식/)).toBeInTheDocument();
    // 추가 stays disabled and nothing was uploaded.
    expect(screen.getByRole("button", { name: "추가" })).toBeDisabled();
    expect(uploaded).toHaveLength(0);
  });

  it("when removes a source from the open notebook", async () => {
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
