/** Rough token estimate: ~4 chars per token. */
export function estimateTokensFromText(text: string): number {
  return Math.ceil(text.length / 4);
}
