/**
 * Native bridge for memory search functions.
 *
 * Delegates to the Rust native addon when available, falling back to
 * the TypeScript implementations. Callers should use these functions
 * instead of importing the TS implementations directly.
 */

import { loadCoreRs } from "../bindings/core-rs.js";

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

export function cosineSimilarity(a: number[], b: number[]): number {
  const native = loadCoreRs();
  if (native) {
    return native.memoryCosineSimilarity(a, b);
  }
  return cosineSimilarityTS(a, b);
}

export function bm25RankToScore(rank: number): number {
  const native = loadCoreRs();
  if (native) {
    return native.memoryBm25RankToScore(rank);
  }
  return bm25RankToScoreTS(rank);
}

export function buildFtsQuery(raw: string): string | null {
  const native = loadCoreRs();
  if (native) {
    return native.memoryBuildFtsQuery(raw);
  }
  return buildFtsQueryTS(raw);
}

export function calculateTemporalDecayMultiplier(params: {
  ageInDays: number;
  halfLifeDays: number;
}): number {
  const native = loadCoreRs();
  if (native) {
    return native.memoryTemporalDecayMultiplier(params.ageInDays, params.halfLifeDays);
  }
  return calculateTemporalDecayMultiplierTS(params);
}

export function applyTemporalDecayToScore(params: {
  score: number;
  ageInDays: number;
  halfLifeDays: number;
}): number {
  const native = loadCoreRs();
  if (native) {
    return native.memoryApplyTemporalDecay(params.score, params.ageInDays, params.halfLifeDays);
  }
  return applyTemporalDecayToScoreTS(params);
}

export function parseMemoryDateFromPath(filePath: string): string | null {
  const native = loadCoreRs();
  if (native) {
    return native.memoryParseMemoryDateFromPath(filePath);
  }
  return null; // TS implementation is inline in temporal-decay.ts; only used via native
}

export function isEvergreenMemoryPath(filePath: string): boolean {
  const native = loadCoreRs();
  if (native) {
    return native.memoryIsEvergreenMemoryPath(filePath);
  }
  return false; // TS implementation is inline in temporal-decay.ts
}

export function textSimilarity(a: string, b: string): number {
  const native = loadCoreRs();
  if (native) {
    return native.memoryTextSimilarity(a, b);
  }
  return textSimilarityTS(a, b);
}

export function extractKeywords(query: string): string[] {
  const native = loadCoreRs();
  if (native) {
    return native.memoryExtractKeywords(query);
  }
  return extractKeywordsTS(query);
}

export function isQueryStopWordToken(token: string): boolean {
  const native = loadCoreRs();
  if (native) {
    return native.memoryIsQueryStopWord(token);
  }
  return isQueryStopWordTokenTS(token);
}
