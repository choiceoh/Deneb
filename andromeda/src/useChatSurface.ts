import { type ChangeEvent, type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";

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
// 동시성 방어 2겹 (원 구현의 주석 요지):
// - attaching(컴포넌트 소유 state) — busy가 파일 읽기 틈에 잠깐 내려가는 동안에도 세션
//   전환/삭제/새 대화를 막는다. 컴포넌트가 useSessions에 넘겨야 해서 state는 밖에 산다.
// - attachingRef — 동기 재진입 차단. busyRef 미러는 useLayoutEffect(커밋 시 동기 반영)라
//   다음 매크로태스크(FileReader 콜백)가 낡은 값을 읽을 수 없다. 파일 읽기 틈에 배치 밖의
//   턴(다시 생성 등)이 시작됐다면 남은 파일은 건너뛴다 — 턴 인터리브 방지.
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
  const busyRef = useRef(busy);
  useLayoutEffect(() => {
    busyRef.current = busy;
  });

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
          // 읽기(≥1 태스크 경계) 후 확인이라 직전 capture의 busy 해제는 ref에 반영돼 있다.
          if (busyRef.current) {
            showAttachNote([`${item.name} — 다른 응답이 진행 중이라 건너뜀`]);
            continue;
          }
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
