// The rendered right-click menu (see useContextMenu in ContextMenu.tsx). Lives
// in its own file so the hook module stays component-free (react-refresh).
import { useEffect, useRef, useState } from "react";
import type { MenuItem } from "./ContextMenu";

export interface MenuState {
  x: number;
  y: number;
  items: MenuItem[];
}

export function ContextMenuView({ x, y, items, onClose }: MenuState & { onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  // Clamp into the viewport once mounted (menu size known only after render).
  const [pos, setPos] = useState({ x, y });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      x: Math.min(x, window.innerWidth - r.width - 8),
      y: Math.min(y, window.innerHeight - r.height - 8),
    });
  }, [x, y]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    // Capture-phase so a click that opens something else still dismisses first.
    window.addEventListener("mousedown", onClose, true);
    window.addEventListener("blur", onClose);
    window.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onClose, true);
    window.addEventListener("resize", onClose);
    return () => {
      window.removeEventListener("mousedown", onClose, true);
      window.removeEventListener("blur", onClose);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onClose, true);
      window.removeEventListener("resize", onClose);
    };
  }, [onClose]);

  return (
    <div ref={ref} className="ctx-menu panel" style={{ left: pos.x, top: pos.y }} role="menu">
      {items.map((item) => (
        <button
          key={item.label}
          type="button"
          role="menuitem"
          className={"ctx-menu-item" + (item.danger ? " danger" : "")}
          disabled={item.disabled}
          // mousedown fires before the window capture listener closes the menu,
          // so act on mousedown and let the close ride the same event.
          onMouseDown={(e) => {
            if (e.button !== 0 || item.disabled) return;
            item.onSelect();
          }}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
