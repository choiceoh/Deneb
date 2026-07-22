import type { ChangeEvent, RefObject } from "react";

import type { StagedAttachment } from "@/useChatSurface";
import { Icon } from "./Icon";

// The shared composer of the two chat surfaces (chat tab · AI side panel):
// attach button + autosizing textarea + honest stop/send. One component so the
// affordances (paste-to-attach, Enter/IME handling, non-abortable capture state)
// can't drift between the surfaces.
export function ChatComposer({
  composeRef,
  fileRef,
  busy,
  stoppable,
  connected,
  input,
  placeholder,
  note,
  onInput,
  onSubmit,
  onStop,
  onPick,
  onAttachFiles,
  staged,
  onRemoveStaged,
}: {
  composeRef: RefObject<HTMLTextAreaElement | null>;
  fileRef: RefObject<HTMLInputElement | null>;
  busy: boolean;
  stoppable: boolean;
  connected: boolean;
  input: string;
  placeholder: string; // idle placeholder — busy shows "응답 중…" on both surfaces
  note?: string; // attach-skip notice (useAttachPipeline.attachNote)
  onInput: (value: string) => void;
  onSubmit: () => void;
  onStop: () => void;
  onPick: (e: ChangeEvent<HTMLInputElement>) => void;
  onAttachFiles: (files: File[]) => void;
  staged?: StagedAttachment[];
  onRemoveStaged?: (id: string) => void;
}) {
  const hasStaged = (staged?.length ?? 0) > 0;
  return (
    <>
      {note && (
        <div className="attach-notice" role="status">
          {note}
        </div>
      )}
      {hasStaged && (
        <div className="attach-chips" role="group" aria-label="첨부 대기 파일">
          {staged!.map((s) => (
            <span key={s.id} className="attach-chip" title={s.name}>
              {s.previewUrl ? <img src={s.previewUrl} alt="" /> : <Icon name="attach" size={13} />}
              <span className="attach-chip-name">{s.name}</span>
              <button
                type="button"
                className="row-btn"
                onClick={() => onRemoveStaged?.(s.id)}
                aria-label={`첨부 제거: ${s.name}`}
                title="첨부 제거"
              >
                <Icon name="close" size={11} />
              </button>
            </span>
          ))}
        </div>
      )}
      <form
        className="ai-composer"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
      >
        <input
          ref={fileRef}
          type="file"
          accept="image/*,audio/*,video/*,.png,.jpg,.jpeg,.webp,.gif,.mp3,.m4a,.wav,.ogg,.webm,.mp4,.mov,.mkv,.avi,.m4v,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.rtf,.odt,.ods,.odp,.hwp,.hwpx,.csv,.txt"
          multiple
          hidden
          onChange={onPick}
        />
        <button
          type="button"
          className="row-btn"
          onClick={() => fileRef.current?.click()}
          disabled={busy || !connected}
          title="파일 첨부 (이미지·문서·녹음)"
          aria-label="파일 첨부"
          style={{ padding: 5, alignSelf: "flex-end" }}
        >
          <Icon name="attach" size={18} />
        </button>
        <textarea
          ref={composeRef}
          className="ai-compose"
          aria-label="Deneb에게 메시지"
          placeholder={busy ? "응답 중…" : placeholder}
          rows={1}
          value={input}
          disabled={busy}
          onChange={(e) => onInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing) return;
            e.preventDefault();
            onSubmit();
          }}
          onPaste={(e) => {
            // 클립보드에 파일(스크린샷·복사한 이미지)이 있으면 첨부로 — 텍스트 붙여넣기는 그대로.
            const files = Array.from(e.clipboardData?.files ?? []);
            if (files.length === 0) return;
            e.preventDefault();
            onAttachFiles(files);
          }}
        />
        {busy ? (
          stoppable ? (
            <button type="button" className="ai-send ai-send-stop" onClick={onStop} aria-label="중단" title="응답 중단">
              <Icon name="stop" size={15} />
            </button>
          ) : (
            // 첨부 분석(capture)은 중간에 끊을 수 없다 — 되는 척하는 중단 버튼 대신 정직한 표시.
            <button
              type="button"
              className="ai-send"
              disabled
              aria-label="첨부 분석 중"
              title="첨부 분석 중에는 중단할 수 없습니다"
            >
              <Icon name="attach" size={15} />
            </button>
          )
        ) : (
          <button
            type="submit"
            className="ai-send"
            disabled={!connected || (input.trim().length === 0 && !hasStaged)}
            aria-label="전송"
          >
            <Icon name="send" size={16} />
          </button>
        )}
      </form>
    </>
  );
}

// 위로 스크롤해 읽는 중일 때만 — 최신으로 복귀. Anchored by the surface's
// position:relative panel; identical on both surfaces.
export function ScrollToBottomButton({ visible, onClick }: { visible: boolean; onClick: () => void }) {
  if (!visible) return null;
  return (
    <button type="button" className="chat-scroll-bottom" onClick={onClick} aria-label="맨 아래로" title="맨 아래로">
      <Icon name="chevron-down" size={18} />
    </button>
  );
}
