import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { FileViewer } from "./FileViewer";
import { maxHwpBytes, maxTextPreviewBytes } from "./fileView";

function textBlob(text: string, type = "text/plain"): Blob {
  return new Blob([text], { type });
}

beforeEach(() => {
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: vi.fn(() => "blob:test-preview"),
    revokeObjectURL: vi.fn(),
  });
});

afterEach(() => vi.unstubAllGlobals());

describe("FileViewer loading states", () => {
  it("shows loading while an asynchronous blob is pending", () => {
    render(<FileViewer name="notes.txt" load={() => new Promise(() => {})} />);
    expect(screen.getByText("불러오는 중...")).toBeInTheDocument();
  });

  it("does not call load for an unsupported file", () => {
    const load = vi.fn(async () => textBlob("binary"));
    render(<FileViewer name="archive.zip" load={load} downloadUrl="https://files.test/archive.zip" />);

    expect(screen.getByText(/이 형식\(zip\)은 미리보기를 지원하지 않습니다/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toHaveAttribute("href", "https://files.test/archive.zip");
    expect(load).not.toHaveBeenCalled();
  });

  it("omits the download escape hatch when no URL exists", () => {
    render(<FileViewer name="archive.zip" load={async () => textBlob("binary")} />);
    expect(screen.queryByRole("link", { name: "다운로드" })).not.toBeInTheDocument();
  });

  it.each([
    ["large.txt", maxTextPreviewBytes + 1],
    ["large.hwp", maxHwpBytes + 1],
  ])("refuses known oversized %s before loading", (name, size) => {
    const load = vi.fn(async () => textBlob("too large"));
    render(<FileViewer name={name} size={size} load={load} downloadUrl="https://files.test/large" />);

    expect(screen.getByText(/파일이 미리보기 한도를 넘습니다/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toBeInTheDocument();
    expect(load).not.toHaveBeenCalled();
  });

  it.each([
    ["empty.txt", 0],
    ["unknown.txt", undefined],
    ["within.txt", maxTextPreviewBytes],
  ])("loads %s when the declared size does not exceed the cap", async (name, size) => {
    const load = vi.fn(async () => textBlob("body"));
    render(<FileViewer name={name} size={size} load={load} />);

    expect(await screen.findByRole("textbox", { name })).toHaveValue("body");
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("refuses an oversized text blob when its size was unknown beforehand", async () => {
    const blob = { size: maxTextPreviewBytes + 1, text: vi.fn() } as unknown as Blob;
    render(<FileViewer name="large.log" load={async () => blob} downloadUrl="https://files.test/large.log" />);

    expect(await screen.findByText(/파일이 미리보기 한도를 넘습니다/)).toBeInTheDocument();
    expect(blob.text).not.toHaveBeenCalled();
  });

  it("surfaces an Error message and a download fallback", async () => {
    render(
      <FileViewer
        name="broken.txt"
        load={async () => {
          throw new Error("gateway offline");
        }}
        downloadUrl="https://files.test/broken.txt"
      />,
    );

    expect(await screen.findByText("불러오기 실패: gateway offline")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toHaveAttribute("target", "_blank");
    expect(screen.getByRole("link", { name: "다운로드" })).toHaveAttribute("rel", "noreferrer");
  });

  it("stringifies a non-Error rejection", async () => {
    render(
      <FileViewer
        name="broken.txt"
        load={async () => {
          throw "plain failure";
        }}
      />,
    );
    expect(await screen.findByText("불러오기 실패: plain failure")).toBeInTheDocument();
  });

  it("ignores a late blob after unmount", async () => {
    let resolve!: (blob: Blob) => void;
    const load = () => new Promise<Blob>((done) => (resolve = done));
    const { unmount } = render(<FileViewer name="late.txt" load={load} />);
    unmount();
    resolve(textBlob("late body"));
    await Promise.resolve();
  });
});

describe("FileViewer media", () => {
  it("renders an image through a managed object URL", async () => {
    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: "image/png" });
    const { unmount } = render(<FileViewer name="photo.png" load={async () => blob} />);

    const image = await screen.findByRole("img", { name: "photo.png" });
    expect(image).toHaveAttribute("src", "blob:test-preview");
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob);

    unmount();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:test-preview");
  });

  it("re-stamps and embeds a mistyped PDF", async () => {
    const blob = textBlob("%PDF", "text/plain");
    const { container } = render(
      <FileViewer name="contract.pdf" load={async () => blob} downloadUrl="https://files.test/contract.pdf" />,
    );

    await waitFor(() => expect(container.querySelector("embed")).toHaveAttribute("src", "blob:test-preview"));
    const embedded = (URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls[0][0] as Blob;
    expect(embedded.type).toBe("application/pdf");
    expect(container.querySelector("embed")).toHaveAttribute("type", "application/pdf");
    expect(container.querySelector("embed")).toHaveAttribute("title", "contract.pdf");
    expect(screen.getByRole("link", { name: "다운로드" })).toBeInTheDocument();
  });

  it("does not render edit controls for binary media", async () => {
    render(
      <FileViewer name="photo.png" load={async () => new Blob(["image"], { type: "image/png" })} onSave={vi.fn()} />,
    );
    await screen.findByRole("img");
    expect(screen.queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });
});

describe("FileViewer text editing", () => {
  it("renders a read-only plain-text preview without save controls", async () => {
    render(<FileViewer name="notes.txt" load={async () => textBlob("read only")} />);

    const editor = await screen.findByRole("textbox", { name: "notes.txt" });
    expect(editor).toHaveValue("read only");
    expect(editor).toHaveAttribute("readonly");
    expect(editor).toHaveAttribute("spellcheck", "false");
    expect(screen.queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
  });

  it("starts an editable text file in a clean saved state", async () => {
    render(<FileViewer name="notes.txt" load={async () => textBlob("original")} onSave={vi.fn()} />);

    await screen.findByDisplayValue("original");
    expect(screen.getByText("저장됨")).not.toHaveClass("dirty");
    expect(screen.getByRole("button", { name: "저장" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "되돌리기" })).toBeDisabled();
    expect(screen.getByRole("textbox")).not.toHaveAttribute("readonly");
  });

  it("tracks dirty state and notifies the parent on transitions", async () => {
    const onDirtyChange = vi.fn();
    render(
      <FileViewer
        name="notes.txt"
        load={async () => textBlob("original")}
        onSave={vi.fn()}
        onDirtyChange={onDirtyChange}
      />,
    );
    const editor = await screen.findByRole("textbox");
    await userEvent.clear(editor);
    await userEvent.type(editor, "changed");

    expect(screen.getByText("수정됨")).toHaveClass("dirty");
    expect(screen.getByRole("button", { name: "저장" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "되돌리기" })).toBeEnabled();
    expect(onDirtyChange).toHaveBeenCalledWith(true);
  });

  it("reverts unsaved edits to the last saved body", async () => {
    render(<FileViewer name="notes.txt" load={async () => textBlob("original")} onSave={vi.fn()} />);
    const editor = await screen.findByRole("textbox");
    await userEvent.clear(editor);
    await userEvent.type(editor, "changed");

    await userEvent.click(screen.getByRole("button", { name: "되돌리기" }));

    expect(editor).toHaveValue("original");
    expect(screen.getByText("저장됨")).toBeInTheDocument();
  });

  it("commits successful saves as the new revert point", async () => {
    const onSave = vi.fn(async () => true);
    render(<FileViewer name="notes.txt" load={async () => textBlob("original")} onSave={onSave} />);
    const editor = await screen.findByRole("textbox");
    await userEvent.clear(editor);
    await userEvent.type(editor, "saved revision");

    await userEvent.click(screen.getByRole("button", { name: "저장" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledWith("saved revision"));
    expect(screen.getByText("저장됨")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "저장" })).toBeDisabled();
  });

  it("keeps failed saves dirty and re-enables actions", async () => {
    const onSave = vi.fn(async () => false);
    render(<FileViewer name="notes.txt" load={async () => textBlob("original")} onSave={onSave} />);
    const editor = await screen.findByRole("textbox");
    await userEvent.clear(editor);
    await userEvent.type(editor, "unsaved");

    await userEvent.click(screen.getByRole("button", { name: "저장" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledWith("unsaved"));
    expect(screen.getByText("수정됨")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "저장" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "되돌리기" })).toBeEnabled();
  });

  it("blocks duplicate saves while the first save is pending", async () => {
    let resolve!: (ok: boolean) => void;
    const onSave = vi.fn(() => new Promise<boolean>((done) => (resolve = done)));
    render(<FileViewer name="notes.txt" load={async () => textBlob("original")} onSave={onSave} />);
    const editor = await screen.findByRole("textbox");
    await userEvent.clear(editor);
    await userEvent.type(editor, "revision");
    const save = screen.getByRole("button", { name: "저장" });

    await userEvent.click(save);
    expect(save).toBeDisabled();
    await userEvent.click(save);
    expect(onSave).toHaveBeenCalledTimes(1);

    resolve(true);
    await waitFor(() => expect(screen.getByText("저장됨")).toBeInTheDocument());
  });

  it("reports clean state after a successful save", async () => {
    const onDirtyChange = vi.fn();
    render(
      <FileViewer
        name="notes.txt"
        load={async () => textBlob("original")}
        onSave={async () => true}
        onDirtyChange={onDirtyChange}
      />,
    );
    const editor = await screen.findByRole("textbox");
    await userEvent.clear(editor);
    await userEvent.type(editor, "saved");
    await userEvent.click(screen.getByRole("button", { name: "저장" }));

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false));
    expect(onDirtyChange).toHaveBeenCalledWith(true);
  });
});

describe("FileViewer rendered text modes", () => {
  it("opens Markdown in preview and toggles to editing", async () => {
    render(<FileViewer name="README.md" load={async () => textBlob("# Heading")} onSave={vi.fn()} />);

    expect(await screen.findByRole("heading", { name: "Heading" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "미리보기" })).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(screen.getByRole("button", { name: "편집" }));
    expect(screen.getByRole("textbox")).toHaveValue("# Heading");
    expect(screen.getByRole("button", { name: "편집" })).toHaveAttribute("aria-pressed", "true");
  });

  it("labels a read-only Markdown source tab as 원본", async () => {
    render(<FileViewer name="README.md" load={async () => textBlob("# Heading")} />);
    await screen.findByRole("heading", { name: "Heading" });
    await userEvent.click(screen.getByRole("button", { name: "원본" }));
    expect(screen.getByRole("textbox")).toBeDisabled();
  });

  it("renders CSV headers, quoted values, and rows", async () => {
    render(<FileViewer name="prices.csv" load={async () => textBlob('품목,금액\n"모듈, A","1,200"')} />);

    const table = await screen.findByRole("table");
    expect(within(table).getByRole("columnheader", { name: "품목" })).toBeInTheDocument();
    expect(within(table).getByRole("cell", { name: "모듈, A" })).toBeInTheDocument();
    expect(within(table).getByRole("cell", { name: "1,200" })).toBeInTheDocument();
  });

  it("uses tabs for TSV files", async () => {
    render(<FileViewer name="prices.tsv" load={async () => textBlob("품목\t금액\n모듈\t1200")} />);
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("columnheader", { name: "금액" })).toBeInTheDocument();
    expect(within(table).getByRole("cell", { name: "1200" })).toBeInTheDocument();
  });

  it("reports an empty CSV without rendering a table", async () => {
    render(<FileViewer name="empty.csv" load={async () => textBlob("")} />);
    expect(await screen.findByText("빈 파일")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("caps a large CSV preview at five hundred rows", async () => {
    const body = ["n", ...Array.from({ length: 520 }, (_, index) => String(index + 1))].join("\n");
    render(<FileViewer name="large.csv" load={async () => textBlob(body)} />);

    expect(await screen.findByText("전체 521행 중 500행 표시 (편집 모드에서 전문 확인)")).toBeInTheDocument();
    expect(screen.getAllByRole("row")).toHaveLength(500);
  });

  it("colorizes unified diff line categories", async () => {
    const diff = ["diff --git a/x b/x", "--- a/x", "+++ b/x", "@@ -1 +1 @@", "-old", "+new", " same", ""].join("\n");
    const { container } = render(<FileViewer name="change.patch" load={async () => textBlob(diff)} />);
    await screen.findByText("+new");

    expect(container.querySelectorAll(".diff-meta")).toHaveLength(3);
    expect(container.querySelectorAll(".diff-hunk")).toHaveLength(1);
    expect(container.querySelectorAll(".diff-del")).toHaveLength(1);
    expect(container.querySelectorAll(".diff-add")).toHaveLength(1);
    expect(Array.from(container.querySelectorAll(".diff-line")).at(-1)?.textContent).toBe(" ");
  });

  it("toggles a diff to its editable source", async () => {
    render(<FileViewer name="change.diff" load={async () => textBlob("-old\n+new")} onSave={vi.fn()} />);
    await screen.findByText("+new");
    await userEvent.click(screen.getByRole("button", { name: "편집" }));
    expect(screen.getByRole("textbox")).toHaveValue("-old\n+new");
  });
});
