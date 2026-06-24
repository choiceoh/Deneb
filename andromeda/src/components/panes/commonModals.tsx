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
  onSubmit: (value: string) => void;
}) {
  const [value, setValue] = useState(initialValue);
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={width}
      footer={
        <ModalFooter
          action={action}
          canSubmit={Boolean(value.trim())}
          onClose={onClose}
          onSubmit={() => onSubmit(value)}
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
  onDelete: () => void;
}) {
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={420}
      footer={<ModalFooter action="삭제" onClose={onClose} onSubmit={onDelete} />}
    >
      <p style={{ ...muted, margin: 0 }}>{path}</p>
    </Modal>
  );
}
