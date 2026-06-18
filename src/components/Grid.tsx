// Generic dense data grid + the shared disconnected/error/loading/empty notice.
// Panes describe their columns declaratively; layout, header, and row chrome are
// handled here once.
import type { CSSProperties, ReactNode } from "react";
import { line, muted, td, th, color } from "../theme";
import { errText } from "../format";
import { useWorkspace } from "../workspaceContext";

export interface Column<T> {
  header: ReactNode;
  width?: number;
  cell: (row: T) => ReactNode;
  tdStyle?: CSSProperties; // merged onto the cell <td> (e.g. ellipsis, nowrap)
}

export function Grid<T>({
  columns,
  rows,
  getKey,
  maxWidth,
  rowStyle,
}: {
  columns: Column<T>[];
  rows: T[];
  getKey: (row: T) => string;
  maxWidth?: number;
  rowStyle?: (row: T) => CSSProperties;
}) {
  return (
    <table style={{ borderCollapse: "collapse", width: "100%", maxWidth }}>
      <thead>
        <tr style={{ textAlign: "left", opacity: 0.6, fontSize: 13 }}>
          {columns.map((c, i) => (
            <th key={i} style={c.width ? { ...th, width: c.width } : th}>
              {c.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={getKey(row)} style={{ borderTop: line, ...rowStyle?.(row) }}>
            {columns.map((c, i) => (
              <td key={i} style={{ ...td, ...c.tdStyle }}>
                {c.cell(row)}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// Renders a notice (and nothing else) when the grid can't show rows yet; checks
// error BEFORE empty so a failed RPC isn't misreported as "no data". Otherwise
// renders its children (the table).
export function GridNotice({
  query,
  count,
  empty,
  children,
}: {
  query: { isLoading: boolean; isError?: boolean; error?: unknown };
  count: number;
  empty: string;
  children: ReactNode;
}) {
  const { connected } = useWorkspace();
  if (!connected) return <p style={muted}>게이트웨이에 연결하면 표시됩니다 (좌측 하단).</p>;
  if (query.isError) return <p style={{ ...muted, color: color.danger }}>불러오기 실패: {errText(query.error)}</p>;
  if (query.isLoading) return <p style={muted}>불러오는 중…</p>;
  if (count === 0) return <p style={muted}>{empty}</p>;
  return <>{children}</>;
}
