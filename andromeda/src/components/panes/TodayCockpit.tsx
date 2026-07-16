// 오늘 cockpit widgets — the KPI strip, the day timeline, and the deadline
// radar. Presentational: all numbers/layout come precomputed from todayData.ts,
// and every element deep-links into its owning pane. Style follows the
// card-stat rule: numbers carry the hierarchy via typography, no boxed tiles.
import type { PaneTarget } from "@/workspaceContext";
import { ddayLabel, type DayTimeline, type DeadlineEntry, type Kpi } from "./todayData";

export function KpiStrip({ kpis, onOpen }: { kpis: Kpi[]; onOpen: (target: PaneTarget) => void }) {
  return (
    <div className="kpi-strip" role="group" aria-label="오늘 지표">
      {kpis.map((k) => (
        <button
          key={k.key}
          className={"kpi" + (k.value > 0 && k.tone ? ` ${k.tone}` : "") + (k.value === 0 ? " zero" : "")}
          onClick={() => onOpen(k.target)}
          title={`${k.label} — 열기`}
        >
          <span className="kpi-value">
            {k.value}
            <span className="kpi-unit">건</span>
          </span>
          <span className="kpi-label">{k.label}</span>
          {k.hint && <span className="kpi-hint">{k.hint}</span>}
        </button>
      ))}
    </div>
  );
}

const LANE_H = 30; // px per timeline lane (block 26 + 4 gap)

export function DayTimelineCard({
  timeline,
  startHour,
  endHour,
  onOpen,
}: {
  timeline: DayTimeline;
  startHour: number;
  endHour: number;
  onOpen: (target: PaneTarget) => void;
}) {
  const hours: number[] = [];
  for (let h = startHour; h <= endHour; h++) hours.push(h);
  const empty = timeline.blocks.length === 0 && timeline.allDay.length === 0;
  return (
    <div className="tl">
      {empty ? (
        <p className="tl-empty">오늘 일정이 없습니다 — 딥워크 하기 좋은 날.</p>
      ) : (
        <>
          {timeline.allDay.length > 0 && (
            <div className="tl-allday">
              {timeline.allDay.map((a) => (
                <button key={String(a.id)} className="tl-chip" onClick={() => onOpen(a.target)} title="일정에서 열기">
                  {a.title}
                </button>
              ))}
            </div>
          )}
          <div className="tl-track" style={{ height: timeline.laneCount * LANE_H + 22 }}>
            {hours.map((h) => (
              <div key={h} className="tl-hour" style={{ left: `${((h - startHour) / (endHour - startHour)) * 100}%` }}>
                <span className="tl-hour-label">{h}</span>
              </div>
            ))}
            {timeline.nowPct != null && (
              <div className="tl-now" style={{ left: `${timeline.nowPct}%` }} aria-label="현재 시각" />
            )}
            {timeline.blocks.map((b) => (
              <button
                key={String(b.id)}
                className={"tl-block" + (b.clampedStart ? " clamped" : "")}
                style={{ left: `${b.leftPct}%`, width: `${b.widthPct}%`, top: b.lane * LANE_H + 18 }}
                onClick={() => onOpen(b.target)}
                title={`${b.timeLabel} ${b.title} — 일정에서 열기`}
              >
                <span className="tl-block-time">{b.timeLabel}</span>
                <span className="tl-block-title">{b.title}</span>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

export function DeadlineRadar({ entries, onOpen }: { entries: DeadlineEntry[]; onOpen: (target: PaneTarget) => void }) {
  if (entries.length === 0) return <p className="tl-empty">30일 내 마감이 없습니다.</p>;
  return (
    <div className="radar" role="list" aria-label="마감 레이더">
      {entries.map((e) => (
        <button key={e.key} role="listitem" className="radar-row" onClick={() => onOpen(e.target)} title="열기">
          <span className={"radar-dday" + (e.dday <= 0 ? " danger" : e.dday <= 3 ? " warn" : "")}>
            {ddayLabel(e.dday)}
          </span>
          <span className="radar-title">{e.title}</span>
          <span className="radar-meta">
            {e.kind} · {e.dateLabel}
          </span>
        </button>
      ))}
    </div>
  );
}
