// App-styled right-click menu — the mouse-first idiom the webview's default
// (English browser) menu can't provide. One open menu at a time; closes on any
// click, Escape, scroll, or resize. Positioned within the viewport.
import { useState, type MouseEvent as ReactMouseEvent } from "react";
import { ContextMenuView, type MenuState } from "./ContextMenuView";

export interface MenuItem {
  label: string;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

export function useContextMenu() {
  const [menu, setMenu] = useState<MenuState | null>(null);

  function openMenu(e: ReactMouseEvent, items: MenuItem[]) {
    if (items.length === 0) return;
    e.preventDefault();
    e.stopPropagation();
    setMenu({ x: e.clientX, y: e.clientY, items });
  }

  const element = menu ? <ContextMenuView {...menu} onClose={() => setMenu(null)} /> : null;
  return { openMenu, menuElement: element };
}
