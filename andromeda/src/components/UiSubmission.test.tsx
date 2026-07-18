import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { UiSubmissionBubble } from "./UiSubmission";

describe("UiSubmissionBubble", () => {
  it("renders collected values as key/value chips under a 카드 응답 label", () => {
    render(
      <UiSubmissionBubble
        sub={{
          event: "",
          values: [
            ["slot", "14:00"],
            ["dept", "영업"],
          ],
        }}
      />,
    );
    expect(screen.getByRole("group", { name: "카드 응답" })).toBeInTheDocument();
    expect(screen.getByText("slot")).toBeInTheDocument();
    expect(screen.getByText("14:00")).toBeInTheDocument();
    expect(screen.getByText("영업")).toBeInTheDocument();
    // the raw wire text must not leak through
    expect(screen.queryByText(/Responded with/)).toBeNull();
  });

  it("renders a bare press as an event chip", () => {
    render(<UiSubmissionBubble sub={{ event: "confirm_slot", values: [] }} />);
    expect(screen.getByText("confirm_slot")).toBeInTheDocument();
    expect(screen.queryByText(/Pressed/)).toBeNull();
  });
});
