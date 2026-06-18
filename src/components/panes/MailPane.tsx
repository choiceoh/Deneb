import { useList } from "@refinedev/core";
import type { Mail } from "../../types";
import { color, ellipsis } from "../../theme";
import { fmtDate, text } from "../../format";
import { useRegisterPane, useWorkspace } from "../../workspaceContext";
import { Column, Grid, GridNotice } from "../Grid";

export function MailPane() {
  const { connected } = useWorkspace();
  const { result, query } = useList<Mail>({ resource: "mail", queryOptions: { enabled: connected } });
  const mails = result?.data ?? [];

  // Mirror the grid (subject · sender · date + snippet) so the AI sees what the user sees.
  const aiText = mails.length
    ? `[메일 ${mails.length}건]\n` +
      mails
        .map((m) => {
          const who = text(m.from) || text(m.sender);
          const head = `- ${m.unread ? "● " : ""}${m.subject ?? "(제목 없음)"}${who ? ` · ${who}` : ""}${
            m.date ? ` · ${fmtDate(m.date)}` : ""
          }`;
          return m.snippet ? `${head}\n    ${m.snippet}` : head;
        })
        .join("\n")
    : "";
  useRegisterPane("mail", aiText);

  const columns: Column<Mail>[] = [
    {
      header: "보낸이",
      width: 180,
      tdStyle: { fontSize: 13, opacity: 0.85, ...ellipsis(180) },
      cell: (m) => {
        const who = text(m.from) || text(m.sender);
        return (
          <>
            {m.unread && <span style={{ color: color.accent, marginRight: 4 }}>●</span>}
            {who || "—"}
          </>
        );
      },
    },
    {
      header: "제목",
      cell: (m) => (
        <>
          <div>{m.subject ?? "(제목 없음)"}</div>
          {m.snippet && <div style={{ fontSize: 12, opacity: 0.55, ...ellipsis(520) }}>{m.snippet}</div>}
        </>
      ),
    },
    {
      header: "날짜",
      width: 130,
      tdStyle: { fontSize: 13, opacity: 0.7, whiteSpace: "nowrap" },
      cell: (m) => fmtDate(m.date),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>메일</h2>
      <GridNotice query={query} count={mails.length} empty="메일이 없습니다.">
        <Grid
          columns={columns}
          rows={mails}
          getKey={(m) => String(m.id)}
          rowStyle={(m) => ({ fontWeight: m.unread ? 600 : 400 })}
        />
      </GridNotice>
    </>
  );
}
