import { describe, expect, it } from "vitest";
import { loadCoreRs } from "./core-rs.js";

// The native addon may not be built in all environments.
// These tests verify the interface and graceful fallback.

describe("core-rs loader", () => {
  it("returns a module or null without throwing", () => {
    const mod = loadCoreRs();
    expect(mod === null || typeof mod === "object").toBe(true);
  });

  it("caches the result across calls", () => {
    const a = loadCoreRs();
    const b = loadCoreRs();
    expect(a).toBe(b);
  });
});

// Only run functional tests when the native addon is available.
const mod = loadCoreRs();
const describeNative = mod ? describe : describe.skip;

describeNative("core-rs native functions", () => {
  it("validateFrame returns 'req' for valid request frame", () => {
    expect(mod!.validateFrame('{"type":"req","id":"1","method":"chat.send"}')).toBe("req");
  });

  it("validateFrame returns 'res' for valid response frame", () => {
    expect(mod!.validateFrame('{"type":"res","id":"1","ok":true}')).toBe("res");
  });

  it("validateFrame returns 'event' for valid event frame", () => {
    expect(mod!.validateFrame('{"type":"event","event":"health","seq":5}')).toBe("event");
  });

  it("validateFrame throws on invalid JSON", () => {
    expect(() => mod!.validateFrame("{not json}")).toThrow();
  });

  it("validateFrame throws on unknown frame type", () => {
    expect(() => mod!.validateFrame('{"type":"unknown"}')).toThrow();
  });

  it("validateFrame throws on missing required fields", () => {
    expect(() => mod!.validateFrame('{"type":"req","id":"abc"}')).toThrow();
  });

  it("constantTimeEq returns true for equal buffers", () => {
    expect(mod!.constantTimeEq(Buffer.from("secret"), Buffer.from("secret"))).toBe(true);
  });

  it("constantTimeEq returns false for different buffers", () => {
    expect(mod!.constantTimeEq(Buffer.from("secret"), Buffer.from("differ"))).toBe(false);
  });

  it("constantTimeEq returns false for different lengths", () => {
    expect(mod!.constantTimeEq(Buffer.from("short"), Buffer.from("longer"))).toBe(false);
  });

  it("constantTimeEq returns true for empty buffers", () => {
    expect(mod!.constantTimeEq(Buffer.from(""), Buffer.from(""))).toBe(true);
  });

  it("detectMime identifies PNG", () => {
    const png = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00]);
    expect(mod!.detectMime(png)).toBe("image/png");
  });

  it("detectMime identifies JPEG", () => {
    const jpeg = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x00]);
    expect(mod!.detectMime(jpeg)).toBe("image/jpeg");
  });

  it("detectMime identifies PDF", () => {
    expect(mod!.detectMime(Buffer.from("%PDF-1.4"))).toBe("application/pdf");
  });

  it("detectMime returns octet-stream for unknown", () => {
    expect(mod!.detectMime(Buffer.from([0x00, 0x01, 0x02, 0x03]))).toBe("application/octet-stream");
  });

  it("isSafeInput returns true for normal text", () => {
    expect(mod!.isSafeInput("normal text")).toBe(true);
  });

  it("isSafeInput returns false for script injection", () => {
    expect(mod!.isSafeInput("<script>alert(1)</script>")).toBe(false);
  });

  it("sanitizeControlChars removes control characters", () => {
    expect(mod!.sanitizeControlChars("hello\x00world")).toBe("helloworld");
  });

  it("sanitizeControlChars keeps newlines and tabs", () => {
    expect(mod!.sanitizeControlChars("keep\nnewlines\tand\ttabs")).toBe(
      "keep\nnewlines\tand\ttabs",
    );
  });
});
