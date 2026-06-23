// Generic dense data grid + the shared disconnected/error/loading/empty notice.
// Panes describe their columns declaratively; the table chrome (header, row
// hairlines, hover) lives in the .dgrid class in styles.css.
import type { CSSProperties, ReactNode } from "react";
import { color, muted } from "@/theme";
import { errText } from "@/format";
import { useWorkspace } from "@/workspaceContext";

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
  onRowClick,
  isRowSelected,
  rowTitle,
}: {
  columns: Column<T>[];
  rows: T[];
  getKey: (row: T) => string;
  maxWidth?: number;
  rowStyle?: (row: T) => CSSProperties;
  onRowClick?: (row: T) => void;
  isRowSelected?: (row: T) => boolean;
  rowTitle?: (row: T) => string;
}) {
  return (
    <table className="dgrid" style={{ maxWidth }}>
      <thead>
        <tr>
          {columns.map((c, i) => (
            <th key={i} style={c.width ? { width: c.width } : undefined}>
              {c.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => {
          const interactive = Boolean(onRowClick);
          const selected = Boolean(isRowSelected?.(row));
          const className = [interactive ? "clickable" : "", selected ? "selected" : ""].filter(Boolean).join(" ");
          return (
            <tr
              key={getKey(row)}
              className={className || undefined}
              style={rowStyle?.(row)}
              tabIndex={interactive ? 0 : undefined}
              aria-selected={interactive ? selected : undefined}
              title={rowTitle?.(row)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              onKeyDown={
                onRowClick
                  ? (e) => {
                      if (e.key !== "Enter" && e.key !== " ") return;
                      e.preventDefault();
                      onRowClick(row);
                    }
                  : undefined
              }
            >
              {columns.map((c, i) => (
                <td key={i} style={c.tdStyle}>
                  {c.cell(row)}
                </td>
              ))}
            </tr>
          );
        })}
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
  const notice: CSSProperties = { ...muted, fontSize: 13 };
  if (!connected) return <p style={notice}>게이트웨이에 연결하면 표시됩니다 (좌측 하단).</p>;
  if (query.isError) return <p style={{ fontSize: 13, color: color.danger }}>불러오기 실패: {errText(query.error)}</p>;
  if (query.isLoading) return <p style={notice}>불러오는 중…</p>;
  if (count === 0) return <p style={notice}>{empty}</p>;
  return <>{children}</>;
}

// Compact inline action button for grid rows (읽음 / 보관 / 삭제 / 실행 …).
export function RowBtn({
  children,
  onClick,
  disabled,
  danger,
  title,
}: {
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
  title?: string;
}) {
  return (
    <button
      className="row-btn"
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      disabled={disabled}
      title={title}
      style={danger ? { color: "var(--due)" } : undefined}
    >
      {children}
    </button>
  );
}
