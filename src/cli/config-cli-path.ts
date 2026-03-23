/**
 * Path manipulation utilities for config-cli: parse, traverse, and mutate
 * dot-notation paths into config objects.
 */

import JSON5 from "json5";
import { isBlockedObjectKey } from "../config/prototype-keys.js";

export type PathSegment = string;
export type ConfigSetParseOpts = {
  strictJson?: boolean;
};

export function isIndexSegment(raw: string): boolean {
  return /^[0-9]+$/.test(raw);
}

export function parsePath(raw: string): PathSegment[] {
  const trimmed = raw.trim();
  if (!trimmed) {
    return [];
  }
  const parts: string[] = [];
  let current = "";
  let i = 0;
  while (i < trimmed.length) {
    const ch = trimmed[i];
    if (ch === "\\") {
      const next = trimmed[i + 1];
      if (next) {
        current += next;
      }
      i += 2;
      continue;
    }
    if (ch === ".") {
      if (current) {
        parts.push(current);
      }
      current = "";
      i += 1;
      continue;
    }
    if (ch === "[") {
      if (current) {
        parts.push(current);
      }
      current = "";
      const close = trimmed.indexOf("]", i);
      if (close === -1) {
        throw new Error(`Invalid path (missing "]"): ${raw}`);
      }
      const inside = trimmed.slice(i + 1, close).trim();
      if (!inside) {
        throw new Error(`Invalid path (empty "[]"): ${raw}`);
      }
      parts.push(inside);
      i = close + 1;
      continue;
    }
    current += ch;
    i += 1;
  }
  if (current) {
    parts.push(current);
  }
  return parts.map((part) => part.trim()).filter(Boolean);
}

export function parseValue(raw: string, opts: ConfigSetParseOpts): unknown {
  const trimmed = raw.trim();
  if (opts.strictJson) {
    try {
      return JSON5.parse(trimmed);
    } catch (err) {
      throw new Error(`Failed to parse JSON5 value: ${String(err)}`, { cause: err });
    }
  }

  try {
    return JSON5.parse(trimmed);
  } catch {
    return raw;
  }
}

export function hasOwnPathKey(value: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

export function validatePathSegments(path: PathSegment[]): void {
  for (const segment of path) {
    if (!isIndexSegment(segment) && isBlockedObjectKey(segment)) {
      throw new Error(`Invalid path segment: ${segment}`);
    }
  }
}

export function parseRequiredPath(path: string): PathSegment[] {
  const parsedPath = parsePath(path);
  if (parsedPath.length === 0) {
    throw new Error("Path is empty.");
  }
  validatePathSegments(parsedPath);
  return parsedPath;
}

export function toDotPath(path: PathSegment[]): string {
  return path.join(".");
}

export function pathEquals(path: PathSegment[], expected: PathSegment[]): boolean {
  return (
    path.length === expected.length && path.every((segment, index) => segment === expected[index])
  );
}

export function getAtPath(root: unknown, path: PathSegment[]): { found: boolean; value?: unknown } {
  let current: unknown = root;
  for (const segment of path) {
    if (!current || typeof current !== "object") {
      return { found: false };
    }
    if (Array.isArray(current)) {
      if (!isIndexSegment(segment)) {
        return { found: false };
      }
      const index = Number.parseInt(segment, 10);
      if (!Number.isFinite(index) || index < 0 || index >= current.length) {
        return { found: false };
      }
      current = current[index];
      continue;
    }
    const record = current as Record<string, unknown>;
    if (!hasOwnPathKey(record, segment)) {
      return { found: false };
    }
    current = record[segment];
  }
  return { found: true, value: current };
}

export function setAtPath(
  root: Record<string, unknown>,
  path: PathSegment[],
  value: unknown,
): void {
  let current: unknown = root;
  for (let i = 0; i < path.length - 1; i += 1) {
    const segment = path[i];
    const next = path[i + 1];
    const nextIsIndex = Boolean(next && isIndexSegment(next));
    if (Array.isArray(current)) {
      if (!isIndexSegment(segment)) {
        throw new Error(`Expected numeric index for array segment "${segment}"`);
      }
      const index = Number.parseInt(segment, 10);
      const existing = current[index];
      if (!existing || typeof existing !== "object") {
        current[index] = nextIsIndex ? [] : {};
      }
      current = current[index];
      continue;
    }
    if (!current || typeof current !== "object") {
      throw new Error(`Cannot traverse into "${segment}" (not an object)`);
    }
    const record = current as Record<string, unknown>;
    const existing = hasOwnPathKey(record, segment) ? record[segment] : undefined;
    if (!existing || typeof existing !== "object") {
      record[segment] = nextIsIndex ? [] : {};
    }
    current = record[segment];
  }

  const last = path[path.length - 1];
  if (Array.isArray(current)) {
    if (!isIndexSegment(last)) {
      throw new Error(`Expected numeric index for array segment "${last}"`);
    }
    const index = Number.parseInt(last, 10);
    current[index] = value;
    return;
  }
  if (!current || typeof current !== "object") {
    throw new Error(`Cannot set "${last}" (parent is not an object)`);
  }
  (current as Record<string, unknown>)[last] = value;
}

export function unsetAtPath(root: Record<string, unknown>, path: PathSegment[]): boolean {
  let current: unknown = root;
  for (let i = 0; i < path.length - 1; i += 1) {
    const segment = path[i];
    if (!current || typeof current !== "object") {
      return false;
    }
    if (Array.isArray(current)) {
      if (!isIndexSegment(segment)) {
        return false;
      }
      const index = Number.parseInt(segment, 10);
      if (!Number.isFinite(index) || index < 0 || index >= current.length) {
        return false;
      }
      current = current[index];
      continue;
    }
    const record = current as Record<string, unknown>;
    if (!hasOwnPathKey(record, segment)) {
      return false;
    }
    current = record[segment];
  }

  const last = path[path.length - 1];
  if (Array.isArray(current)) {
    if (!isIndexSegment(last)) {
      return false;
    }
    const index = Number.parseInt(last, 10);
    if (!Number.isFinite(index) || index < 0 || index >= current.length) {
      return false;
    }
    current.splice(index, 1);
    return true;
  }
  if (!current || typeof current !== "object") {
    return false;
  }
  const record = current as Record<string, unknown>;
  if (!hasOwnPathKey(record, last)) {
    return false;
  }
  delete record[last];
  return true;
}
