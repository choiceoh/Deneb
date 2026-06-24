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

// Merge a user's `saved` order with the `catalog` of valid items: keep the saved
// order for items still in the catalog, then append any catalog items missing
// from it (in catalog order). New catalog items surface; removed ones drop. Used
// for both the nav rail (orderedViews) and the 오늘 dashboard sections.
export function orderedItems<T>(saved: readonly T[], catalog: readonly T[]): T[] {
  const inSaved = saved.filter((k) => catalog.includes(k));
  return [...inSaved, ...catalog.filter((k) => !inSaved.includes(k))];
}
