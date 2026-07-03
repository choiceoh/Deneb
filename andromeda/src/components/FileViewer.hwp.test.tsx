import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { FileViewer } from "./FileViewer";
import { buildCfb, buildFileHeader, buildParaTextRecord } from "./hwp/testfixtures";

// End-to-end through the viewer: a synthetic UNCOMPRESSED HWP blob (jsdom has
// no DecompressionStream, so the fixture sets the uncompressed flag) renders
// its extracted paragraphs read-only, with the download link kept.
describe("FileViewer HWP", () => {
  it("extracts and shows paragraph text read-only", async () => {
    const section = concat(buildParaTextRecord("탑솔라 견적서"), buildParaTextRecord("금액: 1,200,000원"));
    const buf = buildCfb([
      { name: "FileHeader", data: buildFileHeader({ compressed: false, version: [0, 0, 1, 5] }) },
      { name: "Section0", data: section },
    ]);
    const blob = {
      size: buf.byteLength,
      arrayBuffer: async () => buf,
    } as unknown as Blob;

    render(<FileViewer name="계약서.hwp" load={async () => blob} downloadUrl="http://gw/dl" />);

    const area = (await screen.findByLabelText("계약서.hwp")) as HTMLTextAreaElement;
    expect(area.value).toContain("탑솔라 견적서");
    expect(area.value).toContain("금액: 1,200,000원");
    expect(area).toHaveAttribute("readOnly"); // extracted text is never editable
    // The 한글 문서 note and the download escape hatch are both present.
    expect(screen.getByText(/한글 문서/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toHaveAttribute("href", "http://gw/dl");
  });

  it("shows a clear message when the file is not an HWP document", async () => {
    // A valid compound file whose FileHeader is not the HWP signature.
    const buf = buildCfb([{ name: "FileHeader", data: new Uint8Array(40) }]);
    const blob = { size: buf.byteLength, arrayBuffer: async () => buf } as unknown as Blob;

    render(<FileViewer name="가짜.hwp" load={async () => blob} downloadUrl="http://gw/dl" />);

    expect(await screen.findByText(/불러오기 실패/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "다운로드" })).toBeInTheDocument();
  });
});

function concat(...parts: Uint8Array[]): Uint8Array {
  let total = 0;
  for (const p of parts) total += p.length;
  const out = new Uint8Array(total);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}
