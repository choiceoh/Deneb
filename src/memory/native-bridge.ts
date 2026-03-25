/**
 * Native bridge for memory search functions.
 *
 * Delegates to the Rust native addon. The native addon is required.
 */

import { type CoreRsModule, loadCoreRs } from "../bindings/core-rs.js";

// Cache the native module reference once at first use.
let native: CoreRsModule | undefined;

function getNative(): CoreRsModule {
  if (!native) {
    native = loadCoreRs();
  }
  return native;
}

export function cosineSimilarity(a: number[], b: number[]): number {
  return getNative().memoryCosineSimilarity(a, b);
}

export function bm25RankToScore(rank: number): number {
  return getNative().memoryBm25RankToScore(rank);
}

export function buildFtsQuery(raw: string): string | null {
  return getNative().memoryBuildFtsQuery(raw);
}

export function calculateTemporalDecayMultiplier(params: {
  ageInDays: number;
  halfLifeDays: number;
}): number {
  return getNative().memoryTemporalDecayMultiplier(params.ageInDays, params.halfLifeDays);
}

export function applyTemporalDecayToScore(params: {
  score: number;
  ageInDays: number;
  halfLifeDays: number;
}): number {
  return getNative().memoryApplyTemporalDecay(params.score, params.ageInDays, params.halfLifeDays);
}

/**
 * Parse a date from a memory file path like `memory/2026-01-07.md`.
 * Returns ISO date string "YYYY-MM-DD" or null.
 */
export function parseMemoryDateFromPath(filePath: string): string | null {
  return getNative().memoryParseMemoryDateFromPath(filePath);
}

/**
 * Check if a memory file path is "evergreen" (not date-specific).
 */
export function isEvergreenMemoryPath(filePath: string): boolean {
  return getNative().memoryIsEvergreenMemoryPath(filePath);
}

export function textSimilarity(a: string, b: string): number {
  return getNative().memoryTextSimilarity(a, b);
}

export function extractKeywords(query: string): string[] {
  return getNative().memoryExtractKeywords(query);
}

export function isQueryStopWordToken(token: string): boolean {
  return getNative().memoryIsQueryStopWord(token);
}
