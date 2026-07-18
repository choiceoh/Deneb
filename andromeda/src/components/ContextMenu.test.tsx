import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useContextMenu, type MenuItem } from "./ContextMenu";

function Harness({ items }: { items: MenuItem[] }) {
  const { openMenu, menuElement } = useContextMenu();
  return (
    <div>
      <button onContextMenu={(e) => openMenu(e, items)}>row</button>
      {menuElement}
    </div>
  );
}

describe("ContextMenu", () => {
  it("opens on right-click and fires the action on mousedown", () => {
    const onSelect = vi.fn();
    render(<Harness items={[{ label: "보관", onSelect }]} />);
    fireEvent.contextMenu(screen.getByText("row"));

    const item = screen.getByRole("menuitem", { name: "보관" });
    fireEvent.mouseDown(item, { button: 0 });

    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  it("closes on Escape and skips disabled items", () => {
    const onSelect = vi.fn();
    render(<Harness items={[{ label: "삭제", danger: true, disabled: true, onSelect }]} />);
    fireEvent.contextMenu(screen.getByText("row"));

    const item = screen.getByRole("menuitem", { name: "삭제" });
    expect(item).toBeDisabled();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("does not open for an empty action list", () => {
    render(<Harness items={[]} />);
    fireEvent.contextMenu(screen.getByText("row"));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });
});
