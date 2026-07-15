import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { Grid, GridNotice, RowBtn, type Column } from "./Grid";
import { renderWithProviders } from "@/test/util";

interface Row {
  id: string;
  name: string;
  score: number;
}

const rows: Row[] = [
  { id: "alpha", name: "Alpha", score: 7 },
  { id: "beta", name: "Beta", score: 11 },
];

const columns: Column<Row>[] = [
  { header: "Name", width: 180, cell: (row) => row.name },
  { header: "Score", cell: (row) => row.score, tdStyle: { textAlign: "right" } },
];

describe("Grid", () => {
  it("renders declarative headers and cells in row order", () => {
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} />);

    expect(screen.getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Name", "Score"]);
    expect(screen.getAllByRole("row")).toHaveLength(3);
    expect(screen.getAllByRole("cell").map((cell) => cell.textContent)).toEqual(["Alpha", "7", "Beta", "11"]);
  });

  it("when applies fixed column widths to visible headers", () => {
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} />);

    const [name, score] = screen.getAllByRole("columnheader");
    expect(name).toHaveStyle({ width: "180px" });
    expect(score).not.toHaveAttribute("style");
  });

  it("uses a colgroup to preserve widths when the header is hidden", () => {
    const { container } = render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} hideHeader />);

    expect(screen.queryByRole("columnheader")).not.toBeInTheDocument();
    const cols = container.querySelectorAll("colgroup col");
    expect(cols).toHaveLength(2);
    expect(cols[0]).toHaveStyle({ width: "180px" });
    expect(cols[1]).not.toHaveAttribute("style");
  });

  it("when passes each row to cell, style, title, and key projections", () => {
    const rowStyle = vi.fn((row: Row) => ({ opacity: row.score > 10 ? 1 : 0.5 }));
    const rowTitle = vi.fn((row: Row) => `Open ${row.id}`);
    render(
      <Grid columns={columns} rows={rows} getKey={(row) => `key-${row.id}`} rowStyle={rowStyle} rowTitle={rowTitle} />,
    );

    const alpha = screen.getByText("Alpha").closest("tr");
    const beta = screen.getByText("Beta").closest("tr");
    expect(alpha).toHaveStyle({ opacity: "0.5" });
    expect(beta).toHaveStyle({ opacity: "1" });
    expect(alpha).toHaveAttribute("title", "Open alpha");
    expect(beta).toHaveAttribute("title", "Open beta");
    expect(rowStyle).toHaveBeenCalledTimes(2);
    expect(rowTitle).toHaveBeenCalledTimes(2);
  });

  it("preserves noninteractive rows outside the tab order", () => {
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} />);

    for (const row of screen.getAllByRole("row").slice(1)) {
      expect(row).not.toHaveAttribute("tabindex");
      expect(row).not.toHaveAttribute("aria-selected");
      expect(row).not.toHaveClass("clickable");
    }
  });

  it("when invokes the row action on click", async () => {
    const onRowClick = vi.fn();
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} onRowClick={onRowClick} />);

    await userEvent.click(screen.getByText("Beta"));

    expect(onRowClick).toHaveBeenCalledTimes(1);
    expect(onRowClick).toHaveBeenCalledWith(rows[1]);
  });

  it.each(["{Enter}", " "])("invokes the row action with %s", async (key) => {
    const onRowClick = vi.fn();
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} onRowClick={onRowClick} />);
    const row = screen.getByText("Alpha").closest("tr")!;
    row.focus();

    await userEvent.keyboard(key);

    expect(onRowClick).toHaveBeenCalledTimes(1);
    expect(onRowClick).toHaveBeenCalledWith(rows[0]);
  });

  it("ignores unrelated keyboard input", async () => {
    const onRowClick = vi.fn();
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} onRowClick={onRowClick} />);
    screen.getByText("Alpha").closest("tr")!.focus();

    await userEvent.keyboard("{ArrowDown}a{Escape}");

    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("when marks the selected interactive row and exposes its expanded content", () => {
    render(
      <Grid
        columns={columns}
        rows={rows}
        getKey={(row) => row.id}
        onRowClick={() => {}}
        isRowSelected={(row) => row.id === "beta"}
        renderExpandedRow={(row) => <aside>Details for {row.name}</aside>}
      />,
    );

    const alpha = screen.getByText("Alpha").closest("tr")!;
    const beta = screen.getByText("Beta").closest("tr")!;
    expect(alpha).toHaveClass("clickable");
    expect(alpha).not.toHaveClass("selected");
    expect(alpha).toHaveAttribute("aria-selected", "false");
    expect(beta).toHaveClass("clickable", "selected");
    expect(beta).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Details for Beta").closest("td")).toHaveAttribute("colspan", "2");
    expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument();
  });

  it("does not emit an empty expanded row", () => {
    const { container } = render(
      <Grid
        columns={columns}
        rows={rows}
        getKey={(row) => row.id}
        onRowClick={() => {}}
        isRowSelected={(row) => row.id === "beta"}
        renderExpandedRow={() => null}
      />,
    );

    expect(container.querySelector(".dgrid-expanded-row")).toBeNull();
  });

  it("when passes maxWidth through to the table", () => {
    render(<Grid columns={columns} rows={rows} getKey={(row) => row.id} maxWidth={640} />);

    expect(screen.getByRole("table")).toHaveStyle({ maxWidth: "640px" });
  });
});

describe("GridNotice", () => {
  it("returns a disconnected workspace before query state", () => {
    renderWithProviders(
      <GridNotice query={{ isLoading: true, isError: true, error: new Error("boom") }} count={3} empty="비었음">
        <span>grid</span>
      </GridNotice>,
      { connected: false },
    );

    expect(screen.getByText("미연결")).toBeInTheDocument();
    expect(screen.queryByText(/실패/)).not.toBeInTheDocument();
  });

  it("reports an error before loading or empty state", () => {
    renderWithProviders(
      <GridNotice
        query={{ isLoading: true, isError: true, error: new Error("RPC unavailable") }}
        count={0}
        empty="비었음"
      >
        <span>grid</span>
      </GridNotice>,
      { connected: true },
    );

    expect(screen.getByText("불러오기 실패: RPC unavailable")).toBeInTheDocument();
    expect(screen.queryByText("불러오는 중…")).not.toBeInTheDocument();
  });

  it("reports loading before empty state", () => {
    renderWithProviders(
      <GridNotice query={{ isLoading: true }} count={0} empty="비었음">
        <span>grid</span>
      </GridNotice>,
      { connected: true },
    );

    expect(screen.getByText("불러오는 중…")).toBeInTheDocument();
    expect(screen.queryByText("비었음")).not.toBeInTheDocument();
  });

  it("uses the caller's empty-state copy", () => {
    renderWithProviders(
      <GridNotice query={{ isLoading: false }} count={0} empty="오늘 항목이 없습니다">
        <span>grid</span>
      </GridNotice>,
      { connected: true },
    );

    expect(screen.getByText("오늘 항목이 없습니다")).toBeInTheDocument();
  });

  it("renders its children only when rows can be shown", () => {
    renderWithProviders(
      <GridNotice query={{ isLoading: false }} count={2} empty="비었음">
        <table aria-label="result grid" />
      </GridNotice>,
      { connected: true },
    );

    expect(screen.getByRole("table", { name: "result grid" })).toBeInTheDocument();
    expect(screen.queryByText("비었음")).not.toBeInTheDocument();
  });
});

describe("RowBtn", () => {
  it("stops the row click while running its own action", async () => {
    const onRow = vi.fn();
    const onAction = vi.fn();
    render(
      <div onClick={onRow}>
        <RowBtn onClick={onAction}>Archive</RowBtn>
      </div>,
    );

    await userEvent.click(screen.getByRole("button", { name: "Archive" }));

    expect(onAction).toHaveBeenCalledTimes(1);
    expect(onRow).not.toHaveBeenCalled();
  });

  it("when forwards disabled and title attributes", () => {
    render(
      <RowBtn onClick={() => {}} disabled title="권한 없음">
        Delete
      </RowBtn>,
    );

    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete" })).toHaveAttribute("title", "권한 없음");
  });

  it("when uses the danger color only for destructive actions", () => {
    const { rerender } = render(
      <RowBtn onClick={() => {}} danger>
        Delete
      </RowBtn>,
    );
    expect(screen.getByRole("button")).toHaveStyle({ color: "var(--due)" });

    rerender(<RowBtn onClick={() => {}}>Archive</RowBtn>);
    expect(screen.getByRole("button")).not.toHaveStyle({ color: "var(--due)" });
  });
});
