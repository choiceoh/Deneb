import { describe, expect, it } from "vitest";

import type { FleetNode } from "@/fleet";
import {
  asArray,
  bytes,
  clamp,
  gpuText,
  memoryText,
  nodeHasIssue,
  nodeIssueText,
  nodeIssueView,
  oneLine,
  percent,
} from "./fleetHelpers";

const node = (overrides: Partial<FleetNode> = {}): FleetNode => ({ name: "srv1", ...overrides });

describe("fleet display helpers", () => {
  it("formats nullable metrics without leaking invalid values", () => {
    expect(asArray(null)).toEqual([]);
    expect(gpuText(node())).toBe("");
    expect(
      gpuText(
        node({
          metrics: {
            gpus: [
              { index: 0, utilPct: 72, tempC: 64 },
              { index: 1, utilPct: null, tempC: 70 },
            ],
          },
        }),
      ),
    ).toBe("GPU0 72% 64°C, GPU1 70°C");
    expect(memoryText(node({ metrics: { memory: { totalKB: 1024, availableKB: 256 } } }))).toBe("75% · 768 KB/1.0 MB");
    expect(memoryText(node({ metrics: { memory: { totalKB: 0, availableKB: 0 } } }))).toBe("");
  });

  it("keeps numeric and one-line formatting bounded", () => {
    expect(percent(9, 10)).toBe(90);
    expect(percent(1, 0)).toBe(0);
    expect(bytes(Number.NaN)).toBe("0 B");
    expect(bytes(1536)).toBe("1.5 KB");
    expect(bytes(10 * 1024 * 1024)).toBe("10 MB");
    expect(clamp(Number.POSITIVE_INFINITY)).toBe(0);
    expect(clamp(-1)).toBe(0);
    expect(clamp(101)).toBe(100);
    expect(oneLine("  여러\n  줄\t상태  ")).toBe("여러 줄 상태");
    expect(oneLine("x".repeat(200))).toHaveLength(140);
  });
});

describe("fleet issue classification", () => {
  it("when treats the documented resource thresholds as inclusive", () => {
    const healthy = node({
      metrics: {
        memory: { totalKB: 100, availableKB: 11 },
        disks: [{ path: "/", usePct: 89 }],
        gpus: [{ index: 0, tempC: 84 }],
        services: [{ name: "gateway", ok: true }],
      },
    });
    expect(nodeHasIssue(healthy)).toBe(false);

    expect(nodeHasIssue(node({ metrics: { memory: { totalKB: 100, availableKB: 10 } } }))).toBe(true);
    expect(nodeIssueText(node({ metrics: { memory: { totalKB: 100, availableKB: 10 } } }))).toBe("메모리 90%");
    expect(nodeHasIssue(node({ metrics: { disks: [{ path: "/data", usePct: 90 }] } }))).toBe(true);
    expect(nodeIssueText(node({ metrics: { disks: [{ path: "/data", usePct: 90 }] } }))).toBe("디스크 90% · /data");
    expect(nodeHasIssue(node({ metrics: { gpus: [{ index: 2, tempC: 85 }] } }))).toBe(true);
    expect(nodeIssueText(node({ metrics: { gpus: [{ index: 2, tempC: 85 }] } }))).toBe("GPU2 85°C");
  });

  it("reports connectivity first and routes service failures to the services view", () => {
    const unreachable = node({
      reachable: false,
      error: "timeout",
      metrics: { services: [{ name: "gateway", ok: false }] },
    });
    expect(nodeIssueText(unreachable)).toBe("연결 안 됨");
    expect(nodeIssueView(unreachable)).toBe("nodes");

    const serviceDown = node({
      metrics: {
        services: [
          { name: "gateway", ok: false },
          { name: "db", ok: true },
        ],
      },
    });
    expect(nodeHasIssue(serviceDown)).toBe(true);
    expect(nodeIssueText(serviceDown)).toBe("서비스 다운: gateway");
    expect(nodeIssueView(serviceDown)).toBe("services");
  });

  it("uses the explicit node error before derived metric findings", () => {
    const failed = node({ error: "probe failed", metrics: { gpus: [{ index: 0, tempC: 95 }] } });
    expect(nodeIssueText(failed)).toBe("probe failed");
    expect(nodeIssueView(failed)).toBe("nodes");
  });
});
