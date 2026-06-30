// Shared day navigator for the day-pager panes (mail, work feed): ‹ 이전 / [날짜
// N건] / 다음 › / 오늘로. Render it ABOVE the grid notice so it stays put while a
// day loads or comes back empty — burying it inside GridNotice would strand the
// user on an empty day with no arrows. The caller owns the day state and fetch;
// this is pure presentation. Reuses the `workfeed-daynav` stylesheet block.

export function DayPager({
  label,
  count,
  canPrev,
  canNext,
  atToday,
  onPrev,
  onNext,
  onToday,
}: {
  label: string;
  count: number;
  canPrev: boolean;
  canNext: boolean;
  atToday: boolean;
  onPrev: () => void;
  onNext: () => void;
  onToday: () => void;
}) {
  return (
    <div className="workfeed-daynav">
      <button className="row-btn" onClick={onPrev} disabled={!canPrev} aria-label="이전 날">
        ‹ 이전
      </button>
      <div className="workfeed-daynav-label" aria-live="polite">
        <span className="workfeed-daynav-day">{label}</span>
        <span className="workfeed-daynav-count">{count}건</span>
      </div>
      <button className="row-btn" onClick={onNext} disabled={!canNext} aria-label="다음 날">
        다음 ›
      </button>
      <div className="workfeed-daynav-spacer" />
      {!atToday && (
        <button className="row-btn" onClick={onToday}>
          오늘로
        </button>
      )}
    </div>
  );
}
