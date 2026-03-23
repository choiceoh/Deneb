import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

export function loadJsonFile(pathname: string): unknown {
  try {
    if (!fs.existsSync(pathname)) {
      return undefined;
    }
    const raw = fs.readFileSync(pathname, "utf8");
    return JSON.parse(raw) as unknown;
  } catch (err) {
    // Log corrupt/unreadable files so the issue is visible instead of silently returning undefined.
    // Avoid importing the full subsystem logger to keep this low-level utility dependency-light.
    const message = err instanceof Error ? err.message : String(err);
    if (typeof process !== "undefined" && process.stderr) {
      process.stderr.write(`[json-file] failed to load ${pathname}: ${message}\n`);
    }
    return undefined;
  }
}

export function saveJsonFile(pathname: string, data: unknown) {
  const dir = path.dirname(pathname);
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  }
  // Atomic write: write to temp file first, then rename to avoid partial/corrupt writes on crash.
  const tempPath = path.join(
    dir,
    `${path.basename(pathname)}.${process.pid}.${crypto.randomUUID()}.tmp`,
  );
  try {
    fs.writeFileSync(tempPath, `${JSON.stringify(data, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
    fs.renameSync(tempPath, pathname);
  } catch (err) {
    // Clean up temp file on failure; fall back to direct write on rename failure (e.g. Windows cross-device).
    try {
      fs.unlinkSync(tempPath);
    } catch {
      // best-effort cleanup
    }
    throw err;
  }
}
