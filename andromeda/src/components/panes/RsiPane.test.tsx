import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@/test/util";
import { RsiPane } from "./RsiPane";

describe("RsiPane", () => {
  // The sandbox has no gateway; the disconnected path is what's deterministically
  // testable (CLAUDE.md) — it must guide, not spin or blank.
  it("shows the disconnected state when not connected", () => {
    renderWithProviders(<RsiPane />, { connected: false });
    expect(screen.getByText("미연결")).toBeInTheDocument();
  });
});
