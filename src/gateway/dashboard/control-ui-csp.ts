import { createHash } from "node:crypto";

/**
 * Compute SHA-256 CSP hashes for inline `<script>` blocks in HTML.
 * External scripts (those with a `src` attribute) are skipped.
 * Returns an array of `'sha256-<base64>'` strings suitable for CSP `script-src`.
 */
export function computeInlineScriptHashes(html: string): string[] {
  const hashes: string[] = [];
  // Match <script> blocks without src= attribute.
  // Uses a non-greedy match for content between tags.
  const scriptRe = /<script(?:\s[^>]*)?>([^]*?)<\/script>/gi;
  let match: RegExpExecArray | null;
  while ((match = scriptRe.exec(html)) !== null) {
    const fullTag = match[0];
    // Skip external scripts (those with src="..." attribute).
    if (/\bsrc\s*=/i.test(fullTag.slice(0, fullTag.indexOf(">")))) {
      continue;
    }
    const body = match[1];
    if (!body.trim()) {
      continue;
    }
    const hash = createHash("sha256").update(body, "utf-8").digest("base64");
    hashes.push(`'sha256-${hash}'`);
  }
  return hashes;
}

/**
 * Build the Content-Security-Policy header for the control UI.
 * If inline script hashes are provided, they are included in `script-src`
 * to allow specific inline bootstrap scripts while keeping CSP strict.
 */
export function buildControlUiCspHeader(opts?: { inlineScriptHashes?: string[] }): string {
  const scriptHashes = opts?.inlineScriptHashes ?? [];
  const scriptSrc =
    scriptHashes.length > 0 ? `script-src 'self' ${scriptHashes.join(" ")}` : "script-src 'self'";

  // Control UI: block framing, block inline scripts (except hashed), keep styles permissive
  // (UI uses a lot of inline style attributes in templates).
  // Keep Google Fonts origins explicit in CSP for deployments that load
  // external Google Fonts stylesheets/font files.
  return [
    "default-src 'self'",
    "base-uri 'none'",
    "object-src 'none'",
    "frame-ancestors 'none'",
    scriptSrc,
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
    "img-src 'self' data: https:",
    "font-src 'self' https://fonts.gstatic.com",
    "connect-src 'self' ws: wss:",
  ].join("; ");
}
