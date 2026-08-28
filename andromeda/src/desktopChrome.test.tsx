import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useDesktopChrome } from "./desktopChrome";

const cleanupZoom = vi.fn();
vi.mock("./zoom", () => ({ initZoom: () => cleanupZoom }));
vi.mock("./tauri", () => ({ isTauri: () => true }));

function fireContextMenu(target: EventTarget): MouseEvent {
  const ev = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
  target.dispatchEvent(ev);
  return ev;
}

describe("useDesktopChrome", () => {
  afterEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
  });

  it("suppresses the default context menu on static chrome but not in editable fields", () => {
    const { unmount } = renderHook(() => useDesktopChrome());
    const div = document.createElement("div");
    const input = document.createElement("input");
    document.body.append(div, input);

    expect(fireContextMenu(div).defaultPrevented).toBe(true);
    expect(fireContextMenu(input).defaultPrevented).toBe(false);

    // Unmount detaches the listener and tears down zoom.
    unmount();
    expect(fireContextMenu(div).defaultPrevented).toBe(false);
    expect(cleanupZoom).toHaveBeenCalled();
  });
});
