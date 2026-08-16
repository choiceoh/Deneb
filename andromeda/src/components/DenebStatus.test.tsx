import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { DenebStatus } from "./DenebStatus";

afterEach(() => vi.restoreAllMocks());

describe("DenebStatus", () => {
  it("renders the sparkle and an initial waiting word", () => {
    render(<DenebStatus />);
    expect(screen.getByText("생각 중…")).toBeInTheDocument();
  });

  it("when appends the gateway thinking preview as an inline summary", () => {
    render(<DenebStatus summary="메일 확인 중" />);
    expect(screen.getByText(/메일 확인 중/)).toBeInTheDocument();
  });

  it("omits the summary span when no preview is given", () => {
    const { container } = render(<DenebStatus />);
    expect(container.querySelector(".deneb-status-summary")).toBeNull();
  });

  it("formats a quiet elapsed suffix only for genuinely long turns", () => {
    const now = vi.spyOn(Date, "now").mockReturnValue(10_999);
    const short = render(<DenebStatus startedAt={1_000} />);
    expect(short.container.querySelector(".deneb-status-elapsed")).toBeNull();
    short.unmount();

    now.mockReturnValue(72_000);
    render(<DenebStatus startedAt={1_000} />);
    expect(screen.getByText(/1분 11초 경과/)).toBeInTheDocument();
  });
});
