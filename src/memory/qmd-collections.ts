import { extractKeywords } from "./query-expansion.js";

const NUL_MARKER_RE = /(?:\^@|\\0|\\x00|\\u0000|null\s*byte|nul\s*byte)/i;
const HAN_SCRIPT_RE = /[\u3400-\u9fff]/u;
const QMD_BM25_HAN_KEYWORD_LIMIT = 12;

export type ListedCollection = {
  path?: string;
  pattern?: string;
};

export function hasHanScript(value: string): boolean {
  return HAN_SCRIPT_RE.test(value);
}

export function normalizeHanBm25Query(query: string): string {
  const trimmed = query.trim();
  if (!trimmed || !hasHanScript(trimmed)) {
    return trimmed;
  }
  const keywords = extractKeywords(trimmed);
  const normalizedKeywords: string[] = [];
  const seen = new Set<string>();
  for (const keyword of keywords) {
    const token = keyword.trim();
    if (!token || seen.has(token)) {
      continue;
    }
    const includesHan = hasHanScript(token);
    // Han unigrams are usually too broad for BM25 and can drown signal.
    if (includesHan && Array.from(token).length < 2) {
      continue;
    }
    if (!includesHan && token.length < 2) {
      continue;
    }
    seen.add(token);
    normalizedKeywords.push(token);
    if (normalizedKeywords.length >= QMD_BM25_HAN_KEYWORD_LIMIT) {
      break;
    }
  }
  return normalizedKeywords.length > 0 ? normalizedKeywords.join(" ") : trimmed;
}

export function parseListedCollections(output: string): Map<string, ListedCollection> {
  const listed = new Map<string, ListedCollection>();
  const trimmed = output.trim();
  if (!trimmed) {
    return listed;
  }
  try {
    const parsed = JSON.parse(trimmed) as unknown;
    if (Array.isArray(parsed)) {
      for (const entry of parsed) {
        if (typeof entry === "string") {
          listed.set(entry, {});
          continue;
        }
        if (!entry || typeof entry !== "object") {
          continue;
        }
        const name = (entry as { name?: unknown }).name;
        if (typeof name !== "string") {
          continue;
        }
        const listedPath = (entry as { path?: unknown }).path;
        const listedPattern = (entry as { pattern?: unknown; mask?: unknown }).pattern;
        const listedMask = (entry as { mask?: unknown }).mask;
        listed.set(name, {
          path: typeof listedPath === "string" ? listedPath : undefined,
          pattern:
            typeof listedPattern === "string"
              ? listedPattern
              : typeof listedMask === "string"
                ? listedMask
                : undefined,
        });
      }
      return listed;
    }
  } catch {
    // Some qmd builds ignore `--json` and still print table output.
  }

  let currentName: string | null = null;
  for (const rawLine of output.split(/\r?\n/)) {
    const line = rawLine.trimEnd();
    if (!line.trim()) {
      currentName = null;
      continue;
    }
    const collectionLine = /^\s*([a-z0-9._-]+)\s+\(qmd:\/\/[^)]+\)\s*$/i.exec(line);
    if (collectionLine) {
      currentName = collectionLine[1];
      if (!listed.has(currentName)) {
        listed.set(currentName, {});
      }
      continue;
    }
    if (/^\s*collections\b/i.test(line)) {
      continue;
    }
    const bareNameLine = /^\s*([a-z0-9._-]+)\s*$/i.exec(line);
    if (bareNameLine && !line.includes(":")) {
      currentName = bareNameLine[1];
      if (!listed.has(currentName)) {
        listed.set(currentName, {});
      }
      continue;
    }
    if (!currentName) {
      continue;
    }
    const patternLine = /^\s*(?:pattern|mask)\s*:\s*(.+?)\s*$/i.exec(line);
    if (patternLine) {
      const existing = listed.get(currentName) ?? {};
      existing.pattern = patternLine[1].trim();
      listed.set(currentName, existing);
      continue;
    }
    const pathLine = /^\s*path\s*:\s*(.+?)\s*$/i.exec(line);
    if (pathLine) {
      const existing = listed.get(currentName) ?? {};
      existing.path = pathLine[1].trim();
      listed.set(currentName, existing);
    }
  }
  return listed;
}

export function isDirectoryGlobPattern(pattern: string): boolean {
  return pattern.includes("*") || pattern.includes("?") || pattern.includes("[");
}

export function isCollectionAlreadyExistsError(message: string): boolean {
  const lower = message.toLowerCase();
  return lower.includes("already exists") || lower.includes("exists");
}

export function isCollectionMissingError(message: string): boolean {
  const lower = message.toLowerCase();
  return (
    lower.includes("not found") || lower.includes("does not exist") || lower.includes("missing")
  );
}

export function isMissingCollectionSearchError(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  return isCollectionMissingError(message) && message.toLowerCase().includes("collection");
}

export function shouldRepairNullByteCollectionError(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  const lower = message.toLowerCase();
  return (
    (lower.includes("enotdir") || lower.includes("not a directory")) && NUL_MARKER_RE.test(message)
  );
}

export function shouldRepairDuplicateDocumentConstraint(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  const lower = message.toLowerCase();
  return (
    lower.includes("unique constraint failed") &&
    lower.includes("documents.collection") &&
    lower.includes("documents.path")
  );
}
