import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import { ProgressPane } from "./ProgressPane";
import { fakeProvider, renderWithProviders } from "@/test/util";

describe("ProgressPane (프로젝트 진행상황)", () => {
  it("renders a card per project with headline, bullets, and a due badge", async () => {
    const dataProvider = fakeProvider({
      progress: [
        {
          project: "새만금 태양광",
          headline: "PPA 협상 막바지",
          bullets: ["계통연계 승인 대기", "EPC 입찰 마감 임박"],
          due: "6/30",
          updatedAtMs: 1718000000000,
          path: "projects/saemangeum",
        },
        { project: "영광 풍력", headline: "환경영향평가 진행", bullets: ["주민설명회 완료"] },
      ],
    });
    renderWithProviders(<ProgressPane />, { connected: true, dataProvider });

    expect(await screen.findByText("새만금 태양광")).toBeInTheDocument();
    expect(screen.getByText("PPA 협상 막바지")).toBeInTheDocument();
    expect(screen.getByText("계통연계 승인 대기")).toBeInTheDocument();
    expect(screen.getByText("EPC 입찰 마감 임박")).toBeInTheDocument();
    // The deadline surfaces as a "마감 …" badge.
    expect(screen.getByText(/마감 6\/30/)).toBeInTheDocument();
    // A second project with no due still renders.
    expect(screen.getByText("영광 풍력")).toBeInTheDocument();
  });

  it("shows the empty notice when there are no digests", async () => {
    renderWithProviders(<ProgressPane />, { connected: true, dataProvider: fakeProvider({ progress: [] }) });
    expect(await screen.findByText(/진행 중인 프로젝트가 없습니다/)).toBeInTheDocument();
  });

  it("shows a connect notice when disconnected", () => {
    renderWithProviders(<ProgressPane />, { connected: false });
    expect(screen.getByText(/게이트웨이에 연결하면/)).toBeInTheDocument();
  });
});
