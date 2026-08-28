import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { server } from "@/mocks/server";
import { renderWithProviders } from "@/test/util";
import { FleetPane } from "./FleetPane";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// Recipe control left the human surfaces on 2026-08-28 (AI-only via the
// gateway fleet chat tool); the pane's remaining contract is overview/nodes/
// models/services/jobs over the authenticated passthrough.
describe("FleetPane", () => {
  it("loads fleet status into workflow tabs", async () => {
    const user = userEvent.setup();
    renderWithProviders(<FleetPane />, { connected: true, cfg: { url: "http://mock.local", token: "mock" } });

    expect(await screen.findByRole("tab", { name: /개요/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /노드/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /모델/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /서비스/ })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /레시피/ })).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /작업/ })).toBeInTheDocument();
    expect(screen.getAllByText("1/2").length).toBeGreaterThanOrEqual(1);

    await user.click(screen.getByRole("tab", { name: /노드/ }));
    const nodes = screen.getByRole("region", { name: "노드" });
    expect(within(nodes).getByText("srv1")).toBeInTheDocument();
    expect(within(nodes).getByText("controller")).toBeInTheDocument();

    await user.click(within(nodes).getByLabelText("문제만"));
    expect(within(nodes).getByText("srv3")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /모델/ }));
    const models = screen.getByRole("region", { name: "모델" });
    expect(within(models).getByText("Qwen3-30B-A3B")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /서비스/ }));
    const services = screen.getByRole("region", { name: "서비스" });
    expect(within(services).getByText("sparkfleet")).toBeInTheDocument();
    await user.click(within(services).getByRole("button", { name: "다운" }));
    expect(within(services).getByText("node")).toBeInTheDocument();
  });
});
