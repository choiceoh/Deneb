import type { CalEvent, Mail, Todo, View, WorkItem } from "@/types";
import { useCachedList } from "@/cachedList";
import { calSpan, eventDayKeys, fmtDate, text } from "@/format";
import { Icon, type IconName } from "@/components/Icon";
import { GridNotice } from "@/components/Grid";
import { type PaneTarget, useRegisterPane, useWorkspace } from "@/workspaceContext";

// "오늘" dashboard — the workstation's landing pane. It adds NO new gateway
// plumbing: it fans out the existing list resources (calendar/mail/todo/workfeed)
// through the data provider, shows the top items per section as compact briefings,
// and serializes the whole briefing to the AI panel via useRegisterPane.
// Prioritizing ("오늘 뭐부터?") is delegated to Deneb through that text projection.

const MAX = 6; // a briefing is a glance; the full list lives in each resource's own pane.

interface Brief {
  label: string;
  icon: IconName;
  view: View;
  empty: string;
  total: number; // relevant items before the cap — shown in the header and AI projection
  lines: BriefLine[]; // up to MAX compact rows (what the AI reads === what's on screen)
  query: { isLoading: boolean; isError?: boolean; error?: unknown };
}

// A briefing row carries its fields STRUCTURED (title + muted meta) so the human
// view can show a clean two-tier row while the AI projection still gets one line.
interface BriefLine {
  title: string;
  meta?: string; // secondary: time / sender / due — muted, on its own line
  unread?: boolean;
  accent?: boolean; // emphasize (e.g. an overdue todo)
  target?: PaneTarget;
}

function lineText(l: BriefLine): string {
  return `${l.unread ? "● " : ""}${l.title}${l.meta ? ` · ${l.meta}` : ""}`;
}

// One briefing section as text: counted header + capped lines + overflow note.
function sectionText(b: Brief): string {
  if (b.total === 0) return "";
  const more = b.total > b.lines.length ? `\n- …외 ${b.total - b.lines.length}건` : "";
  return `[${b.label} ${b.total}건]\n` + b.lines.map((l) => `- ${lineText(l)}`).join("\n") + more;
}

export function TodayPane() {
  const { connected, openPane, setView } = useWorkspace();
  const cal = useCachedList<CalEvent>("calendar", connected);
  const mail = useCachedList<Mail>("mail", connected);
  const todo = useCachedList<Todo>("todo", connected);
  const work = useCachedList<WorkItem>("workfeed", connected);

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
  const now = Date.now();

  const briefs: Brief[] = [
    {
      label: "일정",
      icon: "calendar",
      view: "calendar",
      empty: "다가오는 일정 없음",
      query: cal.query,
      total: events.length,
      lines: events.slice(0, MAX).map((ev) => ({
        title: ev.summary ?? ev.title ?? "(제목 없음)",
        meta: calSpan(ev.start, ev.end) || undefined,
        target: { view: "calendar", id: ev.id, dayKey: eventDayKeys(ev.start, ev.end)[0] },
      })),
    },
    {
      label: "메일",
      icon: "mail",
      view: "mail",
      empty: "메일 없음",
      query: mail.query,
      total: mails.length,
      lines: mails.slice(0, MAX).map((m) => ({
        title: m.subject ?? "(제목 없음)",
        meta: text(m.from) || undefined,
        unread: Boolean(m.isUnread),
        target: { view: "mail", id: m.id },
      })),
    },
    {
      label: "할일",
      icon: "todo",
      view: "todo",
      empty: "할일 없음",
      query: todo.query,
      total: todos.length,
      lines: todos.slice(0, MAX).map((t) => {
        const ts = due(t.due);
        return {
          title: t.title,
          meta: t.due ? `마감 ${fmtDate(t.due)}` : undefined,
          accent: Number.isFinite(ts) && ts < now, // overdue
          target: { view: "todo", id: t.id },
        };
      }),
    },
    {
      label: "작업피드",
      icon: "workfeed",
      view: "workfeed",
      empty: "작업피드 비어 있음",
      query: work.query,
      total: items.length,
      lines: items.slice(0, MAX).map((w) => ({
        title: w.title ?? "(항목)",
        meta: w.source || undefined,
        target: { view: "workfeed", id: w.id },
      })),
    },
  ];

  // The full briefing the AI panel reads — exactly what's on screen, as text.
  const body = briefs.map(sectionText).filter(Boolean).join("\n\n");
  useRegisterPane(undefined, body ? `[오늘 브리핑]\n${body}` : "");

  const today = new Date().toLocaleDateString(undefined, { month: "long", day: "numeric", weekday: "short" });

  return (
    <>
      <h2 style={{ marginTop: 2 }}>
        오늘 <span className="today-date">{today}</span>
      </h2>
      {!connected ? (
        <p style={{ color: "var(--muted-2)", fontSize: 13 }}>미연결</p>
      ) : (
        <div className="today-grid">
          {briefs.map((b, i) => (
            <Section
              key={b.label}
              brief={b}
              index={i}
              onNav={() => setView(b.view)}
              onOpenLine={(target) => openPane(target.view, target)}
            />
          ))}
        </div>
      )}
    </>
  );
}

function Section({
  brief,
  index,
  onNav,
  onOpenLine,
}: {
  brief: Brief;
  index: number;
  onNav: () => void;
  onOpenLine: (target: PaneTarget) => void;
}) {
  const { label, icon, total, lines, empty, query } = brief;
  return (
    <section className="today-card fade-up" style={{ animationDelay: `${index * 60}ms` }}>
      <button className="today-head" onClick={onNav} aria-label={`${label} 열기`} title={`${label} 열기`}>
        <Icon name={icon} size={15} className="ico" />
        <span className="today-head-label">{label}</span>
        {total > 0 && <span className="today-count">{total}</span>}
        <span className="today-arrow">
          <Icon name="arrow-right" size={14} />
        </span>
      </button>
      <GridNotice query={query} count={lines.length} empty={empty}>
        <div className="today-rows">
          {lines.map((line, i) => (
            <button
              key={i}
              type="button"
              className={"today-row" + (line.accent ? " accent" : "")}
              onClick={() => (line.target ? onOpenLine(line.target) : onNav())}
              title={`${label}에서 열기`}
            >
              {line.unread && <span className="today-dot" aria-hidden="true" />}
              <span className="today-row-main">
                <span className="today-row-title">{line.title}</span>
                {line.meta && <span className="today-row-meta">{line.meta}</span>}
              </span>
            </button>
          ))}
          {total > lines.length && <div className="today-more">…외 {total - lines.length}건</div>}
        </div>
      </GridNotice>
    </section>
  );
}
