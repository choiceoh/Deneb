import { useEffect, useRef } from "react";

// Keep a scroll container pinned to the bottom as new content arrives — but stop
// following once the user scrolls up to read earlier content, and resume on the
// next pin(). Wire `ref` onto the scroll element and `onScroll` onto its onScroll.
// `deps` is what "new content arrived" means to the caller (e.g. [turns, thinking]).
export function useStickyScroll(deps: unknown[]) {
  const ref = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  useEffect(() => {
    const el = ref.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  function onScroll() {
    const el = ref.current;
    if (el) pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }

  // Force-follow the bottom again (e.g. when the user sends a new message).
  function pin() {
    pinned.current = true;
  }

  return { ref, onScroll, pin };
}
