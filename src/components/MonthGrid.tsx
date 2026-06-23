// Month calendar grid for the 일정 pane — a Sunday-first month view. The grid
// only marks that a day has events; the list below owns event titles/details.
import type { CalEvent } from "@/types";
import { dayKey, eventDayKeys, monthLabel, monthMatrix } from "@/format";
import { Icon } from "@/components/Icon";

const DOW = ["일", "월", "화", "수", "목", "금", "토"];
const MAX_MARKERS = 4;

// Category → marker tint (semantic, via theme tokens). Most upcoming events are the
// user's own, so an absent category styles as "mine".
function markerClass(ev: CalEvent): string {
  const kind = eventDayKeys(ev.start, ev.end).length > 1 || ev.allDay ? "line" : "dot";
  const category = ev.category === "deadline" ? "deadline" : ev.category === "others" ? "others" : "mine";
  return `cal-marker ${kind} ${category}`;
}

export function MonthGrid({
  year,
  month0,
  eventsByDay,
  todayKey,
  selectedKey,
  onSelectDay,
  onPrev,
  onNext,
  onToday,
}: {
  year: number;
  month0: number;
  eventsByDay: Map<string, CalEvent[]>;
  todayKey: string;
  selectedKey?: string | null;
  onSelectDay?: (key: string) => void;
  onPrev: () => void;
  onNext: () => void;
  onToday: () => void;
}) {
  const weeks = monthMatrix(year, month0);

  return (
    <div style={{ maxWidth: 780 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 10 }}>
        <button className="cal-nav-btn" onClick={onPrev} aria-label="이전 달" title="이전 달">
          <span style={{ display: "inline-flex", transform: "rotate(180deg)" }}>
            <Icon name="arrow-right" size={15} />
          </span>
        </button>
        <button className="cal-nav-btn" onClick={onNext} aria-label="다음 달" title="다음 달">
          <Icon name="arrow-right" size={15} />
        </button>
        <span style={{ fontSize: 15, fontWeight: 600, letterSpacing: 0, marginLeft: 2 }}>
          {monthLabel(year, month0)}
        </span>
        <button className="btn" style={{ marginLeft: "auto", padding: "5px 12px", fontSize: 12 }} onClick={onToday}>
          오늘
        </button>
      </div>

      <div className="cal-grid">
        {DOW.map((d, i) => (
          <div key={d} className="cal-dow" style={i === 0 ? { color: "var(--due)" } : undefined}>
            {d}
          </div>
        ))}
        {weeks.flat().map((date) => {
          const key = dayKey(date);
          const inMonth = date.getMonth() === month0;
          const dayEvents = eventsByDay.get(key) ?? [];
          const shown = dayEvents.slice(0, MAX_MARKERS);
          const selected = inMonth && key === selectedKey;
          const cls = [
            "cal-cell",
            inMonth ? "" : "out",
            key === todayKey ? "cal-today" : "",
            selected ? "cal-selected" : "",
          ]
            .filter(Boolean)
            .join(" ");
          // Only in-month days are selectable targets (out-of-month days are context).
          return (
            <div
              key={key}
              className={cls}
              role={inMonth ? "button" : undefined}
              tabIndex={inMonth ? 0 : undefined}
              aria-pressed={inMonth ? selected : undefined}
              aria-label={
                inMonth
                  ? `${month0 + 1}월 ${date.getDate()}일${dayEvents.length ? `, 일정 ${dayEvents.length}건` : ""}`
                  : undefined
              }
              onClick={inMonth ? () => onSelectDay?.(key) : undefined}
              onKeyDown={
                inMonth
                  ? (e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onSelectDay?.(key);
                      }
                    }
                  : undefined
              }
            >
              <span className="cal-daynum">{date.getDate()}</span>
              {dayEvents.length > 0 && (
                <span className="cal-markers" aria-hidden="true" title={`일정 ${dayEvents.length}건`}>
                  {shown.map((ev, i) => (
                    <span key={`${ev.id ?? i}-${i}`} className={markerClass(ev)} />
                  ))}
                  {dayEvents.length > shown.length && <span className="cal-marker overflow" />}
                </span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
