import { useCallback, useState } from "react";
import type { Mail } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList, useCachedOne } from "@/cachedList";
import { MAIL_RPC } from "@/resources";
import { color, ellipsis } from "@/theme";
import { fmtMailDate, senderName } from "@/format";
import { usePaneTarget } from "@/usePaneTarget";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { MailDetail, mailBody } from "./MailDetail";

export function MailPane() {
  const { connected } = useWorkspace();
  const { result, query } = useCachedList<Mail>("mail", connected);
  const mails = result?.data ?? [];
  const [selectedId, setSelectedId] = useState<string | number | undefined>();
  const selectedPreview = mails.find((m) => String(m.id) === String(selectedId));
  const detail = useCachedOne<Mail>("mail", selectedId, connected && selectedId !== undefined);
  const selectedMail = detail.result ?? selectedPreview;
  const { run, error, busy } = useAction(() => void query.refetch());

  // An id-less mail target is meaningless — keep it pending instead of clearing the
  // current selection. (The detail fetches by id, so no need to wait for the list.)
  const openTargetedMail = useCallback((t: PaneTarget) => {
    if (t.id === undefined) return false;
    setSelectedId(t.id);
  }, []);
  usePaneTarget("mail", openTargetedMail);

  // Mirror the grid (subject · sender · date) so the AI sees what the user sees.
  const listText = serializeList("메일", mails, (m) => {
    const who = senderName(m.from);
    return `- ${m.isUnread ? "● " : ""}${m.subject ?? "(제목 없음)"}${who ? ` · ${who}` : ""}${
      m.date ? ` · ${fmtMailDate(m.date)}` : ""
    }`;
  });
  const detailBody = mailBody(selectedMail);
  const detailText = selectedMail
    ? `[선택한 메일]\n제목: ${selectedMail.subject ?? "(제목 없음)"}\n보낸이: ${senderName(selectedMail.from) || "—"}${
        selectedMail.date ? `\n날짜: ${fmtMailDate(selectedMail.date)}` : ""
      }\n\n${detailBody}`
    : "";
  const aiText = [listText, detailText].filter(Boolean).join("\n\n");
  useRegisterPane("mail", aiText);

  // Detail actions. Archive/trash drop the now-gone selection so the row collapses.
  const act = (method: string) => {
    if (selectedId !== undefined) void run(method, { id: selectedId });
  };
  const closeAfter = (method: string) => {
    if (selectedId === undefined) return;
    void run(method, { id: selectedId });
    setSelectedId(undefined);
  };

  const columns: Column<Mail>[] = [
    {
      header: "보낸이",
      width: 170,
      tdStyle: { fontSize: 13, opacity: 0.85, ...ellipsis(170) },
      cell: (m) => {
        const who = senderName(m.from);
        return (
          <>
            {m.isUnread && <span style={{ color: color.accent, marginRight: 5 }}>●</span>}
            {who || "—"}
          </>
        );
      },
    },
    {
      header: "제목",
      cell: (m) => m.subject ?? "(제목 없음)",
    },
    {
      header: "날짜",
      width: 116,
      tdStyle: { fontSize: 13, opacity: 0.7, whiteSpace: "nowrap" },
      cell: (m) => fmtMailDate(m.date),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>메일</h2>
      {error && <p className="pane-error">오류: {error}</p>}
      <GridNotice query={query} count={mails.length} empty="메일이 없습니다.">
        <Grid
          columns={columns}
          rows={mails}
          getKey={(m) => String(m.id)}
          rowStyle={(m) => ({ fontWeight: m.isUnread ? 600 : 400 })}
          onRowClick={(m) => setSelectedId((current) => (String(current) === String(m.id) ? undefined : m.id))}
          isRowSelected={(m) => String(m.id) === String(selectedId)}
          rowTitle={(m) => `${m.subject ?? "(제목 없음)"} 읽기`}
          renderExpandedRow={() => (
            <MailDetail
              mail={selectedMail}
              query={detail.query}
              busy={busy}
              onMarkRead={() => act(MAIL_RPC.markRead)}
              onArchive={() => closeAfter(MAIL_RPC.archive)}
              onTrash={() => closeAfter(MAIL_RPC.trash)}
            />
          )}
        />
      </GridNotice>
    </>
  );
}
