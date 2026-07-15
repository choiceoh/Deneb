import { useMemo } from "react";
import type { ProjectSiteRow } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { GridNotice } from "@/components/Grid";
import { KOREA_W, KOREA_H, PROVINCES, SIGUNGU, PROVINCE_CENTROID } from "./koreaGeo";

// 현장 지도 — plots each project's 현장(sites) onto a map of Korea, keyed by the
// administrative path the gateway already stores (Meta.Sites, "광역약칭 시/군 …").
// Reuses the miniapp.project.digests feed (now carrying `sites`), so no extra
// RPC. A site resolves to its 시군구 centroid (near the real address); one that
// names no known 시군구 falls back to the 시도, and one whose 시도 doesn't match
// is surfaced in the 미배치 tray rather than dropped — an honest map shows what
// it cannot place.

interface Pin {
  x: number;
  y: number;
  site: string;
  project: string;
  client?: string;
  path?: string;
  urgency: "overdue" | "soon" | "normal";
  due?: string;
}

// Whole calendar days until a YYYY-MM-DD due date; null when absent/unparseable.
// Compared date-to-date (both at local midnight) so a project due *today* reads
// as 0 (임박), not a fraction of a day negative (지연).
function dueDays(due?: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec((due ?? "").trim());
  if (!m) return null;
  const dueDate = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getTime();
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  return Math.round((dueDate - today) / 86_400_000);
}

function urgencyOf(due?: string): Pin["urgency"] {
  const d = dueDays(due);
  if (d === null) return "normal";
  if (d < 0) return "overdue";
  if (d <= 7) return "soon";
  return "normal";
}

const URGENCY_COLOR: Record<Pin["urgency"], string> = {
  overdue: "var(--danger)",
  soon: "var(--due)",
  normal: "var(--accent)",
};

// The 2013 boundary asset predates a few district renames/splits; alias those
// current 시군구 to the nearest legacy centroid so their 현장 still land in-area.
const DISTRICT_ALIAS: Record<string, string> = {
  "인천|미추홀구": "인천|남구",
  "충북|청주시서원구": "충북|청주시흥덕구",
  "충북|청주시청원구": "충북|청주시상당구",
};

function lookupSigungu(key: string): [number, number] | undefined {
  return SIGUNGU[key] ?? SIGUNGU[DISTRICT_ALIAS[key] ?? ""];
}

// Resolve a site string to map coords. Warded cities are keyed combined in the
// centroid table (e.g. "경기|성남시분당구"), so try 시+구 first, then the plain
// 시/군, then the 시도 centroid; null when even the 시도 doesn't match.
function resolveSite(site: string): [number, number] | null {
  const toks = site.trim().split(/\s+/);
  const sido = toks[0];
  if (!sido) return null;
  if (toks[1] && toks[2]) {
    const ward = lookupSigungu(`${sido}|${toks[1]}${toks[2]}`);
    if (ward) return ward;
  }
  if (toks[1]) {
    const city = lookupSigungu(`${sido}|${toks[1]}`);
    if (city) return city;
  }
  return PROVINCE_CENTROID[sido] ?? null;
}

interface Placed {
  pins: Pin[];
  unplaced: { site: string; project: string }[];
}

function placeSites(rows: ProjectSiteRow[]): Placed {
  const pins: Pin[] = [];
  const unplaced: { site: string; project: string }[] = [];
  const seen = new Map<string, number>(); // coord key → how many already there (jitter)
  for (const r of rows) {
    const project = r.project ?? "";
    const urgency = urgencyOf(r.due);
    for (const site of r.sites ?? []) {
      const xy = resolveSite(site);
      if (!xy) {
        unplaced.push({ site, project });
        continue;
      }
      // Nudge overlapping pins (same 시군구) apart deterministically.
      const key = `${xy[0]},${xy[1]}`;
      const n = seen.get(key) ?? 0;
      seen.set(key, n + 1);
      const ang = n * 2.399; // golden-angle spread
      const rad = n === 0 ? 0 : 5 + n * 1.5;
      pins.push({
        x: xy[0] + Math.cos(ang) * rad,
        y: xy[1] + Math.sin(ang) * rad,
        site,
        project,
        client: r.client,
        path: r.path,
        urgency,
        due: r.due,
      });
    }
  }
  return { pins, unplaced };
}

export function SiteMapPane() {
  const { connected, openWiki } = useWorkspace();
  const { result, query } = useCachedList<ProjectSiteRow>("sitemap", connected);
  const rows = result?.data ?? [];

  const { pins, unplaced } = useMemo(() => placeSites(rows), [rows]);

  const aiText = serializeList("현장 지도", pins, (p) => {
    const head = `- ${p.client ? `[${p.client}] ` : ""}${p.project} · ${p.site}`;
    return p.due ? `${head} (마감 ${p.due})` : head;
  });
  const aiFull = unplaced.length
    ? `${aiText}\n\n미배치(주소 매칭 실패) ${unplaced.length}건: ${unplaced.map((u) => u.site).join(", ")}`
    : aiText;
  useRegisterPane("sitemap", aiFull);

  const soon = pins.filter((p) => p.urgency !== "normal").length;

  return (
    <>
      <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginTop: 2 }}>
        <h2 style={{ margin: 0 }}>현장 지도</h2>
        <span style={{ fontSize: 12, color: "var(--muted)" }}>
          현장 {pins.length}
          {soon > 0 && <span style={{ color: "var(--due)" }}> · 임박 {soon}</span>}
          {unplaced.length > 0 && <span style={{ color: "var(--faint)" }}> · 미배치 {unplaced.length}</span>}
        </span>
      </div>

      <GridNotice query={query} count={pins.length + unplaced.length} empty="현장이 있는 프로젝트가 없습니다.">
        <div
          className="fade-up"
          style={{
            marginTop: 12,
            border: "1px solid var(--line)",
            borderRadius: "var(--radius-panel)",
            background: "var(--panel-sunken)",
            padding: 12,
          }}
        >
          <svg
            viewBox={`0 0 ${KOREA_W} ${KOREA_H}`}
            preserveAspectRatio="xMidYMid meet"
            role="img"
            aria-label="시도별 현장 지도"
            style={{ width: "100%", height: "auto", display: "block", maxHeight: "62vh" }}
          >
            {PROVINCES.map((p) => (
              <path
                key={p.key}
                d={p.d}
                fill="var(--panel)"
                stroke="var(--line-2)"
                strokeWidth={0.8}
                strokeLinejoin="round"
              />
            ))}
            {PROVINCES.map((p) => (
              <text
                key={`l-${p.key}`}
                x={p.cx}
                y={p.cy}
                textAnchor="middle"
                style={{ fontSize: 10, fill: "var(--faint)", pointerEvents: "none", userSelect: "none" }}
              >
                {p.key}
              </text>
            ))}
            {pins.map((pin, i) => (
              <g
                key={i}
                transform={`translate(${pin.x.toFixed(1)},${pin.y.toFixed(1)})`}
                onClick={pin.path ? () => openWiki(pin.path as string) : undefined}
                style={{ cursor: pin.path ? "pointer" : "default" }}
              >
                <title>
                  {pin.project} · {pin.site}
                  {pin.due ? ` · 마감 ${pin.due}` : ""}
                </title>
                <circle r={6.5} fill={URGENCY_COLOR[pin.urgency]} fillOpacity={0.18} />
                <circle r={4} fill={URGENCY_COLOR[pin.urgency]} stroke="var(--panel)" strokeWidth={1.2} />
              </g>
            ))}
          </svg>

          <div
            style={{
              display: "flex",
              gap: 16,
              marginTop: 8,
              paddingTop: 10,
              borderTop: "1px solid var(--line)",
              fontSize: 11,
              color: "var(--muted)",
              flexWrap: "wrap",
            }}
          >
            <Legend color="var(--accent)" label="진행" />
            <Legend color="var(--due)" label="임박 (마감 7일 내)" />
            <Legend color="var(--danger)" label="지연" />
          </div>
        </div>
      </GridNotice>

      {unplaced.length > 0 && (
        <section
          className="fade-up"
          style={{
            marginTop: 12,
            border: "1px dashed var(--line-2)",
            borderRadius: "var(--radius-ctl)",
            padding: "11px 14px",
          }}
        >
          <div style={{ fontSize: 12, fontWeight: 600, color: "var(--ink-2)", marginBottom: 6 }}>
            미배치 {unplaced.length} — 주소를 지도에 매칭하지 못한 현장
          </div>
          <ul style={{ margin: 0, padding: 0, listStyle: "none", display: "grid", gap: 4 }}>
            {unplaced.map((u, i) => (
              <li key={i} style={{ fontSize: 12, color: "var(--muted)" }}>
                {u.site} <span style={{ color: "var(--faint)" }}>· {u.project}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span style={{ width: 9, height: 9, borderRadius: "50%", background: color, display: "inline-block" }} />
      {label}
    </span>
  );
}
