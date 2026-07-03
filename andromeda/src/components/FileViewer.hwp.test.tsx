import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { FileViewer } from "./FileViewer";
import { buildCfb, buildFileHeader, buildParagraph, buildTable, concatBytes } from "./hwp/testfixtures";

// End-to-end through the viewer: a synthetic UNCOMPRESSED HWP blob (jsdom has
// no DecompressionStream, so the fixture sets the uncompressed flag) renders
// paragraphs, a table grid, and an image — read-only, with the download link.
function hwpBlob(...streams: { name: string; data: Uint8Array }[]): Blob {
  const buf = buildCfb([
    { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
    ...streams,
  ]);
  return { size: buf.byteLength, arrayBuffer: async () => buf } as unknown as Blob;
}

describe("FileViewer HWP", () => {
  it("renders paragraphs, a table, and an image read-only", async () => {
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4]);
    const blob = hwpBlob(
      {
        name: "Section0",
        data: concatBytes(
          buildParagraph("발주 내역서"),
          buildTable([
            ["품목", "금액"],
            ["모듈", "1,200,000"],
          ]),
        ),
      },
      { name: "BIN0001.png", data: png },
    );

    render(<FileViewer name="발주.hwp" load={async () => blob} downloadUrl="http://gw/dl" />);

    // Paragraph text.
    expect(await screen.findByText("발주 내역서")).toBeInTheDocument();
    // Table grid with header + data row.
    const table = document.querySelector(".hwp-table") as HTMLTableElement;
    expect(table).toBeTruthy();
    expect(within(table).getByText("품목")).toBeInTheDocument();
    expect(within(table).getByText("1,200,000")).toBeInTheDocument();
    // Image rendered from the BinData data URI.
    const img = document.querySelector(".hwp-figure img") as HTMLImageElement;
    expect(img).toBeTruthy();
    expect(img.getAttribute("src")).toMatch(/^data:image\/png;base64,/);
    // Read-only: no save/edit affordance, and the note + download are present.
    expect(screen.queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
    expect(screen.getByText(/한글 문서/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toHaveAttribute("href", "http://gw/dl");
  });

  it("shows a clear message when the file is not an HWP document", async () => {
    // A valid compound file whose FileHeader lacks the HWP signature.
    const buf = buildCfb([{ name: "FileHeader", data: new Uint8Array(40) }]);
    const bogus = { size: buf.byteLength, arrayBuffer: async () => buf } as unknown as Blob;

    render(<FileViewer name="가짜.hwp" load={async () => bogus} downloadUrl="http://gw/dl" />);

    expect(await screen.findByText(/불러오기 실패/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toBeInTheDocument();
  });
});
