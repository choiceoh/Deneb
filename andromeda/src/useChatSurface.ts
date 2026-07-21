import { type ChangeEvent, type RefObject, useEffect, useRef, useState } from "react";

import { inferAttachmentMimeType } from "@/attachmentMime";
import { readFileBase64, splitAttachable } from "@/attachments";
import { type GatewayConfig, type ModelsList, listModels } from "@/gateway";

// Shared behavior of the two chat surfaces (chat tab · AI side panel). Each hook
// here used to exist as a byte-identical copy in ChatView.tsx AND AIPanel.tsx —
// extracted so the surfaces can't drift (the 인쇄 button landed twice; never again).

// Load the model registry once connected; best-effort (older gateway / the
// offline test path just leaves it empty). The disconnect reset is a render
// adjustment (react.dev adjust-state pattern).
export function useModels(cfg: GatewayConfig, connected: boolean) {
  const [models, setModels] = useState<ModelsList | null>(null);
  const [model, setModel] = useState(""); // selected override id ("" → gateway main)
  const [prevConn, setPrevConn] = useState(connected);
  if (prevConn !== connected) {
    setPrevConn(connected);
    if (!connected) setModels(null);
  }
  useEffect(() => {
    if (!connected) return;
    let cancelled = false;
    void listModels(cfg)
      .then((m) => {
        if (cancelled) return;
        setModels(m);
        setModel((prev) => prev || m.current || "");
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);
  return { models, model, setModel };
}

// Composer textarea behavior:
// - autosize to content, re-measured on reveal too — a hidden (display:none) tab
//   measures 0 height, so without `hidden` in the deps it would open collapsed.
// - focus restore when a turn/attachment finishes — busy disables the textarea and
//   drops focus; restore only when focus fell to body (don't steal a moved focus).
// - focusOnReveal: the full-size chat tab focuses on reveal so you can type right
//   away; the side panel opts out (it must not steal focus from the work area).
export function useComposerBehavior(
  composeRef: RefObject<HTMLTextAreaElement | null>,
  {
    input,
    busy,
    hidden,
    focusOnReveal = false,
  }: { input: string; busy: boolean; hidden: boolean; focusOnReveal?: boolean },
) {
  useEffect(() => {
    const el = composeRef.current;
    if (!el || hidden) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [input, hidden, composeRef]);

  useEffect(() => {
    if (focusOnReveal && !hidden) composeRef.current?.focus();
  }, [hidden, focusOnReveal, composeRef]);

  const wasBusy = useRef(false);
  useEffect(() => {
    if (wasBusy.current && !busy && !hidden) {
      const active = document.activeElement;
      if (!active || active === document.body || active === composeRef.current) {
        composeRef.current?.focus();
      }
    }
    wasBusy.current = busy;
  }, [busy, hidden, composeRef]);
}

// 첨부 인입(클립 버튼·드롭·붙여넣기 공용): 형식·크기를 거른 뒤(splitAttachable) 한 파일씩
// 순서대로 capture(이미지 OCR·음성 전사·문서 추출)에 보낸다. 입력창의 텍스트는 첫 비-음성
// 파일의 캡션으로 동봉한다.
//
// 배치는 파일마다 `await capture(...)`로 직렬화된다 — 한 번에 하나만 처리되므로 자기들끼리
// 겹칠 일이 없다. 배치가 도는 동안 attaching=true라 새 전송·세션 전환·삭제·새 대화가 모두
// 막힌다(attaching state는 useSessions로 넘어가고, attachingRef는 동기 재진입을 막는다).
//
// ⚠️ 과거엔 파일 사이마다 공유 busy(useChat 상태)를 재확인해 "배치 밖 턴이 끼면 남은 파일
// 건너뛰기"를 시도했으나, 실제 capture가 그 busy를 켰다 끄는 주체다 — 직전 capture의 busy
// 해제가 다음 파일 검사 시점까지 리렌더로 반영된다는 보장이 없어(Tauri webview 스케줄러),
// 배치가 자기 자신의 첨부를 "다른 응답 진행 중"으로 오인해 첫 파일만 남기고 전부 드롭했다.
// 그래서 그 재확인을 없앴다 — 직렬 await + attaching 게이트로 충분하다.
// A file waiting in the composer (frontier staging UX: pick/drop/paste collects
// chips; nothing uploads until 전송). previewUrl is an object URL for images —
// intentionally NOT revoked on send, because the transcript keeps rendering it
// as the user-turn thumbnail for the rest of the session (bounded, local).
export interface StagedAttachment {
  id: string;
  file: File;
  name: string;
  mimeType: string;
  previewUrl?: string;
}

let stagedSeq = 0;

export function useAttachPipeline({
  connected,
  busy,
  input,
  setInput,
  setAttaching,
  pin,
  capture,
  onBatchDone,
}: {
  connected: boolean;
  busy: boolean;
  input: string;
  setInput: (v: string) => void;
  setAttaching: (v: boolean) => void;
  pin: () => void;
  // The surface binds its own sessionKey: (file, caption, previewUrl) =>
  // capture(file, {sessionKey, caption, previewUrl}).
  capture: (
    file: { name: string; mimeType: string; base64: string },
    caption: string,
    previewUrl?: string,
  ) => Promise<void>;
  onBatchDone?: () => void; // e.g. refresh the session list once the batch lands
}) {
  const attachingRef = useRef(false);

  // 건너뛴 파일 안내(미지원 형식·크기 초과·읽기 실패) — 컴포저 위에 잠깐 떴다 사라진다.
  const [attachNote, setAttachNote] = useState("");
  const noteTimer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (noteTimer.current !== null) window.clearTimeout(noteTimer.current);
    },
    [],
  );
  function showAttachNote(lines: string[]) {
    if (lines.length === 0) return;
    setAttachNote(lines.join(" · "));
    if (noteTimer.current !== null) window.clearTimeout(noteTimer.current);
    noteTimer.current = window.setTimeout(() => setAttachNote(""), 6000);
  }

  // Composer staging: picked/dropped/pasted files wait here as chips. Nothing
  // uploads until 전송 (sendStaged) — frontier-style consent + one message can
  // carry text and files together.
  const [staged, setStaged] = useState<StagedAttachment[]>([]);

  function attachFiles(files: File[]) {
    if (busy || !connected || files.length === 0) return;
    const { ok, skipped } = splitAttachable(files);
    showAttachNote(skipped);
    if (ok.length === 0) return;
    setStaged((prev) => [
      ...prev,
      ...ok.map((file) => {
        const mimeType = inferAttachmentMimeType(file.name, file.type);
        // createObjectURL is missing in jsdom — thumbnails just degrade there.
        const canPreview = mimeType.startsWith("image/") && typeof URL.createObjectURL === "function";
        return {
          id: `staged-${++stagedSeq}`,
          file,
          name: file.name,
          mimeType,
          previewUrl: canPreview ? URL.createObjectURL(file) : undefined,
        };
      }),
    ]);
  }

  function removeStaged(id: string) {
    setStaged((prev) => {
      const hit = prev.find((s) => s.id === id);
      if (hit?.previewUrl) URL.revokeObjectURL(hit.previewUrl);
      return prev.filter((s) => s.id !== id);
    });
  }

  // Send the staged batch, one capture per file; the composer text rides as the
  // first non-audio file's caption (existing gateway contract).
  async function sendStaged(captionOverride?: string) {
    if (busy || attachingRef.current || !connected || staged.length === 0) return;
    const batch = staged;
    setStaged([]);
    const captionTarget = batch.find((s) => !s.mimeType.startsWith("audio/"));
    // Audio-only batches never consume the composer text (existing contract).
    const caption = captionTarget ? (captionOverride ?? input).trim() : "";
    if (caption) setInput("");
    attachingRef.current = true;
    setAttaching(true);
    try {
      for (const item of batch) {
        try {
          const base64 = await readFileBase64(item.file);
          pin();
          await capture(
            { name: item.name, mimeType: item.mimeType, base64 },
            item === captionTarget ? caption : "",
            item.previewUrl,
          );
        } catch {
          showAttachNote([`${item.name} — 읽기 실패라 건너뜀`]);
        }
      }
    } finally {
      attachingRef.current = false;
      setAttaching(false);
    }
    onBatchDone?.();
  }

  function onPick(e: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files ?? []);
    e.target.value = ""; // let the same selection be picked again later
    attachFiles(files);
  }

  return { attachNote, attachingRef, attachFiles, onPick, staged, removeStaged, sendStaged };
}
