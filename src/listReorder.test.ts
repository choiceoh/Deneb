import { describe, expect, it } from "vitest";

import { moveItem } from "./listReorder";

describe("moveItem", () => {
  it("swaps an item with its neighbour in the given direction", () => {
    expect(moveItem(["a", "b", "c"], "b", -1)).toEqual(["b", "a", "c"]);
    expect(moveItem(["a", "b", "c"], "b", 1)).toEqual(["a", "c", "b"]);
  });

  it("returns the array unchanged when the item is already at the edge", () => {
    expect(moveItem(["a", "b", "c"], "a", -1)).toEqual(["a", "b", "c"]);
    expect(moveItem(["a", "b", "c"], "c", 1)).toEqual(["a", "b", "c"]);
  });

  it("returns the array unchanged when the item is not found", () => {
    expect(moveItem(["a", "b", "c"], "z", 1)).toEqual(["a", "b", "c"]);
  });

  it("does not mutate the input array", () => {
    const input = ["a", "b", "c"];
    moveItem(input, "b", 1);
    expect(input).toEqual(["a", "b", "c"]);
  });
});
