// Conversation history drawer for the AI panel. Lists miniapp.sessions.recent;
// selecting a row loads its transcript (continuing that conversation), and the ×
// deletes it (miniapp.sessions.delete). "새 대화" returns to a fresh client:main.
import type { SessionRow } from "@/gateway";
import { fmtDate } from "@/format";
import { Icon } from "./Icon";

export function SessionDrawer({
  sessions,
  currentKey,
  busy,
  error,
  onSelect,
  onDelete,
  onNew,
}: {
  sessions: SessionRow[];
  currentKey: string;
  busy: boolean;
  error: string;
  onSelect: (key: string) => void;
  onDelete: (key: string) => void;
  onNew: () => void;
}) {
  return (
    <div className="session-drawer" role="group" aria-label="대화 기록">
      <div className="session-drawer-head">
        <span className="micro">대화 기록</span>
        <button type="button" className="row-btn" onClick={onNew} disabled={busy} title="새 대화">
          <Icon name="plus" size={13} /> 새 대화
        </button>
      </div>
      {error ? <div className="session-drawer-status error">{error}</div> : null}
      {sessions.length === 0 && !error ? (
        <div className="session-drawer-status">최근 대화가 없습니다.</div>
      ) : (
        <ul className="session-list">
          {sessions.map((s) => (
            <li key={s.key} className={"session-row" + (s.key === currentKey ? " active" : "")}>
              <button type="button" className="session-row-main" onClick={() => onSelect(s.key)} disabled={busy}>
                <span className="session-row-title">{s.label?.trim() || s.key}</span>
                <span className="session-row-meta">
                  {[s.model, s.updatedAtMs ? fmtDate(s.updatedAtMs) : ""].filter(Boolean).join(" · ")}
                </span>
              </button>
              <button
                type="button"
                className="row-btn session-row-del"
                onClick={() => onDelete(s.key)}
                disabled={busy}
                title="대화 삭제"
                aria-label={`대화 삭제: ${s.label?.trim() || s.key}`}
              >
                <Icon name="trash" size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
