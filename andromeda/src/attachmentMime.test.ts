import { describe, expect, it } from "vitest";

import { inferAttachmentMimeType } from "./attachmentMime";

describe("inferAttachmentMimeType", () => {
  it("keeps the browser-provided type when available", () => {
    expect(inferAttachmentMimeType("quote.pdf", "application/x-custom")).toBe("application/x-custom");
  });

  it("infers known document types when File.type is empty", () => {
    expect(inferAttachmentMimeType("quote.PDF", "")).toBe("application/pdf");
    expect(inferAttachmentMimeType("report.docx", "")).toBe(
      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    );
    expect(inferAttachmentMimeType("sheet.xlsx", "")).toBe(
      "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    );
  });

  it("infers image and audio types when File.type is empty", () => {
    expect(inferAttachmentMimeType("receipt.PNG", "")).toBe("image/png");
    expect(inferAttachmentMimeType("voice.MP3", "")).toBe("audio/mpeg");
  });

  it("falls back to octet-stream for unknown extensions", () => {
    expect(inferAttachmentMimeType("archive.bin", "")).toBe("application/octet-stream");
  });
});
