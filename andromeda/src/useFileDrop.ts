import { type DragEvent as ReactDragEvent, useEffect, useRef, useState } from "react";

// 채팅 영역 전체를 무표시 드롭존으로 만든다: 평소엔 아무 표시 없고, 파일 드래그가 실제로
// 존 위에 있을 때만 `over`가 켜져 살짝 표시(스타일은 caller — `.drop-over`), 놓으면 onFiles로
// 넘긴다(여러 개 가능). dragenter/leave는 자식 요소마다 쌍으로 발화하므로 depth 카운터로
// 깜빡임 없이 유지한다. 파일이 아닌 드래그(텍스트 선택 등)는 건드리지 않아 평소 동작 그대로다.
// Tauri 창에서는 dragDropEnabled:false(src-tauri/tauri.conf.json)가 전제 — 그래야 OS 파일
// 드래그가 웹뷰 HTML5 DnD 이벤트(File 객체)로 그대로 들어온다.
// Singleton window-level guard, refcounted across mounted drop zones.
let windowGuardRefs = 0;
function windowDragGuard(e: globalThis.DragEvent) {
  if (e.dataTransfer && Array.from(e.dataTransfer.types).includes("Files")) e.preventDefault();
}
function acquireWindowDragGuard() {
  if (windowGuardRefs++ === 0) {
    window.addEventListener("dragover", windowDragGuard);
    window.addEventListener("drop", windowDragGuard);
  }
}
function releaseWindowDragGuard() {
  if (--windowGuardRefs === 0) {
    window.removeEventListener("dragover", windowDragGuard);
    window.removeEventListener("drop", windowDragGuard);
  }
}

export function useFileDrop(enabled: boolean, onFiles: (files: File[]) => void) {
  const depth = useRef(0);
  const [over, setOver] = useState(false);

  // 존 밖(창 아무 데나)에 파일을 놓으면 브라우저/웹뷰가 그 파일로 이동해 앱이 날아간다 —
  // 파일 드래그에 한해 창 수준 기본 동작을 막는다 (텍스트 드래그는 통과). 여러 드롭존이
  // 마운트돼도(우측 패널+채팅 탭) 리스너는 refcount로 한 벌만 단다 — dragover는 고빈도
  // 이벤트라 중복 등록이 그대로 중복 비용이 된다.
  useEffect(() => {
    acquireWindowDragGuard();
    return releaseWindowDragGuard;
  }, []);

  const hasFiles = (e: ReactDragEvent) => Array.from(e.dataTransfer?.types ?? []).includes("Files");

  return {
    over,
    dropProps: {
      onDragEnter(e: ReactDragEvent) {
        if (!hasFiles(e)) return;
        e.preventDefault();
        depth.current += 1;
        if (enabled) setOver(true);
      },
      onDragOver(e: ReactDragEvent) {
        if (!hasFiles(e)) return;
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = enabled ? "copy" : "none";
      },
      onDragLeave(e: ReactDragEvent) {
        if (!hasFiles(e)) return;
        depth.current = Math.max(0, depth.current - 1);
        if (depth.current === 0) setOver(false);
      },
      onDrop(e: ReactDragEvent) {
        if (!hasFiles(e)) return;
        e.preventDefault();
        depth.current = 0;
        setOver(false);
        const files = Array.from(e.dataTransfer?.files ?? []);
        if (enabled && files.length > 0) onFiles(files);
      },
    },
  };
}
