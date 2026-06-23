// Reorderable list helpers — used by the TodayPane section editor and the
// SettingsPane rail editor. Extracted so the ▲▼ swap logic lives in one place
// (and stays identical across the two editors).

// Swap an item with its neighbour in direction `dir` (-1 up, +1 down). Returns
// the reordered array, or the input array unchanged if the item is already at
// the edge (or not found). Pure: does not mutate the input.
export function moveItem<T>(items: T[], item: T, dir: -1 | 1): T[] {
  const i = items.indexOf(item);
  if (i === -1) return items;
  const j = i + dir;
  if (j < 0 || j >= items.length) return items;
  const next = [...items];
  [next[i], next[j]] = [next[j], next[i]];
  return next;
}
