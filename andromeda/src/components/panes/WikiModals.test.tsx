import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { MovePageModal, NewPageModal, UnsavedWikiModal } from "./WikiModals";

describe("MovePageModal", () => {
  function renderMove(overrides: Partial<React.ComponentProps<typeof MovePageModal>> = {}) {
    const props: React.ComponentProps<typeof MovePageModal> = {
      path: "projects/deneb.md",
      categories: [{ name: "archive" }, { category: "people" }, { name: "projects" }, { name: "archive" }, {}],
      onClose: vi.fn(),
      onSubmit: vi.fn(),
      ...overrides,
    };
    render(<MovePageModal {...props} />);
    return props;
  }

  it("shows the current path and keeps the move disabled", () => {
    renderMove();
    expect(screen.getByRole("dialog", { name: "페이지 이동" })).toBeInTheDocument();
    expect(screen.getByText("→ projects/deneb.md")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "이동" })).toBeDisabled();
  });

  it("offers root, existing categories, the source category, and new category", () => {
    renderMove();
    expect(screen.getByRole("button", { name: "최상위" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "archive" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "people" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "projects" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "+ 새 분류" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "archive" })).toHaveLength(1);
  });

  it("uses category as a fallback name and excludes root placeholders", () => {
    renderMove({ categories: [{ category: "clients" }, { name: "(root)" }] });
    expect(screen.getByRole("button", { name: "clients" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "최상위" })).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "(root)" })).not.toBeInTheDocument();
  });

  it("moves a page to the selected existing category", async () => {
    const props = renderMove();
    await userEvent.click(screen.getByRole("button", { name: "archive" }));
    expect(screen.getByText("→ archive/deneb.md")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "이동" })).toBeEnabled();

    await userEvent.click(screen.getByRole("button", { name: "이동" }));
    expect(props.onSubmit).toHaveBeenCalledWith("archive/deneb.md");
  });

  it("moves a nested page to root while preserving its filename", async () => {
    const props = renderMove({ path: "projects/design/decision.md" });
    await userEvent.click(screen.getByRole("button", { name: "최상위" }));
    expect(screen.getByText("→ decision.md")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "이동" }));
    expect(props.onSubmit).toHaveBeenCalledWith("decision.md");
  });

  it("keeps root selected and disabled for an already-root page", () => {
    renderMove({ path: "deneb.md", categories: [] });
    expect(screen.getByText("→ deneb.md")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "이동" })).toBeDisabled();
  });

  it("reveals and focuses the new-category field", async () => {
    renderMove();
    await userEvent.click(screen.getByRole("button", { name: "+ 새 분류" }));
    expect(screen.getByPlaceholderText("새 분류 이름")).toHaveFocus();
    expect(screen.getByRole("button", { name: "이동" })).toBeDisabled();
  });

  it.each(["", " ", "\n\t"])("rejects blank new category %j", async (value) => {
    renderMove();
    await userEvent.click(screen.getByRole("button", { name: "+ 새 분류" }));
    if (value) await userEvent.type(screen.getByPlaceholderText("새 분류 이름"), value);
    expect(screen.getByRole("button", { name: "이동" })).toBeDisabled();
  });

  it("normalizes slashes and whitespace in a new category", async () => {
    const props = renderMove();
    await userEvent.click(screen.getByRole("button", { name: "+ 새 분류" }));
    await userEvent.type(screen.getByPlaceholderText("새 분류 이름"), "  /new/category/  ");
    expect(screen.getByText("→ new/category/deneb.md")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "이동" }));
    expect(props.onSubmit).toHaveBeenCalledWith("new/category/deneb.md");
  });

  it("abandons an unfinished new category when an existing one is selected", async () => {
    renderMove();
    await userEvent.click(screen.getByRole("button", { name: "+ 새 분류" }));
    await userEvent.type(screen.getByPlaceholderText("새 분류 이름"), "temporary");
    await userEvent.click(screen.getByRole("button", { name: "people" }));
    expect(screen.queryByPlaceholderText("새 분류 이름")).not.toBeInTheDocument();
    expect(screen.getByText("→ people/deneb.md")).toBeInTheDocument();
  });

  it("routes cancel without moving", async () => {
    const props = renderMove();
    await userEvent.click(screen.getByRole("button", { name: "취소" }));
    expect(props.onClose).toHaveBeenCalledTimes(1);
    expect(props.onSubmit).not.toHaveBeenCalled();
  });
});

describe("NewPageModal", () => {
  function renderNew() {
    const props = { onClose: vi.fn(), onCreate: vi.fn() };
    render(<NewPageModal {...props} />);
    return props;
  }

  it("starts with empty labeled fields and disabled creation", () => {
    renderNew();
    expect(screen.getByRole("dialog", { name: "새 위키 페이지" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "제목" })).toHaveFocus();
    expect(screen.getByRole("textbox", { name: "분류" })).toHaveValue("");
    expect(screen.getByRole("textbox", { name: "요약" })).toHaveValue("");
    expect(screen.getByRole("textbox", { name: "본문" })).toHaveValue("");
    expect(screen.getByRole("button", { name: "생성" })).toBeDisabled();
  });

  it("requires both title and category", async () => {
    renderNew();
    await userEvent.type(screen.getByRole("textbox", { name: "제목" }), "설계 노트");
    expect(screen.getByRole("button", { name: "생성" })).toBeDisabled();
    await userEvent.type(screen.getByRole("textbox", { name: "분류" }), "projects");
    expect(screen.getByRole("button", { name: "생성" })).toBeEnabled();
  });

  it("treats whitespace-only required fields as missing", async () => {
    renderNew();
    await userEvent.type(screen.getByRole("textbox", { name: "제목" }), "   ");
    await userEvent.type(screen.getByRole("textbox", { name: "분류" }), "\t ");
    expect(screen.getByRole("button", { name: "생성" })).toBeDisabled();
  });

  it("submits the complete exact draft", async () => {
    const props = renderNew();
    await userEvent.type(screen.getByRole("textbox", { name: "제목" }), "설계 노트");
    await userEvent.type(screen.getByRole("textbox", { name: "분류" }), "projects");
    await userEvent.type(screen.getByRole("textbox", { name: "요약" }), "결정 기록");
    await userEvent.type(screen.getByRole("textbox", { name: "본문" }), "# 본문\n\n내용");

    await userEvent.click(screen.getByRole("button", { name: "생성" }));

    expect(props.onCreate).toHaveBeenCalledWith({
      title: "설계 노트",
      category: "projects",
      summary: "결정 기록",
      body: "# 본문\n\n내용",
    });
  });

  it("routes cancel without creating", async () => {
    const props = renderNew();
    await userEvent.click(screen.getByRole("button", { name: "취소" }));
    expect(props.onClose).toHaveBeenCalledTimes(1);
    expect(props.onCreate).not.toHaveBeenCalled();
  });
});

describe("UnsavedWikiModal", () => {
  function renderUnsaved() {
    const props = {
      path: "projects/current.md",
      targetPath: "projects/next.md",
      onClose: vi.fn(),
      onDiscard: vi.fn(),
      onSave: vi.fn(),
    };
    render(<UnsavedWikiModal {...props} />);
    return props;
  }

  it("explains both the dirty source and requested target", () => {
    renderUnsaved();
    expect(screen.getByRole("dialog", { name: "저장하지 않은 변경" })).toBeInTheDocument();
    expect(screen.getByText(/projects\/current.md에 저장하지 않은 변경/)).toBeInTheDocument();
    expect(screen.getByText(/projects\/next.md을 열기 전에 처리하세요/)).toBeInTheDocument();
  });

  it("keeps editing", async () => {
    const props = renderUnsaved();
    await userEvent.click(screen.getByRole("button", { name: "계속 편집" }));
    expect(props.onClose).toHaveBeenCalledTimes(1);
    expect(props.onDiscard).not.toHaveBeenCalled();
    expect(props.onSave).not.toHaveBeenCalled();
  });

  it("discards and opens", async () => {
    const props = renderUnsaved();
    await userEvent.click(screen.getByRole("button", { name: "버리고 열기" }));
    expect(props.onDiscard).toHaveBeenCalledTimes(1);
  });

  it("saves and opens", async () => {
    const props = renderUnsaved();
    await userEvent.click(screen.getByRole("button", { name: "저장 후 열기" }));
    expect(props.onSave).toHaveBeenCalledTimes(1);
  });
});
