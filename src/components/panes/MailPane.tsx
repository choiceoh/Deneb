import { useState } from "react";
import type { Mail } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList, useCachedOne } from "@/cachedList";
import { MAIL_RPC } from "@/resources";
import { color, ellipsis } from "@/theme";
import { fmtDate, text } from "@/format";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice, RowBtn } from "@/components/Grid";

export function MailPane() {
  const { connected } = useWorkspace();
  const { result, query } = useCachedList<Mail>("mail", connected);
  const mails = result?.data ?? [];
  const [selectedId, setSelectedId] = useState<string | number | undefined>();
  const selectedPreview = mails.find((m) => String(m.id) === String(selectedId));
  const detail = useCachedOne<Mail>("mail", selectedId, connected && selectedId !== undefined);
  const selectedMail = detail.result ?? selectedPreview;
  const { run, error, busy } = useAction(() => void query.refetch());

  // Mirror the grid (subject · sender · date + snippet) so the AI sees what the user sees.
  const listText = serializeList("메일", mails, (m) => {
    const who = text(m.from);
    const head = `- ${m.isUnread ? "● " : ""}${m.subject ?? "(제목 없음)"}${who ? ` · ${who}` : ""}${
      m.date ? ` · ${fmtDate(m.date)}` : ""
    }`;
    return m.snippet ? `${head}\n    ${m.snippet}` : head;
  });
  const detailBody = mailBody(selectedMail);
  const detailText = selectedMail
    ? `[선택한 메일]\n제목: ${selectedMail.subject ?? "(제목 없음)"}\n보낸이: ${text(selectedMail.from) || "—"}${
        selectedMail.date ? `\n날짜: ${fmtDate(selectedMail.date)}` : ""
      }\n\n${detailBody}`
    : "";
  const aiText = [listText, detailText].filter(Boolean).join("\n\n");
  useRegisterPane("mail", aiText);

  const columns: Column<Mail>[] = [
    {
      header: "보낸이",
      width: 170,
      tdStyle: { fontSize: 13, opacity: 0.85, ...ellipsis(170) },
      cell: (m) => {
        const who = text(m.from);
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
      cell: (m) => (
        <>
          <div>{m.subject ?? "(제목 없음)"}</div>
          {m.snippet && <div style={{ fontSize: 12, color: "var(--muted-2)", ...ellipsis(520) }}>{m.snippet}</div>}
        </>
      ),
    },
    {
      header: "날짜",
      width: 116,
      tdStyle: { fontSize: 13, opacity: 0.7, whiteSpace: "nowrap" },
      cell: (m) => fmtDate(m.date),
    },
    {
      header: "",
      width: 58,
      tdStyle: { whiteSpace: "nowrap", textAlign: "right" },
      cell: (m) => (
        <span style={{ display: "inline-flex", gap: 2, justifyContent: "flex-end" }}>
          <RowBtn onClick={() => run(MAIL_RPC.trash, { id: m.id })} disabled={busy} danger title="삭제">
            삭제
          </RowBtn>
        </span>
      ),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>메일</h2>
      {error && <p style={{ color: "var(--due)", fontSize: 12, margin: "0 0 8px" }}>오류: {error}</p>}
      <GridNotice query={query} count={mails.length} empty="메일이 없습니다.">
        <div className="mail-split">
          <div className="mail-list">
            <Grid
              columns={columns}
              rows={mails}
              getKey={(m) => String(m.id)}
              rowStyle={(m) => ({ fontWeight: m.isUnread ? 600 : 400 })}
              onRowClick={(m) => setSelectedId(m.id)}
              isRowSelected={(m) => String(m.id) === String(selectedId)}
              rowTitle={(m) => `${m.subject ?? "(제목 없음)"} 읽기`}
            />
          </div>
          <MailDetail mail={selectedMail} query={detail.query} />
        </div>
      </GridNotice>
    </>
  );
}

function mailBody(mail?: Mail): string {
  if (!mail) return "";
  const body = firstString(mail, ["body", "plain", "plainText", "bodyText", "text", "message", "content"]);
  if (body) return body;
  if (mail.snippet) return mail.snippet;
  const html = firstString(mail, ["html"]);
  return html ? htmlToText(html) : "";
}

function firstString(mail: Mail, keys: string[]): string {
  const record = mail as unknown as Record<string, unknown>;
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" && value.trim()) return value;
  }
  return "";
}

function htmlToText(html: string): string {
  if (typeof DOMParser !== "undefined") {
    return new DOMParser().parseFromString(html, "text/html").body.textContent?.trim() ?? "";
  }
  return html
    .replace(/<[^>]*>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function MailDetail({
  mail,
  query,
}: {
  mail?: Mail;
  query: { isLoading: boolean; isError?: boolean; error?: unknown };
}) {
  if (!mail) {
    return (
      <aside className="mail-detail" aria-label="메일 상세">
        <div className="mail-detail-empty">선택된 메일 없음</div>
      </aside>
    );
  }

  const body = mailBody(mail);
  const who = text(mail.from);
  const to = text(mail.to);

  return (
    <aside className="mail-detail" aria-label="메일 상세">
      {query.isLoading && <div className="mail-detail-status">본문 불러오는 중…</div>}
      {query.isError && <div className="mail-detail-status error">본문 불러오기 실패</div>}
      <div className="mail-detail-head">
        <div className="mail-detail-subject">{mail.subject ?? "(제목 없음)"}</div>
        <div className="mail-detail-meta">
          {who || "—"}
          {to ? ` → ${to}` : ""}
          {mail.date ? ` · ${fmtDate(mail.date)}` : ""}
        </div>
        {mail.labels && mail.labels.length > 0 && (
          <div className="mail-labels">
            {mail.labels.map((label) => (
              <span key={label} className="mail-label">
                {label}
              </span>
            ))}
          </div>
        )}
      </div>
      <pre className="mail-body">{body || "본문 없음"}</pre>
    </aside>
  );
}
