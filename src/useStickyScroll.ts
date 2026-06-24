import { useEffect, useRef, useState } from "react";

// Keep a scroll container pinned to the bottom as new content arrives — but stop
// following once the user scrolls up to read earlier content, and resume on the
// next pin(). Wire `ref` onto the scroll element and `onScroll` onto its onScroll.
// `deps` is what "new content arrived" means to the caller (e.g. [turns, thinking]).
export function useStickyScroll(deps: unknown[]) {
  const ref = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  // Mirror of `pinned` as state, so a scroll-to-bottom affordance can react to it.
  const [atBottom, setAtBottom] = useState(true);

  useEffect(() => {
    const el = ref.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  function onScroll() {
    const el = ref.current;
    if (!el) return;
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    pinned.current = bottom;
    setAtBottom(bottom); // no-op re-render when unchanged (primitive bail-out)
  }

  // Force-follow the bottom again (e.g. when the user sends a new message).
  function pin() {
    pinned.current = true;
    setAtBottom(true);
  }

  // Smooth-scroll to the latest and resume following (the scroll-to-bottom button).
  function scrollToBottom() {
    const el = ref.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    pinned.current = true;
    setAtBottom(true);
  }

  return { ref, onScroll, pin, atBottom, scrollToBottom };
}
