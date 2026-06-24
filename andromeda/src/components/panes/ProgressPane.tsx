import type { ProjectDigest } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { fmtDate } from "@/format";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { GridNotice } from "@/components/Grid";

// 프로젝트 진행상황 — ports Deneb's miniapp.project.digests screen (#2834). The
// gateway distills each active project's 대표페이지 into a headline + bullets,
// newest-first; here we render them as scannable cards and project the whole
// briefing to Deneb so "어느 프로젝트부터 챙길까?" can be answered in context.
export function ProgressPane() {
  const { connected, openWiki } = useWorkspace();
  const { result, query } = useCachedList<ProjectDigest>("progress", connected);
  const digests = result?.data ?? [];

  // Counted header + one block per project (headline, indented bullets, due) — the
  // AI reads exactly what's on screen.
  const aiText = serializeList("프로젝트 진행상황", digests, (d) => {
    const head = `- ${d.project}` + (d.headline ? `: ${d.headline}` : "") + (d.due ? ` (마감 ${d.due})` : "");
    const bullets = (d.bullets ?? []).map((b) => `    • ${b}`).join("\n");
    return bullets ? `${head}\n${bullets}` : head;
  });
  useRegisterPane("progress", aiText);

  return (
    <>
      <h2 style={{ marginTop: 2 }}>프로젝트 진행상황</h2>
      <GridNotice query={query} count={digests.length} empty="진행 중인 프로젝트가 없습니다.">
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {digests.map((d, i) => (
            <DigestCard key={d.path || d.project || i} digest={d} index={i} onOpenWiki={openWiki} />
          ))}
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
