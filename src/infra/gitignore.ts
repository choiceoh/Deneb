import fs from "node:fs";
import path from "node:path";
import { loadNative, type NativeGitignoreMatcher } from "../bindings/native.js";

export interface GitignorePattern {
  /** The raw line from the .gitignore file. */
  raw: string;
  /** The normalized pattern (trimmed, without leading `!` or trailing `/`). */
  pattern: string;
  /** Whether this is a negation pattern (prefixed with `!`). */
  negated: boolean;
  /** Whether this pattern only matches directories (trailing `/`). */
  directoryOnly: boolean;
  /** Compiled regex for matching. */
  regex: RegExp;
}

export interface GitignoreResult {
  /** Parsed patterns (empty on error). */
  patterns: GitignorePattern[];
  /** Error encountered while reading/parsing, if any. */
  error: Error | null;
}

/**
 * Escape a string for use in a regular expression.
 */
function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Convert a gitignore glob pattern to a RegExp.
 *
 * Supports:
 * - `*` matches anything except `/`
 * - `**` matches everything including `/`
 * - `?` matches any single character except `/`
 * - Character classes `[abc]`
 */
function patternToRegex(pattern: string): RegExp {
  let regexStr = "";
  let i = 0;
  const len = pattern.length;
  const isAnchored = pattern.includes("/") && !pattern.endsWith("/");

  while (i < len) {
    const char = pattern[i];
    if (char === "*") {
      if (i + 1 < len && pattern[i + 1] === "*") {
        // `**/` or `**` at end
        if (i + 2 < len && pattern[i + 2] === "/") {
          regexStr += "(?:.+/)?";
          i += 3;
        } else {
          regexStr += ".*";
          i += 2;
        }
      } else {
        regexStr += "[^/]*";
        i += 1;
      }
    } else if (char === "?") {
      regexStr += "[^/]";
      i += 1;
    } else if (char === "[") {
      const closeIdx = pattern.indexOf("]", i + 1);
      if (closeIdx === -1) {
        regexStr += escapeRegex(char);
        i += 1;
      } else {
        const classContent = pattern.slice(i, closeIdx + 1);
        regexStr += classContent;
        i = closeIdx + 1;
      }
    } else {
      regexStr += escapeRegex(char);
      i += 1;
    }
  }

  // If the pattern does not contain a slash (besides trailing), it matches basename.
  // If anchored (contains `/`), it matches the full path.
  if (isAnchored) {
    return new RegExp(`^${regexStr}$`);
  }
  return new RegExp(`(?:^|/)${regexStr}$`);
}

/**
 * Parse a single line from a `.gitignore` file into a pattern, or `null` if the line
 * should be skipped (blank or comment).
 */
export function parseGitignoreLine(line: string): GitignorePattern | null {
  // Strip trailing whitespace (leading whitespace is significant in git).
  let trimmed = line.replace(/[\r\n]+$/, "").replace(/\s+$/, "");

  // Blank lines are ignored.
  if (!trimmed) {
    return null;
  }

  // Lines starting with `#` are comments.
  if (trimmed.startsWith("#")) {
    return null;
  }

  // Handle escaped `#` or `!` at start — the backslash makes them literal.
  if (trimmed.startsWith("\\#")) {
    trimmed = trimmed.slice(1);
  }

  let negated = false;
  if (trimmed.startsWith("\\!")) {
    // Escaped `!` — literal, not negation.
    trimmed = trimmed.slice(1);
  } else if (trimmed.startsWith("!")) {
    negated = true;
    trimmed = trimmed.slice(1);
  }

  let directoryOnly = false;
  if (trimmed.endsWith("/")) {
    directoryOnly = true;
    trimmed = trimmed.slice(0, -1);
  }

  // Remove leading `/` (anchors to repo root but doesn't affect matching logic
  // beyond the anchoring already handled by patternToRegex).
  if (trimmed.startsWith("/")) {
    trimmed = trimmed.slice(1);
  }

  if (!trimmed) {
    return null;
  }

  const regex = patternToRegex(trimmed);
  return {
    raw: line,
    pattern: trimmed,
    negated,
    directoryOnly,
    regex,
  };
}

/**
 * Parse the contents of a `.gitignore` file into patterns.
 * Never throws; returns an empty array for invalid/empty input.
 */
export function parseGitignore(content: string): GitignorePattern[] {
  if (!content || typeof content !== "string") {
    return [];
  }

  const lines = content.split("\n");
  const patterns: GitignorePattern[] = [];

  for (const line of lines) {
    try {
      const parsed = parseGitignoreLine(line);
      if (parsed) {
        patterns.push(parsed);
      }
    } catch {
      // Skip malformed lines rather than crashing.
    }
  }

  return patterns;
}

/**
 * Read and parse a `.gitignore` file safely.
 * Never throws; returns `{ patterns: [], error }` on failure.
 */
export function readGitignoreFile(filePath: string): GitignoreResult {
  try {
    const content = fs.readFileSync(filePath, "utf-8");
    const patterns = parseGitignore(content);
    return { patterns, error: null };
  } catch (error) {
    return {
      patterns: [],
      error: error instanceof Error ? error : new Error(String(error)),
    };
  }
}

/**
 * Read and parse a `.gitignore` from a directory (looks for `.gitignore` in the given dir).
 * Never throws; returns `{ patterns: [], error }` on failure.
 */
export function readGitignoreFromDir(dirPath: string): GitignoreResult {
  return readGitignoreFile(path.join(dirPath, ".gitignore"));
}

// Native matcher cache: keyed by the patterns array reference for quick lookup.
const nativeMatcherCache = new WeakMap<GitignorePattern[], NativeGitignoreMatcher>();

/**
 * Check whether a file path is ignored by the given gitignore patterns.
 *
 * Uses the native Rust addon when available for faster matching.
 * Falls back to the pure-TS regex loop otherwise.
 *
 * @param filePath - Relative file path (forward-slash separated).
 * @param patterns - Parsed gitignore patterns.
 * @param isDirectory - Whether the path refers to a directory.
 * @param originalContent - Optional raw .gitignore content for native acceleration.
 * @returns `true` if the path is ignored.
 */
export function isIgnoredByPatterns(
  filePath: string,
  patterns: GitignorePattern[],
  isDirectory = false,
  originalContent?: string,
): boolean {
  // Try native acceleration when original content is available.
  if (originalContent !== undefined) {
    const native = loadNative();
    if (native) {
      let matcher = nativeMatcherCache.get(patterns);
      if (!matcher) {
        try {
          matcher = new native.GitignoreMatcher(originalContent);
          nativeMatcherCache.set(patterns, matcher);
        } catch {
          // Fall through to TS implementation.
        }
      }
      if (matcher) {
        return matcher.isIgnored(filePath, isDirectory);
      }
    }
  }

  // Pure-TS fallback.
  const normalized = filePath.replace(/\\/g, "/").replace(/^\/+/, "");
  if (!normalized) {
    return false;
  }

  let ignored = false;

  for (const pattern of patterns) {
    if (pattern.directoryOnly && !isDirectory) {
      continue;
    }
    if (pattern.regex.test(normalized)) {
      ignored = !pattern.negated;
    }
  }

  return ignored;
}
