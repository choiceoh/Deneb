// The expanded mail reader — native-quality detail. Beyond subject/body it shows
// an action bar (읽음/보관/삭제), an AI-analysis card (cached on open, or analyze
// on demand), a sender-context card (recent volume + curated wiki pages), and a
// grounded Q&A box. The enrichment cards each own their fetch/loading/error state
// and degrade silently on an older gateway that lacks the method.
import { useEffect, useState } from "react";
import { type QATurn, analyzeMail, askMail, cachedMailAnalysis, mailAttachmentUrl, senderContext } from "@/gateway";
import type { Mail, MailAttachment } from "@/types";
import { errText, firstString, fmtMailDate, senderName, text } from "@/format";
import { stripMailChrome } from "@/mailChrome";
import { formatBytes } from "@/components/panes/fileHelpers";
import { useAsyncOnOpen } from "@/useAsyncOnOpen";
import { useWorkspace } from "@/workspaceContext";
import { Markdown } from "@/components/Markdown";

// The displayable mail body, falling back through the gateway's field aliases and
// finally a stripped HTML part. Exported so the pane can project it to the AI.
export function mailBody(mail?: Mail): string {
  if (!mail) return "";
  const body = firstString(mail, ["body", "plain", "plainText", "bodyText", "text", "message", "content"]);
  if (body) return stripMailChrome(body);
  if (mail.snippet) return mail.snippet;
  const html = firstString(mail, ["html"]);
  return html ? stripMailChrome(htmlToText(html)) : "";
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

const HOT_IMPORTANCE = /urgent|high|중요|긴급|priority/i;

export function MailDetail({
  mail,
  query,
  busy,
  onMarkRead,
  onArchive,
  onTrash,
}: {
  mail?: Mail;
  query: { isLoading: boolean; isError?: boolean; error?: unknown };
  busy: boolean;
  onMarkRead: () => void;
  onArchive: () => void;
  onTrash: () => void;
}) {
  if (!mail) return null;

  const body = mailBody(mail);
  const who = senderName(mail.from);
  const to = text(mail.to);
  // sender_context wants the raw "Name <email>" header when we have it.
  const senderRaw = typeof mail.from === "string" ? mail.from : text(mail.from);
  const id = String(mail.id);

  return (
    <section className="mail-detail" aria-label="메일 상세">
      {query.isLoading && <div className="mail-detail-status">본문 불러오는 중…</div>}
      {query.isError && <div className="mail-detail-status error">본문 불러오기 실패</div>}
      <div className="mail-detail-head">
        <div className="mail-detail-subject">{mail.subject ?? "(제목 없음)"}</div>
        <div className="mail-detail-meta">
          {who || "—"}
          {to ? ` → ${to}` : ""}
          {mail.date ? ` · ${fmtMailDate(mail.date)}` : ""}
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

      <div className="mail-actions">
        {mail.isUnread && (
          <button className="btn" onClick={onMarkRead} disabled={busy} title="읽음으로 표시">
            읽음
          </button>
        )}
        <button className="btn" onClick={onArchive} disabled={busy} title="보관(받은편지함에서 제거)">
          보관
        </button>
        <button className="btn" onClick={onTrash} disabled={busy} title="휴지통으로">
          삭제
        </button>
      </div>

      {body ? (
        // The gateway returns the body HTML-converted to Markdown — render it so
        // links are clickable and lists/quotes keep structure.
        <div className="mail-body">
          <Markdown text={body} />
        </div>
      ) : (
        <div className="mail-body mail-detail-empty">본문 없음</div>
      )}

      <AttachmentCard mailId={id} attachments={mail.attachments} count={mail.attachmentCount} />
      <SenderCard sender={senderRaw} />
      <AnalysisCard mailId={id} />
      <AskBox mailId={id} />
    </section>
  );
}

function AttachmentCard({
  mailId,
  attachments,
  count,
}: {
  mailId: string;
  attachments?: MailAttachment[];
  count?: number;
}) {
  const { cfg } = useWorkspace();
  const list = attachments ?? [];
  const knownCount = count ?? list.length;
  if (knownCount <= 0 && list.length === 0) return null;

  return (
    <div className="mail-card">
      <div className="mail-card-title">첨부파일</div>
      {list.length === 0 ? (
        <div className="mail-card-line">첨부파일 {knownCount}개</div>
      ) : (
        <div className="mail-attachments">
          {list.map((att, i) => {
            const id = att.attachmentId ?? att.id ?? String(i);
            const name = att.filename ?? att.name ?? `attachment-${i + 1}`;
            const canOpen = Boolean(att.attachmentId ?? att.id);
            return canOpen ? (
              <a
                key={id}
                className="mail-attachment"
                href={mailAttachmentUrl(cfg, mailId, att)}
                target="_blank"
                rel="noreferrer"
              >
                <span>{name}</span>
                <span>{formatAttachmentMeta(att)}</span>
              </a>
            ) : (
              <div key={id} className="mail-attachment" aria-disabled="true">
                <span>{name}</span>
                <span>{formatAttachmentMeta(att)}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function formatAttachmentMeta(att: MailAttachment): string {
  const bits = [att.mimeType, formatBytes(att.size)].filter(Boolean);
  return bits.join(" · ");
}

// Sender context: recent volume in the last N days + the operator's curated wiki
// pages about this person/company. Renders nothing until something useful loads.
function SenderCard({ sender }: { sender: string }) {
  const { cfg, connected, openWiki } = useWorkspace();
  const [data] = useAsyncOnOpen(() => senderContext(cfg, sender), [cfg, sender], {
    enabled: connected && !!sender.trim(),
  });

  const recent = data?.recent;
  const hits = data?.wikiHits ?? [];
  if (!recent && hits.length === 0 && !data?.wikiFacts) return null;

  return (
    <div className="mail-card">
      <div className="mail-card-title">발신자</div>
      {recent && (
        <div className="mail-card-line">
          최근 {recent.windowDays}일 {recent.count}
          {recent.truncated ? "+" : ""}건
          {recent.lastReceivedAt ? ` · 마지막 ${fmtMailDate(recent.lastReceivedAt)}` : ""}
        </div>
      )}
      {hits.length > 0 && (
        <div className="mail-chips">
          {hits.map((h) => (
            <button key={h.path} className="mail-chip" onClick={() => openWiki(h.path)} title={h.summary || h.path}>
              {h.title || h.path}
            </button>
          ))}
        </div>
      )}
      {data?.wikiFacts && <div className="mail-card-facts">{data.wikiFacts}</div>}
    </div>
  );
}

// AI analysis: load any cached result on open; otherwise offer an analyze button.
function AnalysisCard({ mailId }: { mailId: string }) {
  const { cfg, connected, openWiki } = useWorkspace();
  // Load any cached analysis on open (a miss / older gateway just leaves data null,
  // so we fall through to the analyze button). `setData` is reused by the manual run.
  const [data, setData] = useAsyncOnOpen(() => cachedMailAnalysis(cfg, mailId), [cfg, mailId], {
    enabled: connected,
  });
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  // Drop a stale manual-analysis error whenever the cached load re-runs (message
  // switch, reconnect, or config change) — matches the same triggers that reset
  // `data`, so a transient analyze failure can't strand the error after reconnect.
  useEffect(() => setErr(""), [cfg, connected, mailId]);

  async function run(force = false) {
    setLoading(true);
    setErr("");
    try {
      setData(await analyzeMail(cfg, mailId, force));
    } catch (e) {
      setErr(errText(e));
    } finally {
      setLoading(false);
    }
  }

  const analysis = data?.analysis?.trim() ? data.analysis : "";
  const importance = data?.analysisQuality?.trim();

  return (
    <div className="mail-card">
      <div className="mail-card-head">
        <span className="mail-card-title">AI 분석</span>
        {importance && (
          <span className={"mail-badge" + (HOT_IMPORTANCE.test(importance) ? " hot" : "")}>{importance}</span>
        )}
        {analysis && !loading && (
          <button className="row-btn" onClick={() => void run(true)} disabled={!connected} title="다시 분석">
            다시 분석
          </button>
        )}
      </div>
      {loading ? (
        <div className="mail-card-line">분석 중… (수십 초 걸릴 수 있어요)</div>
      ) : analysis ? (
        <>
          <Markdown text={analysis} />
          {(data?.relatedProjects?.length ?? 0) > 0 && (
            <div className="mail-chips">
              {data!.relatedProjects!.map((p) => (
                <button key={p.path} className="mail-chip" onClick={() => openWiki(p.path)} title={p.summary || p.path}>
                  {p.title || p.path}
                </button>
              ))}
            </div>
          )}
          {((data?.calendarProposalCount ?? 0) > 0 || (data?.todoCount ?? 0) > 0) && (
            <div className="mail-card-line">
              {(data?.calendarProposalCount ?? 0) > 0 && `일정 제안 ${data!.calendarProposalCount}`}
              {(data?.calendarProposalCount ?? 0) > 0 && (data?.todoCount ?? 0) > 0 && " · "}
              {(data?.todoCount ?? 0) > 0 && `할일 후보 ${data!.todoCount}`}
            </div>
          )}
        </>
      ) : err ? (
        <div className="mail-card-line error">{err}</div>
      ) : (
        <button className="btn" onClick={() => void run()} disabled={!connected}>
          🔍 이 메일 분석
        </button>
      )}
    </div>
  );
}

// Grounded follow-up Q&A about this message. Stateless on the server — we resend
// the accumulated turns each time (gmail.ask history).
function AskBox({ mailId }: { mailId: string }) {
  const { cfg, connected } = useWorkspace();
  const [turns, setTurns] = useState<QATurn[]>([]);
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    setTurns([]);
    setQ("");
    setErr("");
  }, [mailId]);

  async function ask() {
    const question = q.trim();
    if (!question || busy || !connected) return;
    setBusy(true);
    setErr("");
    setQ("");
    try {
      const answer = await askMail(cfg, mailId, question, turns);
      setTurns((t) => [...t, { q: question, a: answer }]);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mail-card">
      <div className="mail-card-title">이 메일에 질문</div>
      {turns.map((t, i) => (
        <div key={i} className="mail-qa">
          <div className="mail-qa-q">{t.q}</div>
          <div className="mail-qa-a">
            <Markdown text={t.a} />
          </div>
        </div>
      ))}
      {err && <div className="mail-card-line error">{err}</div>}
      <form
        className="mail-ask"
        onSubmit={(e) => {
          e.preventDefault();
          void ask();
        }}
      >
        <input
          className="field"
          placeholder={busy ? "답변 중…" : "예: 핵심 요청이 뭐야?"}
          value={q}
          disabled={busy || !connected}
          onChange={(e) => setQ(e.target.value)}
        />
        <button className="btn btn-accent" type="submit" disabled={busy || !connected || q.trim().length === 0}>
          질문
        </button>
      </form>
    </div>
  );
}
