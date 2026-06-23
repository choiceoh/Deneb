import { useState } from "react";
import { useCreate, useDelete, useList, useUpdate } from "@refinedev/core";
import type { Todo } from "@/types";
import { serializeList } from "@/aiText";
import { errText, fmtDate } from "@/format";
import { field } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";
import { Field, Modal } from "@/components/Modal";

export function TodoPane() {
  const { connected } = useWorkspace();
  const [newTodo, setNewTodo] = useState("");
  const [editing, setEditing] = useState<Todo | null>(null);
  const { result, query } = useList<Todo>({ resource: "todo", queryOptions: { enabled: connected } });
  const todos = result?.data ?? [];
  const { mutate: createTodo } = useCreate();
  const { mutate: updateTodo } = useUpdate();
  const { mutate: deleteTodo } = useDelete();

  // Serialize the grid to text so Deneb's AI reads exactly what's on screen.
  const aiText = serializeList(
    "할일",
    todos,
    (t) =>
      `- [${t.done ? "x" : " "}] ${t.title}${t.due ? ` (마감 ${fmtDate(t.due)})` : ""}${t.note ? `\n    ${t.note}` : ""}`,
  );
  useRegisterPane("todo", aiText);

  function addTodo() {
    const title = newTodo.trim();
    if (!title) return;
    setNewTodo("");
    createTodo({ resource: "todo", values: { title } }, { onSuccess: () => void query.refetch() });
  }
  function toggleTodo(t: Todo) {
    updateTodo({ resource: "todo", id: t.id, values: { done: !t.done } }, { onSuccess: () => void query.refetch() });
  }
  function removeTodo(t: Todo) {
    deleteTodo({ resource: "todo", id: t.id }, { onSuccess: () => void query.refetch() });
  }

  const columns: Column<Todo>[] = [
    {
      header: "완료",
      width: 40,
      cell: (t) => <input type="checkbox" checked={Boolean(t.done)} onChange={() => toggleTodo(t)} />,
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
          <RowBtn onClick={() => setEditing(t)} title="수정">
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
      <h2 style={{ marginTop: 2 }}>할일</h2>
      <div style={{ display: "flex", gap: 6, marginBottom: 12, maxWidth: 540 }}>
        <input
          style={{ ...field, flex: 1 }}
          placeholder="새 할일…"
          value={newTodo}
          onChange={(e) => setNewTodo(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") addTodo();
          }}
        />
        <button onClick={addTodo} style={{ padding: "8px 14px" }}>
          추가
        </button>
      </div>
      <GridNotice query={query} count={todos.length} empty="할일이 없습니다.">
        <Grid
          columns={columns}
          rows={todos}
          getKey={(t) => String(t.id)}
          maxWidth={680}
          onRowClick={(t) => setEditing(t)}
        />
      </GridNotice>
      {editing && <TodoModal todo={editing} onClose={() => setEditing(null)} onSaved={() => void query.refetch()} />}
    </>
  );
}

// Edit an existing todo: title + due date + note (the quick-add input only sets a
// title). Maps to miniapp.todo.update via the data provider (non-`done` fields).
function TodoModal({ todo, onClose, onSaved }: { todo: Todo; onClose: () => void; onSaved: () => void }) {
  const [title, setTitle] = useState(todo.title);
  const [due, setDue] = useState(todo.due ? todo.due.slice(0, 10) : "");
  const [note, setNote] = useState(todo.note ?? "");
  const [status, setStatus] = useState("");
  const { mutate: updateTodo } = useUpdate();

  function save() {
    const t = title.trim();
    if (!t) return setStatus("제목을 입력하세요");
    setStatus("저장 중…");
    updateTodo(
      // due as a date-only string (cleared with ""); best-effort vs the live gateway.
      { resource: "todo", id: todo.id, values: { title: t, due, note: note.trim() } },
      {
        onSuccess: () => {
          onSaved();
          onClose();
        },
        onError: (e: unknown) => setStatus(`오류: ${errText(e)}`),
      },
    );
  }

  return (
    <Modal
      title="할일 수정"
      onClose={onClose}
      footer={
        <>
          {status && <span style={{ fontSize: 12, color: "var(--muted)", marginRight: "auto" }}>{status}</span>}
          <button className="btn" onClick={onClose}>
            취소
          </button>
          <button className="btn btn-accent" onClick={save}>
            저장
          </button>
        </>
      }
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
