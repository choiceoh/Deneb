// Shared entity matcher for screen-follow features (결재 검토 모드 · 컨텍스트
// 팔로우 모드): find which known projects/people a piece of text talks about,
// so Deneb can pull the matching wiki page alongside. Pure, no fetching — the
// caller feeds it the already-cached digest/people lists.
export interface EntityHit {
  kind: "project" | "person";
  name: string;
  path: string; // wiki path to open
}

export interface EntitySource {
  projects: { name?: string; path?: string }[];
  people: { name?: string; path?: string }[];
}

// Korean titles embed entity names verbatim ("완도 관산포 프로젝트 관련 …"), so
// substring inclusion is the right primitive. Guards against noise: project
// names need ≥4 chars, person names ≥3 (two-char given names collide with
// ordinary words). Longest name wins; results dedupe by path, capped small.
export function matchEntities(text: string, source: EntitySource, limit = 2): EntityHit[] {
  const hay = text.trim();
  if (!hay) return [];
  const hits: EntityHit[] = [];
  const seen = new Set<string>();
  const candidates: EntityHit[] = [];
  for (const p of source.projects) {
    const name = (p.name ?? "").trim();
    if (name.length >= 4 && p.path) candidates.push({ kind: "project", name, path: p.path });
  }
  for (const p of source.people) {
    const name = (p.name ?? "").trim();
    if (name.length >= 3 && p.path) candidates.push({ kind: "person", name, path: p.path });
  }
  candidates.sort((a, b) => b.name.length - a.name.length);
  for (const c of candidates) {
    if (hits.length >= limit) break;
    if (seen.has(c.path)) continue;
    if (!hay.includes(c.name)) continue;
    seen.add(c.path);
    hits.push(c);
  }
  return hits;
}
