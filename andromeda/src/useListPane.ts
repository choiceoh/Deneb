// The shared skeleton every CRUD-style list pane repeats: fetch a resource via
// useCachedList, derive rows, hold the selected row for the detail modal, and
// push the serialized rows to the AI panel. Extracted so a pane that just renders
// a grid + detail modal can skip re-stating this wiring each time.
//
// Panes that need more (a run/error action like CronsPane, a deep-link like
// TodoPane, or multi-resource fan-out like TodayPane) still keep that extra
// wiring themselves — this only covers the common fetch+select+project spine.
import { useMemo, useState } from "react";
import { type BaseRecord } from "@refinedev/core";
import { serializeList } from "./aiText";
import { useCachedList } from "./cachedList";
import { useRegisterPane, useWorkspace } from "./workspaceContext";

// The query observer Refine hands back from useCachedList (re-exported so callers
// don't need to reach into @tanstack/react-query just to type a prop).
export type ListPaneQuery<T extends BaseRecord> = ReturnType<typeof useCachedList<T>>["query"];

export interface ListPaneResult<T extends BaseRecord> {
  rows: T[];
  query: ListPaneQuery<T>;
  selected: T | null;
  setSelected: (row: T | null) => void;
}

export function useListPane<T extends BaseRecord>(
  resource: string,
  label: string,
  line: (row: T) => string,
  unit = "건",
): ListPaneResult<T> {
  const { connected } = useWorkspace();
  const { result, query } = useCachedList<T>(resource, connected);
  const rows = useMemo(() => result?.data ?? [], [result?.data]);
  const [selected, setSelected] = useState<T | null>(null);

  const aiText = serializeList(label, rows, line, unit);
  useRegisterPane(resource, aiText);

  return { rows, query, selected, setSelected };
}
