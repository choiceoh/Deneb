// App-styled confirm — replaces window.confirm (OS-chrome dialog that breaks
// the warm-Zen surface). Same Modal primitives as every form dialog.
import { Modal, ModalFooter } from "./Modal";

export function ConfirmModal({
  title = "확인",
  message,
  action,
  onConfirm,
  onClose,
}: {
  title?: string;
  message: string;
  action: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={400}
      footer={
        <ModalFooter
          action={action}
          onClose={onClose}
          onSubmit={() => {
            onClose();
            onConfirm();
          }}
        />
      }
    >
      <p style={{ margin: 0, fontSize: 14, lineHeight: 1.6, whiteSpace: "pre-wrap" }}>{message}</p>
    </Modal>
  );
}
