// Modals shared by the file-like panes (WikiPane · FilesPane). Both deal in
// paths, so they share a one-input prompt (rename/move/merge/new-folder) and a
// confirm-delete dialog. Built from the Modal primitives in components/Modal.
import { useState } from "react";
import { muted } from "@/theme";
import { Field, Modal, ModalFooter } from "@/components/Modal";

// A single labelled input + a 취소/<action> footer. The accent button is disabled
// until the field is non-empty. `action` names the verb (이동/병합/생성…).
export function OneFieldModal({
  title,
  label,
  initialValue = "",
  action,
  width = 460,
  onClose,
  onSubmit,
}: {
  title: string;
  label: string;
  initialValue?: string;
  action: string;
  width?: number;
  onClose: () => void;
  // May return a promise — the footer stays disabled until it settles, so a
  // double-click can't fire the mutation twice (the modal often outlives the
  // first click when the parent keeps it open on failure).
  onSubmit: (value: string) => void | Promise<unknown>;
}) {
  const [value, setValue] = useState(initialValue);
  const [busy, setBusy] = useState(false);
  function submit() {
    if (busy) return;
    const r = onSubmit(value);
    if (r && typeof r.then === "function") {
      setBusy(true);
      void r.finally(() => setBusy(false));
    }
  }
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={width}
      footer={
        <ModalFooter
          action={action}
          canSubmit={Boolean(value.trim())}
          busy={busy}
          onClose={onClose}
          onSubmit={submit}
        />
      }
    >
      <Field label={label}>
        <input className="field" value={value} onChange={(e) => setValue(e.target.value)} autoFocus />
      </Field>
    </Modal>
  );
}

// Confirm deletion of the thing at `path`. `title` names what's being deleted
// (e.g. "페이지 삭제" / "파일 삭제").
export function DeleteModal({
  title,
  path,
  onClose,
  onDelete,
}: {
  title: string;
  path: string;
  onClose: () => void;
  onDelete: () => void | Promise<unknown>;
}) {
  const [busy, setBusy] = useState(false);
  function submit() {
    if (busy) return;
    const r = onDelete();
    if (r && typeof r.then === "function") {
      setBusy(true);
      void r.finally(() => setBusy(false));
    }
  }
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={420}
      footer={<ModalFooter action="삭제" busy={busy} onClose={onClose} onSubmit={submit} />}
    >
      <p style={{ ...muted, margin: 0 }}>{path}</p>
    </Modal>
  );
}
