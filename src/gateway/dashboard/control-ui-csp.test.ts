import { describe, expect, it } from "vitest";
import { buildControlUiCspHeader, computeInlineScriptHashes } from "./control-ui-csp.js";

describe("computeInlineScriptHashes", () => {
  it("extracts hashes from inline scripts", () => {
    const html = `<html><head><script>console.log("boot")</script></head></html>`;
    const hashes = computeInlineScriptHashes(html);
    expect(hashes).toHaveLength(1);
    expect(hashes[0]).toMatch(/^'sha256-[A-Za-z0-9+/=]+'$/);
  });

  it("skips external scripts with src attribute", () => {
    const html = `<script src="/app.js"></script><script>boot()</script>`;
    const hashes = computeInlineScriptHashes(html);
    expect(hashes).toHaveLength(1);
  });

  it("skips empty script blocks", () => {
    const html = `<script></script><script>  </script>`;
    const hashes = computeInlineScriptHashes(html);
    expect(hashes).toHaveLength(0);
  });

  it("returns empty array for no scripts", () => {
    const hashes = computeInlineScriptHashes("<html><body>hi</body></html>");
    expect(hashes).toHaveLength(0);
  });

  it("handles multiple inline scripts", () => {
    const html = `<script>a()</script><script>b()</script>`;
    const hashes = computeInlineScriptHashes(html);
    expect(hashes).toHaveLength(2);
    // Different content produces different hashes.
    expect(hashes[0]).not.toBe(hashes[1]);
  });

  it("handles script tags with attributes but no src", () => {
    const html = `<script type="module">init()</script>`;
    const hashes = computeInlineScriptHashes(html);
    expect(hashes).toHaveLength(1);
  });
});

describe("buildControlUiCspHeader", () => {
  it("builds default CSP without hashes", () => {
    const csp = buildControlUiCspHeader();
    expect(csp).toContain("script-src 'self'");
    expect(csp).not.toContain("sha256");
  });

  it("includes script hashes when provided", () => {
    const csp = buildControlUiCspHeader({
      inlineScriptHashes: ["'sha256-abc123='"],
    });
    expect(csp).toContain("script-src 'self' 'sha256-abc123='");
  });

  it("includes multiple hashes", () => {
    const csp = buildControlUiCspHeader({
      inlineScriptHashes: ["'sha256-aaa='", "'sha256-bbb='"],
    });
    expect(csp).toContain("script-src 'self' 'sha256-aaa=' 'sha256-bbb='");
  });

  it("includes all required directives", () => {
    const csp = buildControlUiCspHeader();
    expect(csp).toContain("default-src 'self'");
    expect(csp).toContain("base-uri 'none'");
    expect(csp).toContain("object-src 'none'");
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("connect-src 'self' ws: wss:");
    expect(csp).toContain("style-src 'self' 'unsafe-inline' https://fonts.googleapis.com");
    expect(csp).toContain("img-src 'self' data: https:");
    expect(csp).toContain("font-src 'self' https://fonts.gstatic.com");
  });
});
