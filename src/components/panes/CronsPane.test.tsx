import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { CronsPane } from "./CronsPane";

describe("CronsPane", () => {
  const erroring = {
    id: "c3",
    name: "환율 수집",
    schedule: "15분마다",
    enabled: true,
    payloadKind: "httpFetch",
    payloadPreview: "GET https://api.example/fx",
    consecutiveErrors: 3,
    lastError: "타임아웃",
  };

  it("surfaces consecutive errors and the last error inline", async () => {
    renderWithProviders(<CronsPane />, { connected: true, dataProvider: fakeProvider({ crons: [erroring] }) });
    expect(await screen.findByText("오류 3회")).toBeInTheDocument();
    expect(screen.getByText("타임아웃")).toBeInTheDocument();
  });

  it("opens a cron detail modal with the payload preview", async () => {
    renderWithProviders(<CronsPane />, { connected: true, dataProvider: fakeProvider({ crons: [erroring] }) });
    await userEvent.click(await screen.findByText("환율 수집"));
    expect(await screen.findByText("GET https://api.example/fx")).toBeInTheDocument();
  });
});
