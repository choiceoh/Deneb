import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DeleteModal, OneFieldModal } from "./commonModals";

describe("OneFieldModal", () => {
  function renderModal(overrides: Partial<React.ComponentProps<typeof OneFieldModal>> = {}) {
    const props: React.ComponentProps<typeof OneFieldModal> = {
      title: "파일 이동",
      label: "새 경로",
      action: "이동",
      onClose: vi.fn(),
      onSubmit: vi.fn(),
      ...overrides,
    };
    render(<OneFieldModal {...props} />);
    return props;
  }

  it("labels the dialog and its single field", () => {
    renderModal();
    expect(screen.getByRole("dialog", { name: "파일 이동" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "새 경로" })).toBeInTheDocument();
  });

  it("focuses the field for immediate typing", () => {
    renderModal();
    expect(screen.getByRole("textbox", { name: "새 경로" })).toHaveFocus();
  });

  it("starts from the caller's initial value", () => {
    renderModal({ initialValue: "projects/old.md" });
    expect(screen.getByRole("textbox")).toHaveValue("projects/old.md");
  });

  it.each(["", " ", "\n\t"])("disables the action for blank value %j", (initialValue) => {
    renderModal({ initialValue });
    expect(screen.getByRole("button", { name: "이동" })).toBeDisabled();
  });

  it("enables the action after meaningful input", async () => {
    renderModal();
    await userEvent.type(screen.getByRole("textbox"), "projects/new.md");
    expect(screen.getByRole("button", { name: "이동" })).toBeEnabled();
  });

  it("submits the exact field value", async () => {
    const props = renderModal({ initialValue: "  projects/new.md  " });
    await userEvent.click(screen.getByRole("button", { name: "이동" }));
    expect(props.onSubmit).toHaveBeenCalledWith("  projects/new.md  ");
  });

  it("updates the submitted value after edits", async () => {
    const props = renderModal({ initialValue: "old" });
    const input = screen.getByRole("textbox");
    await userEvent.clear(input);
    await userEvent.type(input, "new/path");
    await userEvent.click(screen.getByRole("button", { name: "이동" }));
    expect(props.onSubmit).toHaveBeenCalledWith("new/path");
  });

  it("routes cancel without submitting", async () => {
    const props = renderModal({ initialValue: "path" });
    await userEvent.click(screen.getByRole("button", { name: "취소" }));
    expect(props.onClose).toHaveBeenCalledTimes(1);
    expect(props.onSubmit).not.toHaveBeenCalled();
  });

  it("uses the requested dialog width", () => {
    renderModal({ width: 640 });
    expect(screen.getByRole("dialog")).toHaveStyle({ width: "640px" });
  });

  it.each(["이동", "병합", "생성", "이름 변경"])("uses caller action copy %s", (action) => {
    renderModal({ action, initialValue: "value" });
    expect(screen.getByRole("button", { name: action })).toBeEnabled();
  });
});

describe("DeleteModal", () => {
  function renderDelete(overrides: Partial<React.ComponentProps<typeof DeleteModal>> = {}) {
    const props: React.ComponentProps<typeof DeleteModal> = {
      title: "파일 삭제",
      path: "projects/obsolete.md",
      onClose: vi.fn(),
      onDelete: vi.fn(),
      ...overrides,
    };
    render(<DeleteModal {...props} />);
    return props;
  }

  it("names the dialog and shows the exact path", () => {
    renderDelete();
    expect(screen.getByRole("dialog", { name: "파일 삭제" })).toBeInTheDocument();
    expect(screen.getByText("projects/obsolete.md")).toBeInTheDocument();
  });

  it("uses compact confirmation width", () => {
    renderDelete();
    expect(screen.getByRole("dialog")).toHaveStyle({ width: "420px" });
  });

  it("confirms deletion", async () => {
    const props = renderDelete();
    await userEvent.click(screen.getByRole("button", { name: "삭제" }));
    expect(props.onDelete).toHaveBeenCalledTimes(1);
    expect(props.onClose).not.toHaveBeenCalled();
  });

  it("cancels without deleting", async () => {
    const props = renderDelete();
    await userEvent.click(screen.getByRole("button", { name: "취소" }));
    expect(props.onClose).toHaveBeenCalledTimes(1);
    expect(props.onDelete).not.toHaveBeenCalled();
  });

  it("closes on Escape", async () => {
    const props = renderDelete();
    await userEvent.keyboard("{Escape}");
    expect(props.onClose).toHaveBeenCalledTimes(1);
  });
});
