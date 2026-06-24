import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";

import { WindowControls } from "./WindowControls";

describe("WindowControls", () => {
  it("renders nothing outside the Tauri desktop shell", () => {
    // jsdom has no __TAURI_INTERNALS__, so the web build draws no custom titlebar
    // (and never reaches the lazy @tauri-apps/api import).
    const { container } = render(<WindowControls />);
    expect(container).toBeEmptyDOMElement();
  });
});
