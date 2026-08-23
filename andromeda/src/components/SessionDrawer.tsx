// Conversation history drawer for the AI panel and chat tab. Lists
// miniapp.sessions.recent; selecting a row loads its transcript, ✎ renames it,
// 핀 pins it to the top, 기본 clears a per-conversation model override, and ×
// deletes it. "새 대화" mints a fresh client:main:<id> on the chat tab (the work
// panel returns to client:main). Search filters the loaded page immediately and
// asks miniapp.sessions.search for transcript hits beyond it.
import { useEffect, useState } from "react";
import type { SessionRow, SessionSearchHit } from "@/gateway";
import { fmtDate } from "@/format";
import { Icon } from "./Icon";

const HOME_KEY = "client:main";

export function SessionDrawer({
  sessions,
  currentKey,
  busy,
  error,
  onSelect,
  onDelete,
  onNew,
  onRename,
  onPin,
  onResetModel,
  onSearch,
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
  onPin?: (key: string, pinned: boolean) => void;
  onResetModel?: (key: string) => void;
  onSearch?: (query: string) => Promise<SessionSearchHit[]>;
  canLoadMore?: boolean;
  onLoadMore?: () => void;
}) {
  const [filter, setFilter] = useState("");
  const [hits, setHits] = useState<SessionSearchHit[]>([]);
  const [hitsQuery, setHitsQuery] = useState("");
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [draft, setDraft] = useState("");

  const q = filter.trim();
  useEffect(() => {
    if (!q || !onSearch) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      void onSearch(q).then((rows) => {
        if (cancelled) return;
        setHits(rows);
        setHitsQuery(q);
      });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
    // Search identity is the query; a new onSearch closure must not retrigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  const needle = q.toLowerCase();
  const local = needle
    ? sessions.filter((s) => (s.label ?? "").toLowerCase().includes(needle) || s.key.toLowerCase().includes(needle))
    : sessions;
  const extra =
    needle && hitsQuery === q
      ? hits
          .filter((h) => h.sessionKey && !local.some((s) => s.key === h.sessionKey))
          .map((h): SessionRow => ({
            key: h.sessionKey,
            label: h.label,
            model: h.snippet,
          }))
      : [];
  const shown = [...local, ...extra];

  function commitRename(key: string) {
    const next = draft.trim();
    setEditingKey(null);
    const row = sessions.find((s) => s.key === key);
    if (!next || next === (row?.label ?? "")) return;
    onRename?.(key, next);
  }

  function titleOf(s: SessionRow) {
    return s.label?.trim() || s.key;
  }

  return (
    <div className="session-drawer" role="group" aria-label="대화 기록">
      <div className="session-drawer-head">
        <span className="micro">대화 기록</span>
        <button type="button" className="row-btn" onClick={onNew} disabled={busy} title="새 대화">
          <Icon name="plus" size={13} /> 새 대화
        </button>
      </div>
      <input
        className="field session-filter"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="대화 검색…"
        aria-label="대화 검색"
      />
      {error ? <div className="session-drawer-status error">{error}</div> : null}
      {shown.length === 0 && !error ? (
        <div className="session-drawer-status">{q ? "검색 결과가 없습니다." : "최근 대화가 없습니다."}</div>
      ) : (
        <ul className="session-list">
          {shown.map((s) => {
            const home = s.key === HOME_KEY;
            const title = titleOf(s);
            return (
              <li
                key={s.key}
                className={"session-row" + (s.key === currentKey ? " active" : "") + (s.pinned ? " pinned" : "")}
              >
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
                    <span className="session-row-title">{s.pinned ? `핀 · ${title}` : title}</span>
                    {home ? <span className="session-row-hint">선제 보고가 모이는 업무 홈</span> : null}
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
                    aria-label={`대화 이름 변경: ${title}`}
                  >
                    <Icon name="pencil" size={13} />
                  </button>
                )}
                {onPin && !home && (
                  <button
                    type="button"
                    className="row-btn session-row-pin"
                    onClick={() => onPin(s.key, !s.pinned)}
                    disabled={busy}
                    title={s.pinned ? "고정 해제" : "위에 고정"}
                    aria-label={s.pinned ? `고정 해제: ${title}` : `위에 고정: ${title}`}
                  >
                    {s.pinned ? "고정" : "핀"}
                  </button>
                )}
                {onResetModel && s.model?.trim() ? (
                  <button
                    type="button"
                    className="row-btn session-row-reset"
                    onClick={() => onResetModel(s.key)}
                    disabled={busy}
                    title="기본 모델로"
                    aria-label={`기본 모델로: ${title}`}
                  >
                    기본
                  </button>
                ) : null}
                {!home && (
                  <button
                    type="button"
                    className="row-btn session-row-del"
                    onClick={() => onDelete(s.key)}
                    disabled={busy}
                    title="대화 삭제"
                    aria-label={`대화 삭제: ${title}`}
                  >
                    <Icon name="trash" size={13} />
                  </button>
                )}
              </li>
            );
          })}
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
