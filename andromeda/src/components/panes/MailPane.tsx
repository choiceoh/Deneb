import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Mail } from "@/types";
import { serializeList } from "@/aiText";
import { useCachedList, useCachedOne } from "@/cachedList";
import { MAIL_RPC } from "@/resources";
import { color, ellipsis } from "@/theme";
import { callRpc } from "@/gateway";
import { addDays, dayLabel, fmtMailDate, senderName, startOfDay } from "@/format";
import { usePaneTarget } from "@/usePaneTarget";
import { useAction } from "@/useAction";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { DayPager } from "@/components/DayPager";
import { MailDetail } from "./MailDetail";
import { mailBody } from "./mailBody";

// How far back the day-pager can step before the ‹이전 arrow stops (matches the
// work feed's lookback so a quiet stretch never traps you on today).
const MAIL_LOOKBACK_DAYS = 31;

// Gmail query date token (YYYY/M/D) for after:/before: day scoping. Built from the
// client-local calendar day; Gmail evaluates after:/before: in the account's own
// timezone. On this single-machine deployment the client and Gmail account share
// one timezone, so the day lines up; a cross-timezone setup would be off by a day.
function gmailDay(dayMs: number): string {
  const d = new Date(dayMs);
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
}

export function MailPane() {
  const { connected, cfg } = useWorkspace();
  // The day currently in view (local midnight). Lands on today; ← / → step it. The
  // inbox is browsed one day at a time (like the work feed) rather than one flat
  // unread list: each day fetches that day's inbox (read + unread) via a Gmail
  // after:/before: query, refetched on day change. Per-day cacheKey snapshots each
  // day separately; the resource stays "mail" so sync/useEvents invalidation still
  // refetches the visible day when new mail lands.
  const [dayMs, setDayMs] = useState<number>(() => startOfDay());
  const { result, query } = useCachedList<Mail>("mail", connected, {
    cacheKey: `mail.${dayMs}`,
    meta: {
      rpcParams: { query: `in:inbox after:${gmailDay(dayMs)} before:${gmailDay(addDays(dayMs, 1))}`, limit: 100 },
    },
  });
  const fetchedMails = result?.data;
  const [selectedId, setSelectedId] = useState<string | number | undefined>();
  const [locallyReadIds, setLocallyReadIds] = useState<Set<string>>(readLocallyReadIds);
  const mountedRef = useRef(true);
  const markingReadIdsRef = useRef(new Set<string>());
  const { run, error, busy } = useAction(() => void query.refetch());
  const mails = useMemo(
    () => (fetchedMails ?? []).map((mail) => applyLocalRead(mail, locallyReadIds)),
    [fetchedMails, locallyReadIds],
  );
  const selectedPreview = mails.find((m) => String(m.id) === String(selectedId));
  const detail = useCachedOne<Mail>("mail", selectedId, connected && selectedId !== undefined);
  const selectedMail = detail.result ? applyLocalRead(detail.result, locallyReadIds) : selectedPreview;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Land on the newest inbox day: mornings often have no mail *today* yet, and
  // an empty "오늘 0건" first paint reads broken next to a cockpit full of
  // recent mail. One probe (newest inbox mail regardless of day) decides where
  // to land; fires once, and only while still parked on today's empty result —
  // manual paging wins after that.
  const landedRef = useRef(false);
  useEffect(() => {
    if (landedRef.current || !connected || query.isLoading || query.isError) return;
    if (fetchedMails === undefined) return;
    const today = startOfDay();
    if (dayMs !== today || fetchedMails.length > 0) {
      landedRef.current = true;
      return;
    }
    landedRef.current = true;
    void callRpc<{ messages?: Mail[] }>(cfg, "miniapp.mail.list_recent", { query: "in:inbox", limit: 1 })
      .then((payload) => {
        if (!mountedRef.current) return;
        const newest = payload?.messages?.[0];
        const ms = newest?.date ? new Date(newest.date).getTime() : NaN;
        if (!Number.isFinite(ms)) return;
        const day = startOfDay(ms);
        if (day !== today) setDayMs(day);
      })
      .catch(() => {}); // best-effort — staying on today is the safe fallback
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, dayMs, fetchedMails, query.isLoading, query.isError]);

  const markMailRead = useCallback(
    (id: string | number) => {
      const key = String(id);
      // Optimistic local echo, deferred one microtask: the selection effect calls
      // this synchronously, and microtasks still flush before paint, so the row
      // flips just as instantly without a sync setState inside an effect pass.
      queueMicrotask(() =>
        setLocallyReadIds((prev) => {
          if (prev.has(key)) return prev;
          const next = new Set(prev);
          next.add(key);
          writeLocallyReadIds(next);
          return next;
        }),
      );
      if (markingReadIdsRef.current.has(key)) return;
      markingReadIdsRef.current.add(key);
      void run(MAIL_RPC.markRead, { id })
        .then((result) => {
          if (result !== undefined) return;
          if (!mountedRef.current) return;
          setLocallyReadIds((prev) => {
            if (!prev.has(key)) return prev;
            const next = new Set(prev);
            next.delete(key);
            writeLocallyReadIds(next);
            return next;
          });
        })
        .finally(() => {
          markingReadIdsRef.current.delete(key);
        });
    },
    [run],
  );

  useEffect(() => {
    if (selectedId === undefined || selectedMail?.isUnread !== true) return;
    markMailRead(selectedId);
  }, [markMailRead, selectedId, selectedMail?.isUnread]);

  // Render-time clock read — "today" must track wall-clock at paint so the pager's
  // forward ceiling stays honest; a state snapshot would go stale across midnight.
  // eslint-disable-next-line react-hooks/purity
  const nowMs = Date.now();
  const todayMs = startOfDay(nowMs);
  // Step back freely across the lookback window (forward stops at today). A deep-
  // linked mail older than the window extends the floor so you can keep stepping
  // back from where it landed.
  const minDayMs = Math.min(addDays(todayMs, -MAIL_LOOKBACK_DAYS), dayMs);
  function goToDay(next: number) {
    setDayMs(next);
    setSelectedId(undefined); // the selection likely isn't on the new day
  }
  function stepDay(delta: number) {
    goToDay(addDays(dayMs, delta));
  }

  // An id-less mail target is meaningless — keep it pending instead of clearing the
  // current selection. (The detail fetches by id, so no need to wait for the list.)
  const openTargetedMail = useCallback((t: PaneTarget) => {
    // 날짜 점프(데네브 "지난 화요일 메일 보여줘") — 자동 랜딩보다 우선.
    if (t.date) {
      const ms = new Date(`${t.date}T00:00:00`).getTime();
      if (Number.isFinite(ms)) {
        landedRef.current = true;
        setDayMs(startOfDay(ms));
        // id 없는 날짜 점프는 기존 선택을 비운다 — 선택 메일의 날짜-보정 렌더
        // 로직이 페이저를 곧바로 되돌리는 것을 막는다.
        if (t.id === undefined) setSelectedId(undefined);
      }
    }
    if (t.id === undefined) return t.date ? undefined : false;
    setSelectedId(t.id);
  }, []);
  usePaneTarget("mail", openTargetedMail);

  // A mail opened by id (work feed / search / notification deep-link) may belong to
  // another day than the one in view — there'd be no row to expand. Once its detail
  // lands, jump the pager to that mail's day so the row appears. Only for mails NOT
  // on the visible list: `date` is the sender's Date header while the day query
  // buckets by received time (Gmail after:/before:), so the two can straddle
  // midnight — a row the user can see must expand in place, never bounce the pager
  // to a day whose query may not even return it. Adjusted during render — converges
  // in one extra pass because the next render sees dayMs === md.
  const selectedDateMs = selectedMail?.date ? new Date(selectedMail.date).getTime() : NaN;
  if (selectedId !== undefined && selectedPreview === undefined && !Number.isNaN(selectedDateMs)) {
    const md = startOfDay(selectedDateMs);
    if (md !== dayMs) setDayMs(md);
  }

  // The same header-vs-received mismatch can leave a deep-linked mail off even its
  // own header-date day. Pin the selection as an extra top row whenever it isn't on
  // the visible list, so an opened mail always has a row to expand — its 날짜 cell
  // tells the user where it actually sits.
  const rows = selectedMail !== undefined && selectedPreview === undefined ? [selectedMail, ...mails] : mails;

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
      {connected && (
        <DayPager
          label={dayLabel(dayMs, nowMs)}
          count={mails.length}
          canPrev={dayMs > minDayMs}
          canNext={dayMs < todayMs}
          atToday={dayMs === todayMs}
          onPrev={() => stepDay(-1)}
          onNext={() => stepDay(1)}
          onToday={() => goToDay(todayMs)}
        />
      )}
      <GridNotice query={query} count={rows.length} empty="이 날짜에는 메일이 없습니다.">
        <Grid
          columns={columns}
          rows={rows}
          getKey={(m) => String(m.id)}
          rowStyle={(m) => ({ fontWeight: m.isUnread ? 600 : 400 })}
          onRowClick={(m) => setSelectedId((current) => (String(current) === String(m.id) ? undefined : m.id))}
          isRowSelected={(m) => String(m.id) === String(selectedId)}
          rowTitle={(m) => `${m.subject ?? "(제목 없음)"} 읽기`}
          rowMenu={(m) => [
            { label: "읽음 처리", disabled: m.isUnread !== true, onSelect: () => markMailRead(m.id) },
            { label: "보관", disabled: busy, onSelect: () => void run(MAIL_RPC.archive, { id: m.id }) },
            { label: "휴지통", danger: true, disabled: busy, onSelect: () => void run(MAIL_RPC.trash, { id: m.id }) },
          ]}
          renderExpandedRow={() => (
            <MailDetail
              mail={selectedMail}
              query={detail.query}
              busy={busy}
              onMarkRead={() => {
                if (selectedId !== undefined) markMailRead(selectedId);
              }}
              onArchive={() => closeAfter(MAIL_RPC.archive)}
              onTrash={() => closeAfter(MAIL_RPC.trash)}
            />
          )}
        />
      </GridNotice>
    </>
  );
}

function applyLocalRead(mail: Mail, locallyReadIds: Set<string>): Mail {
  if (!locallyReadIds.has(String(mail.id)) || mail.isUnread !== true) return mail;
  return { ...mail, isUnread: false };
}

const LOCALLY_READ_STORAGE_KEY = "andromeda.mail.locallyReadIds";
const LOCALLY_READ_TTL_MS = 5 * 60_000;

interface LocallyReadEntry {
  id: string;
  at: number;
}

function readLocallyReadIds(now = Date.now()): Set<string> {
  try {
    const raw = localStorage.getItem(LOCALLY_READ_STORAGE_KEY);
    const parsed = raw ? (JSON.parse(raw) as unknown) : [];
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.flatMap((entry) => locallyReadIdFromEntry(entry, now)));
  } catch {
    return new Set();
  }
}

function locallyReadIdFromEntry(entry: unknown, now: number): string[] {
  // Backward-compatible with the first persisted shape: ["gmail-id", ...].
  if (typeof entry === "string") return [entry];
  if (!entry || typeof entry !== "object") return [];
  const { id, at } = entry as Partial<LocallyReadEntry>;
  if (typeof id !== "string" || typeof at !== "number") return [];
  return now - at <= LOCALLY_READ_TTL_MS ? [id] : [];
}

function writeLocallyReadIds(ids: Set<string>): void {
  try {
    const at = Date.now();
    localStorage.setItem(LOCALLY_READ_STORAGE_KEY, JSON.stringify([...ids].map((id) => ({ id, at }))));
  } catch {
    // Ignore private-mode/quota failures; the in-memory optimistic state still applies.
  }
}
