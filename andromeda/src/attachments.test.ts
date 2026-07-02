import { describe, expect, it } from "vitest";

import { MAX_ATTACH_MB, isAttachableMime, readFileBase64, splitAttachable } from "./attachments";

describe("splitAttachable", () => {
  it("keeps supported files and skips unsupported/oversized ones with notices", () => {
    const img = new File(["x"], "a.png", { type: "image/png" });
    const doc = new File(["x"], "b.pdf", { type: "" }); // OS drops often omit the type — inferred from the extension
    const vid = new File(["x"], "c.mp4", { type: "video/mp4" });
    const unknown = new File(["x"], "d.xyz", { type: "" });
    const big = new File(["x"], "e.pdf", { type: "application/pdf" });
    Object.defineProperty(big, "size", { value: (MAX_ATTACH_MB + 1) * 1024 * 1024 });

    const { ok, skipped } = splitAttachable([img, doc, vid, unknown, big]);
    expect(ok).toEqual([img, doc]);
    expect(skipped).toHaveLength(3);
    expect(skipped[0]).toContain("c.mp4");
    expect(skipped[0]).toContain("형식");
    expect(skipped[1]).toContain("d.xyz");
    expect(skipped[2]).toContain(`${MAX_ATTACH_MB}MB`);
  });
});

describe("isAttachableMime", () => {
  it("accepts image/audio prefixes and known document MIMEs only", () => {
    expect(isAttachableMime("image/webp")).toBe(true);
    expect(isAttachableMime("audio/mp4")).toBe(true);
    expect(isAttachableMime("text/csv")).toBe(true);
    expect(isAttachableMime("video/mp4")).toBe(false);
    expect(isAttachableMime("application/octet-stream")).toBe(false);
  });
});

describe("readFileBase64", () => {
  it("returns the bare base64 payload (no data-URL prefix)", async () => {
    const b64 = await readFileBase64(new File(["hi"], "t.txt", { type: "text/plain" }));
    expect(atob(b64)).toBe("hi");
  });
});
