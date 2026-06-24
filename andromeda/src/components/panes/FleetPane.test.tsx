import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { server } from "@/mocks/server";
import { renderWithProviders } from "@/test/util";
import { FleetPane } from "./FleetPane";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("FleetPane", () => {
  it("loads fleet nodes from the gateway passthrough", async () => {
    renderWithProviders(<FleetPane />, { connected: true, cfg: { url: "http://mock.local", token: "mock" } });

    expect(await screen.findByText("srv1")).toBeInTheDocument();
    expect(screen.getByText("controller")).toBeInTheDocument();
    expect(screen.getByText(/GPU0 72%/)).toBeInTheDocument();
    expect(screen.getAllByText("1/2").length).toBeGreaterThanOrEqual(1);
  });

  it("runs a recipe action through a confirmation dialog", async () => {
    const user = userEvent.setup();
    renderWithProviders(<FleetPane />, { connected: true, cfg: { url: "http://mock.local", token: "mock" } });

    await screen.findByText("srv1");
    await user.click(screen.getByRole("tab", { name: "레시피" }));

    const recipeCell = await screen.findByText("deepseek-v4-flash");
    const recipeRow = recipeCell.closest("tr");
    expect(recipeRow).not.toBeNull();
    await user.click(within(recipeRow as HTMLTableRowElement).getByRole("button", { name: "기동" }));

    const dialog = await screen.findByRole("dialog", { name: /deepseek-v4-flash 기동/ });
    await user.click(within(dialog).getByRole("button", { name: "기동" }));

    expect(await screen.findByText(/mock-deepseek-v4-flash-launch/)).toBeInTheDocument();
  });
});
