import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Modal } from "./Modal";

describe("Modal", () => {
  it("names the dialog via its title (aria-labelledby)", () => {
    render(
      <Modal title="제목입니다" onClose={() => {}}>
        본문
      </Modal>,
    );
    const dialog = screen.getByRole("dialog");
    const labelId = dialog.getAttribute("aria-labelledby");
    expect(labelId).toBeTruthy();
    expect(document.getElementById(labelId as string)?.textContent).toBe("제목입니다");
  });

  it("closes on Escape", async () => {
    const onClose = vi.fn();
    render(
      <Modal title="t" onClose={onClose}>
        <button>안쪽</button>
      </Modal>,
    );
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("returns focus to the trigger when it unmounts", () => {
    const trigger = document.createElement("button");
    document.body.appendChild(trigger);
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    const { unmount } = render(
      <Modal title="t" onClose={() => {}}>
        본문
      </Modal>,
    );
    unmount();

    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });
});
