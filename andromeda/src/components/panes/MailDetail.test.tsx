import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MailDetail } from "./MailDetail";
import type { Mail } from "@/types";
import { renderWithProviders } from "@/test/util";

// The enrichment cards fetch on open. Mock just the two RPCs this layout test
// cares about: a populated sender context (so the 발신자 card has something to
// fold) and a cached-analysis miss (so the AI 분석 card renders its idle state).
// Everything else stays real via importOriginal.
vi.mock("@/gateway", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/gateway")>();
  return {
    ...actual,
    cachedMailAnalysis: vi.fn(async () => null),
    senderContext: vi.fn(async () => ({
      sender: "유광열 <yu@topsolar.co.kr>",
      recent: { count: 5, windowDays: 30, truncated: false, lastReceivedAt: "2026-06-20T09:00:00+09:00" },
      wikiHits: [{ path: "프로젝트/탑솔라", title: "탑솔라", summary: "거래처" }],
    })),
  };
});

const mail: Mail = {
  id: "m1",
  subject: "견적 회신",
  from: "유광열 <yu@topsolar.co.kr>",
  body: "이것은 본문입니다.",
};

function renderDetail() {
  return renderWithProviders(
    <MailDetail
      mail={mail}
      query={{ isLoading: false }}
      busy={false}
      onMarkRead={() => {}}
      onArchive={() => {}}
      onTrash={() => {}}
    />,
    { connected: true },
  );
}

describe("MailDetail layout", () => {
  it("defaults to the 분석 view and toggles to 본문", async () => {
    renderDetail();
    // Let the sender fetch settle so the enrichment cards' state lands inside act().
    await screen.findByText("최근 30일 5건");
    // 분석 is the default: the AI-analysis card shows (idle 분석 button, cached=null)
    // and the raw body is hidden behind the 본문 tab.
    expect(screen.getByRole("button", { name: /이 메일 분석/ })).toBeInTheDocument();
    expect(screen.queryByText("이것은 본문입니다.")).not.toBeInTheDocument();
    // Switch to 본문 → the body appears and the analysis card is hidden.
    await userEvent.click(screen.getByRole("button", { name: "본문" }));
    expect(screen.getByText("이것은 본문입니다.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /이 메일 분석/ })).not.toBeInTheDocument();
  });

  it("collapses the 발신자 card by default and expands it on click", async () => {
    renderDetail();
    // Wait for the sender context to load — the collapsed header carries a teaser.
    expect(await screen.findByText("최근 30일 5건")).toBeInTheDocument();
    // Folded: the wiki-page chip is not rendered even though the data is present.
    expect(screen.queryByRole("button", { name: "탑솔라" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /발신자/ }));

    // Expanded: the curated wiki page now shows as a chip.
    expect(await screen.findByRole("button", { name: "탑솔라" })).toBeInTheDocument();
  });
});
