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
// Mutable analysis payload so one test can return a card-leading analysis.
let mockAnalysis: { analysis?: string; analysisQuality?: string } | null = null;

vi.mock("@/gateway", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/gateway")>();
  return {
    ...actual,
    cachedMailAnalysis: vi.fn(async () => mockAnalysis),
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

function renderDetail(over?: Partial<Mail>) {
  return renderWithProviders(
    <MailDetail
      mail={{ ...mail, ...over }}
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
  it("toggles to 본문 view when tab selected", async () => {
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

  it("expands 발신자 card when header clicked", async () => {
    renderDetail();
    // Wait for the sender context to load — the collapsed header carries a teaser.
    expect(await screen.findByText("최근 30일 5건")).toBeInTheDocument();
    // Folded: the wiki-page chip is not rendered even though the data is present.
    expect(screen.queryByRole("button", { name: "탑솔라" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /발신자/ }));

    // Expanded: the curated wiki page now shows as a chip.
    expect(await screen.findByRole("button", { name: "탑솔라" })).toBeInTheDocument();
  });

  it("previews PDF in-app and renders non-previewable attachment as link", async () => {
    const originalFetch = globalThis.fetch;
    // jsdom has no object-URL support; the PDF viewer creates one for its
    // <embed>. Patch createObjectURL directly (spreading URL drops its static
    // methods); the viewer guards revoke, so it need not exist.
    const urlObj = globalThis.URL as unknown as { createObjectURL?: (b: Blob) => string };
    const prevCreate = urlObj.createObjectURL;
    urlObj.createObjectURL = () => "blob:pdf";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (typeof url === "string" && url.includes("/gmail/attachment")) {
          return { ok: true, blob: async () => ({ size: 1024, type: "application/pdf" }) } as unknown as Response;
        }
        return originalFetch(url);
      }),
    );
    try {
      renderDetail({
        attachmentCount: 2,
        attachments: [
          { attachmentId: "a1", filename: "견적서.pdf", mimeType: "application/pdf", size: 1024 },
          { attachmentId: "a2", filename: "정산.xlsx", mimeType: "application/octet-stream", size: 2048 },
        ],
      });

      // The PDF is previewable → a button that opens the in-app viewer modal.
      const pdf = await screen.findByRole("button", { name: /견적서\.pdf/ });
      // The XLSX has no previewer → stays a plain download link (HWP would be a
      // preview button — the in-browser HWP text extractor covers it).
      const xlsx = screen.getByRole("link", { name: /정산\.xlsx/ });
      expect(xlsx).toHaveAttribute("href", expect.stringContaining("/gmail/attachment"));

      await userEvent.click(pdf);
      await screen.findByRole("dialog");
      // The PDF viewer renders an <embed> once the blob loads; the modal also
      // offers a download link. Re-query each poll (the embed appears async).
      await vi.waitFor(() =>
        expect(document.querySelector('.mail-attachment-preview embed[type="application/pdf"]')).toBeTruthy(),
      );
    } finally {
      urlObj.createObjectURL = prevCreate;
      vi.unstubAllGlobals();
    }
  });

  it("renders a card-leading analysis as a deneb-ui card, not a raw code block", async () => {
    // Regression: mail analyses now lead with a ```deneb-ui card. MailDetail
    // rendered the analysis via plain Markdown, which leaked the fence as a
    // copyable code block. It must route through AssistantText/splitDenebUi.
    mockAnalysis = {
      analysis:
        "```deneb-ui\n" +
        '<column><card><text style="title">중요도: 확인</text>' +
        '<text style="body">근저당 말소 예정</text></card></column>\n' +
        "```\n\n## 왜 지금 왔나\n\n곡성 리스 PF 심의 후속입니다.",
      analysisQuality: "중요",
    };
    try {
      const { container } = renderDetail();
      // The card renders through the deneb-ui renderer (.dui root), and the
      // prose tail still renders. The literal fence must NOT survive as a
      // <code>/<pre> block.
      await vi.waitFor(() => expect(container.querySelector(".dui")).toBeTruthy());
      expect(screen.getByText("근저당 말소 예정")).toBeTruthy();
      expect(screen.getByText(/왜 지금 왔나/)).toBeTruthy();
      expect(container.querySelector("pre code")).toBeNull();
      expect(container.textContent).not.toContain("```deneb-ui");
    } finally {
      mockAnalysis = null;
    }
  });
});
