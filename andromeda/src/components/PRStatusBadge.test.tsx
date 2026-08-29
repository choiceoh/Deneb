import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { SessionPR } from "@/gateway";
import { PRStatusBadge } from "./PRStatusBadge";

afterEach(cleanup);

describe("PRStatusBadge", () => {
  it("renders nothing when there is no pull request", () => {
    // A permanent empty badge would be chrome, not information.
    const { container } = render(<PRStatusBadge pr={{ state: "none" }} />);
    expect(container.querySelector(".pr-badge")).toBeNull();
    const nothing = render(<PRStatusBadge pr={null} />);
    expect(nothing.container.querySelector(".pr-badge")).toBeNull();
  });

  // ★The distinction the whole feature rests on: "could not ask" must not look
  // like "no pull request", or the operator reads untracked work as fine.
  it("shows an explicit unknown rather than staying silent", () => {
    render(<PRStatusBadge pr={{ state: "unknown" }} />);
    expect(screen.getByRole("img", { name: "상태 확인 불가" })).toBeInTheDocument();
  });

  it("names the failing state and counts what broke", () => {
    const pr: SessionPR = { state: "failing", number: 4931, failing: 2, url: "https://x/4931" };
    render(<PRStatusBadge pr={pr} />);
    // Meaning lives in the label, not only in the colour.
    expect(screen.getByLabelText("#4931 검사 실패 2건 실패")).toBeInTheDocument();
  });

  it("links to the pull request when it knows where to go", () => {
    render(<PRStatusBadge pr={{ state: "merged", number: 4932, url: "https://x/4932" }} />);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "https://x/4932");
    expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
  });

  it("stays plain text when there is nothing to open", () => {
    // unknown carries no pull request, so a link would go nowhere.
    render(<PRStatusBadge pr={{ state: "unknown" }} />);
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("reports remaining checks while running", () => {
    render(<PRStatusBadge pr={{ state: "running", number: 7, pending: 3, url: "https://x/7" }} />);
    expect(screen.getByLabelText("#7 검사 중 3건 남음")).toBeInTheDocument();
  });

  it("ignores a state it does not know instead of rendering a blank chip", () => {
    render(<PRStatusBadge pr={{ state: "bogus" } as unknown as SessionPR} />);
    expect(screen.queryByRole("img")).toBeNull();
  });
});
