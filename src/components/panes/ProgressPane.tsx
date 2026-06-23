import { useList } from "@refinedev/core";
import type { ProjectDigest } from "@/types";
import { serializeList } from "@/aiText";
import { fmtDate } from "@/format";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { GridNotice } from "@/components/Grid";

// 프로젝트 진행상황 — ports Deneb's miniapp.project.digests screen (#2834). The
// gateway distills each active project's 대표페이지 into a headline + bullets,
// newest-first; here we render them as scannable cards and project the whole
// briefing to Deneb so "어느 프로젝트부터 챙길까?" can be answered in context.
export function ProgressPane() {
  const { connected } = useWorkspace();
  const { result, query } = useList<ProjectDigest>({
    resource: "progress",
    queryOptions: { enabled: connected },
  });
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
      <p style={{ color: "var(--muted-2)", fontSize: 12, margin: "6px 0 16px", lineHeight: 1.5 }}>
        프로젝트별 최신 진행을 한눈에 — 데네브에게 “어느 프로젝트부터 챙길까?”라고 물어보세요.
      </p>
      <GridNotice query={query} count={digests.length} empty="진행 중인 프로젝트가 없습니다.">
        <div style={{ display: "flex", flexDirection: "column", gap: 10, maxWidth: 720 }}>
          {digests.map((d, i) => (
            <DigestCard key={d.path || d.project || i} digest={d} index={i} />
          ))}
        </div>
      </GridNotice>
    </>
  );
}

function DigestCard({ digest, index }: { digest: ProjectDigest; index: number }) {
  const { project, headline, bullets, due, updatedAtMs } = digest;
  const updated = fmtDate(updatedAtMs);
  return (
    <section
      className="fade-up"
      style={{
        border: "1px solid var(--line)",
        borderRadius: "var(--radius-ctl)",
        padding: "13px 15px",
        animationDelay: `${index * 50}ms`,
      }}
    >
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
        <p style={{ fontSize: 13.5, color: "var(--ink-2)", lineHeight: 1.5, margin: "5px 0 0" }}>{headline}</p>
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
    </section>
  );
}
