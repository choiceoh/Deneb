import { Fragment, useState } from "react";
import type { ProjectDigest } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { fmtDate } from "@/format";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { GridNotice } from "@/components/Grid";

// 프로젝트 진행상황 — ports Deneb's miniapp.project.digests screen (#2834). The
// gateway distills each active project's 대표페이지 into a headline + bullets,
// newest-first; here we render them as scannable cards grouped by 거래처 (the
// top level of the project hierarchy) and project the whole briefing to Deneb
// so "어느 프로젝트부터 챙길까?" can be answered in context.

// Group rows by client, first-occurrence order (the list is newest-first, so a
// client sorts where its most recently active project does). Rows keep their
// order within a group; "" collects the clientless rows.
function groupByClient(digests: ProjectDigest[]): { client: string; rows: ProjectDigest[] }[] {
  const groups: { client: string; rows: ProjectDigest[] }[] = [];
  for (const d of digests) {
    const key = (d.client ?? "").trim();
    const g = groups.find((x) => x.client === key);
    if (g) g.rows.push(d);
    else groups.push({ client: key, rows: [d] });
  }
  return groups;
}

export function ProgressPane() {
  const { connected, openWiki } = useWorkspace();
  const { result, query } = useCachedList<ProjectDigest>("progress", connected);
  const digests = result?.data ?? [];

  // Collapsed 거래처 groups (by client key). Default expanded; a click on the
  // group header folds that client's cards away so a long briefing stays
  // scannable — the top hierarchy level (거래처) is the natural fold point.
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggle = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  // Counted header + one block per project (client tag, headline, indented
  // bullets, due) — the AI reads exactly what's on screen.
  const aiText = serializeList("프로젝트 진행상황", digests, (d) => {
    const head =
      `- ${d.client ? `[${d.client}] ` : ""}${d.project}` +
      (d.headline ? `: ${d.headline}` : "") +
      (d.due ? ` (마감 ${d.due})` : "");
    const bullets = (d.bullets ?? []).map((b) => `    • ${b}`).join("\n");
    return bullets ? `${head}\n${bullets}` : head;
  });
  useRegisterPane("progress", aiText);

  // No client anywhere → the flat pre-backfill list, no group labels.
  const groups = groupByClient(digests);
  const grouped = groups.some((g) => g.client !== "");
  let cardIndex = 0;

  return (
    <>
      <h2 style={{ marginTop: 2 }}>프로젝트 진행상황</h2>
      <GridNotice query={query} count={digests.length} empty="진행 중인 프로젝트가 없습니다.">
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {groups.map((g) => {
            const key = g.client || "(미지정)";
            const isCollapsed = grouped && collapsed.has(key);
            return (
              <Fragment key={key}>
                {grouped && (
                  <button
                    type="button"
                    onClick={() => toggle(key)}
                    aria-expanded={!isCollapsed}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 6,
                      width: "100%",
                      background: "transparent",
                      border: "none",
                      padding: 0,
                      margin: "8px 0 -2px",
                      fontSize: 12,
                      fontWeight: 600,
                      letterSpacing: "0.04em",
                      color: "var(--muted)",
                      fontFamily: "inherit",
                      cursor: "pointer",
                      textAlign: "left",
                    }}
                  >
                    <span
                      aria-hidden
                      style={{
                        display: "inline-block",
                        transform: isCollapsed ? "rotate(0deg)" : "rotate(90deg)",
                        transition: "transform 120ms",
                        color: "var(--faint)",
                      }}
                    >
                      ▸
                    </span>
                    {g.client || "거래처 미지정"}
                    <span style={{ color: "var(--faint)", fontWeight: 400 }}>{g.rows.length}</span>
                  </button>
                )}
                {!isCollapsed &&
                  g.rows.map((d, i) => (
                    <DigestCard key={d.path || d.project || i} digest={d} index={cardIndex++} onOpenWiki={openWiki} />
                  ))}
              </Fragment>
            );
          })}
        </div>
      </GridNotice>
    </>
  );
}

function DigestCard({
  digest,
  index,
  onOpenWiki,
}: {
  digest: ProjectDigest;
  index: number;
  onOpenWiki: (path: string) => void;
}) {
  const { project, headline, bullets, due, updatedAtMs, path } = digest;
  const updated = fmtDate(updatedAtMs);
  const cardStyle = {
    border: "1px solid var(--line)",
    borderRadius: "var(--radius-ctl)",
    padding: "13px 15px",
    animationDelay: `${index * 50}ms`,
  };
  const body = (
    <>
      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <span style={{ fontWeight: 600, fontSize: 15, letterSpacing: "-0.01em", minWidth: 0 }}>{project}</span>
        {due && (
          <span
            style={{
              marginLeft: "auto",
              flex: "0 0 auto",
              background: "var(--accent-soft)",
              color: "var(--accent-deep)",
              fontSize: 12,
              padding: "2px 9px",
              borderRadius: "var(--radius-pill)",
              whiteSpace: "nowrap",
            }}
          >
            마감 {due}
          </span>
        )}
      </div>
      {headline && (
        <p style={{ fontSize: 14, color: "var(--ink-2)", lineHeight: 1.5, margin: "5px 0 0" }}>{headline}</p>
      )}
      {bullets && bullets.length > 0 && (
        <ul style={{ margin: "8px 0 0", padding: 0, listStyle: "none", display: "grid", gap: 4 }}>
          {bullets.map((b, j) => (
            <li
              key={j}
              style={{
                fontSize: 13,
                color: "var(--muted)",
                lineHeight: 1.5,
                paddingLeft: 13,
                position: "relative",
              }}
            >
              <span style={{ position: "absolute", left: 0, color: "var(--faint)" }}>•</span>
              {b}
            </li>
          ))}
        </ul>
      )}
      {updated && <div style={{ fontSize: 11, color: "var(--faint)", marginTop: 9 }}>업데이트 {updated}</div>}
    </>
  );

  return path ? (
    <button
      className="fade-up"
      type="button"
      onClick={() => onOpenWiki(path)}
      title="위키 열기"
      style={{
        ...cardStyle,
        display: "block",
        width: "100%",
        background: "transparent",
        color: "inherit",
        textAlign: "left",
        fontFamily: "inherit",
        cursor: "pointer",
      }}
    >
      {body}
    </button>
  ) : (
    <section className="fade-up" style={cardStyle}>
      {body}
    </section>
  );
}
