// The expanded mail reader — native-quality detail. The AI-analysis card leads
// (above the body) so the synthesis comes first; then the body, attachments, a
// collapsed-by-default sender-context card (recent volume + curated wiki pages),
// and a grounded Q&A box. The enrichment cards each own their fetch/loading/error
// state and degrade silently on an older gateway that lacks the method.
import { useRef, useState } from "react";

import { depsEqual } from "@/depsEqual";
import { printElement } from "@/print";
import {
  type QATurn,
  analyzeMail,
  askMail,
  cachedMailAnalysis,
  fetchGatewayBlob,
  mailAttachmentUrl,
  senderContext,
} from "@/gateway";
import type { Mail, MailAttachment } from "@/types";
import { errText, fmtMailDate, senderName, text } from "@/format";
import { formatBytes } from "@/components/panes/fileHelpers";
import { Icon } from "@/components/Icon";
import { useAsyncOnOpen } from "@/useAsyncOnOpen";
import { useWorkspace } from "@/workspaceContext";
import { Markdown } from "@/components/Markdown";
import { AssistantText } from "@/components/DenebUi";
import { Modal } from "@/components/Modal";
import { FileViewer } from "@/components/FileViewer";
import { viewKindFor } from "@/components/fileView";
import { mailBody } from "./mailBody";

const HOT_IMPORTANCE = /urgent|high|중요|긴급|priority/i;

// The mail analysis card is read-only here (this reader can't route card answers
// back to the agent), so its submit callback is a no-op — mirrors WorkfeedPane.
const ignoreMailUiSubmit = () => {};

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
  // 분석 ↔ 본문 토글. 분석이 기본값 — "왜 지금 중요한가"(합성)를 본문보다 먼저 보여준다.
  // (Hook before the early return below — rules-of-hooks.)
  const [mailView, setMailView] = useState<"analysis" | "body">("analysis");
  // 인쇄 대상 = 상세 전체 subtree. 인쇄 시 액션바·탭·질문칸(.no-print)은 빠지고, 현재
  // 보고 있는 분석/본문이 그대로 리포트로 나간다 ("보이는 대로 인쇄").
  const detailRef = useRef<HTMLElement>(null);

  if (!mail) return null;

  const body = mailBody(mail);
  const who = senderName(mail.from);
  const to = text(mail.to);
  // sender_context wants the raw "Name <email>" header when we have it.
  const senderRaw = typeof mail.from === "string" ? mail.from : text(mail.from);
  const id = String(mail.id);

  return (
    <section className="mail-detail" aria-label="메일 상세" ref={detailRef}>
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

      <div className="mail-actions no-print">
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
        <button
          className="btn"
          onClick={() => printElement(detailRef.current)}
          title="이 메일을 인쇄 (프린터 또는 PDF)"
        >
          인쇄
        </button>
      </div>

      {/* 분석 ↔ 본문 토글 (분석 기본): the synthesis (왜 지금 중요한가) leads; switch to
          본문 for the full raw text. */}
      <div className="mail-view-tabs no-print" role="group" aria-label="메일 보기 방식">
        <button
          className={"mail-view-tab" + (mailView === "analysis" ? " active" : "")}
          aria-pressed={mailView === "analysis"}
          onClick={() => setMailView("analysis")}
        >
          분석
        </button>
        <button
          className={"mail-view-tab" + (mailView === "body" ? " active" : "")}
          aria-pressed={mailView === "body"}
          onClick={() => setMailView("body")}
        >
          본문
        </button>
      </div>

      {mailView === "analysis" ? (
        <AnalysisCard mailId={id} />
      ) : body ? (
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
  const [previewing, setPreviewing] = useState<MailAttachment | null>(null);
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
            // Previewable formats open in the in-app viewer; the rest keep the
            // plain download link (a broken preview is worse than none).
            const previewable = canOpen && viewKindFor(name, att.mimeType) !== "none";
            if (previewable) {
              return (
                <button key={id} className="mail-attachment" onClick={() => setPreviewing(att)} title="미리보기">
                  <span>{name}</span>
                  <span>{formatAttachmentMeta(att)}</span>
                </button>
              );
            }
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
      {previewing && (
        <AttachmentPreviewModal mailId={mailId} attachment={previewing} onClose={() => setPreviewing(null)} />
      )}
    </div>
  );
}

// AttachmentPreviewModal shows one attachment in the in-app viewer (read-only —
// mail bytes have no save-back), with the download link as escape hatch.
function AttachmentPreviewModal({
  mailId,
  attachment,
  onClose,
}: {
  mailId: string;
  attachment: MailAttachment;
  onClose: () => void;
}) {
  const { cfg } = useWorkspace();
  const name = attachment.filename ?? attachment.name ?? "attachment";
  const url = mailAttachmentUrl(cfg, mailId, attachment);
  return (
    <Modal title={name} onClose={onClose} width={860}>
      <div className="mail-attachment-preview">
        <FileViewer name={name} mime={attachment.mimeType} load={() => fetchGatewayBlob(url)} downloadUrl={url} />
      </div>
    </Modal>
  );
}

function formatAttachmentMeta(att: MailAttachment): string {
  const bits = [att.mimeType, formatBytes(att.size)].filter(Boolean);
  return bits.join(" · ");
}

// Sender context: recent volume in the last N days + the operator's curated wiki
// pages about this person/company. Renders nothing until something useful loads.
// Collapsed by default behind a disclosure header — the sender dossier is reference,
// not the main read, so it stays folded with a one-line teaser; tap to expand.
function SenderCard({ sender }: { sender: string }) {
  const { cfg, connected, openWiki } = useWorkspace();
  const [data] = useAsyncOnOpen(() => senderContext(cfg, sender), [cfg, sender], {
    enabled: connected && !!sender.trim(),
  });
  const [open, setOpen] = useState(false);

  const recent = data?.recent;
  const hits = data?.wikiHits ?? [];
  if (!recent && hits.length === 0 && !data?.wikiFacts) return null;

  // Collapsed teaser: recent volume if known, else the count of cited wiki pages —
  // enough signal to decide whether to expand without unfolding the whole card.
  const teaser = recent
    ? `최근 ${recent.windowDays}일 ${recent.count}${recent.truncated ? "+" : ""}건`
    : hits.length > 0
      ? `위키 ${hits.length}건`
      : "";

  return (
    <div className="mail-card">
      <button
        className={"mail-card-disclosure" + (open ? " open" : "")}
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        title={open ? "발신자 접기" : "발신자 펼치기"}
      >
        <span className="mail-card-title">발신자</span>
        {!open && teaser && <span className="mail-card-teaser">{teaser}</span>}
        <span className="mail-card-caret">{open ? "▾" : "▸"}</span>
      </button>
      {open && (
        <>
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
        </>
      )}
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
  // Adjusted during render, same as useAsyncOnOpen's own reset.
  const errKey: unknown[] = [cfg, connected, mailId];
  const [prevErrKey, setPrevErrKey] = useState(errKey);
  if (!depsEqual(prevErrKey, errKey)) {
    setPrevErrKey(errKey);
    setErr("");
  }

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
      {/* The 분석/본문 toggle in MailDetail owns show/hide now, so this card has no
          collapse of its own — just the importance badge + a 다시 분석 re-run. */}
      {(importance || (analysis && !loading)) && (
        <div className="mail-card-head">
          {importance && (
            <span className={"mail-badge" + (HOT_IMPORTANCE.test(importance) ? " hot" : "")}>{importance}</span>
          )}
          {analysis && !loading && (
            <button className="row-btn" onClick={() => void run(true)} disabled={!connected} title="다시 분석">
              다시 분석
            </button>
          )}
        </div>
      )}
      {loading ? (
        <div className="mail-card-line">분석 중… (수십 초 걸릴 수 있어요)</div>
      ) : analysis ? (
        <>
          {/* Mail analyses now lead with a deneb-ui card (analysisSystemPrompt
              authors one), so render through AssistantText/splitDenebUi — a plain
              Markdown render leaks the ```deneb-ui fence as a raw code block. The
              analysis card is informational and this reader can't route answers,
              so it renders non-interactively (same as WorkfeedPane's body). */}
          <AssistantText text={analysis} onUiSubmit={ignoreMailUiSubmit} interactive={false} />
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
        <>
          <div className="mail-card-line error">{err}</div>
          <button className="btn" onClick={() => void run()} disabled={!connected}>
            <Icon name="search" size={14} /> 이 메일 분석
          </button>
        </>
      ) : (
        <button className="btn" onClick={() => void run()} disabled={!connected}>
          <Icon name="search" size={14} /> 이 메일 분석
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

  // Switching messages starts a fresh Q&A thread — adjusted during render.
  const [prevMailId, setPrevMailId] = useState(mailId);
  if (prevMailId !== mailId) {
    setPrevMailId(mailId);
    setTurns([]);
    setQ("");
    setErr("");
  }

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
    // Interactive follow-up Q&A — belongs on screen, not in a printed report.
    <div className="mail-card no-print">
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
