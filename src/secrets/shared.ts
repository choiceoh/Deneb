import fs from "node:fs";
import path from "node:path";

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

export function parseEnvValue(raw: string): string {
  const trimmed = raw.trim();
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"')) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

export function normalizePositiveInt(value: unknown, fallback: number): number {
  if (typeof value === "number" && Number.isFinite(value)) {
    return Math.max(1, Math.floor(value));
  }
  return Math.max(1, Math.floor(fallback));
}

export function parseDotPath(pathname: string): string[] {
  return pathname
    .split(".")
    .map((segment) => segment.trim())
    .filter((segment) => segment.length > 0);
}

export function toDotPath(segments: string[]): string {
  return segments.join(".");
}

export function ensureDirForFile(filePath: string): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true, mode: 0o700 });
}

export function writeJsonFileSecure(pathname: string, value: unknown): void {
  ensureDirForFile(pathname);
  // Atomic write: write to temp file first, then rename to avoid partial/corrupt writes on crash.
  const tempPath = `${pathname}.tmp-${process.pid}-${Date.now()}`;
  try {
    fs.writeFileSync(tempPath, `${JSON.stringify(value, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
    fs.renameSync(tempPath, pathname);
  } catch (err) {
    try {
      fs.unlinkSync(tempPath);
    } catch {
      // best-effort cleanup
    }
    throw err;
  }
}

export function readTextFileIfExists(pathname: string): string | null {
  if (!fs.existsSync(pathname)) {
    return null;
  }
  return fs.readFileSync(pathname, "utf8");
}

export function writeTextFileAtomic(pathname: string, value: string, mode = 0o600): void {
  ensureDirForFile(pathname);
  const tempPath = `${pathname}.tmp-${process.pid}-${Date.now()}`;
  fs.writeFileSync(tempPath, value, "utf8");
  fs.chmodSync(tempPath, mode);
  fs.renameSync(tempPath, pathname);
}

export function describeUnknownError(err: unknown): string {
  if (err instanceof Error && err.message.trim().length > 0) {
    return err.message;
  }
  if (typeof err === "string" && err.trim().length > 0) {
    return err;
  }
  if (typeof err === "number" || typeof err === "bigint") {
    return err.toString();
  }
  if (typeof err === "boolean") {
    return err ? "true" : "false";
  }
  try {
    const serialized = JSON.stringify(err);
    return serialized ?? "unknown error";
  } catch {
    return "unknown error";
  }
}
