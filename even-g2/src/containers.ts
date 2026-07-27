// Structural checks over the container declarations in main.ts.
//
// These exist because the SDK's page rules are checkable from the source and
// one of them shipped broken anyway: three containers carried `zOrderIndex` and
// the fourth did not, so the SDK rejected the entire startup page and the
// glasses drew nothing — indistinguishable from a dead plugin, diagnosable only
// through a WebView console nobody can read.
//
// Pure over source text so the rules can be proven against known-bad samples.
// A guard that has never been shown to fail is not a guard.

export type ContainerIssue = string;

/** Extracts each `new *ContainerProperty({ … })` option block. */
export function containerBlocks(src: string): string[] {
  const out: string[] = [];
  const re = /new (?:Text|Image|List)ContainerProperty\(\{/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    const start = m.index;
    const end = src.indexOf("})", start);
    if (end > start) out.push(src.slice(start, end));
  }
  return out;
}

/**
 * checkContainers returns every violation of the page rules, empty when sound.
 *
 * Rules come from the SDK's own documentation; the consequence of breaking any
 * of them is the same and it is total — `createStartUpPageContainer` returns
 * invalid and nothing renders.
 */
export function checkContainers(src: string): ContainerIssue[] {
  const blocks = containerBlocks(src);
  const issues: ContainerIssue[] = [];
  if (blocks.length === 0) return ["no container declarations found"];

  const withZ = blocks.filter((b) => b.includes("zOrderIndex"));
  if (withZ.length !== 0 && withZ.length !== blocks.length) {
    issues.push(
      `zOrderIndex on ${withZ.length}/${blocks.length} containers — the SDK rejects the whole page when it is partial`,
    );
  }

  const z = blocks
    .map((b) => b.match(/zOrderIndex:\s*(\d+)/)?.[1])
    .filter((v): v is string => v !== undefined);
  if (new Set(z).size !== z.length) issues.push("duplicate zOrderIndex");

  const ids = blocks
    .map((b) => b.match(/containerID:\s*(\w+)/)?.[1])
    .filter((v): v is string => v !== undefined);
  if (new Set(ids).size !== ids.length) issues.push("duplicate containerID");

  const capturing = blocks.filter((b) => /isEventCapture:\s*1/.test(b));
  if (capturing.length !== 1) {
    issues.push(`${capturing.length} event-capture containers, want exactly 1`);
  }

  for (const b of blocks.filter((x) => x.includes("Image"))) {
    const w = Number(b.match(/width:\s*(\d+)/)?.[1] ?? NaN);
    const h = Number(b.match(/height:\s*(\d+)/)?.[1] ?? NaN);
    // Bounds are 20~288 × 20~144; sitting ON the maximum risks an
    // inclusive/exclusive edge for two pixels of nothing.
    if (Number.isFinite(w) && (w >= 288 || w < 20))
      issues.push(`image width ${w}`);
    if (Number.isFinite(h) && (h >= 144 || h < 20))
      issues.push(`image height ${h}`);
  }
  return issues;
}
