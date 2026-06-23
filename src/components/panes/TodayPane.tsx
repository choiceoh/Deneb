import { useList } from "@refinedev/core";
import type { CalEvent, Mail, Todo, View, WorkItem } from "@/types";
import { useCachedList } from "@/cachedList";
import { calSpan, fmtDate, text } from "@/format";
import { Icon } from "@/components/Icon";
import { GridNotice } from "@/components/Grid";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// "오늘" dashboard — the workstation's landing pane. It adds NO new gateway
// plumbing: it fans out the existing list resources (calendar/mail/todo/workfeed)
// through the data provider, shows the top items per section as compact briefings,
// and serializes the whole briefing to the AI panel via useRegisterPane.
// Prioritizing ("오늘 뭐부터?") is delegated to Deneb through that text projection.

const MAX = 6; // a briefing is a glance; the full list lives in each resource's own pane.

interface Brief {
  label: string;
  view: View;
  empty: string;
  total: number; // relevant items before the cap — shown in the header and AI projection
  lines: string[]; // up to MAX compact one-liners (what the AI reads === what's on screen)
  query: { isLoading: boolean; isError?: boolean; error?: unknown };
}

// One briefing section as text: counted header + capped lines + overflow note.
function sectionText(b: Brief): string {
  if (b.total === 0) return "";
  const more = b.total > b.lines.length ? `\n- …외 ${b.total - b.lines.length}건` : "";
  return `[${b.label} ${b.total}건]\n` + b.lines.map((l) => `- ${l}`).join("\n") + more;
}

export function TodayPane() {
  const { connected, setView } = useWorkspace();
  const cal = useCachedList<CalEvent>("calendar", connected);
  const mail = useCachedList<Mail>("mail", connected);
  const todo = useList<Todo>({ resource: "todo", queryOptions: { enabled: connected } });
  const work = useList<WorkItem>({ resource: "workfeed", queryOptions: { enabled: connected } });

  const events = cal.result?.data ?? [];
  // Recent mail, unread first — robust if `isUnread` is absent (order is just preserved).
  const mails = [...(mail.result?.data ?? [])].sort(
    (a, b) => Number(Boolean(b.isUnread)) - Number(Boolean(a.isUnread)),
  );
  // Open todos, soonest due first; missing/unparseable due dates sink to the bottom.
  const due = (s?: string) => {
    if (!s) return Infinity;
    const t = new Date(s).getTime();
    return Number.isNaN(t) ? Infinity : t;
  };
  const todos = (todo.result?.data ?? []).filter((t) => !t.done).sort((a, b) => due(a.due) - due(b.due));
  const items = work.result?.data ?? [];

  const briefs: Brief[] = [
    {
      label: "일정",
      view: "calendar",
      empty: "다가오는 일정 없음",
      query: cal.query,
      total: events.length,
      lines: events.slice(0, MAX).map((ev) => {
        const span = calSpan(ev.start, ev.end);
        return `${ev.summary ?? ev.title ?? "(제목 없음)"}${span ? ` · ${span}` : ""}`;
      }),
    },
    {
      label: "메일",
      view: "mail",
      empty: "메일 없음",
      query: mail.query,
      total: mails.length,
      lines: mails.slice(0, MAX).map((m) => {
        const who = text(m.from);
        return `${m.isUnread ? "● " : ""}${m.subject ?? "(제목 없음)"}${who ? ` · ${who}` : ""}`;
      }),
    },
    {
      label: "할일",
      view: "todo",
      empty: "할일 없음",
      query: todo.query,
      total: todos.length,
      lines: todos.slice(0, MAX).map((t) => `${t.title}${t.due ? ` · 마감 ${fmtDate(t.due)}` : ""}`),
    },
    {
      label: "작업피드",
      view: "workfeed",
      empty: "작업피드 비어 있음",
      query: work.query,
      total: items.length,
      lines: items.slice(0, MAX).map((w) => `${w.title ?? "(항목)"}${w.source ? ` · ${w.source}` : ""}`),
    },
  ];

  // The full briefing the AI panel reads — exactly what's on screen, as text.
  const body = briefs.map(sectionText).filter(Boolean).join("\n\n");
  useRegisterPane(undefined, body ? `[오늘 브리핑]\n${body}` : "");

  const today = new Date().toLocaleDateString(undefined, { month: "long", day: "numeric", weekday: "short" });

  return (
    <>
      <h2 style={{ marginTop: 0 }}>
        오늘 <span style={{ fontSize: 14, fontWeight: 400, color: "var(--muted-2)", letterSpacing: 0 }}>{today}</span>
      </h2>
      <p style={{ color: "var(--muted-2)", fontSize: 12, margin: "6px 0 20px", lineHeight: 1.5 }}>
        우측 데네브에게 “오늘 뭐부터?”라고 물어보세요 — 아래 컨텍스트로 우선순위를 제안합니다.
      </p>
      {!connected ? (
        <p style={{ color: "var(--muted-2)", fontSize: 13 }}>게이트웨이에 연결하면 표시됩니다 (좌측 하단).</p>
      ) : (
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(232px, 1fr))",
            gap: "20px 28px",
            maxWidth: 760,
          }}
        >
          {briefs.map((b, i) => (
            <Section key={b.label} brief={b} index={i} onNav={() => setView(b.view)} />
          ))}
        </div>
      )}
    </>
  );
}

function Section({ brief, index, onNav }: { brief: Brief; index: number; onNav: () => void }) {
  const { label, total, lines, empty, query } = brief;
  return (
    <section className="fade-up" style={{ minWidth: 0, animationDelay: `${index * 60}ms` }}>
      <button
        onClick={onNav}
        style={{
          display: "flex",
          alignItems: "baseline",
          gap: 7,
          width: "100%",
          background: "none",
          border: "none",
          borderBottom: "1px solid var(--line)",
          padding: "0 0 7px",
          margin: "0 0 9px",
          cursor: "pointer",
          color: "var(--ink)",
        }}
      >
        <span style={{ fontSize: 14, fontWeight: 600, letterSpacing: "-0.01em" }}>{label}</span>
        {total > 0 && <span style={{ fontSize: 11, color: "var(--muted-2)" }}>{total}</span>}
        <span style={{ marginLeft: "auto", color: "var(--faint)", display: "inline-flex" }}>
          <Icon name="arrow-right" size={14} />
        </span>
      </button>
      <GridNotice query={query} count={lines.length} empty={empty}>
        <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
          {lines.map((l, i) => (
            <div
              key={i}
              style={{
                fontSize: 13,
                color: "var(--ink-2)",
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
                lineHeight: 1.5,
              }}
            >
              {l}
            </div>
          ))}
          {total > lines.length && (
            <div style={{ fontSize: 12, color: "var(--muted-2)", marginTop: 2 }}>…외 {total - lines.length}건</div>
          )}
        </div>
      </GridNotice>
    </section>
  );
}
