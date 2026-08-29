// Which repository this conversation works in.
//
// Binding a conversation to a registered repo gives it its own git worktree on
// its own branch, so the agent's commands run there instead of the server-wide
// workspace. Registration is deliberately a separate, explicit step: only what
// the operator registered can be bound, and the gateway refuses paths it must
// never touch (its own production checkout).
import { useState } from "react";

import type { CodeRepo } from "@/gateway";

export function RepoPicker({
  repos,
  value,
  busy,
  onSelect,
  onRegister,
}: {
  repos: CodeRepo[];
  value: string;
  busy?: boolean;
  onSelect: (repoId: string) => void;
  onRegister: (path: string) => Promise<void>;
}) {
  const [adding, setAdding] = useState(false);
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function submit() {
    const trimmed = path.trim();
    if (!trimmed || saving) return;
    setSaving(true);
    setError("");
    try {
      await onRegister(trimmed);
      setPath("");
      setAdding(false);
    } catch (e) {
      // The gateway's refusals name their reason (not a repository, protected
      // production checkout) and are written for the operator — show them
      // rather than a generic failure.
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }

  if (adding) {
    return (
      <span className="repo-picker adding">
        <input
          className="field repo-picker-path"
          value={path}
          autoFocus
          disabled={saving}
          placeholder="저장소 절대 경로"
          aria-label="등록할 저장소 경로"
          onChange={(e) => setPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.nativeEvent.isComposing) {
              e.preventDefault();
              void submit();
            }
            if (e.key === "Escape") {
              setAdding(false);
              setError("");
            }
          }}
        />
        <button type="button" className="row-btn" onClick={() => void submit()} disabled={saving || !path.trim()}>
          등록
        </button>
        <button
          type="button"
          className="row-btn"
          onClick={() => {
            setAdding(false);
            setError("");
          }}
        >
          취소
        </button>
        {error ? (
          <span className="repo-picker-error" role="alert">
            {error}
          </span>
        ) : null}
      </span>
    );
  }

  return (
    <span className="repo-picker">
      <select
        className="field"
        value={value}
        disabled={busy}
        aria-label="작업할 저장소"
        title="이 대화가 작업할 저장소 — 대화마다 자기 워크트리를 받습니다"
        onChange={(e) => {
          if (e.target.value === "__add__") {
            setAdding(true);
            return;
          }
          onSelect(e.target.value);
        }}
      >
        {/* Empty value is the default workspace, and picking it is how a
            conversation is unbound again. */}
        <option value="">기본 워크스페이스</option>
        {repos.map((r) => (
          <option key={r.id} value={r.id}>
            {r.name}
          </option>
        ))}
        <option value="__add__">저장소 등록…</option>
      </select>
    </span>
  );
}
