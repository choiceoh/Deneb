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

// 첨부 인입(클립 버튼·드롭·붙여넣기 공용): 형식·크기를 거른 뒤(splitAttachable) 스테이징
// 칩으로 대기시키고, 전송 시 스테이징된 모든 파일을 base64로 읽어 captureBatch에 **한 번에**
// 넘긴다 — 게이트웨이가 파일들을 저장하고 포인터 한 턴으로 묶어 에이전트가 교차 분석한다
// (파일당 개별 턴이 아니라 6개 첨부=1턴). 입력창 텍스트는 캡션으로 배치에 동봉된다(오디오만인
// 배치는 캡션을 소비하지 않는다 — 기존 계약).
//
// 배치는 captureBatch **단일 호출**이라 파일끼리 겹치거나 서로를 드롭할 일이 없다(과거의
// 파일당-루프 + 공유 busy 재확인이 첫 파일만 남기고 드롭하던 문제를 구조적으로 제거). 배치가
// 도는 동안 attaching=true라 새 전송·세션 전환·삭제·새 대화가 모두 막힌다(attaching state는
// useSessions로 넘어가고, attachingRef는 동기 재진입을 막는다).
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
  captureBatch,
  onBatchDone,
}: {
  connected: boolean;
  busy: boolean;
  input: string;
  setInput: (v: string) => void;
  setAttaching: (v: boolean) => void;
  pin: () => void;
  // The surface binds its own sessionKey: (files, caption) =>
  // captureBatch(files, {sessionKey, caption}).
  captureBatch: (
    files: { name: string; mimeType: string; base64: string; previewUrl?: string }[],
    caption: string,
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

  // Send the staged files as ONE batch turn: read every file's base64, then hand the
  // whole set to captureBatch (the gateway materializes them and runs a single
  // pointer turn). The composer text rides as the caption; an audio-only batch keeps
  // it (existing contract). Per-file read failures are noted and dropped from the
  // batch, not fatal.
  async function sendStaged(captionOverride?: string) {
    if (busy || attachingRef.current || !connected || staged.length === 0) return;
    const batch = staged;
    setStaged([]);
    const hasNonAudio = batch.some((s) => !s.mimeType.startsWith("audio/"));
    const caption = hasNonAudio ? (captionOverride ?? input).trim() : "";
    if (caption) setInput("");
    attachingRef.current = true;
    setAttaching(true);
    try {
      const files: { name: string; mimeType: string; base64: string; previewUrl?: string }[] = [];
      for (const item of batch) {
        try {
          const base64 = await readFileBase64(item.file);
          files.push({ name: item.name, mimeType: item.mimeType, base64, previewUrl: item.previewUrl });
        } catch {
          showAttachNote([`${item.name} — 읽기 실패라 건너뜀`]);
        }
      }
      if (files.length > 0) {
        pin();
        // captureBatch owns its own error rendering (an error turn); guard anyway so
        // a rejected batch never escapes as an unhandled rejection from the
        // fire-and-forget caller, and attaching always resets in the finally.
        try {
          await captureBatch(files, caption);
        } catch {
          showAttachNote(["첨부 전송에 실패했습니다"]);
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
