import { useState } from "react";

import type { WikiCategory } from "@/types";
import { color, line, muted } from "@/theme";
import { Field, Modal, ModalFooter } from "@/components/Modal";

// WikiPane's page-level modals — split from WikiPane.tsx (commonModals pattern)
// so the pane file keeps only browse/edit/save flow and stays within the repo's
// size guideline. The path helpers below are private to MovePageModal.

export interface NewPageDraft {
  title: string;
  category: string;
  summary: string;
  body: string;
}

function categoryName(c: WikiCategory): string {
  return c.name ?? c.category ?? "(root)";
}

// Korean display label per category path: code-named project folders carry
// displayName from the gateway ("프로젝트/pl2-kia-epc-001" → "기아 오토랜드 화성
// 태양광"); everything else renders its path. The raw path stays the move VALUE
// either way — only the row label changes.
function categoryLabels(categories: WikiCategory[]): Map<string, string> {
  const labels = new Map<string, string>();
  for (const c of categories) {
    const name = categoryName(c);
    if (c.displayName) labels.set(name, `${c.displayName} (${name})`);
  }
  return labels;
}

// Split a page path into its directory and filename so each is editable on its own.
function dirOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}
function baseOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? path : path.slice(i + 1);
}
function joinPath(dir: string, name: string): string {
  const d = dir.trim().replace(/^\/+|\/+$/g, "");
  const n = name.trim().replace(/^\/+|\/+$/g, "");
  return d ? `${d}/${n}` : n;
}

// Move a page to another folder. The destination 분류 is picked by clicking an
// existing category (or 최상위 / + 새 분류); the page keeps its filename. The
// resulting path is shown before applying.
export function MovePageModal({
  path,
  categories,
  onClose,
  onSubmit,
}: {
  path: string;
  categories: WikiCategory[];
  onClose: () => void;
  onSubmit: (to: string) => void;
}) {
  const [dir, setDir] = useState(() => dirOf(path));
  const [addingCat, setAddingCat] = useState(false);
  const [newCat, setNewCat] = useState("");

  const name = baseOf(path);
  // Existing categories + the page's current directory, deduped — so the source
  // category is always offered even if the registry hasn't surfaced it.
  const labels = categoryLabels(categories);
  const options = Array.from(
    new Set([dirOf(path), ...categories.map(categoryName)].filter((c) => c && c !== "(root)")),
  ).sort((a, b) => (labels.get(a) ?? a).localeCompare(labels.get(b) ?? b, "ko"));

  const effectiveDir = addingCat ? newCat : dir;
  const to = joinPath(effectiveDir, name);
  const ready = to !== path && (!addingCat || Boolean(newCat.trim()));

  const catRow = (label: string, selected: boolean, onClick: () => void) => (
    <button
      key={label}
      className="wiki-category-row"
      onClick={onClick}
      style={{ background: selected ? color.active : "transparent" }}
    >
      <span>{label}</span>
    </button>
  );

  return (
    <Modal
      title="페이지 이동"
      onClose={onClose}
      width={460}
      footer={<ModalFooter action="이동" canSubmit={ready} onClose={onClose} onSubmit={() => onSubmit(to)} />}
    >
      <div style={{ fontSize: 12, color: color.muted, marginBottom: 5 }}>분류</div>
      <div
        style={{ display: "grid", gap: 2, maxHeight: 240, overflow: "auto", border: line, borderRadius: 8, padding: 4 }}
      >
        {catRow("최상위", !addingCat && dir === "", () => {
          setAddingCat(false);
          setDir("");
        })}
        {options.map((c) =>
          catRow(labels.get(c) ?? c, !addingCat && dir === c, () => {
            setAddingCat(false);
            setDir(c);
          }),
        )}
        {catRow("+ 새 분류", addingCat, () => {
          setAddingCat(true);
          setNewCat("");
        })}
      </div>
      {addingCat && (
        <input
          className="field"
          value={newCat}
          onChange={(e) => setNewCat(e.target.value)}
          placeholder="새 분류 이름"
          autoFocus
          style={{ marginTop: 6 }}
        />
      )}
      <p style={{ ...muted, margin: "8px 0 0", fontSize: 12, wordBreak: "break-all" }}>→ {to || "—"}</p>
    </Modal>
  );
}

export function NewPageModal({ onClose, onCreate }: { onClose: () => void; onCreate: (draft: NewPageDraft) => void }) {
  const [draft, setDraft] = useState<NewPageDraft>({ title: "", category: "", summary: "", body: "" });
  const ready = draft.title.trim() && draft.category.trim();
  const set = (key: keyof NewPageDraft, value: string) => setDraft((d) => ({ ...d, [key]: value }));
  return (
    <Modal
      title="새 위키 페이지"
      onClose={onClose}
      width={520}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            취소
          </button>
          <button className="btn btn-accent" onClick={() => onCreate(draft)} disabled={!ready}>
            생성
          </button>
        </>
      }
    >
      <Field label="제목">
        <input
          className="field"
          value={draft.title}
          onChange={(e) => set("title", e.target.value)}
          placeholder="예: Andromeda 개선 노트"
          autoFocus
        />
      </Field>
      <Field label="분류">
        <input
          className="field"
          value={draft.category}
          onChange={(e) => set("category", e.target.value)}
          placeholder="예: projects"
        />
      </Field>
      <Field label="요약">
        <input className="field" value={draft.summary} onChange={(e) => set("summary", e.target.value)} />
      </Field>
      <Field label="본문">
        <textarea
          className="field"
          value={draft.body}
          onChange={(e) => set("body", e.target.value)}
          rows={6}
          style={{ resize: "vertical" }}
        />
      </Field>
    </Modal>
  );
}

export function UnsavedWikiModal({
  path,
  targetPath,
  onClose,
  onDiscard,
  onSave,
}: {
  path: string;
  targetPath: string;
  onClose: () => void;
  onDiscard: () => void;
  onSave: () => void;
}) {
  return (
    <Modal
      title="저장하지 않은 변경"
      onClose={onClose}
      width={460}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            계속 편집
          </button>
          <button className="btn" onClick={onDiscard}>
            버리고 열기
          </button>
          <button className="btn btn-accent" onClick={onSave}>
            저장 후 열기
          </button>
        </>
      }
    >
      <p style={{ ...muted, margin: 0 }}>
        {path}에 저장하지 않은 변경이 있습니다. {targetPath}을 열기 전에 처리하세요.
      </p>
    </Modal>
  );
}
