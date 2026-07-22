import { describe, expect, it } from "vitest";

import { MAX_ATTACH_MB, isAttachableMime, readFileBase64, splitAttachable } from "./attachments";

describe("splitAttachable", () => {
  it("keeps supported files and skips unsupported/oversized ones with notices", () => {
    const img = new File(["x"], "a.png", { type: "image/png" });
    const doc = new File(["x"], "b.pdf", { type: "" }); // OS drops often omit the type — inferred from the extension
    // OS-backed drops/pastes often report a GENERIC type — must fall back to the
    // extension, not get rejected as unsupported (리뷰 지적: octet-stream pdf 거부).
    const generic = new File(["x"], "b2.docx", { type: "application/octet-stream" });
    const vid = new File(["x"], "c.mp4", { type: "video/mp4" }); // video rides the ASR path (ffmpeg → transcript)
    const unknown = new File(["x"], "d.xyz", { type: "" });
    const big = new File(["x"], "e.pdf", { type: "application/pdf" });
    Object.defineProperty(big, "size", { value: (MAX_ATTACH_MB + 1) * 1024 * 1024 });

    const { ok, skipped } = splitAttachable([img, doc, generic, vid, unknown, big]);
    expect(ok).toEqual([img, doc, generic, vid]);
    expect(skipped).toHaveLength(2);
    expect(skipped[0]).toContain("d.xyz");
    expect(skipped[0]).toContain("형식");
    expect(skipped[1]).toContain(`${MAX_ATTACH_MB}MB`);
  });
});

describe("isAttachableMime", () => {
  it("allows image/audio/video prefixes and known document MIMEs", () => {
    expect(isAttachableMime("image/webp")).toBe(true);
    expect(isAttachableMime("audio/mp4")).toBe(true);
    expect(isAttachableMime("video/mp4")).toBe(true); // ffmpeg → ASR
    expect(isAttachableMime("text/csv")).toBe(true);
    expect(isAttachableMime("application/x-hwp")).toBe(true); // gateway converts (hwp5txt)
    expect(isAttachableMime("application/octet-stream")).toBe(false);
  });
});

describe("readFileBase64", () => {
  it("returns the bare base64 payload (no data-URL prefix)", async () => {
    const b64 = await readFileBase64(new File(["hi"], "t.txt", { type: "text/plain" }));
    expect(atob(b64)).toBe("hi");
  });
});
