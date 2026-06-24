import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { DenebStatus } from "./DenebStatus";

describe("DenebStatus", () => {
  it("renders the sparkle and an initial waiting word", () => {
    render(<DenebStatus />);
    expect(screen.getByText("생각 중…")).toBeInTheDocument();
  });

  it("appends the gateway thinking preview as an inline summary", () => {
    render(<DenebStatus summary="메일 확인 중" />);
    expect(screen.getByText(/메일 확인 중/)).toBeInTheDocument();
  });

  it("omits the summary span when no preview is given", () => {
    const { container } = render(<DenebStatus />);
    expect(container.querySelector(".deneb-status-summary")).toBeNull();
  });
});
