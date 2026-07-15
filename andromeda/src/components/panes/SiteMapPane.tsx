import { useMemo, useRef, useState } from "react";
import type { ProjectSiteRow } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { GridNotice } from "@/components/Grid";
import { Modal, Detail } from "@/components/Modal";
import { KOREA_W, KOREA_H, PROVINCES, SIGUNGU, EUPMYEON, PROVINCE_CENTROID } from "./koreaGeo";

// 현장 지도 — plots each active project's 현장(sites) onto a map of Korea, keyed by
// the administrative path the gateway stores (Meta.Sites, "광역약칭 시/군 읍/면/동").
// A pin encodes three business dimensions from the project's Meta:
//   색  = 에너지원  (Kinds 상위: 태양광/풍력/기자재/기타)
//   모양 = 특성      (Kinds 하위: 토지/루프탑/수상 …)
//   크기 = 용량      (Capacity, MW)
// Filter by 에너지원, 특성, or 시도(click a province); click a pin/row for detail.
// A site that names no known 시도 lands in the 미배치 tray, not silently dropped.

type Shape = "circle" | "square" | "triangle" | "diamond";

interface Pin {
  x: number;
  y: number;
  r: number; // radius = 용량 (MW) scaled
  site: string;
  sido: string;
  project: string;
  client?: string;
  path?: string;
  kinds: string[];
  source: string; // 에너지원 (Kinds 상위: 태양광/풍력/기자재/기타/"")
  type: string; // 특성 (Kinds 하위: 토지/루프탑/수상/…/"")
  capacity: number; // MW, 0 = unrecorded
  status: string; // 현장 lifecycle (후보/계약/개설/준공); "" = 미분류
  due?: string; // kept for the detail card only — not a visual dimension
}

// 후보(prospective) 현장 are hidden by default — the map is for real, contracted
// sites ("계약해서 개설된 현장"). A "" status (미분류, e.g. 대표페이지 fallback rows)
// is always shown; only an explicit 후보 is gated behind the toggle.
const PROSPECTIVE = "후보";

// 에너지원 → color. The warm-Zen palette is deliberately narrow, so we map the
// four sources onto the theme's semantic tokens rather than inventing hues:
// 태양광 = the hero product (warm clay), 풍력 = cool sage, 기자재 = neutral slate,
// 기타 = recessive gray.
const SOURCE_COLOR: Record<string, string> = {
  태양광: "var(--accent)",
  풍력: "var(--online)",
  기자재: "var(--ink-2)",
  기타: "var(--muted-2)",
};
const SOURCE_FALLBACK = "var(--muted-2)";
const SOURCE_ORDER = ["태양광", "풍력", "기자재", "기타"];
function sourceColor(source: string): string {
  return SOURCE_COLOR[source] ?? SOURCE_FALLBACK;
}

// 특성 → shape. The 태양광 site types the operator named drive the mark; any other
// sub-kind (ESS/육상/해상/모듈…) falls to 기타 (triangle).
const TYPE_SHAPE: Record<string, Shape> = {
  토지: "circle",
  루프탑: "square",
  수상: "diamond",
};
const TYPE_ORDER = ["토지", "루프탑", "수상", "기타"];
function shapeOfType(type: string): Shape {
  return TYPE_SHAPE[type] ?? "triangle";
}
function typeLabel(type: string): string {
  return type && TYPE_SHAPE[type] ? type : "기타";
}

// A project's Kinds are "상위/하위" (e.g. "태양광/루프탑"); the primary kind is the
// first entry. Split it into 에너지원 (source) and 특성 (type).
function primaryKind(kinds: string[]): { source: string; type: string } {
  const k = kinds[0] ?? "";
  const [source = "", type = ""] = k.split("/");
  return { source, type };
}

// Pin radius from 용량(MW): sqrt so a 100MW farm isn't 100× a 1MW rooftop, just
// visibly larger. Unrecorded (0) draws at a small base so it's still placeable.
function radiusOf(capacity: number): number {
  if (!capacity || capacity <= 0) return 3.2;
  return Math.min(3 + Math.sqrt(capacity) * 0.95, 13);
}

// Resolve a site string to map coords, finest first: 읍/면 → 시군구 → 시도, else
// null (unplaceable). Warded cities are keyed combined ("경기|성남시분당구"), so a
// site like "경기 성남시 분당구 정자동" resolves 시군구 from 시+구 and 읍면 from the
// next token; a plain "전북 군산시 옥구읍 수산리" resolves from 군산시/옥구읍.
function resolveSite(site: string): [number, number] | null {
  const t = site.trim().split(/\s+/);
  const sido = t[0];
  if (!sido) return null;
  let sgg: string | null = null;
  let dong: string | undefined;
  if (t[1] && t[2] && SIGUNGU[`${sido}|${t[1]}${t[2]}`]) {
    sgg = `${t[1]}${t[2]}`; // warded 시 (성남시분당구)
    dong = t[3];
  } else if (t[1] && SIGUNGU[`${sido}|${t[1]}`]) {
    sgg = t[1];
    dong = t[2];
  }
  if (sgg && dong) {
    const emd = EUPMYEON[`${sido}|${sgg}|${dong}`];
    if (emd) return emd;
  }
  if (sgg) return SIGUNGU[`${sido}|${sgg}`];
  return PROVINCE_CENTROID[sido] ?? null;
}

interface Placed {
  pins: Pin[];
  unplaced: { site: string; project: string }[];
}

function placeSites(rows: ProjectSiteRow[]): Placed {
  const pins: Pin[] = [];
  const unplaced: { site: string; project: string }[] = [];
  const seen = new Map<string, number>();
  for (const r of rows) {
    const project = r.project ?? "";
    const kinds = r.kinds ?? [];
    const { source, type } = primaryKind(kinds);
    const capacity = r.capacity ?? 0;
    const rad = radiusOf(capacity);
    for (const site of r.sites ?? []) {
      const xy = resolveSite(site);
      if (!xy) {
        unplaced.push({ site, project });
        continue;
      }
      const key = `${xy[0]},${xy[1]}`;
      const n = seen.get(key) ?? 0;
      seen.set(key, n + 1);
      const ang = n * 2.399;
      const spread = n === 0 ? 0 : rad + 4 + n * 1.5;
      pins.push({
        x: xy[0] + Math.cos(ang) * spread,
        y: xy[1] + Math.sin(ang) * spread,
        r: rad,
        site,
        sido: site.trim().split(/\s+/)[0],
        project,
        client: r.client,
        path: r.path,
        kinds,
        source,
        type,
        capacity,
        status: (r.status ?? "").trim(),
        due: r.due,
      });
    }
  }
  return { pins, unplaced };
}

// A filled 특성 mark (shape) of radius r at the origin; caller translates it.
function Mark({ shape, fill, r }: { shape: Shape; fill: string; r: number }) {
  const common = { fill, stroke: "var(--panel)", strokeWidth: 1.2 };
  if (shape === "square") return <rect x={-r} y={-r} width={r * 2} height={r * 2} rx={1.4} {...common} />;
  if (shape === "triangle") return <polygon points={`0,${-r * 1.15} ${r},${r * 0.75} ${-r},${r * 0.75}`} {...common} />;
  if (shape === "diamond")
    return <polygon points={`0,${-r * 1.2} ${r * 1.05},0 0,${r * 1.2} ${-r * 1.05},0`} {...common} />;
  return <circle r={r} {...common} />;
}

function capacityText(mw: number): string {
  if (!mw || mw <= 0) return "미기재";
  return `${Number.isInteger(mw) ? mw : mw.toFixed(1)}MW`;
}

export function SiteMapPane() {
  const { connected, openWiki } = useWorkspace();
  const { result, query } = useCachedList<ProjectSiteRow>("sitemap", connected);
  const { pins, unplaced } = useMemo(() => placeSites(result?.data ?? []), [result?.data]);

  const [sourceFilter, setSourceFilter] = useState<Set<string>>(new Set());
  const [typeFilter, setTypeFilter] = useState<Set<string>>(new Set());
  const [sidoFilter, setSidoFilter] = useState<string | null>(null);
  const [showProspective, setShowProspective] = useState(false);
  const [selected, setSelected] = useState<Pin | null>(null);

  // Wheel-zoom + drag-pan via a controlled viewBox. Full extent = the whole map;
  // scrolling the wheel zooms toward the cursor, dragging pans (only meaningful
  // once zoomed in). pannedRef suppresses the click that ends a drag so a pan
  // doesn't also select a pin / filter a 시도.
  const svgRef = useRef<SVGSVGElement | null>(null);
  const [view, setView] = useState({ x: 0, y: 0, w: KOREA_W, h: KOREA_H });
  const dragRef = useRef<{ x: number; y: number } | null>(null);
  const pannedRef = useRef(false);
  const zoomed = view.w < KOREA_W - 0.5;
  const resetView = () => setView({ x: 0, y: 0, w: KOREA_W, h: KOREA_H });

  // Wheel must be a non-passive native listener to preventDefault (stop the pane
  // from scrolling under the zoom). One stable handler (functional setView keeps
  // it closure-free) attached via a callback ref — the <svg> mounts late (inside
  // GridNotice, after data loads), so a mount-time useEffect would miss it.
  const wheelHandler = useRef((e: WheelEvent) => {
    e.preventDefault();
    const svg = svgRef.current;
    if (!svg) return;
    const rect = svg.getBoundingClientRect();
    const px = (e.clientX - rect.left) / rect.width;
    const py = (e.clientY - rect.top) / rect.height;
    setView((v) => {
      const factor = e.deltaY < 0 ? 0.85 : 1 / 0.85;
      const minW = KOREA_W / 8;
      const nw = Math.min(Math.max(v.w * factor, minW), KOREA_W);
      const nh = nw * (KOREA_H / KOREA_W);
      const cx = v.x + px * v.w;
      const cy = v.y + py * v.h;
      const nx = Math.min(Math.max(cx - px * nw, 0), KOREA_W - nw);
      const ny = Math.min(Math.max(cy - py * nh, 0), KOREA_H - nh);
      return { x: nx, y: ny, w: nw, h: nh };
    });
  }).current;
  const attachSvg = (node: SVGSVGElement | null) => {
    if (svgRef.current) svgRef.current.removeEventListener("wheel", wheelHandler);
    svgRef.current = node;
    if (node) node.addEventListener("wheel", wheelHandler, { passive: false });
  };

  const onPointerDown = (e: React.PointerEvent<SVGSVGElement>) => {
    dragRef.current = { x: e.clientX, y: e.clientY };
    pannedRef.current = false;
  };
  const onPointerMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const d = dragRef.current;
    const svg = svgRef.current;
    if (!d || !svg) return;
    const rect = svg.getBoundingClientRect();
    const dx = (e.clientX - d.x) / rect.width;
    const dy = (e.clientY - d.y) / rect.height;
    if (Math.abs(e.clientX - d.x) + Math.abs(e.clientY - d.y) > 3) pannedRef.current = true;
    dragRef.current = { x: e.clientX, y: e.clientY };
    setView((v) => ({
      ...v,
      x: Math.min(Math.max(v.x - dx * v.w, 0), KOREA_W - v.w),
      y: Math.min(Math.max(v.y - dy * v.h, 0), KOREA_H - v.h),
    }));
  };
  const endPan = () => {
    dragRef.current = null;
  };

  // 에너지원/특성 chips are shown only for values actually present, in a stable order.
  const sourcesPresent = useMemo(() => {
    const s = new Set(pins.map((p) => p.source).filter(Boolean));
    return SOURCE_ORDER.filter((k) => s.has(k)).concat([...s].filter((k) => !SOURCE_ORDER.includes(k)));
  }, [pins]);
  const typesPresent = useMemo(() => {
    const s = new Set(pins.map((p) => typeLabel(p.type)));
    return TYPE_ORDER.filter((k) => s.has(k));
  }, [pins]);

  const prospectiveCount = useMemo(() => pins.filter((p) => p.status === PROSPECTIVE).length, [pins]);

  const shown = useMemo(
    () =>
      pins.filter(
        (p) =>
          (showProspective || p.status !== PROSPECTIVE) &&
          (sourceFilter.size === 0 || sourceFilter.has(p.source)) &&
          (typeFilter.size === 0 || typeFilter.has(typeLabel(p.type))) &&
          (!sidoFilter || p.sido === sidoFilter),
      ),
    [pins, sourceFilter, typeFilter, sidoFilter, showProspective],
  );

  const totalMw = useMemo(() => shown.reduce((sum, p) => sum + (p.capacity || 0), 0), [shown]);

  const aiText = serializeList("현장 지도", shown, (p) => {
    const tags = [p.source, typeLabel(p.type), p.capacity ? capacityText(p.capacity) : ""].filter(Boolean).join("/");
    const head = `- ${p.client ? `[${p.client}] ` : ""}${p.project} · ${p.site}${tags ? ` (${tags})` : ""}`;
    return p.due ? `${head} — 마감 ${p.due}` : head;
  });
  const aiFull = unplaced.length
    ? `${aiText}\n\n미배치(주소 매칭 실패) ${unplaced.length}건: ${unplaced.map((u) => u.site).join(", ")}`
    : aiText;
  useRegisterPane("sitemap", aiFull);

  const toggle = <T,>(set: Set<T>, v: T, apply: (s: Set<T>) => void) => {
    const next = new Set(set);
    if (next.has(v)) next.delete(v);
    else next.add(v);
    apply(next);
  };
  const filtered = sourceFilter.size > 0 || typeFilter.size > 0 || !!sidoFilter;

  return (
    <>
      <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginTop: 2 }}>
        <h2 style={{ margin: 0 }}>현장 지도</h2>
        <span style={{ fontSize: 12, color: "var(--muted)" }}>
          현장 {shown.length}
          {shown.length !== pins.length && <span style={{ color: "var(--faint)" }}>/{pins.length}</span>}
          {totalMw > 0 && <span style={{ color: "var(--faint)" }}> · 총 {capacityText(totalMw)}</span>}
          {unplaced.length > 0 && <span style={{ color: "var(--faint)" }}> · 미배치 {unplaced.length}</span>}
        </span>
      </div>

      {/* Filters — 에너지원 chips + 특성 chips + 시도 (map click) */}
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 10, alignItems: "center" }}>
        {sourcesPresent.map((k) => (
          <Chip
            key={k}
            on={sourceFilter.has(k)}
            onClick={() => toggle(sourceFilter, k, setSourceFilter)}
            dot={sourceColor(k)}
          >
            {k}
          </Chip>
        ))}
        {sourcesPresent.length > 0 && typesPresent.length > 0 && (
          <span style={{ width: 1, height: 16, background: "var(--line-2)", margin: "0 2px" }} />
        )}
        {typesPresent.map((k) => (
          <Chip
            key={k}
            on={typeFilter.has(k)}
            onClick={() => toggle(typeFilter, k, setTypeFilter)}
            shape={shapeOfType(k)}
          >
            {k}
          </Chip>
        ))}
        {sidoFilter && (
          <Chip on onClick={() => setSidoFilter(null)}>
            {sidoFilter} ✕
          </Chip>
        )}
        {prospectiveCount > 0 && (
          <Chip on={showProspective} onClick={() => setShowProspective((v) => !v)}>
            후보 포함 {prospectiveCount}
          </Chip>
        )}
        {filtered && (
          <button
            type="button"
            onClick={() => {
              setSourceFilter(new Set());
              setTypeFilter(new Set());
              setSidoFilter(null);
            }}
            style={{
              marginLeft: "auto",
              fontSize: 11,
              color: "var(--muted)",
              background: "none",
              border: "none",
              cursor: "pointer",
            }}
          >
            필터 초기화
          </button>
        )}
      </div>

      <GridNotice query={query} count={pins.length + unplaced.length} empty="현장이 있는 프로젝트가 없습니다.">
        <div
          className="fade-up"
          style={{
            position: "relative",
            marginTop: 10,
            border: "1px solid var(--line)",
            borderRadius: "var(--radius-panel)",
            background: "var(--panel-sunken)",
            padding: 12,
          }}
        >
          {zoomed && (
            <button
              type="button"
              onClick={resetView}
              style={{
                position: "absolute",
                top: 18,
                right: 18,
                zIndex: 1,
                fontSize: 11,
                padding: "3px 10px",
                borderRadius: "var(--radius-pill)",
                border: "1px solid var(--line-2)",
                background: "var(--panel)",
                color: "var(--ink-2)",
                cursor: "pointer",
              }}
            >
              맞춤
            </button>
          )}
          <svg
            ref={attachSvg}
            viewBox={`${view.x.toFixed(1)} ${view.y.toFixed(1)} ${view.w.toFixed(1)} ${view.h.toFixed(1)}`}
            preserveAspectRatio="xMidYMid meet"
            role="img"
            aria-label="시도별 현장 지도 (휠 확대·드래그 이동)"
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={endPan}
            onPointerLeave={endPan}
            style={{
              width: "100%",
              height: "auto",
              display: "block",
              maxHeight: "58vh",
              cursor: zoomed ? "grab" : "default",
              touchAction: "none",
            }}
          >
            {PROVINCES.map((p) => (
              <path
                key={p.key}
                d={p.d}
                fill={sidoFilter === p.key ? "var(--accent-soft)" : "var(--panel)"}
                stroke={sidoFilter === p.key ? "var(--accent)" : "var(--line-2)"}
                strokeWidth={sidoFilter === p.key ? 1.2 : 0.8}
                strokeLinejoin="round"
                style={{ cursor: "pointer", opacity: sidoFilter && sidoFilter !== p.key ? 0.55 : 1 }}
                onClick={() => {
                  if (pannedRef.current) return;
                  setSidoFilter((cur) => (cur === p.key ? null : p.key));
                }}
              >
                <title>{p.name}</title>
              </path>
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
            {shown.map((pin, i) => (
              <g
                key={i}
                transform={`translate(${pin.x.toFixed(1)},${pin.y.toFixed(1)})`}
                onClick={() => {
                  if (pannedRef.current) return;
                  setSelected(pin);
                }}
                style={{ cursor: "pointer" }}
              >
                <title>
                  {pin.project} · {pin.site}
                  {pin.source ? ` · ${pin.source}` : ""}
                  {pin.type ? `/${typeLabel(pin.type)}` : ""}
                  {pin.capacity ? ` · ${capacityText(pin.capacity)}` : ""}
                </title>
                <circle r={pin.r + 3} fill={sourceColor(pin.source)} fillOpacity={0.16} />
                <Mark shape={shapeOfType(pin.type)} fill={sourceColor(pin.source)} r={pin.r} />
              </g>
            ))}
          </svg>

          <div
            style={{
              display: "flex",
              gap: 14,
              marginTop: 8,
              paddingTop: 10,
              borderTop: "1px solid var(--line)",
              fontSize: 11,
              color: "var(--muted)",
              flexWrap: "wrap",
              alignItems: "center",
            }}
          >
            <span style={{ color: "var(--faint)" }}>색=에너지원</span>
            {sourcesPresent.map((k) => (
              <LegendDot key={k} color={sourceColor(k)} label={k} />
            ))}
            <span style={{ width: 1, height: 12, background: "var(--line-2)" }} />
            <span style={{ color: "var(--faint)" }}>모양=특성</span>
            {typesPresent.map((k) => (
              <LegendShape key={k} shape={shapeOfType(k)} label={k} />
            ))}
            <span style={{ width: 1, height: 12, background: "var(--line-2)" }} />
            <span style={{ color: "var(--faint)" }}>크기=용량</span>
            <LegendSize />
          </div>
        </div>
      </GridNotice>

      {/* Filtered site list — click a row for the same detail card as a pin. */}
      {shown.length > 0 && (
        <div style={{ marginTop: 12, display: "grid", gap: 4 }}>
          {shown
            .slice()
            .sort((a, b) => (b.capacity || 0) - (a.capacity || 0))
            .map((pin, i) => (
              <button
                key={i}
                type="button"
                className="fade-up"
                onClick={() => setSelected(pin)}
                style={{
                  display: "grid",
                  gridTemplateColumns: "1fr auto",
                  alignItems: "center",
                  gap: 10,
                  padding: "9px 12px",
                  border: "1px solid var(--line)",
                  borderRadius: "var(--radius-ctl)",
                  background: "transparent",
                  textAlign: "left",
                  cursor: "pointer",
                  animationDelay: `${Math.min(i, 12) * 30}ms`,
                }}
              >
                <span style={{ minWidth: 0, display: "inline-flex", alignItems: "center", gap: 8 }}>
                  <svg width={14} height={14} viewBox="-7 -7 14 14" aria-hidden style={{ flexShrink: 0 }}>
                    <Mark shape={shapeOfType(pin.type)} fill={sourceColor(pin.source)} r={5} />
                  </svg>
                  <span style={{ fontSize: 13, color: "var(--ink)" }}>{pin.site}</span>
                  <span style={{ fontSize: 11, color: "var(--muted)" }}>
                    {pin.project}
                    {pin.client ? ` · ${pin.client}` : ""}
                  </span>
                </span>
                <span style={{ fontSize: 11, color: "var(--muted)", whiteSpace: "nowrap" }}>
                  {[pin.source, capacityText(pin.capacity)].filter(Boolean).join(" · ")}
                </span>
              </button>
            ))}
        </div>
      )}

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

      {selected && (
        <Modal
          title={selected.project}
          onClose={() => setSelected(null)}
          footer={
            selected.path ? (
              <button
                type="button"
                className="btn btn-accent"
                onClick={() => {
                  openWiki(selected.path as string);
                  setSelected(null);
                }}
              >
                위키 열기
              </button>
            ) : undefined
          }
        >
          {selected.client && <Detail label="거래처" value={selected.client} />}
          <Detail label="현장" value={selected.site} />
          {selected.status && <Detail label="상태" value={selected.status} />}
          {selected.source && <Detail label="에너지원" value={selected.source} />}
          {selected.type && <Detail label="특성" value={typeLabel(selected.type)} />}
          <Detail label="용량" value={capacityText(selected.capacity)} />
          {selected.kinds.length > 0 && <Detail label="분류" value={selected.kinds.join(", ")} />}
          <Detail label="마감" value={selected.due || "미정"} />
        </Modal>
      )}
    </>
  );
}

function Chip({
  children,
  on,
  onClick,
  dot,
  shape,
}: {
  children: React.ReactNode;
  on: boolean;
  onClick: () => void;
  dot?: string;
  shape?: Shape;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 5,
        fontSize: 12,
        padding: "3px 10px",
        borderRadius: "var(--radius-pill)",
        border: `1px solid ${on ? "var(--accent)" : "var(--line-2)"}`,
        background: on ? "var(--accent-soft)" : "transparent",
        color: on ? "var(--accent-deep)" : "var(--ink-2)",
        cursor: "pointer",
      }}
    >
      {dot && <span style={{ width: 8, height: 8, borderRadius: "50%", background: dot }} />}
      {shape && (
        <svg width={11} height={11} viewBox="-6 -6 12 12" aria-hidden>
          <Mark shape={shape} fill="var(--muted)" r={4.5} />
        </svg>
      )}
      {children}
    </button>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
      <span style={{ width: 9, height: 9, borderRadius: "50%", background: color, display: "inline-block" }} />
      {label}
    </span>
  );
}
function LegendShape({ shape, label }: { shape: Shape; label: string }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
      <svg width={12} height={12} viewBox="-6 -6 12 12" aria-hidden>
        <Mark shape={shape} fill="var(--muted)" r={4.5} />
      </svg>
      {label}
    </span>
  );
}
// Two reference circles — small vs large — to read the 용량→크기 mapping.
function LegendSize() {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}>
      <svg width={30} height={16} viewBox="0 0 30 16" aria-hidden>
        <circle cx={6} cy={8} r={3} fill="var(--muted)" />
        <circle cx={22} cy={8} r={6.5} fill="var(--muted)" />
      </svg>
      소→대
    </span>
  );
}
