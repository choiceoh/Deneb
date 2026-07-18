// Conversation history drawer for the AI panel. Lists miniapp.sessions.recent;
// selecting a row loads its transcript (continuing that conversation), the ✎
// renames it (miniapp.sessions.rename — persisted by the gateway label sidecar),
// and the × deletes it (miniapp.sessions.delete). "새 대화" returns to a fresh
// client:main. A quiet filter input narrows long lists; "이전 대화 더 보기"
// raises the fetch to the gateway cap.
import { useState } from "react";
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
  onRename,
  canLoadMore,
  onLoadMore,
}: {
  sessions: SessionRow[];
  currentKey: string;
  busy: boolean;
  error: string;
  onSelect: (key: string) => void;
  onDelete: (key: string) => void;
  onNew: () => void;
  onRename?: (key: string, label: string) => void;
  canLoadMore?: boolean;
  onLoadMore?: () => void;
}) {
  const [filter, setFilter] = useState("");
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [draft, setDraft] = useState("");

  const q = filter.trim().toLowerCase();
  const shown = q
    ? sessions.filter((s) => (s.label ?? "").toLowerCase().includes(q) || s.key.toLowerCase().includes(q))
    : sessions;

  function commitRename(key: string) {
    const next = draft.trim();
    setEditingKey(null);
    const row = sessions.find((s) => s.key === key);
    if (!next || next === (row?.label ?? "")) return;
    onRename?.(key, next);
  }

  return (
    <div className="session-drawer" role="group" aria-label="대화 기록">
      <div className="session-drawer-head">
        <span className="micro">대화 기록</span>
        <button type="button" className="row-btn" onClick={onNew} disabled={busy} title="새 대화">
          <Icon name="plus" size={13} /> 새 대화
        </button>
      </div>
      {sessions.length > 5 && (
        <input
          className="field session-filter"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="대화 검색…"
          aria-label="대화 검색"
        />
      )}
      {error ? <div className="session-drawer-status error">{error}</div> : null}
      {shown.length === 0 && !error ? (
        <div className="session-drawer-status">{q ? "검색 결과가 없습니다." : "최근 대화가 없습니다."}</div>
      ) : (
        <ul className="session-list">
          {shown.map((s) => (
            <li key={s.key} className={"session-row" + (s.key === currentKey ? " active" : "")}>
              {editingKey === s.key ? (
                <input
                  className="field session-rename-input"
                  value={draft}
                  autoFocus
                  onChange={(e) => setDraft(e.target.value)}
                  onBlur={() => commitRename(s.key)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") commitRename(s.key);
                    if (e.key === "Escape") setEditingKey(null);
                  }}
                  aria-label="대화 이름 변경"
                />
              ) : (
                <button type="button" className="session-row-main" onClick={() => onSelect(s.key)} disabled={busy}>
                  <span className="session-row-title">{s.label?.trim() || s.key}</span>
                  <span className="session-row-meta">
                    {[s.model, s.updatedAtMs ? fmtDate(s.updatedAtMs) : ""].filter(Boolean).join(" · ")}
                  </span>
                </button>
              )}
              {onRename && editingKey !== s.key && (
                <button
                  type="button"
                  className="row-btn session-row-rename"
                  onClick={() => {
                    setEditingKey(s.key);
                    setDraft(s.label?.trim() || "");
                  }}
                  disabled={busy}
                  title="대화 이름 변경"
                  aria-label={`대화 이름 변경: ${s.label?.trim() || s.key}`}
                >
                  <Icon name="pencil" size={13} />
                </button>
              )}
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
      {canLoadMore && onLoadMore && !q && (
        <button type="button" className="btn history-more" onClick={onLoadMore} disabled={busy}>
          이전 대화 더 보기
        </button>
      )}
    </div>
  );
}
