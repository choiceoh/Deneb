import crypto from "node:crypto";
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

  it("if loaded, exposes all expected functions", () => {
    const mod = loadCoreRs();
    if (!mod) {
      return;
    }
    expect(typeof mod.validateFrame).toBe("function");
    expect(typeof mod.constantTimeEq).toBe("function");
    expect(typeof mod.detectMime).toBe("function");
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

  // --- Edge cases ---

  it("validateFrame handles empty string", () => {
    expect(() => mod!.validateFrame("")).toThrow();
  });

  it("validateFrame handles malformed nested JSON", () => {
    // Malformed nesting — should throw, not hang.
    expect(() => mod!.validateFrame("{".repeat(100))).toThrow();
  });

  it("constantTimeEq handles large equal buffers", () => {
    const big = Buffer.alloc(1024 * 64, 0xab);
    expect(mod!.constantTimeEq(big, Buffer.from(big))).toBe(true);
  });

  it("detectMime handles empty buffer", () => {
    expect(mod!.detectMime(Buffer.alloc(0))).toBe("application/octet-stream");
  });

  it("detectMime handles single-byte buffer", () => {
    expect(mod!.detectMime(Buffer.from([0xff]))).toBe("application/octet-stream");
  });

  it("detectMime handles GIF87a", () => {
    expect(mod!.detectMime(Buffer.from("GIF87a..."))).toBe("image/gif");
  });

  it("detectMime handles GIF89a", () => {
    expect(mod!.detectMime(Buffer.from("GIF89a..."))).toBe("image/gif");
  });

  // --- Size limit guards ---

  it("validateFrame rejects oversized input", () => {
    const huge =
      '{"type":"req","id":"1","method":"x","params":"' + "a".repeat(17 * 1024 * 1024) + '"}';
    expect(() => mod!.validateFrame(huge)).toThrow(/size limit/);
  });

  it("validateFrame rejects negative seq in event frame", () => {
    expect(() => mod!.validateFrame('{"type":"event","event":"x","seq":-1}')).toThrow();
  });

  it("validateFrame accepts seq of 0", () => {
    expect(mod!.validateFrame('{"type":"event","event":"x","seq":0}')).toBe("event");
  });

  it("validateFrame rejects uppercase frame type", () => {
    expect(() => mod!.validateFrame('{"type":"REQ","id":"1","method":"test"}')).toThrow();
  });

  it("validateFrame ignores unknown extra fields", () => {
    expect(mod!.validateFrame('{"type":"req","id":"1","method":"test","extra":42}')).toBe("req");
  });

  // --- Parity: native vs Node.js built-in ---

  it("constantTimeEq matches crypto.timingSafeEqual for equal inputs", () => {
    const a = Buffer.from("test-secret-value");
    const b = Buffer.from("test-secret-value");
    expect(mod!.constantTimeEq(a, b)).toBe(crypto.timingSafeEqual(a, b));
  });

  it("constantTimeEq matches crypto.timingSafeEqual for different inputs", () => {
    const a = Buffer.from("secret-a-value!!");
    const b = Buffer.from("secret-b-value!!");
    expect(mod!.constantTimeEq(a, b)).toBe(crypto.timingSafeEqual(a, b));
  });

  it("constantTimeEq handles binary buffers with null bytes", () => {
    const a = Buffer.from([0x00, 0x01, 0x02, 0x00]);
    const b = Buffer.from([0x00, 0x01, 0x02, 0x00]);
    expect(mod!.constantTimeEq(a, b)).toBe(true);
  });

  it("constantTimeEq single-byte difference", () => {
    const a = Buffer.from([0x00, 0x01, 0x02, 0x03]);
    const b = Buffer.from([0x00, 0x01, 0x02, 0x04]);
    expect(mod!.constantTimeEq(a, b)).toBe(false);
  });
});
