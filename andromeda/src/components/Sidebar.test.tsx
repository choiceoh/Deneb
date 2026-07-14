import { beforeEach, describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import { Sidebar } from "./Sidebar";
import { renderWithProviders } from "@/test/util";

describe("Sidebar nav rail", () => {
  beforeEach(() => localStorage.clear());

  it("hides configured panes when listed in andromeda.hiddenPanes and preserves 설정 entry", () => {
    localStorage.setItem("andromeda.hiddenPanes", JSON.stringify(["mail", "wiki"]));
    renderWithProviders(<Sidebar />);
    const nav = screen.getByRole("navigation");

    // hidden panes drop out of the rail
    expect(within(nav).queryByRole("button", { name: /메일/ })).not.toBeInTheDocument();
    expect(within(nav).queryByRole("button", { name: /위키/ })).not.toBeInTheDocument();
    // everything else stays, and 설정 is never hideable
    expect(within(nav).getByRole("button", { name: /오늘/ })).toBeInTheDocument();
    expect(within(nav).getByRole("button", { name: /설정/ })).toBeInTheDocument();
  });
});
