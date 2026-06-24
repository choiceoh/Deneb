// Pure path / formatting helpers for the files pane (no React) — extracted for
// isolated unit testing.
import type { FileEntry } from "@/types";

// A file entry's canonical path, falling back through the gateway's field aliases.
export function entryPath(entry: FileEntry): string {
  return entry.pathDisplay ?? entry.pathLower ?? entry.name ?? "";
}

export function isFolder(entry: FileEntry): boolean {
  return entry.tag === "folder" || entry.tag === "dir";
}

// Join a base dir and a name into a path, tolerating stray leading/trailing slashes.
export function joinPath(base: string, name: string): string {
  const cleanName = name.replace(/^\/+/, "");
  const cleanBase = base.replace(/\/+$/, "");
  return cleanBase ? `${cleanBase}/${cleanName}` : cleanName;
}

// The parent directory of a path ("" at the root).
export function parentPath(value: string): string {
  const clean = value.replace(/^\/+|\/+$/g, "");
  if (!clean) return "";
  const parts = clean.split("/");
  parts.pop();
  return parts.join("/");
}

// Human byte size ("" for unknown / zero / non-finite).
export function formatBytes(size?: number): string {
  if (typeof size !== "number" || !Number.isFinite(size) || size <= 0) return "";
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
