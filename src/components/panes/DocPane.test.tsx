import { afterEach, describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DOC_STORAGE_KEY } from "@/docStorage";
import { renderWithProviders } from "@/test/util";
import { DocPane } from "./DocPane";

afterEach(() => {
  localStorage.clear();
});

describe("DocPane", () => {
  it("restores a saved draft from local storage", () => {
    localStorage.setItem(DOC_STORAGE_KEY, "저장된 초안");

    renderWithProviders(<DocPane />);

    expect(screen.getByRole("textbox")).toHaveValue("저장된 초안");
  });

  it("auto-saves edits locally", async () => {
    renderWithProviders(<DocPane />);

    await userEvent.type(screen.getByRole("textbox"), "새 문서");

    await waitFor(() => expect(localStorage.getItem(DOC_STORAGE_KEY)).toBe("새 문서"));
  });
});
