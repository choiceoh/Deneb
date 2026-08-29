import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CodeRepo } from "@/gateway";
import { RepoPicker } from "./RepoPicker";

const repos: CodeRepo[] = [
  { id: "api-1", name: "개발", path: "/home/me/dev" },
  { id: "web-2", name: "웹", path: "/home/me/web" },
];

afterEach(cleanup);

describe("RepoPicker", () => {
  it("offers the default workspace as the way to unbind", async () => {
    const onSelect = vi.fn();
    const user = userEvent.setup();
    render(<RepoPicker repos={repos} value="api-1" onSelect={onSelect} onRegister={async () => {}} />);

    // Picking the empty option is how a conversation returns to the server-wide
    // workspace — it must be reachable, not just an initial state.
    await user.selectOptions(screen.getByLabelText("작업할 저장소"), "");
    expect(onSelect).toHaveBeenCalledWith("");
  });

  it("selects a registered repository", async () => {
    const onSelect = vi.fn();
    const user = userEvent.setup();
    render(<RepoPicker repos={repos} value="" onSelect={onSelect} onRegister={async () => {}} />);

    await user.selectOptions(screen.getByLabelText("작업할 저장소"), "web-2");
    expect(onSelect).toHaveBeenCalledWith("web-2");
  });

  it("registers through an inline field, not a prompt", async () => {
    // window.prompt is a no-op inside the Tauri shell, so registration has to be
    // an in-page input or the button would silently do nothing on the desktop.
    const onRegister = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<RepoPicker repos={repos} value="" onSelect={() => {}} onRegister={onRegister} />);

    await user.selectOptions(screen.getByLabelText("작업할 저장소"), "__add__");
    await user.type(screen.getByLabelText("등록할 저장소 경로"), "/home/me/new");
    await user.click(screen.getByRole("button", { name: "등록" }));

    expect(onRegister).toHaveBeenCalledWith("/home/me/new");
  });

  it("shows the gateway's refusal instead of a generic failure", async () => {
    // The server explains WHY (not a repository, protected production
    // checkout); swallowing that would leave the operator guessing.
    const onRegister = vi.fn().mockRejectedValue(new Error("프로덕션 체크아웃이라 대상이 될 수 없습니다"));
    const user = userEvent.setup();
    render(<RepoPicker repos={repos} value="" onSelect={() => {}} onRegister={onRegister} />);

    await user.selectOptions(screen.getByLabelText("작업할 저장소"), "__add__");
    await user.type(screen.getByLabelText("등록할 저장소 경로"), "/home/me/deneb");
    await user.click(screen.getByRole("button", { name: "등록" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("프로덕션 체크아웃");
    // The field stays open with the path intact so the operator can correct it.
    expect(screen.getByLabelText("등록할 저장소 경로")).toHaveValue("/home/me/deneb");
  });

  it("does not register a blank path", async () => {
    const onRegister = vi.fn();
    const user = userEvent.setup();
    render(<RepoPicker repos={repos} value="" onSelect={() => {}} onRegister={onRegister} />);

    await user.selectOptions(screen.getByLabelText("작업할 저장소"), "__add__");
    expect(screen.getByRole("button", { name: "등록" })).toBeDisabled();
    expect(onRegister).not.toHaveBeenCalled();
  });
});
