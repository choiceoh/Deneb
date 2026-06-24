import { afterEach, describe, expect, it } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@refinedev/core";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { TodoPane } from "./TodoPane";

afterEach(() => {
  localStorage.clear();
});

function capturing(fixtures: Record<string, unknown[]>, sink: Record<string, unknown>[]): DataProvider {
  const base = fakeProvider(fixtures);
  return {
    ...base,
    create: async (a) => {
      sink.push({ ...a });
      return base.create(a);
    },
    update: async (a) => {
      sink.push({ ...a });
      return base.update(a);
    },
  };
}

function expectRpcDateTime(value: unknown) {
  expect(typeof value).toBe("string");
  expect(String(value)).toContain("T");
  expect(Number.isNaN(Date.parse(String(value)))).toBe(false);
}

describe("TodoPane", () => {
  it("renders a todo's note under its title", async () => {
    const dataProvider = fakeProvider({ todo: [{ id: "t1", title: "보고서", note: "초안 먼저", done: false }] });
    renderWithProviders(<TodoPane />, { connected: true, dataProvider });
    expect(await screen.findByText("초안 먼저")).toBeInTheDocument();
  });

  it("edits due date and note through the modal", async () => {
    const calls: Record<string, unknown>[] = [];
    const dataProvider = capturing({ todo: [{ id: "t1", title: "보고서", done: false }] }, calls);
    renderWithProviders(<TodoPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByRole("button", { name: "수정" }));
    fireEvent.change(screen.getByLabelText("마감"), { target: { value: "2026-07-15" } });
    await userEvent.type(screen.getByLabelText("메모"), "초안 먼저");
    await userEvent.click(screen.getByRole("button", { name: "저장" }));

    expect(calls).toHaveLength(1);
    expect(calls[0].resource).toBe("todo");
    expect(calls[0].id).toBe("t1");
    const v = calls[0].variables as Record<string, unknown>;
    expect(v).toMatchObject({ title: "보고서", note: "초안 먼저", dueAllDay: true });
    expectRpcDateTime(v.due);
  });

  it("adds a todo through the + 새 할일 modal", async () => {
    const calls: Record<string, unknown>[] = [];
    const dataProvider = capturing({ todo: [] }, calls);
    renderWithProviders(<TodoPane />, { connected: true, dataProvider });

    // No inline add row — the modal only appears after pressing the + button.
    expect(screen.queryByLabelText("제목")).not.toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: /새 할일/ }));
    await userEvent.type(screen.getByLabelText("제목"), "새 보고서");
    fireEvent.change(screen.getByLabelText("마감"), { target: { value: "2026-08-01" } });
    await userEvent.click(screen.getByRole("button", { name: "저장" }));

    expect(calls).toHaveLength(1);
    expect(calls[0].resource).toBe("todo");
    const v = calls[0].variables as Record<string, unknown>;
    expect(v).toMatchObject({ title: "새 보고서", dueAllDay: true });
    expectRpcDateTime(v.due);
  });
});
