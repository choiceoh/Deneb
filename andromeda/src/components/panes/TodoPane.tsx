import { useCallback, useMemo, useRef, useState } from "react";
import { useCreate, useDelete, useUpdate } from "@/crud";
import type { Todo } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList } from "@/cachedList";
import { errText, fmtDate } from "@/format";
import { usePaneTarget } from "@/usePaneTarget";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { Field, Modal, ModalFooter } from "@/components/Modal";
import { showToast } from "@/toast";

export function TodoPane() {
  const { connected } = useWorkspace();
  // null = closed · "new" = add modal · Todo = edit that todo.
  const [modal, setModal] = useState<Todo | "new" | null>(null);
  const { result, query } = useCachedList<Todo>("todo", connected);
  const todos = useMemo(() => result?.data ?? [], [result?.data]);
  const { mutate: updateTodo } = useUpdate();
  const { mutate: deleteTodo } = useDelete();
  const { mutate: createTodo } = useCreate(); // 삭제 실행취소(재생성) 경로

  // Deep-link: open the matching todo's edit modal when another pane targets it.
  // While the list is still loading, keep the target pending (return false) so it
  // retries once the rows arrive instead of being dropped.
  // 데네브 prefill 커맨드용 초안 — 새 할일 모달이 채워진 채 열린다 (저장은 사용자).
  // seq는 모달 remount 키: 이미 열려 있는 모달 위로 새 초안이 오면 useState
  // 초기값이 다시 뛰도록 인스턴스를 갈아끼운다.
  const [draft, setDraft] = useState<{ seq: number; title: string; due?: string; note?: string } | null>(null);
  const draftSeqRef = useRef(0);
  const openTargetedTodo = useCallback(
    (t: PaneTarget) => {
      if (t.prefill) {
        setDraft({ seq: ++draftSeqRef.current, ...t.prefill });
        setModal("new");
        return;
      }
      if (t.id == null) return;
      const match = todos.find((x) => String(x.id) === String(t.id));
      if (!match) return query.isLoading ? false : undefined;
      setModal(match);
    },
    [todos, query.isLoading],
  );
  usePaneTarget("todo", openTargetedTodo);

  // Serialize the grid to text so Deneb's AI reads exactly what's on screen.
  const aiText = serializeList(
    "할일",
    todos,
    (t) =>
      `- [${t.done ? "x" : " "}] ${t.title}${t.due ? ` (마감 ${fmtDate(t.due)})` : ""}${t.note ? `\n    ${t.note}` : ""}`,
  );
  useRegisterPane("todo", aiText);

  function toggleTodo(t: Todo) {
    updateTodo({ resource: "todo", id: t.id, values: { done: !t.done } }, { onSuccess: () => void query.refetch() });
  }
  function removeTodo(t: Todo) {
    deleteTodo(
      { resource: "todo", id: t.id },
      {
        onSuccess: () => {
          void query.refetch();
          // 확인 다이얼로그 대신 실행취소 — 삭제는 즉시, 복구는 원클릭 (재생성이라
          // id는 바뀌지만 제목/마감/메모는 그대로 돌아온다).
          const values: Record<string, string | boolean> = { title: t.title };
          if (t.due) {
            values.due = t.due;
            if (t.dueAllDay != null) values.dueAllDay = t.dueAllDay;
          }
          if (t.note) values.note = t.note;
          showToast(`"${t.title}" 삭제됨`, {
            undo: () => createTodo({ resource: "todo", values }, { onSuccess: () => void query.refetch() }),
          });
        },
      },
    );
  }

  const columns: Column<Todo>[] = [
    {
      header: "완료",
      width: 40,
      cell: (t) => (
        <input
          type="checkbox"
          checked={Boolean(t.done)}
          onChange={() => toggleTodo(t)}
          aria-label={`${t.title} 완료 토글`}
        />
      ),
    },
    {
      header: "제목",
      cell: (t) => (
        <>
          <span style={{ textDecoration: t.done ? "line-through" : "none", opacity: t.done ? 0.5 : 1 }}>{t.title}</span>
          {t.note && <div style={{ fontSize: 12, color: "var(--muted-2)", lineHeight: 1.45 }}>{t.note}</div>}
        </>
      ),
    },
    { header: "마감", width: 116, cell: (t) => fmtDate(t.due), tdStyle: { fontSize: 13, opacity: 0.7 } },
    {
      header: "",
      width: 96,
      tdStyle: { textAlign: "right" },
      cell: (t) => (
        <>
          <RowBtn onClick={() => setModal(t)} title="수정">
            수정
          </RowBtn>
          <RowBtn onClick={() => removeTodo(t)} danger title="삭제">
            삭제
          </RowBtn>
        </>
      ),
    },
  ];

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 2, marginBottom: 12 }}>
        <h2 style={{ margin: 0 }}>할일</h2>
        <button className="btn btn-accent" onClick={() => setModal("new")} style={{ padding: "6px 12px" }}>
          + 새 할일
        </button>
      </div>
      <GridNotice query={query} count={todos.length} empty="할일이 없습니다.">
        <Grid columns={columns} rows={todos} getKey={(t) => String(t.id)} onRowClick={(t) => setModal(t)} />
      </GridNotice>
      {modal && (
        <TodoModal
          key={modal === "new" ? `new-${draft?.seq ?? 0}` : String(modal.id)}
          todo={modal === "new" ? null : modal}
          draft={modal === "new" ? draft : null}
          onClose={() => {
            setModal(null);
            setDraft(null);
          }}
          onSaved={() => void query.refetch()}
        />
      )}
    </>
  );
}

// Create or edit a todo: title + due date + note. New todos go to miniapp.todo.create,
// existing ones to miniapp.todo.update (non-`done` fields) via the data provider.
function TodoModal({
  todo,
  draft,
  onClose,
  onSaved,
}: {
  todo: Todo | null;
  // 데네브 prefill 초안 — 새 할일일 때만 초기값으로 쓴다.
  draft?: { title: string; due?: string; note?: string } | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [title, setTitle] = useState(todo?.title ?? draft?.title ?? "");
  const [due, setDue] = useState(todo?.due ? todo.due.slice(0, 10) : (draft?.due ?? ""));
  const [note, setNote] = useState(todo?.note ?? draft?.note ?? "");
  const [status, setStatus] = useState("");
  const [saving, setSaving] = useState(false);
  const { mutate: createTodo } = useCreate();
  const { mutate: updateTodo } = useUpdate();

  function save() {
    if (saving) return;
    const t = title.trim();
    if (!t) return setStatus("제목을 입력하세요");
    setStatus("저장 중…");
    setSaving(true);
    const handlers = {
      onSuccess: () => {
        onSaved();
        onClose();
      },
      onError: (e: unknown) => {
        setSaving(false);
        setStatus(`오류: ${errText(e)}`);
      },
    };
    if (todo) {
      const dueValue = due ? dateOnlyToRpcDue(due) : "";
      updateTodo(
        {
          resource: "todo",
          id: todo.id,
          values: { title: t, due: dueValue, dueAllDay: Boolean(due), note: note.trim() },
        },
        handlers,
      );
    } else {
      // A fresh todo only carries what was filled in (mirrors the old quick-add).
      const values: Record<string, string | boolean> = { title: t };
      if (due) {
        values.due = dateOnlyToRpcDue(due);
        values.dueAllDay = true;
      }
      if (note.trim()) values.note = note.trim();
      createTodo({ resource: "todo", values }, handlers);
    }
  }

  return (
    <Modal
      title={todo ? "할일 수정" : "할일 추가"}
      onClose={onClose}
      footer={<ModalFooter action="저장" busy={saving} status={status} onClose={onClose} onSubmit={save} />}
    >
      <Field label="제목">
        <input className="field" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
      </Field>
      <Field label="마감">
        <input type="date" className="field" value={due} onChange={(e) => setDue(e.target.value)} />
      </Field>
      <Field label="메모">
        <textarea
          className="field"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          rows={3}
          style={{ resize: "vertical", fontFamily: "inherit", lineHeight: 1.5 }}
        />
      </Field>
    </Modal>
  );
}

function dateOnlyToRpcDue(ymd: string): string {
  const d = new Date(`${ymd}T00:00`);
  return Number.isNaN(d.getTime()) ? ymd : d.toISOString();
}
