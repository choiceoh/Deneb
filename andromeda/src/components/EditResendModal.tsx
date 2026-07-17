// Edit the last user message and resend it — the trailing exchange is replaced
// (native #3870 parity). Kept as a modal (not inline) so the transcript stays
// read-only and the composer keeps its own draft.
import { useState } from "react";
import { Modal, ModalFooter } from "./Modal";

export function EditResendModal({
  initial,
  onClose,
  onResend,
}: {
  initial: string;
  onClose: () => void;
  onResend: (message: string) => void;
}) {
  const [text, setText] = useState(initial);
  function resend() {
    const msg = text.trim();
    if (!msg) return;
    onResend(msg);
    onClose();
  }
  return (
    <Modal
      title="메시지 수정 후 재전송"
      onClose={onClose}
      width={480}
      footer={<ModalFooter action="재전송" canSubmit={Boolean(text.trim())} onClose={onClose} onSubmit={resend} />}
    >
      <textarea
        className="field"
        value={text}
        onChange={(e) => setText(e.target.value)}
        rows={5}
        autoFocus
        style={{ resize: "vertical", fontFamily: "inherit", lineHeight: 1.5, width: "100%" }}
        aria-label="수정할 메시지"
      />
    </Modal>
  );
}
