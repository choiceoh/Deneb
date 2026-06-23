import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Markdown } from "./Markdown";

describe("Markdown", () => {
  it("renders bold, italic, and inline code as semantic tags", () => {
    render(<Markdown text={"**굵게** _기울임_ `코드`"} />);
    expect(screen.getByText("굵게").tagName).toBe("STRONG");
    expect(screen.getByText("기울임").tagName).toBe("EM");
    expect(screen.getByText("코드").tagName).toBe("CODE");
  });

  it("renders an http link but drops a javascript: scheme to plain text", () => {
    render(<Markdown text={"[안전](https://example.com) [위험](javascript:alert(1))"} />);
    const link = screen.getByRole("link", { name: "안전" });
    expect(link).toHaveAttribute("href", "https://example.com");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(screen.queryByRole("link", { name: /위험/ })).toBeNull();
    expect(screen.getByText(/위험/)).toBeInTheDocument();
  });

  it("renders an unordered list", () => {
    render(<Markdown text={"- 하나\n- 둘"} />);
    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("하나");
  });

  it("renders a fenced code block verbatim with a copy affordance", () => {
    render(<Markdown text={"```js\nconst a = 1;\n```"} />);
    expect(screen.getByText("const a = 1;").tagName).toBe("CODE");
    expect(screen.getByRole("button", { name: "코드 복사" })).toBeInTheDocument();
  });

  it("renders a GFM table", () => {
    render(<Markdown text={"| 이름 | 값 |\n| --- | --- |\n| A | 1 |"} />);
    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "이름" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "A" })).toBeInTheDocument();
  });

  it("renders plain text as a single paragraph", () => {
    render(<Markdown text="그냥 텍스트" />);
    expect(screen.getByText("그냥 텍스트").tagName).toBe("P");
  });

  it("renders strikethrough as <del>", () => {
    render(<Markdown text={"~~취소~~"} />);
    expect(screen.getByText("취소").tagName).toBe("DEL");
  });

  it("renders a task list with checkbox state", () => {
    render(<Markdown text={"- [ ] 할 일\n- [x] 완료"} />);
    const boxes = screen.getAllByRole("checkbox");
    expect(boxes).toHaveLength(2);
    expect(boxes[0]).not.toBeChecked();
    expect(boxes[1]).toBeChecked();
    expect(screen.getByText("완료")).toBeInTheDocument();
  });

  it("nests a sub-list under its parent item", () => {
    render(<Markdown text={"- a\n  - b\n- c"} />);
    expect(screen.getAllByRole("list")).toHaveLength(2); // outer + nested
    expect(screen.getAllByRole("listitem")).toHaveLength(3); // a, b, c
  });

  it("respects an ordered list's start number", () => {
    render(<Markdown text={"3. 셋\n4. 넷"} />);
    expect(screen.getByRole("list")).toHaveAttribute("start", "3");
  });

  it("renders a safe image but drops a dangerous one to its alt text", () => {
    const { rerender } = render(<Markdown text={"![고양이](https://img.example/cat.png)"} />);
    const img = screen.getByRole("img", { name: "고양이" });
    expect(img).toHaveAttribute("src", "https://img.example/cat.png");

    rerender(<Markdown text={"![나쁨](javascript:alert(1))"} />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText(/나쁨/)).toBeInTheDocument();
  });

  it("autolinks bare and angle-bracket URLs, trimming trailing punctuation", () => {
    const { rerender } = render(<Markdown text={"방문: https://example.com."} />);
    expect(screen.getByRole("link", { name: "https://example.com" })).toHaveAttribute("href", "https://example.com");

    rerender(<Markdown text={"<https://deneb.example>"} />);
    expect(screen.getByRole("link", { name: "https://deneb.example" })).toBeInTheDocument();
  });

  it("treats a backslash-escaped marker as a literal, not emphasis", () => {
    const { container } = render(<Markdown text={"\\*리터럴\\*"} />);
    expect(container.querySelector("em, strong")).toBeNull();
    expect(screen.getByText("*리터럴*")).toBeInTheDocument();
  });

  it("renders a hard line break (two trailing spaces) as <br>", () => {
    const { container } = render(<Markdown text={"첫 줄  \n둘째 줄"} />);
    expect(container.querySelector("br")).not.toBeNull();
  });

  it("applies GFM table column alignment", () => {
    render(<Markdown text={"| L | C | R |\n|:--|:-:|--:|\n| a | b | c |"} />);
    expect(screen.getByRole("columnheader", { name: "C" })).toHaveStyle({ textAlign: "center" });
    expect(screen.getByRole("columnheader", { name: "R" })).toHaveStyle({ textAlign: "right" });
  });

  it("renders block content inside a blockquote", () => {
    const { container } = render(<Markdown text={"> - a\n> - b"} />);
    const quote = container.querySelector("blockquote");
    expect(quote).not.toBeNull();
    expect(quote?.querySelectorAll("li")).toHaveLength(2);
  });

  it("shows the code fence language label", () => {
    render(<Markdown text={"```python\nx = 1\n```"} />);
    expect(screen.getByText("python")).toBeInTheDocument();
    expect(screen.getByText("x = 1").tagName).toBe("CODE");
  });
});
