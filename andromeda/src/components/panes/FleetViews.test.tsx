import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  ConfirmAction,
  FleetJobsView,
  FleetMetric,
  FleetModelsView,
  FleetNodesView,
  FleetOverview,
  FleetRecipesView,
  FleetServicesView,
} from "./FleetViews";
import type { FleetJob, FleetNode, FleetRecipe } from "@/fleet";
import type { FleetIssue, FleetModelRow, FleetServiceRow } from "./fleetHelpers";

const healthyNode: FleetNode = {
  name: "spark-1",
  role: "worker",
  reachable: true,
  metrics: {
    gpus: [
      { index: 0, utilPct: 72, tempC: 63 },
      { index: 1, utilPct: null, tempC: null },
    ],
    memory: { totalKB: 8 * 1024 * 1024, availableKB: 2 * 1024 * 1024 },
    disks: [{ path: "/data", usePct: 45 }],
    services: [
      { name: "vllm", ok: true },
      { name: "metrics", ok: true },
    ],
  },
  models: [{ name: "model-a", sizeBytes: 2 * 1024 * 1024 * 1024 }, { name: "model-b" }],
};

const unhealthyNode: FleetNode = {
  name: "spark-2",
  role: "inference",
  reachable: false,
  error: "connection refused",
  metrics: { services: [{ name: "vllm", ok: false }] },
};

const runningRecipe: FleetRecipe = {
  name: "smart-model",
  description: "고품질 추론",
  node: "spark-1",
  container: "vllm-smart",
  port: 8000,
  vllm: { gpuMemoryUtilization: 0.85, maxModelLen: 32768, maxNumSeqs: 8 },
  status: { running: true, weightsPresent: true, node: "spark-1" },
};

const stoppedRecipe: FleetRecipe = {
  name: "tiny-model",
  node: "spark-2",
  status: { running: false, weightsPresent: true },
};

const missingWeightsRecipe: FleetRecipe = {
  name: "missing-model",
  status: { running: false, weightsPresent: false },
};

const runningJob: FleetJob = {
  id: "job-running",
  title: "모델 기동",
  state: "running",
  log: "pulling\nstarting",
  startedAt: "2025-07-11T09:00:00+09:00",
};

const doneJob: FleetJob = {
  id: "job-done",
  title: "모델 중지",
  state: "done",
  log: "stopped",
  startedAt: "2025-07-10T09:00:00+09:00",
  endedAt: "2025-07-10T09:01:00+09:00",
};

describe("FleetMetric", () => {
  it.each(["ok", "warn", "bad", "neutral"] as const)("renders the %s tone", (tone) => {
    const { container } = render(<FleetMetric label="노드" value="3/4" hint="1 offline" tone={tone} />);
    expect(container.firstElementChild).toHaveClass("fleet-metric", tone);
    expect(screen.getByText("노드")).toBeInTheDocument();
    expect(screen.getByText("3/4").tagName).toBe("STRONG");
    expect(screen.getByText("1 offline").tagName).toBe("SMALL");
  });
});

describe("FleetOverview", () => {
  function renderOverview(overrides: Partial<React.ComponentProps<typeof FleetOverview>> = {}) {
    const props: React.ComponentProps<typeof FleetOverview> = {
      issues: [],
      runningRecipes: [],
      recentJobs: [],
      busyAction: "",
      expandedJob: "",
      onView: vi.fn(),
      onRecipeAction: vi.fn(),
      onJobToggle: vi.fn(),
      ...overrides,
    };
    render(<FleetOverview {...props} />);
    return props;
  }

  it("renders empty states for all three overview sections", () => {
    renderOverview();
    expect(screen.getByText("확인할 항목이 없습니다.")).toBeInTheDocument();
    expect(screen.getByText("실행 중인 레시피가 없습니다.")).toBeInTheDocument();
    expect(screen.getByText("작업이 없습니다.")).toBeInTheDocument();
  });

  it("shows section counts", () => {
    renderOverview({
      issues: [{ key: "issue", title: "노드 다운", detail: "spark-2", view: "nodes", tone: "bad" }],
      runningRecipes: [runningRecipe],
      recentJobs: [runningJob, doneJob],
    });
    expect(within(screen.getByRole("region", { name: "확인 필요" })).getByText("1")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "실행 중 레시피" })).getByText("1")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "최근 작업" })).getByText("2")).toBeInTheDocument();
  });

  it.each([
    ["bad", "위험"],
    ["warn", "주의"],
  ] as const)("routes a %s issue to its target view", async (tone, label) => {
    const issue: FleetIssue = { key: tone, title: "확인", detail: "상세", view: "services", tone };
    const props = renderOverview({ issues: [issue] });
    const row = screen.getByRole("button", { name: /확인/ });
    expect(within(row).getByText(label)).toHaveClass(tone);
    await userEvent.click(row);
    expect(props.onView).toHaveBeenCalledWith("services");
  });

  it("caps visible issues at eight", () => {
    const issues: FleetIssue[] = Array.from({ length: 12 }, (_, index) => ({
      key: `issue-${index}`,
      title: `Issue ${index}`,
      detail: "detail",
      view: "nodes",
      tone: "warn",
    }));
    renderOverview({ issues });
    expect(screen.getAllByRole("button", { name: /Issue/ })).toHaveLength(8);
    expect(screen.queryByText("Issue 8")).not.toBeInTheDocument();
  });

  it("caps running recipes at four", () => {
    const recipes = Array.from({ length: 6 }, (_, index) => ({ ...runningRecipe, name: `recipe-${index}` }));
    renderOverview({ runningRecipes: recipes });
    expect(screen.getAllByRole("button", { name: /재시작/ })).toHaveLength(4);
    expect(screen.queryByText("recipe-4")).not.toBeInTheDocument();
  });

  it("routes recipe actions with their recipe", async () => {
    const props = renderOverview({ runningRecipes: [runningRecipe] });
    await userEvent.click(screen.getByRole("button", { name: "smart-model 재시작" }));
    await userEvent.click(screen.getByRole("button", { name: "smart-model 중지" }));
    expect(props.onRecipeAction).toHaveBeenNthCalledWith(1, runningRecipe, "restart");
    expect(props.onRecipeAction).toHaveBeenNthCalledWith(2, runningRecipe, "stop");
  });

  it("routes job expansion by id", async () => {
    const props = renderOverview({ recentJobs: [runningJob] });
    await userEvent.click(screen.getByRole("button", { name: /모델 기동/ }));
    expect(props.onJobToggle).toHaveBeenCalledWith("job-running");
  });
});

describe("FleetNodesView", () => {
  function renderNodes(overrides: Partial<React.ComponentProps<typeof FleetNodesView>> = {}) {
    const props: React.ComponentProps<typeof FleetNodesView> = {
      nodes: [healthyNode, unhealthyNode],
      total: 4,
      query: "",
      problemsOnly: false,
      onQuery: vi.fn(),
      onProblemsOnly: vi.fn(),
      ...overrides,
    };
    const view = render(<FleetNodesView {...props} />);
    return { props, ...view };
  }

  it("renders node identity and online state", () => {
    renderNodes({ nodes: [healthyNode] });
    expect(screen.getByText("spark-1")).toBeInTheDocument();
    expect(screen.getByText("worker")).toBeInTheDocument();
    expect(screen.getByText("online")).toBeInTheDocument();
  });

  it("renders GPU, memory, disk, model, and service details", () => {
    const { container } = renderNodes({ nodes: [healthyNode] });
    expect(screen.getByText("GPU0 · 72% · 63°C")).toBeInTheDocument();
    expect(screen.getByText("GPU1")).toBeInTheDocument();
    expect(screen.getByText(/6.0 GB \/ 8.0 GB/)).toBeInTheDocument();
    expect(screen.getByText("45% · /data")).toBeInTheDocument();
    expect(container.querySelector('[aria-label="spark-1 모델"]')).toHaveTextContent("model-a2.0 GB");
    expect(container.querySelector('[aria-label="spark-1 서비스"]')).toHaveTextContent("vllmmetrics");
    expect(container.querySelector(".fleet-card")).not.toHaveClass("danger");
  });

  it("marks unreachable nodes and errors", () => {
    const { container } = renderNodes({ nodes: [unhealthyNode] });
    expect(screen.getByText("offline")).toBeInTheDocument();
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(screen.getByText("다운: vllm")).toBeInTheDocument();
    expect(container.querySelector(".fleet-card")).toHaveClass("danger");
    expect(container.querySelector(".fleet-dot")).toHaveClass("off");
  });

  it("reports filtered and total counts", () => {
    renderNodes({ nodes: [healthyNode], total: 9 });
    expect(screen.getByText("1 / 9")).toBeInTheDocument();
  });

  it("routes search input", async () => {
    const { props } = renderNodes();
    await userEvent.type(screen.getByPlaceholderText("노드, 역할, 모델"), "spark");
    expect(props.onQuery).toHaveBeenLastCalledWith("k");
  });

  it("routes the problems-only toggle", async () => {
    const { props } = renderNodes();
    await userEvent.click(screen.getByRole("checkbox", { name: "문제만" }));
    expect(props.onProblemsOnly).toHaveBeenCalledWith(true);
  });

  it("reflects controlled query and filter state", () => {
    renderNodes({ query: "spark-2", problemsOnly: true });
    expect(screen.getByPlaceholderText("노드, 역할, 모델")).toHaveValue("spark-2");
    expect(screen.getByRole("checkbox", { name: "문제만" })).toBeChecked();
  });

  it("renders a filtered empty state", () => {
    renderNodes({ nodes: [], total: 2 });
    expect(screen.getByText("조건에 맞는 노드가 없습니다.")).toBeInTheDocument();
  });
});

describe("FleetModelsView", () => {
  const models: FleetModelRow[] = [
    { key: "a", name: "Model A", sizeBytes: 1024 ** 3, nodeName: "spark-1", nodeRole: "worker", nodeReachable: true },
    { key: "b", name: "Model B", nodeName: "spark-2", nodeReachable: false },
  ];

  it("renders model size, placement, role, and offline state", () => {
    const { container } = render(<FleetModelsView models={models} total={4} query="" onQuery={() => {}} />);
    expect(screen.getByText("Model A")).toBeInTheDocument();
    expect(screen.getByText("1.0 GB")).toBeInTheDocument();
    expect(screen.getByText("worker")).toBeInTheDocument();
    expect(screen.getByText("offline")).toBeInTheDocument();
    expect(container.querySelectorAll(".fleet-card.danger")).toHaveLength(1);
    expect(screen.getByText("2 / 4")).toBeInTheDocument();
  });

  it("routes model search", async () => {
    const onQuery = vi.fn();
    render(<FleetModelsView models={models} total={2} query="" onQuery={onQuery} />);
    await userEvent.type(screen.getByPlaceholderText("모델, 노드, 역할"), "A");
    expect(onQuery).toHaveBeenCalledWith("A");
  });

  it("renders an empty state", () => {
    render(<FleetModelsView models={[]} total={0} query="x" onQuery={() => {}} />);
    expect(screen.getByText("조건에 맞는 모델이 없습니다.")).toBeInTheDocument();
  });
});

describe("FleetServicesView", () => {
  const services: FleetServiceRow[] = [
    { key: "a", name: "vllm", ok: true, nodeName: "spark-1", nodeRole: "worker", nodeReachable: true },
    { key: "b", name: "metrics", ok: true, nodeName: "spark-2", nodeReachable: false },
    { key: "c", name: "broker", ok: false, nodeName: "spark-3", nodeReachable: true },
  ];

  it("renders effective health from service and node state", () => {
    const { container } = render(<FleetServicesView services={services} total={5} filter="all" onFilter={() => {}} />);
    expect(screen.getAllByText("healthy")).toHaveLength(1);
    expect(screen.getAllByText("down")).toHaveLength(2);
    expect(screen.getByText("node offline")).toBeInTheDocument();
    expect(screen.getByText("worker")).toBeInTheDocument();
    expect(container.querySelectorAll(".fleet-card.danger")).toHaveLength(2);
    expect(screen.getByText("3 / 5")).toBeInTheDocument();
  });

  it.each([
    ["전체", "all"],
    ["정상", "healthy"],
    ["다운", "down"],
  ] as const)("routes %s filter", async (label, value) => {
    const onFilter = vi.fn();
    render(<FleetServicesView services={services} total={3} filter="all" onFilter={onFilter} />);
    await userEvent.click(screen.getByRole("button", { name: label }));
    expect(onFilter).toHaveBeenCalledWith(value);
  });

  it("marks the controlled filter active", () => {
    render(<FleetServicesView services={services} total={3} filter="down" onFilter={() => {}} />);
    expect(screen.getByRole("button", { name: "다운" })).toHaveClass("active");
  });

  it("renders an empty state", () => {
    render(<FleetServicesView services={[]} total={3} filter="down" onFilter={() => {}} />);
    expect(screen.getByText("조건에 맞는 서비스가 없습니다.")).toBeInTheDocument();
  });
});

describe("FleetRecipesView", () => {
  function renderRecipes(overrides: Partial<React.ComponentProps<typeof FleetRecipesView>> = {}) {
    const props: React.ComponentProps<typeof FleetRecipesView> = {
      recipes: [runningRecipe, stoppedRecipe, missingWeightsRecipe],
      total: 5,
      query: "",
      filter: "all",
      busyAction: "",
      onQuery: vi.fn(),
      onFilter: vi.fn(),
      onAction: vi.fn(),
      ...overrides,
    };
    const view = render(<FleetRecipesView {...props} />);
    return { props, ...view };
  }

  it("renders running, idle, and missing-weight states", () => {
    const { container } = renderRecipes();
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("idle")).toBeInTheDocument();
    expect(screen.getByText("no weights")).toBeInTheDocument();
    expect(container.querySelectorAll(".fleet-card.active")).toHaveLength(1);
    expect(container.querySelectorAll(".fleet-card.danger")).toHaveLength(1);
  });

  it("renders node, port, container, and vLLM tuning", () => {
    renderRecipes({ recipes: [runningRecipe] });
    expect(screen.getAllByText("spark-1").length).toBeGreaterThan(0);
    expect(screen.getByText(":8000")).toBeInTheDocument();
    expect(screen.getByText("vllm-smart")).toBeInTheDocument();
    expect(screen.getByText("GPU 0.85 · 32768 ctx · 8 seq")).toBeInTheDocument();
    expect(screen.getByText("고품질 추론", { selector: ".fleet-card-note" })).toBeInTheDocument();
  });

  it("routes launch, restart, and stop actions", async () => {
    const { props } = renderRecipes();
    await userEvent.click(screen.getByRole("button", { name: "tiny-model 기동" }));
    await userEvent.click(screen.getByRole("button", { name: "smart-model 재시작" }));
    await userEvent.click(screen.getByRole("button", { name: "smart-model 중지" }));
    expect(props.onAction).toHaveBeenNthCalledWith(1, stoppedRecipe, "launch");
    expect(props.onAction).toHaveBeenNthCalledWith(2, runningRecipe, "restart");
    expect(props.onAction).toHaveBeenNthCalledWith(3, runningRecipe, "stop");
  });

  it("locks every action while one fleet mutation is busy", () => {
    renderRecipes({ busyAction: "smart-model:restart" });
    for (const button of screen.getAllByRole("button", { name: /tiny-model 기동|smart-model (재시작|중지)/ })) {
      expect(button).toBeDisabled();
    }
  });

  it("routes query and status filters", async () => {
    const { props } = renderRecipes();
    await userEvent.type(screen.getByPlaceholderText("레시피, 노드, 컨테이너"), "smart");
    await userEvent.click(screen.getByRole("button", { name: "실행" }));
    expect(props.onQuery).toHaveBeenLastCalledWith("t");
    expect(props.onFilter).toHaveBeenCalledWith("running");
  });

  it("marks the controlled filter and count", () => {
    renderRecipes({ filter: "stopped" });
    expect(screen.getByRole("button", { name: "중지" })).toHaveClass("active");
    expect(screen.getByText("3 / 5")).toBeInTheDocument();
  });

  it("renders an empty state", () => {
    renderRecipes({ recipes: [], total: 3 });
    expect(screen.getByText("조건에 맞는 레시피가 없습니다.")).toBeInTheDocument();
  });
});

describe("FleetJobsView", () => {
  function renderJobs(overrides: Partial<React.ComponentProps<typeof FleetJobsView>> = {}) {
    const props: React.ComponentProps<typeof FleetJobsView> = {
      jobs: [runningJob, doneJob],
      total: 5,
      filter: "all",
      expandedJob: "",
      onFilter: vi.fn(),
      onToggle: vi.fn(),
      ...overrides,
    };
    render(<FleetJobsView {...props} />);
    return props;
  }

  it("renders job state labels, title, and count", () => {
    renderJobs();
    expect(screen.getByText("진행", { selector: ".fleet-state" })).toBeInTheDocument();
    expect(screen.getByText("완료", { selector: ".fleet-state" })).toBeInTheDocument();
    expect(screen.getByText("모델 기동")).toBeInTheDocument();
    expect(screen.getByText("2 / 5")).toBeInTheDocument();
  });

  it("shows logs only for the expanded job", () => {
    renderJobs({ expandedJob: "job-running" });
    expect(document.querySelector("pre.fleet-log")?.textContent).toBe("pulling\nstarting");
    expect(screen.queryByText("stopped")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /모델 기동/ })).toHaveAttribute("aria-expanded", "true");
  });

  it("shows a fallback for an expanded job without logs", () => {
    renderJobs({ jobs: [{ id: "empty", state: "failed" }], expandedJob: "empty" });
    expect(screen.getByText("로그 없음")).toBeInTheDocument();
    expect(screen.getByText("empty", { selector: ".fleet-job-title" })).toBeInTheDocument();
  });

  it("routes job toggles", async () => {
    const props = renderJobs();
    await userEvent.click(screen.getByRole("button", { name: /모델 중지/ }));
    expect(props.onToggle).toHaveBeenCalledWith("job-done");
  });

  it.each([
    ["전체", "all"],
    ["진행", "running"],
    ["완료", "done"],
    ["실패", "failed"],
  ] as const)("routes %s filter", async (_label, value) => {
    const props = renderJobs();
    await userEvent.click(
      screen
        .getByRole("group", { name: "작업 상태" })
        .querySelector(`button:nth-child(${["all", "running", "done", "failed"].indexOf(value) + 1})`)!,
    );
    expect(props.onFilter).toHaveBeenCalledWith(value);
  });

  it("renders an empty state", () => {
    renderJobs({ jobs: [], total: 5 });
    expect(screen.getByText("조건에 맞는 작업이 없습니다.")).toBeInTheDocument();
  });
});

describe("ConfirmAction", () => {
  it.each([
    ["launch", "기동"],
    ["restart", "재시작"],
    ["stop", "중지"],
  ] as const)("renders and confirms %s", async (action, label) => {
    const onConfirm = vi.fn();
    render(<ConfirmAction recipe={runningRecipe} action={action} onClose={() => {}} onConfirm={onConfirm} />);
    expect(screen.getByRole("dialog", { name: `smart-model ${label}` })).toBeInTheDocument();
    expect(screen.getByText("spark-1")).toBeInTheDocument();
    expect(screen.getByText("실행 중")).toBeInTheDocument();
    expect(screen.getByText("고품질 추론")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: label }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("shows fallbacks for an unassigned stopped recipe", () => {
    render(<ConfirmAction recipe={{ name: "empty" }} action="launch" onClose={() => {}} onConfirm={() => {}} />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.getByText("중지")).toBeInTheDocument();
  });

  it("routes cancel", async () => {
    const onClose = vi.fn();
    render(<ConfirmAction recipe={runningRecipe} action="stop" onClose={onClose} onConfirm={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "취소" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
