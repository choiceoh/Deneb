/**
 * Native bridge for memory search functions.
 *
 * Delegates to the Rust native addon when available, falling back to
 * the TypeScript implementations. Callers should use these functions
 * instead of importing the TS implementations directly.
 */

import { type CoreRsModule, loadCoreRs } from "../bindings/core-rs.js";

import { cosineSimilarity as cosineSimilarityTS } from "./internal.js";
import { bm25RankToScore as bm25RankToScoreTS, buildFtsQuery as buildFtsQueryTS } from "./hybrid.js";
import {
  calculateTemporalDecayMultiplier as calculateTemporalDecayMultiplierTS,
  applyTemporalDecayToScore as applyTemporalDecayToScoreTS,
} from "./temporal-decay.js";
import { textSimilarity as textSimilarityTS } from "./mmr.js";
import {
  extractKeywords as extractKeywordsTS,
  isQueryStopWordToken as isQueryStopWordTokenTS,
} from "./query-expansion.js";

// Cache the native module reference once at first use.
let nativeChecked = false;
let native: CoreRsModule | null = null;

function getNative(): CoreRsModule | null {
  if (!nativeChecked) {
    nativeChecked = true;
    native = loadCoreRs();
  }
  return native;
}

export function cosineSimilarity(a: number[], b: number[]): number {
  const n = getNative();
  if (n) {
    return n.memoryCosineSimilarity(a, b);
  }
  return cosineSimilarityTS(a, b);
}

export function bm25RankToScore(rank: number): number {
  const n = getNative();
  if (n) {
    return n.memoryBm25RankToScore(rank);
  }
  return bm25RankToScoreTS(rank);
}

export function buildFtsQuery(raw: string): string | null {
  const n = getNative();
  if (n) {
    return n.memoryBuildFtsQuery(raw);
  }
  return buildFtsQueryTS(raw);
}

export function calculateTemporalDecayMultiplier(params: {
  ageInDays: number;
  halfLifeDays: number;
}): number {
  const n = getNative();
  if (n) {
    return n.memoryTemporalDecayMultiplier(params.ageInDays, params.halfLifeDays);
  }
  return calculateTemporalDecayMultiplierTS(params);
}

export function applyTemporalDecayToScore(params: {
  score: number;
  ageInDays: number;
  halfLifeDays: number;
}): number {
  const n = getNative();
  if (n) {
    return n.memoryApplyTemporalDecay(params.score, params.ageInDays, params.halfLifeDays);
  }
  return applyTemporalDecayToScoreTS(params);
}

const DATED_MEMORY_PATH_RE = /(?:^|\/)memory\/(\d{4})-(\d{2})-(\d{2})\.md$/;

/**
 * Parse a date from a memory file path like `memory/2026-01-07.md`.
 * Returns ISO date string "YYYY-MM-DD" or null.
 */
export function parseMemoryDateFromPath(filePath: string): string | null {
  const n = getNative();
  if (n) {
    return n.memoryParseMemoryDateFromPath(filePath);
  }
  // TS fallback: inline implementation matching temporal-decay.ts logic
  const normalized = filePath.replaceAll("\\", "/").replace(/^\.\//, "");
  const match = DATED_MEMORY_PATH_RE.exec(normalized);
  if (!match) {
    return null;
  }
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  if (!Number.isInteger(year) || !Number.isInteger(month) || !Number.isInteger(day)) {
    return null;
  }
  const parsed = new Date(Date.UTC(year, month - 1, day));
  if (
    parsed.getUTCFullYear() !== year ||
    parsed.getUTCMonth() !== month - 1 ||
    parsed.getUTCDate() !== day
  ) {
    return null;
  }
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

/**
 * Check if a memory file path is "evergreen" (not date-specific).
 */
export function isEvergreenMemoryPath(filePath: string): boolean {
  const n = getNative();
  if (n) {
    return n.memoryIsEvergreenMemoryPath(filePath);
  }
  // TS fallback: inline implementation matching temporal-decay.ts logic
  const normalized = filePath.replaceAll("\\", "/").replace(/^\.\//, "");
  if (normalized === "MEMORY.md" || normalized === "memory.md") {
    return true;
  }
  if (!normalized.startsWith("memory/")) {
    return false;
  }
  return !DATED_MEMORY_PATH_RE.test(normalized);
}

export function textSimilarity(a: string, b: string): number {
  const n = getNative();
  if (n) {
    return n.memoryTextSimilarity(a, b);
  }
  return textSimilarityTS(a, b);
}

export function extractKeywords(query: string): string[] {
  const n = getNative();
  if (n) {
    return n.memoryExtractKeywords(query);
  }
  return extractKeywordsTS(query);
}

export function isQueryStopWordToken(token: string): boolean {
  const n = getNative();
  if (n) {
    return n.memoryIsQueryStopWord(token);
  }
  return isQueryStopWordTokenTS(token);
}
