import {
  AppLocationAccuracy,
  AudioInputSource,
  ImageContainerProperty,
  ImageRawDataUpdate,
  waitForEvenAppBridge,
  TextContainerProperty,
  TextContainerUpgrade,
  CreateStartUpPageContainer,
} from "@evenrealities/even_hub_sdk";

import { resolveSettings, type GlanceSettings } from "./settings";
import {
  advanceCursor,
  alertSlots,
  clampCursor as clampCursorTo,
  connectionLabel,
  pollInterval,
  mergeWearStatus,
  shouldPoll,
  type WearStatus,
  nextDelayMs,
  payloadSignature,
  resolveSelectionIndex,
  skipPage,
  windowRange,
} from "./refresh";
import { dispatchHubEvent } from "./events";
import {
  onImuCapture,
  onInterpretToggle,
  onGlyphProbe,
  onListenToggle,
  onNavToggle,
  onSettingsSaved,
  renderPhoneUI,
  setPhoneStatus,
} from "./phone";
import { loadCachedGlance, saveCachedGlance } from "./cache";
import {
  CHUNK_MS,
  PcmBuffer,
  RECALL_BYTES,
  RECALL_MS,
  postAudio,
  subtitleLines,
} from "./interpret";
import {
  advanceNav,
  fetchRoute,
  glyphProbeLines,
  initialNavState,
  initialNavStateAt,
  formatMetres,
  navTextLines,
  remainingM,
  type NavCoord,
  type NavRoute,
  type NavState,
} from "./nav";
import {
  ASSIST_PERIOD_MS,
  ASSIST_PROMPT,
  assistLine,
  shouldAssistNow,
} from "./assist";
import {
  ARROW_H,
  ARROW_W,
  SPEED_H,
  SPEED_W,
  arrowPng,
  kmhFromMs,
  arrowText,
  maneuverArrow,
  speedPng,
} from "./arrow";
import {
  CAPTURE_MS,
  IMU_PACE_MS,
  postImuRecording,
  readImuSample,
  type ImuSample,
} from "./imu";
import {
  fetchGlance,
  fetchStatus,
  ackAlert,
  formatAlertDetail,
  formatGeneratedLabel,
  listLabel,
  pageTitle,
  type GlanceItem,
  type GlancePage,
  type GlancePayload,
  mirrorHud,
} from "./deneb";

// The phone screen is drawn FIRST, before the glasses bridge is awaited. That
// await is top-level: everything below it is dead until the glasses connect, so
// rendering afterwards would leave the phone blank at exactly the moment the
// operator needs it — when the connection is what is broken.
renderPhoneUI();

const bridge = await waitForEvenAppBridge();

type Screen =
  | "setup"
  | "page"
  | "detail"
  | "status"
  | "confirmAck"
  | "notice"
  | "interpret"
  | "nav"
  | "recall";

const PAGE_ORDER = ["home", "alerts", "cal", "todo"] as const;
const LIST_PAGE_IDS = new Set(["home", "alerts"]);

let settings: GlanceSettings = { baseUrl: "", token: "" };
let screen: Screen = "setup";
let busy = false;
let pages: GlancePage[] = [];
let items: GlanceItem[] = [];
let pageIndex = 0;
let detailIndex = -1;
let lastGenerated = "";
let lastCached = false;
/** "일정 2 · 할 일 3" — what is behind the alert page, straight from the gateway. */
let lastCounts = "";
/** "지금 금호타이어 · 종료 20분" — the lead line, straight from the gateway. */
let lastNow = "";
/** Notice ids already put on the glass, so a re-offered one is not shown twice. */
let shownNoticeId = "";
/** Last thing the glasses said about themselves; undefined until they say it. */
let wearStatus: WearStatus | undefined;
/** Live navigation state. navRoute is null unless a route is loaded. */
let navRoute: NavRoute | null = null;
let navState: NavState = initialNavState();
let navPos: NavCoord | null = null;
let navDest = "";
/**
 * Recall: a rolling window of what was just said, kept while listening.
 *
 * Separate buffer from the interpretation one because the two want opposite
 * things — interpretation drains each window as it sends it, recall must retain
 * the last half-minute so a tap can look backwards. Deneb can afford to listen
 * continuously in a way the paid-ASR plugins cannot: MOSS runs on srv4, so the
 * marginal cost of holding the mic open is electricity.
 */
let listening = false;
const recallPcm = new PcmBuffer(RECALL_BYTES);
let recallBusy = false;
/** Assist pacing: last analysis start, and the line currently on the glass. */
let assistAt = 0;
let assistBusy = false;
let assistShown = "";

/** Live interpretation state. Off unless the operator turned it on. */
let interpreting = false;
let interpretLang = "ko";
const pcm = new PcmBuffer();
let subtitles: string[] = [];
let sending = false;
/** Non-empty while a labelled IMU window is being recorded. */
let imuLabel = "";
let imuSamples: ImuSample[] = [];
/**
 * Which alert the cursor is on, on an alert page.
 *
 * The app owns this instead of the host's list container, for ONE measured
 * reason: the host list moves its own selection on a swipe and then emits
 * nothing at the end of the list, so the app could never page past the alerts
 * — with a tap opening a detail and a double-tap exiting, cal/todo were
 * unreachable whenever alerts existed (run 30179426969: "list paging: STOPS at
 * the end of the list").
 *
 * It does NOT report the selection wrongly. That was a wrong guess of mine from
 * never seeing `currentSelectItemIndex` on a listEvent — proto3 omits zero
 * scalars, so an absent index simply means item 0, and the same run measured
 * "list tap opens the SELECTED item". `resolveSelectionIndex`'s absent→0
 * fallback is exactly right for that wire format.
 */
let listCursor = 0;
let refreshTimer: ReturnType<typeof setTimeout> | undefined;
let consecutiveFailures = 0;
let lastSignature = "";
/** Connection marker currently ON SCREEN, so it is only redrawn when it flips. */
let shownConnection = "";
/** True while the screen is showing a saved glance rather than a live one. */
let servingCache = false;
let stopped = false;
let paused = false;

const mainText = new TextContainerProperty({
  xPosition: 0,
  yPosition: 0,
  width: 576,
  height: 288,
  borderWidth: 0,
  borderColor: 5,
  paddingLength: 4,
  containerID: 1,
  containerName: "main",
  content: "Deneb\n\n설정 확인 중…",
  isEventCapture: 1,
});

// Every container the app will ever use is declared HERE, at startup, and NONE
// of them sets zOrderIndex.
//
// Even's own image template — the reference that demonstrably renders bitmaps on
// this hardware — uses no zOrderIndex at all. This code set it on all four
// containers and every image push came back sendFailed, for a single 2.4KB PNG,
// paced and retried. Matching the working reference is worth more than the
// stacking control, which nothing here actually needs: the containers do not
// overlap.
//
// The nav screen used to add its image containers through rebuildPageContainer
// and the arrow never appeared on the device. Even's own image template creates
// image containers in the startup page, so that is what this does — switching
// modes now only changes CONTENT, never the page structure, which removes a
// whole class of "did the host accept the rebuild" failure.
//
// An image container with no data drawn into it renders nothing, so these are
// invisible until navigation pushes a frame.
const navText = new TextContainerProperty({
  xPosition: 0,
  yPosition: ARROW_H + 10,
  width: 576,
  height: 288 - ARROW_H - 10,
  borderWidth: 0,
  borderColor: 0,
  paddingLength: 4,
  containerID: 2,
  containerName: "navText",
  content: " ",
  isEventCapture: 0,
});

const arrowImage = new ImageContainerProperty({
  xPosition: 8,
  yPosition: 8,
  width: ARROW_W,
  height: ARROW_H,
  containerID: 3,
  containerName: "arrow",
});

const started = await bridge.createStartUpPageContainer(
  new CreateStartUpPageContainer({
    containerTotalNum: 3,
    textObject: [mainText, navText],
    imageObject: [arrowImage],
  }),
);
if (started !== 0) {
  // Loud everywhere it can be: a failed startup page is indistinguishable from
  // a dead app on the glass, and the console is unreachable from a device
  // report. The phone screen renders before the bridge is awaited, so it is
  // still up to carry this.
  console.error("createStartUpPageContainer failed", started);
  setPhoneStatus({
    line: `안경 화면 생성 실패 (${started}) — 컨테이너 선언을 확인하세요.`,
    tone: "bad",
  });
}

bridge.onEvenHubEvent((event) => {
  // Audio rides the same callback. It is high-rate, so it is handled before
  // the intent mapping rather than through it.
  if (imuLabel) {
    const s = readImuSample(event);
    if (s) {
      imuSamples.push(s);
      return;
    }
  }
  const audio = (event as { audioEvent?: { audioPcm?: Uint8Array } })
    ?.audioEvent;
  if (audio?.audioPcm) {
    // The recall window fills whenever listening, independent of
    // interpretation: the wearer may want to look back at a conversation they
    // were not having translated.
    if (listening) {
      recallPcm.push(audio.audioPcm);
      void assistTick();
    }
    if (!interpreting) return;
    pcm.push(audio.audioPcm);
    if (pcm.ready()) void flushAudio();
    return;
  }
  // Mapping lives in events.ts as a pure function so every host event shape —
  // including the lifecycle events the simulator cannot inject — is unit
  // testable. This block only executes the intent.
  const intent = dispatchHubEvent(event);
  switch (intent.kind) {
    case "tap":
      void onTap();
      break;
    case "openDetail":
      void openDetail(intent.index);
      break;
    case "nextPage":
      void onSwipeNext();
      break;
    case "prevPage":
      void onSwipePrev();
      break;
    case "shutdown":
      // Contextual on purpose. There is no fifth gesture, and a detail is the
      // only place where "I am done with this" has a second meaning. Everywhere
      // else — list, pages, status, setup — a double-tap still exits.
      if (screen === "recall") {
        // Listening holds the mic open; a double-tap here means "stop
        // listening", not "quit the app" — quitting with the mic still open is
        // the one outcome nobody wants.
        void stopListening();
      } else if (screen === "detail") {
        // Straight to the ack, no confirmation screen (operator's call,
        // 2026-07-26). The guard was there because a double-tap exits the app
        // everywhere else, so a stray one here would write — but an ack only
        // marks a card read, it is undoable in the native feed, and paying two
        // gestures for it on a four-gesture device is the worse trade.
        void doAck();
      } else {
        shutdown();
      }
      break;
    case "stopLoop":
      // SYSTEM_EXIT (the wearer confirmed the exit dialog) or ABNORMAL_EXIT.
      // This is the ONLY place teardown belongs: the double-tap itself must not
      // tear anything down, because mode 1 lets the wearer cancel.
      void teardown();
      break;
    case "pause":
      pauseLoop();
      break;
    case "resume":
      void resumeLoop();
      break;
    case "ignore":
      break;
  }
});

onImuCapture((label) => void captureImu(label));

bridge.onAppLocationChanged((loc) => {
  // Only while navigating. Deneb never records where the wearer is
  // otherwise — the stream is opened by startNav and closed by stopNav.
  if (!navRoute) return;
  void onNavFix({ lat: loc.latitude, lon: loc.longitude });
  // Speed bitmap disabled: both images came back sendFailed even after pacing
  // and a retry, and speed is the cheaper of the two to lose. One image at a
  // time is the honest experiment — if the arrow lands alone, the link simply
  // cannot carry two.
  // void pushSpeed(kmhFromMs(loc.speed));
});

onListenToggle((on) => {
  void (on ? startListening() : stopListening());
});

onGlyphProbe(() => {
  void (async () => {
    pauseLoop();
    await showText(glyphProbeLines().join("\n") + "\n\n탭=목록");
  })();
});

onNavToggle((on, destination, mode) => {
  void (on ? startNav(destination, mode) : stopNav());
});

onInterpretToggle((on, lang) => {
  void (on ? startInterpreting(lang) : stopInterpreting());
});

onSettingsSaved((next) => {
  settings = next;
  consecutiveFailures = 0;
  servingCache = false;
  void refreshGlance(true);
  scheduleRefresh();
});

// The glasses report whether they are on a face, in the case, or nearly flat.
// This is what the polling loop should have been keyed on all along — see
// shouldPoll for why guessing at foreground events was the wrong instrument.
/**
 * pollDeviceStatus asks the host outright instead of waiting to be told.
 *
 * onDeviceStatusChanged never fired on the device — the status screen stayed at
 * "착용 상태 미보고" through a whole session and the HUD mirror carried no wear
 * data at all. getDeviceInfo() returns the same DeviceStatus as a QUERY, which
 * does not depend on the host choosing to push.
 */
async function pollDeviceStatus(): Promise<void> {
  try {
    const info = await bridge.getDeviceInfo();
    const st = info?.status;
    if (!st) return;
    wearStatus = mergeWearStatus(wearStatus, {
      isWearing: st.isWearing,
      isInCase: st.isInCase,
      isCharging: st.isCharging,
      batteryLevel: st.batteryLevel,
    });
  } catch {
    // Never fatal: shouldPoll fails open, so an unknown wear state costs a
    // slightly eager refresh and nothing else.
  }
}

void pollDeviceStatus();

bridge.onDeviceStatusChanged((status) => {
  const before = shouldPoll(wearStatus);
  // Merged, not assigned: the SDK defaults absent fields to "not worn, 0%", so
  // a connection-only event would otherwise wipe a perfectly good reading and
  // tell the wearer their glasses are off and flat while they are wearing them.
  wearStatus = mergeWearStatus(wearStatus, {
    isWearing: status?.isWearing,
    isInCase: status?.isInCase,
    isCharging: status?.isCharging,
    batteryLevel: status?.batteryLevel,
  });
  if (shouldPoll(wearStatus) === before) return;
  if (before) {
    pauseLoop();
    return;
  }
  // Back on the face: they are looking NOW, so do not make them wait a cycle.
  void resumeLoop();
});

void boot();

async function boot(): Promise<void> {
  settings = await resolveSettings();
  if (needsSetup(settings)) {
    screen = "setup";
    await showText(setupCopy());
    // Still schedule: the wearer may seed the app while it is open, and the
    // setup screen's own poll is what notices.
    scheduleRefresh();
    return;
  }
  screen = "page";
  pageIndex = 0;
  await refreshGlance(false);
  scheduleRefresh();
}

/** stopLoop cancels the background poll for good. */
function stopLoop(): void {
  stopped = true;
  clearRefreshTimer();
}

/** pauseLoop cancels the pending poll but leaves the loop resumable. */
function pauseLoop(): void {
  paused = true;
  clearRefreshTimer();
}

async function resumeLoop(): Promise<void> {
  if (stopped) return;
  paused = false;
  // Coming back to the foreground, the wearer wants current data, not whatever
  // was on screen when they looked away.
  await refreshGlance(false, true);
  scheduleRefresh();
}

function clearRefreshTimer(): void {
  if (refreshTimer !== undefined) {
    clearTimeout(refreshTimer);
    refreshTimer = undefined;
  }
}

/**
 * shutdown asks the system to close the glasses page.
 *
 * Mode 1, and NOTHING else in this function. Both halves matter:
 *
 * Mode 1 is the system exit-confirmation dialog, which the platform requires on
 * a root page — "Mode `0` (immediate exit) is not acceptable on the root page",
 * and a QA reviewer rejects mode 0 outright (docs/ship/app-submission,
 * docs/build/page-lifecycle, reference/glossary all say so). Mode 0 is for
 * internal pages, where the user has already confirmed. This call site briefly
 * used mode 0 on the strength of the SDK README's example, which contradicts
 * all three doc pages; the README is wrong and nothing acknowledges it.
 *
 * And no teardown here, deliberately. Mode 1 means the wearer can still cancel;
 * stopping the poll (or the mic, or the location stream) before the dialog
 * leaves a live page that has quietly gone deaf when they tap "cancel".
 * Teardown belongs in the SYSTEM_EXIT / ABNORMAL_EXIT handlers, which fire only
 * once the exit is real.
 */
function shutdown(): void {
  void bridge.shutDownPageContainer(1);
}

/**
 * teardown releases everything the app holds, on a confirmed exit.
 *
 * The hardware matters more than the timer: interpretation holds the glasses
 * microphone and navigation holds a location stream, and neither is closed by
 * the page going away. Leaving them open costs battery and, for the location
 * feed, keeps a sensor running that the wearer believes they turned off.
 */
async function teardown(): Promise<void> {
  stopLoop();
  const wasInterpreting = interpreting;
  const wasNavigating = navRoute !== null;
  interpreting = false;
  navRoute = null;
  try {
    if (wasInterpreting) await bridge.audioControl(false);
    if (wasNavigating) await bridge.stopAppLocationUpdates();
  } catch {
    // Exiting anyway — a failed release must not stall the exit path.
  }
}

function needsSetup(s: GlanceSettings): boolean {
  return !s.baseUrl || !s.token;
}

function setupCopy(): string {
  return [
    "Deneb Glance",
    "",
    "설정 필요 (QR 시드)",
    "evenhub qr --url",
    '"http://<lan>:5173/?seed=<…>"',
    "또는 pack 시 runtime-config.json",
    "",
    "탭=재확인 / 더블탭=종료",
  ].join("\n");
}

async function onTap(): Promise<void> {
  if (screen === "recall") {
    await recallNow();
    return;
  }
  if (screen === "nav") {
    await stopNav();
    return;
  }
  if (screen === "interpret") {
    await stopInterpreting();
    return;
  }
  if (screen === "notice") {
    screen = "page";
    await renderCurrentPage();
    return;
  }
  if (screen === "confirmAck") {
    await doAck();
    return;
  }
  if (screen === "setup") {
    settings = await resolveSettings();
    if (!needsSetup(settings)) {
      screen = "page";
      pageIndex = 0;
      await refreshGlance(true);
      scheduleRefresh();
      return;
    }
    await showText(setupCopy());
    return;
  }
  if (screen === "detail") {
    screen = "page";
    detailIndex = -1;
    await renderCurrentPage();
    return;
  }
  if (screen === "status") {
    screen = "page";
    await renderCurrentPage();
    return;
  }
  // On an alert page a tap opens what the CURSOR is on — the primary action,
  // and now unambiguous because the app owns the cursor. Everywhere else a tap
  // is a manual refresh.
  if (onAlertPage()) {
    await openDetail(clampCursor());
    return;
  }
  await refreshGlance(true);
  // A manual refresh re-anchors the schedule: after a backoff the next poll
  // would otherwise still be minutes away even though the link just worked.
  scheduleRefresh();
}

/**
 * openDetail shows one alert full-screen.
 *
 * NO `busy` guard, deliberately. It reads `items` out of memory and touches no
 * network, so gating it on an in-flight poll only served to DROP the wearer's
 * tap: the smoke harness caught exactly that (run 30171967100 — the tap was
 * delivered as a listEvent, the screen never changed, and the next tap 2.5s
 * later worked). A poll runs every 45s and may take up to the 10s deadline, so
 * that is a wide window in which the primary interaction silently does nothing.
 *
 * What the guard was really protecting against — a poll landing mid-read and
 * yanking the wearer back to the list — is handled in refreshGlance instead, by
 * re-reading the live screen after the fetch rather than trusting its entry
 * snapshot.
 */
async function openDetail(index: unknown): Promise<void> {
  if (!LIST_PAGE_IDS.has(currentPageId())) return;
  // Normally called with the app's own cursor. The absent-index fallback still
  // matters because the host may deliver a `listEvent` (it did while alerts
  // were a list container, always WITHOUT currentSelectItemIndex) — opening the
  // top alert beats a dead tap. A present-but-nonsensical index is refused.
  const i = resolveSelectionIndex(index, items.length);
  if (i < 0) return;
  detailIndex = i;
  // Keep the cursor on whatever is being read, so leaving the detail lands back
  // on that alert rather than wherever the cursor happened to be.
  listCursor = i;
  screen = "detail";
  await showText(
    formatAlertDetail(items[i], { index: i, total: items.length }),
  );
}

/**
 * showNoticeIfNew puts one line from Deneb on the glass.
 *
 * This is the one place a poll is ALLOWED to take the screen, and the reason is
 * that the wearer asked for it: a spoken turn that ran past the 15s bridge
 * deadline gets "결과는 폰 데네브에서 이어서 볼게요" and the real answer used to
 * be stranded there. It rides back on the next poll instead.
 *
 * Keyed by id and shown once — the gateway keeps offering it for a while so a
 * dropped poll cannot lose the answer, which means the plugin must be the one
 * that remembers.
 */
async function showNoticeIfNew(notice: GlancePayload["notice"]): Promise<void> {
  if (!notice || notice.id === shownNoticeId) return;
  shownNoticeId = notice.id;
  // Not over a confirm prompt: that screen is waiting on a destructive answer,
  // and replacing it would leave the wearer's next tap pointing at nothing.
  if (screen === "confirmAck") return;
  screen = "notice";
  await showText(`${notice.text}\n\n탭=닫기`);
}

/**
 * stepDetail moves between alert details without going back to the list.
 *
 * Reading a morning's alerts used to mean detail → list → tap → detail for each
 * one. On a HUD that is three interactions per alert; a swipe is one. Stepping
 * past either end leaves the detail, which keeps the list reachable without a
 * separate gesture.
 */
async function stepDetail(dir: 1 | -1): Promise<void> {
  const moved = advanceCursor(detailIndex, items.length, dir);
  if (moved !== "page") {
    await openDetail(moved);
    return;
  }
  screen = "page";
  detailIndex = -1;
  await renderCurrentPage();
}

/**
 * askAck puts a confirmation between the wearer and an irreversible write.
 *
 * Acking removes the alert from the glance for good, and the gesture that
 * reaches it — a double-tap — is the same one that exits the app everywhere
 * else. One stray double-tap must not clear something unread, so the actual
 * write needs a second, deliberate gesture.
 */
async function askAck(): Promise<void> {
  const item = items[detailIndex];
  if (!item) return;
  screen = "confirmAck";
  await showText(
    [
      "Deneb · 확인 처리",
      "",
      item.title,
      "",
      "이 알림을 확인함으로 할까요?",
      "",
      "탭=예 · ↓취소",
    ].join("\n"),
  );
}

/** cancelAck returns to what was being read, unchanged. */
async function cancelAck(): Promise<void> {
  if (detailIndex >= 0 && items[detailIndex]) {
    await openDetail(detailIndex);
    return;
  }
  screen = "page";
  await renderCurrentPage();
}

async function doAck(): Promise<void> {
  const item = items[detailIndex];
  if (!item) {
    screen = "page";
    await renderCurrentPage();
    return;
  }
  await showText("Deneb · 확인 처리\n\n처리 중…");
  try {
    await ackAlert(settings, item.id);
  } catch (err) {
    // Loud on purpose: unlike a background poll, the wearer asked for this and
    // the alert is still there. Silence would read as success.
    const msg = err instanceof Error ? err.message : String(err);
    screen = "detail";
    await showText(
      `Deneb · 확인 처리\n\n실패: ${msg}\n\n탭=목록 · 더블탭=재시도`,
    );
    return;
  }
  // Drop it locally rather than waiting for the next poll: the wearer acted and
  // the screen has to agree with them now. The refresh below reconciles.
  items = items.filter((it) => it.id !== item.id);
  detailIndex = -1;
  listCursor = clampCursor();
  screen = "page";
  await renderCurrentPage();
  await refreshGlance(true);
  scheduleRefresh();
}

async function onSwipeNext(): Promise<void> {
  if (screen === "nav") return;
  // No `busy` guard: paging between already-loaded pages is local. See openDetail.
  if (screen === "interpret") return;
  if (screen === "notice") return void (await onTap());
  if (screen === "confirmAck") return void (await cancelAck());
  if (screen === "setup") return;
  if (screen === "detail") {
    await stepDetail(1);
    return;
  }
  if (screen === "status") {
    screen = "page";
    pageIndex = 0;
    await renderCurrentPage();
    return;
  }
  // On an alert page the swipe walks the cursor DOWN the alerts first, and
  // only leaves the page once it is on the last one. That boundary belongs to
  // the app now: the host list used to keep it and emit nothing, which stranded
  // every page after the alerts.
  if (onAlertPage()) {
    const moved = advanceCursor(listCursor, items.length, 1);
    if (moved !== "page") {
      listCursor = moved;
      await renderCurrentPage();
      return;
    }
  }
  let found = -1;
  for (let i = pageIndex + 1; i < pages.length; i++) {
    if (!isPageEmpty(pages[i])) {
      found = i;
      break;
    }
  }
  if (found >= 0) {
    pageIndex = found;
    listCursor = 0;
    await renderCurrentPage();
    return;
  }
  await showStatus();
}

async function onSwipePrev(): Promise<void> {
  if (screen === "nav") return;
  // No `busy` guard: paging between already-loaded pages is local. See openDetail.
  if (screen === "interpret") return;
  if (screen === "notice") return void (await onTap());
  if (screen === "confirmAck") return void (await cancelAck());
  if (screen === "setup") return;
  if (screen === "detail") {
    await stepDetail(-1);
    return;
  }
  if (screen === "status") {
    screen = "page";
    const last = [...pages.keys()]
      .reverse()
      .find((i) => !isPageEmpty(pages[i]));
    pageIndex = last ?? Math.max(0, pages.length - 1);
    await renderCurrentPage();
    return;
  }
  // Symmetric: walk the cursor back up the alerts before leaving the page.
  if (onAlertPage()) {
    const moved = advanceCursor(listCursor, items.length, -1);
    if (moved !== "page") {
      listCursor = moved;
      await renderCurrentPage();
      return;
    }
  }
  let found = -1;
  for (let i = pageIndex - 1; i >= 0; i--) {
    if (!isPageEmpty(pages[i])) {
      found = i;
      break;
    }
  }
  if (found >= 0) {
    pageIndex = found;
    listCursor = 0;
    await renderCurrentPage();
    return;
  }
  // Past the top of the first page — the one gesture that had no meaning, and
  // the alerts page needs it: a tap there opens a detail, so there was no way
  // left to force a refresh from the page the wearer is actually on. Pulling up
  // past the top is the familiar idiom, and it re-anchors the schedule, which
  // matters after a backoff has pushed the next poll minutes out.
  await refreshGlance(true);
  scheduleRefresh();
}

/**
 * showStatus draws the diagnostics screen.
 *
 * It used to return early while `busy`, which meant a swipe onto it during a
 * background poll did nothing at all — the same dropped-input failure as the
 * tap in openDetail. The navigation now always happens; only the fetch is
 * skipped when one is already in flight, and the screen says so.
 */
async function showStatus(): Promise<void> {
  if (needsSetup(settings)) {
    screen = "setup";
    await showText(setupCopy());
    return;
  }
  screen = "status";
  if (busy) {
    await showText("Deneb · 상태\n\n확인 중…\n\n탭=페이지로");
    return;
  }
  busy = true;
  try {
    const st = await fetchStatus(settings);
    const host = settings.baseUrl.replace(/^https?:\/\//, "");
    await showText(
      [
        "Deneb · 상태",
        "",
        st.ok ? "브리지 OK" : "브리지 이상",
        st.chatReady ? "챗 준비됨" : "챗 미준비",
        `세션 ${st.session || "glasses:main"}`,
        wearLine(),
        host,
        "",
        "↓홈 · ↑이전 · 탭=페이지",
      ].join("\n"),
    );
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    await showText(`Deneb · 상태\n\n오류: ${msg}\n\n탭=페이지로`);
  } finally {
    busy = false;
  }
}

/**
 * refreshGlance pulls a new glance and redraws.
 *
 * `silent` marks a BACKGROUND poll (the timer). A background poll must be
 * invisible: no loading text, no redraw when nothing changed, no error screen.
 * The old loop wrote "불러오는 중…" over the display every 45 seconds — in the
 * wearer's field of view, and on top of a detail they were reading — then
 * rebuilt the container even when the payload was byte-identical.
 */
async function refreshGlance(fresh: boolean, silent = false): Promise<void> {
  void pollDeviceStatus();
  if (busy) return;
  busy = true;
  const keepId = currentPageId();
  const keepDetailId =
    screen === "detail" && detailIndex >= 0 ? items[detailIndex]?.id : "";
  try {
    // Only while unseeded: resolveSettings re-parses the URL, re-reads
    // localStorage and can re-fetch runtime-config.json, and it ran on EVERY
    // 45s poll for the life of the app. Once the settings are complete they
    // cannot change without reloading the WebView, which restarts boot anyway.
    if (needsSetup(settings)) settings = await resolveSettings();
    if (needsSetup(settings)) {
      setPhoneStatus({
        line: "설정 필요 — 아래에 주소와 토큰을 넣으세요.",
        tone: "warn",
      });
      if (screen !== "setup") {
        screen = "setup";
        await showText(setupCopy());
      }
      return;
    }
    if (!silent) {
      if (screen !== "detail") screen = "page";
      await showText("Deneb\n\n불러오는 중…");
    }
    const payload = await fetchGlance(settings, { fresh });
    consecutiveFailures = 0;
    servingCache = false;
    setPhoneStatus({
      line: `연결됨 · 알림 ${payload.items.length}건`,
      tone: "ok",
    });
    saveCachedGlance(payload);

    // Re-read the LIVE screen, not the snapshot taken before the request. The
    // wearer can tap or swipe while a poll is in flight — now that navigation
    // is no longer blocked by `busy`, that is expected — and a background poll
    // must land on where they are NOW, not where they were 10 seconds ago.
    const liveDetailId =
      screen === "detail" && detailIndex >= 0 ? items[detailIndex]?.id : "";
    const anchorId = currentPageId() || keepId;
    const anchorDetailId = liveDetailId || keepDetailId;

    const signature = payloadSignature(payload);
    // A recovery is a change even when the payload is not: the header is
    // showing "연결 끊김" and has to stop.
    const unchanged =
      signature === lastSignature && connectionLabel(0) === shownConnection;
    lastSignature = signature;
    applyPayload(payload, anchorId);

    // Nothing moved and nobody asked — leave the screen exactly as it is.
    // This is what makes the background poll invisible.
    if (silent && unchanged)
      return void (await showNoticeIfNew(payload.notice));

    if (anchorDetailId) {
      const idx = items.findIndex((it) => it.id === anchorDetailId);
      if (idx >= 0) {
        detailIndex = idx;
        screen = "detail";
        // Same shape openDetail draws, counter included — a background poll must
        // not silently swap the wearer's screen for a differently-formatted one.
        await showText(
          formatAlertDetail(items[idx], { index: idx, total: items.length }),
        );
        return;
      }
      // The alert being read is gone. On a background poll, say so instead of
      // silently swapping the wearer's screen for a list they did not ask for.
      if (silent) {
        await showText("이 알림은 처리됐습니다.\n\n탭=목록으로");
        screen = "detail";
        detailIndex = -1;
        return;
      }
    }
    screen = "page";
    detailIndex = -1;
    await renderCurrentPage();
    await showNoticeIfNew(payload.notice);
  } catch (err) {
    consecutiveFailures++;
    setPhoneStatus({
      line: `연결 실패 (${consecutiveFailures}회) · ${err instanceof Error ? err.message : String(err)}`,
      tone: consecutiveFailures >= 2 ? "bad" : "warn",
    });
    // A background failure stays off the display: the wearer did not ask, and
    // an unreachable gateway would otherwise flash an error every cycle while
    // they are simply out of network range.
    //
    // Exactly ONE exception, and only once per outage: when the link goes
    // properly down the header has to say so, or a glance reads quarter-hour-old
    // alerts as current. Not from a detail — that would yank someone out of
    // what they are reading — and not from the status screen, which is its own
    // deliberate view.
    // Nothing has ever rendered — this is the cold open, out of range. A saved
    // glance is what the wearer actually wants there, and it is honest as long
    // as it is labelled: renderCurrentPage puts "오프라인 · 저장본" in the header.
    if (pages.length === 0 && !servingCache) {
      const cached = loadCachedGlance();
      if (cached) {
        servingCache = true;
        lastSignature = payloadSignature(cached);
        applyPayload(cached, "");
        screen = "page";
        await renderCurrentPage();
        return;
      }
    }
    if (silent) {
      if (
        screen === "page" &&
        connectionLabel(consecutiveFailures, servingCache) !== shownConnection
      ) {
        await renderCurrentPage();
      }
      return;
    }
    const msg = err instanceof Error ? err.message : String(err);
    await showText(`Deneb\n\n오류: ${msg}\n\n탭=재시도 / ↓다음`);
  } finally {
    busy = false;
  }
}

function applyPayload(payload: GlancePayload, keepId: string): void {
  // Which alert the cursor was on, by id — a poll must not move it under the
  // wearer just because the gateway reordered or dropped something above it.
  const cursorId = items[clampCursor()]?.id;

  pages = sortPages(payload.pages);
  if (pages.length === 0) {
    pages = [{ id: "home", title: "알림", text: payload.text }];
  }
  items = payload.items || [];
  lastGenerated = payload.generated || "";
  lastCached = !!payload.cached;
  lastCounts = payload.counts || "";
  lastNow = payload.now || "";
  const idx = pages.findIndex((p) => p.id === keepId);
  pageIndex = idx >= 0 ? idx : 0;

  const movedTo = cursorId ? items.findIndex((it) => it.id === cursorId) : -1;
  // Gone (handled/expired) → back to the top, which is the highest priority.
  listCursor = movedTo >= 0 ? movedTo : 0;
}

function sortPages(raw: GlancePage[]): GlancePage[] {
  const byId = new Map(raw.map((p) => [p.id, p]));
  const ordered: GlancePage[] = [];
  for (const id of PAGE_ORDER) {
    const p = byId.get(id);
    if (p) ordered.push(p);
  }
  for (const p of raw) {
    if (!PAGE_ORDER.includes(p.id as (typeof PAGE_ORDER)[number])) {
      ordered.push(p);
    }
  }
  return ordered;
}

function currentPageId(): string {
  return pages[pageIndex]?.id || "home";
}

async function renderCurrentPage(): Promise<void> {
  shownConnection = connectionLabel(consecutiveFailures, servingCache);
  const page = pages[pageIndex] || {
    id: "home",
    title: "알림",
    text: "새 알림 없음",
  };
  const title = page.title || pageTitle(page.id);
  if (LIST_PAGE_IDS.has(page.id) && items.length > 0) {
    await showAlertList(title);
    return;
  }
  const stamp = formatGeneratedLabel(lastGenerated, lastCached);
  const nav = `${pageIndex + 1}/${Math.max(pages.length, 1)}`;
  const footer = [
    "↓다음 · 탭=새로고침",
    stamp || nav,
    connectionLabel(consecutiveFailures, servingCache),
  ]
    .filter(Boolean)
    .join(" · ");
  await showText(`${title}\n\n${page.text}\n\n${footer}`);
}

/**
 * showAlertList draws the alerts on a plain TEXT container with a cursor the
 * app controls, rather than handing the items to a host list container.
 *
 * The host list looked like the obvious fit and was the wrong choice on both
 * counts it was picked for:
 *
 *   - it never told the app which item was highlighted (`listEvent` carried no
 *     `currentSelectItemIndex`), so a tap always opened alert #1 no matter what
 *     the wearer had scrolled to — they read something they did not choose;
 *   - it kept the scroll to itself, emitting nothing at the end of the list, so
 *     the cal/todo pages could not be reached at all while alerts existed.
 *
 * Text containers do deliver scroll (`textEvent{eventType:2}`) and taps, so
 * moving the cursor into the app fixes both — and makes them assertable in the
 * smoke harness instead of being host behaviour nobody can verify.
 */
async function showAlertList(title: string): Promise<void> {
  const stamp = formatGeneratedLabel(lastGenerated, lastCached);
  const cursor = clampCursor();
  const position = items.length > 1 ? ` (${cursor + 1}/${items.length})` : "";
  // The counts line is the briefing the wearer used to lose: the gateway builds
  // a home page that opens with a clock and "일정 2 · 할 일 3", and this page
  // draws itself from `items` and never renders that text at all.
  // The lead line is the top of the glass — the part peripheral vision actually
  // reads. What is happening now beats a count of what is behind this page,
  // which in turn beats the app's own name (which used to own this line).
  const lead = lastNow.trim() || lastCounts.trim();

  // Only a window of the alerts fits, and how many is a budget, not a constant
  // — see HUD_LINES. The window carries no "N건 더" lines of its own; the
  // (3/8) counter in the meta line already says there is more, for free.
  const { start, end } = windowRange(cursor, items.length, alertSlots(!!lead));
  const lines = items
    .slice(start, end)
    // '>' and not '▸': a CI frame showed the nicer glyph rendering as NOTHING on
    // this font, which left the cursor invisible and a tap unpredictable
    // whenever there was more than one alert.
    .map((it, i) => `${start + i === cursor ? ">" : " "}${listLabel(it)}`);

  await showText(
    [
      ...(lead ? [lead] : []),
      [
        `${title} ${items.length}${position}`,
        clockLabel(),
        stamp,
        connectionLabel(consecutiveFailures, servingCache),
      ]
        .filter(Boolean)
        .join(" · "),
      "",
      ...lines,
      [
        cursor < items.length - 1
          ? "탭=상세 · ↓다음알림"
          : "탭=상세 · ↓다음페이지",
        atTop() ? "↑새로고침" : "",
      ]
        .filter(Boolean)
        .join(" · "),
    ].join("\n"),
  );
}

/**
 * clockLabel is drawn from the DEVICE clock, not the payload.
 *
 * The gateway stamps its own time into the home page text, but that text is up
 * to a poll interval old by the time it is on the glass — a clock that lags 45
 * seconds reads as broken. The wearer's own clock is right, and free.
 */
function clockLabel(): string {
  const now = new Date();
  const wd = ["일", "월", "화", "수", "목", "금", "토"][now.getDay()];
  const hh = String(now.getHours()).padStart(2, "0");
  const mm = String(now.getMinutes()).padStart(2, "0");
  return `${now.getMonth() + 1}/${now.getDate()} ${wd} ${hh}:${mm}`;
}

const NAV_ARROW_ID = 3;
const NAV_SPEED_ID = 4;

/**
 * IMAGE_MIN_GAP_MS — floor between BLE image transfers.
 *
 * Measured from the device through the HUD mirror: pushes came back
 * `sendFailed`. The PNG was fine — a bad one returns imageException or
 * imageToGray4Failed — and text updates kept working throughout, so the link was
 * alive and it is images specifically that could not keep up. They go out
 * fragmented and LZ4-compressed while the speed bitmap was being pushed on
 * every position fix.
 */
// The open-source even-img-benchmark pushes frames back to back with no gap at
// all and measures them succeeding, so rate was never the problem — the 4s floor
// here was a wrong guess. Kept small only to avoid piling transfers behind a
// stalled one.
const IMAGE_MIN_GAP_MS = 300;
let lastImageAt = 0;

/**
 * sendImage serializes, paces and retries one image transfer.
 *
 * Serial because updateImageRawData is global rather than per container, paced
 * because the link drops transfers that arrive too close together, and retried
 * once because a dropped frame is transient — the alternative is a blank arrow
 * for the rest of the leg.
 */
async function sendImage(
  name: string,
  containerID: number,
  bytes: Uint8Array,
): Promise<string> {
  const gap = Date.now() - lastImageAt;
  if (gap < IMAGE_MIN_GAP_MS) {
    await new Promise((r) => setTimeout(r, IMAGE_MIN_GAP_MS - gap));
  }
  let res = "";
  for (let attempt = 0; attempt < 2; attempt += 1) {
    lastImageAt = Date.now();
    res = String(
      await bridge.updateImageRawData(
        new ImageRawDataUpdate({
          containerID,
          containerName: name,
          // number[], not Uint8Array. The SDK says it converts either way
          // ("imageData 建议传 number[]（宿主 List<int> 最好接）"), but Visionote —
          // a working image-only G2 app — passes a plain array, and every push
          // from here has come back sendFailed. Following the code that works
          // beats following the doc that says it should not matter.
          imageData: Array.from(bytes),
        }),
      ),
    );
    if (res === "success") break;
    await new Promise((r) => setTimeout(r, IMAGE_MIN_GAP_MS));
  }
  noteImagePush(name, bytes.length, res);
  return res;
}

/**
 * enterNavPage switches to the navigation layout by CHANGING CONTENT ONLY.
 *
 * No rebuildPageContainer: the containers are declared at startup. The device
 * showed no arrow at all while this went through a rebuild, and Even's own image
 * template only ever creates image containers in the startup page — so the page
 * structure is now fixed for the app's lifetime and modes just fill it.
 */
async function enterNavPage(): Promise<boolean> {
  await showText(" ");
  await showNavText("경로 계산 중…");
  return true;
}

/** leaveNavPage clears the nav containers and hands the screen back. */
async function leaveNavPage(): Promise<void> {
  await showNavText(" ");
}

/**
 * pushArrow sends one arrow frame. Serial by construction — the SDK requires
 * one updateImageRawData in flight at a time, and a nav route fires these on
 * every maneuver change.
 */
let arrowChain: Promise<unknown> = Promise.resolve();
let shownArrow = "";
/** Set once an image push fails, so the text carries the direction instead. */
let imageBroken = false;
async function pushArrow(instruction: string, distance: string): Promise<void> {
  const kind = maneuverArrow(instruction);
  if (!kind) {
    // Recorded, not silently dropped: "no arrow drawn" and "no arrow ever
    // attempted" look identical on the glass, and the mirror is the only place
    // that distinction can be made.
    noteImagePush("arrow", 0, `skipped:${instruction.slice(0, 12)}`);
    return;
  }
  // Keyed on kind AND distance: the distance is inside the bitmap now, so it
  // has to be redrawn as the number counts down — but only when the rendered
  // text actually changes, which is far less often than a position fix.
  const key = `${kind}:${distance}`;
  if (key === shownArrow) return;
  shownArrow = key;
  arrowChain = arrowChain.then(async () => {
    try {
      const bytes = await arrowPng(kind, distance);
      const res = await sendImage("arrow", NAV_ARROW_ID, bytes);
      if (res !== "success") imageBroken = true;
      if (res !== "success") {
        // Surfaced to the PHONE, not just the console: the console is
        // unreachable from a device report, and "화살표가 안 나온다" carries no
        // diagnosis on its own. The result code names the cause.
        console.error("updateImageRawData:", res);
        setPhoneStatus({ line: `화살표 렌더 실패: ${res}`, tone: "bad" });
        shownArrow = "";
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error("arrow render failed", err);
      setPhoneStatus({ line: `화살표 생성 실패: ${msg}`, tone: "bad" });
      shownArrow = "";
    }
  });
  await arrowChain;
}

/**
 * startNav plans a route and puts the glasses into turn-by-turn.
 *
 * The G2's own Navigate is not backed by Korean map data — export law keeps the
 * global providers from serving driving directions here at all — so this exists
 * to replace it rather than supplement it. Deneb routes through TMap.
 *
 * The polling loop is paused for the duration, for the same reason interpreting
 * pauses it: a glance redraw over a maneuver is worse than a stale alert count,
 * and nobody reads their todo list while crossing an intersection.
 */
async function startNav(
  destination: string,
  mode: "walk" | "car",
): Promise<void> {
  const dest = destination.trim();
  if (!dest || navRoute) return;

  const fix = await bridge.getAppLocation({
    accuracy: AppLocationAccuracy.High,
    timeoutMs: 8000,
  });
  if (!fix) {
    setPhoneStatus({ line: "현재 위치를 얻지 못했습니다.", tone: "bad" });
    return;
  }
  const from: NavCoord = { lat: fix.latitude, lon: fix.longitude };

  pauseLoop();
  screen = "nav";
  navDest = dest;
  shownArrow = "";
  await enterNavPage();
  await showText(`길찾기\n\n${dest}\n경로 계산 중…`);

  let result;
  try {
    result = await fetchRoute(settings, from, dest, mode);
  } catch (err) {
    navRoute = null;
    screen = "page";
    const msg = err instanceof Error ? err.message : String(err);
    setPhoneStatus({ line: `길찾기 실패: ${msg}`, tone: "bad" });
    await showText(`길찾기 실패\n\n${msg}\n\n탭=목록`);
    await resumeLoop();
    return;
  }

  navRoute = result.route;
  navPos = from;
  // Fold the starting fix through the advance rule immediately.
  //
  // Without this the HUD opens pointed at step 0 — which is 출발, the place the
  // wearer is already standing. That step has no direction by design, so the
  // arrow stayed blank until a later fix happened to arrive, and the first
  // thing shown was a maneuver that had already been completed. Standing at
  // 출발 is within ARRIVE_RADIUS_M of it, so one pass advances to the first
  // real turn.
  navState = initialNavStateAt(result.route, from);
  navDest = result.destination.name || dest;

  // Continuous fixes, not one-shot polling: distanceFilter means the host only
  // wakes us when the wearer actually moved, which is the difference between a
  // day of battery and an afternoon.
  await bridge.startAppLocationUpdates({
    accuracy: AppLocationAccuracy.High,
    intervalMs: 2000,
    distanceFilter: 5,
  });
  // Clear the full-screen container: startNav wrote "경로 계산 중…" into it and
  // nothing else ever touches it, so that line sat above the live numbers for
  // the whole route. Observed on the device as "또 경로 계산중만 떠" while the
  // mirror showed navText correctly counting down.
  await showText(" ");
  setPhoneStatus({ line: `길안내 중 · ${navDest}`, tone: "ok" });
  await renderNav();
}

async function stopNav(): Promise<void> {
  if (!navRoute) return;
  navRoute = null;
  navPos = null;
  navState = initialNavState();
  await bridge.stopAppLocationUpdates();
  shownArrow = "";
  shownSpeed = "";
  await leaveNavPage();
  screen = "page";
  setPhoneStatus({ line: "길안내를 중지했습니다.", tone: "warn" });
  await resumeLoop();
}

/** renderNav draws the current maneuver. */
async function renderNav(): Promise<void> {
  if (!navRoute || !navPos) return;
  const idx = Math.min(navState.stepIndex, navRoute.steps.length - 1);
  const step = navRoute.steps[idx];
  const left = remainingM(navRoute, navState, navPos);

  // The bitmap carries the arrow AND the distance (text containers have no font
  // size, so this is the only way the distance can dominate the panel the way
  // both shipping plugins make it). The text container carries the words.
  const lines = navTextLines(navRoute, navState);
  const kind = step ? maneuverArrow(step.short) : null;
  // Always, not only when the bitmap fails.
  //
  // g2-drive-nav — a working open-source G2 navigation plugin — renders its
  // maneuver in a TEXT container 576×148 and uses images only for a small map
  // inset. On this display the maneuver being text is the normal design, not a
  // consolation prize, and it is the one thing that has actually reached the
  // wearer's eye across six attempts at the bitmap.
  if (kind) {
    lines.unshift(`${arrowText(kind)}  ${formatMetres(left)}`, "");
  }
  await showNavText(lines.join("\n"));
  if (step) await pushArrow(step.short, formatMetres(left));
}

let shownSpeed = "";
/**
 * pushSpeed draws the current speed. Shares the arrow's chain because
 * updateImageRawData must be serial across ALL image containers, not per
 * container.
 */
async function pushSpeed(kmh: number | null): Promise<void> {
  // Bucketed to 5 km/h: at 1 km/h granularity the number changes almost every
  // fix, and each change is a fragmented BLE transfer the link cannot absorb.
  // Nobody reads a HUD speed to the digit.
  const key = kmh == null ? "" : String(Math.round(kmh / 5) * 5);
  if (key === shownSpeed) return;
  shownSpeed = key;
  if (kmh == null) return;
  arrowChain = arrowChain.then(async () => {
    try {
      const bytes = await speedPng(kmh);
      const res = await sendImage("speed", NAV_SPEED_ID, bytes);
      if (res !== "success") {
        console.error("updateImageRawData(speed):", res);
        shownSpeed = "";
      }
    } catch (err) {
      console.error("speed render failed", err);
      shownSpeed = "";
    }
  });
  await arrowChain;
}

/** showNavText writes the nav page's sentence container (not the capture layer). */
async function showNavText(content: string): Promise<void> {
  mirrorHud(settings, {
    screen,
    text: content,
    images: lastImagePushes,
    note: "navText",
  });
  await bridge.textContainerUpgrade(
    new TextContainerUpgrade({
      containerID: 2,
      containerName: "navText",
      content,
    }),
  );
}

/**
 * onNavFix folds a new position into the route.
 *
 * Redraws only when the maneuver actually changed. A HUD that repaints on every
 * fix flickers in the wearer's eye for no new information, and the instruction
 * text is identical between fixes for most of a leg.
 */
async function onNavFix(pos: NavCoord): Promise<void> {
  if (!navRoute) return;
  navPos = pos;
  const next = advanceNav(navRoute, navState, pos);
  const changed =
    next.stepIndex !== navState.stepIndex || next.arrived !== navState.arrived;
  navState = next;
  await renderNav();
  if (changed && next.arrived) {
    // Let the arrival sit on the glass before handing the screen back.
    setPhoneStatus({ line: `도착: ${navDest}`, tone: "ok" });
  }
}

/**
 * assistTick surfaces a wiki-grounded fact about what is being discussed.
 *
 * Runs off the same rolling window the tap uses, so listening buys both: a tap
 * asks "what was that", assist answers "here is what you know about it" without
 * being asked. Silence is the normal outcome and costs nothing on screen.
 */
async function assistTick(): Promise<void> {
  if (!listening || recallBusy) return;
  const now = Date.now();
  if (!shouldAssistNow(now, assistAt, assistBusy, ASSIST_PERIOD_MS)) return;
  const window = recallPcm.snapshot();
  if (!window) return;
  assistBusy = true;
  assistAt = now;
  try {
    const res = await postAudio(settings, window, { ask: ASSIST_PROMPT });
    const line = assistLine(res.reply);
    // Only redraw on a NEW line. Repainting the same fact every cycle pulls the
    // eye during a conversation for no new information, which is the failure
    // this whole feature has to avoid.
    if (line && line !== assistShown) {
      assistShown = line;
      await showText(
        ["● 청취 중", "", line, "", "탭=방금 대화 / 더블탭=종료"].join("\n"),
      );
    }
  } catch {
    // A failed window is not worth telling the wearer about mid-meeting.
  } finally {
    assistBusy = false;
  }
}

/**
 * startListening opens the glasses mic and keeps a rolling window of what was
 * said, WITHOUT sending anything anywhere.
 *
 * The pattern is G2 Fact Check's and it is the right one: deciding to capture
 * before the interesting thing is said is exactly what nobody manages. Audio
 * only leaves the glasses when the wearer taps.
 */
async function startListening(): Promise<void> {
  if (listening) return;
  const ok = await bridge.audioControl(true, AudioInputSource.Glasses);
  if (!ok) {
    setPhoneStatus({ line: "마이크를 열지 못했습니다.", tone: "bad" });
    return;
  }
  listening = true;
  screen = "recall";
  pauseLoop();
  await showText(
    [
      "● 청취 중",
      "",
      `최근 ${Math.round(RECALL_MS / 1000)}초를 기억합니다.`,
      "아무것도 전송하지 않습니다.",
      "",
      "탭=방금 대화 넘기기",
      "더블탭=청취 종료",
    ].join("\n"),
  );
}

async function stopListening(): Promise<void> {
  if (!listening) return;
  listening = false;
  assistAt = 0;
  assistShown = "";
  await bridge.audioControl(false);
  recallPcm.take(); // drop the window — it was never asked for
  screen = "page";
  setPhoneStatus({ line: "청취를 종료했습니다.", tone: "warn" });
  await resumeLoop();
}

/**
 * recallNow hands the buffered conversation to Deneb as a chat turn.
 *
 * Snapshot, not drain: a second tap moments later must still find the audio.
 * The turn goes through the normal agent, so "기록해둬" or a follow-up question
 * reaches the wiki and calendar without any of that being re-plumbed here.
 */
async function recallNow(): Promise<void> {
  if (!listening || recallBusy) return;
  const window = recallPcm.snapshot();
  if (!window) {
    await showText("● 청취 중\n\n아직 들은 것이 없습니다.\n\n탭=다시");
    return;
  }
  recallBusy = true;
  await showText("● 청취 중\n\n방금 대화를 넘기는 중…");
  try {
    const res = await postAudio(settings, window, {
      ask: "방금 들은 대화를 한국어로 짧게 정리해 주세요. 업무 관련 내용이면 위키에 기록하고, 무엇을 기록했는지 한 줄로 알려주세요.",
    });
    const body =
      res.reply || res.askError || res.text || "정리하지 못했습니다.";
    await showText(`● 청취 중\n\n${body}\n\n탭=다시 / 더블탭=종료`);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    await showText(`● 청취 중\n\n실패: ${msg}\n\n탭=다시`);
  } finally {
    recallBusy = false;
  }
}

/**
 * startInterpreting opens the glasses microphone and streams it to Deneb.
 *
 * The polling loop is paused for the duration: a glance redraw would wipe the
 * subtitle mid-sentence, and the wearer is not reading alerts while someone is
 * talking to them.
 */
async function startInterpreting(lang: string): Promise<void> {
  if (interpreting) return;
  interpretLang = lang || "ko";
  interpreting = true;
  subtitles = [];
  pauseLoop();
  screen = "interpret";
  await showText(`통역 중 (${interpretLang})\n\n듣는 중…\n\n탭=중지`);
  // Glasses mic, not the phone's: the wearer's own device is pointed at the
  // person speaking. The startup container already exists, which the SDK
  // requires before this returns true.
  const ok = await bridge.audioControl(true, AudioInputSource.Glasses);
  if (!ok) {
    interpreting = false;
    screen = "page";
    await showText("통역을 시작하지 못했습니다.\n\n탭=목록");
  }
}

async function stopInterpreting(): Promise<void> {
  if (!interpreting) return;
  interpreting = false;
  await bridge.audioControl(false);
  pcm.take();
  screen = "page";
  await resumeLoop();
}

/** renderSubtitles keeps the last few lines so a glance catches the sentence. */
async function renderSubtitles(): Promise<void> {
  const body = subtitles.length ? subtitles.join("\n") : "듣는 중…";
  await showText(`통역 · ${interpretLang}\n\n${body}\n\n탭=중지`);
}

/**
 * flushAudio sends one window and draws what comes back.
 *
 * `sending` is a plain in-flight guard, not a queue: if the network is slower
 * than speech, dropping a window keeps the subtitle current, while a backlog
 * would put the wearer further behind with every sentence.
 */
async function flushAudio(): Promise<void> {
  if (sending || !interpreting) return;
  const window = pcm.take();
  if (!window) return;
  sending = true;
  try {
    const res = await postAudio(settings, window, {
      translateTo: interpretLang,
    });
    const line = res.translation || res.text;
    if (line && interpreting) {
      subtitles = subtitleLines(subtitles, line);
      await renderSubtitles();
    }
  } catch {
    // Silent on purpose: a dropped window during a conversation is not worth
    // taking the subtitle off the glass to announce.
  } finally {
    sending = false;
  }
}

/**
 * captureImu records one labelled motion window.
 *
 * The instrument that has to come before a head-gesture detector: the SDK
 * documents neither the axis convention nor the units of `imuData`, so a
 * detector written now would encode a guess and its unit tests would pin the
 * guess rather than the device. Bounded to one window at a time, and the IMU is
 * switched back off afterwards — it is a battery cost with no other consumer.
 */
async function captureImu(label: string): Promise<void> {
  if (imuLabel) return;
  imuLabel = label;
  imuSamples = [];
  const ok = await bridge.imuControl(true, IMU_PACE_MS);
  if (!ok) {
    imuLabel = "";
    setPhoneStatus({ line: "IMU 를 열지 못했습니다.", tone: "bad" });
    return;
  }
  setTimeout(() => void finishImu(), CAPTURE_MS);
}

async function finishImu(): Promise<void> {
  const label = imuLabel;
  const samples = imuSamples;
  imuLabel = "";
  imuSamples = [];
  await bridge.imuControl(false);
  if (!label) return;
  if (samples.length === 0) {
    // Worth saying out loud: it means the host never sent imuData, which is the
    // one thing about this path that cannot be checked without the glasses.
    setPhoneStatus({
      line: `"${label}" 샘플 0개 — 기기가 IMU 를 보내지 않았습니다.`,
      tone: "bad",
    });
    return;
  }
  try {
    await postImuRecording(settings, label, samples);
    setPhoneStatus({
      line: `"${label}" ${samples.length}개 기록 완료`,
      tone: "ok",
    });
  } catch (err) {
    setPhoneStatus({
      line: `기록 전송 실패: ${err instanceof Error ? err.message : String(err)}`,
      tone: "bad",
    });
  }
}

/** wearLine reports what the glasses said about themselves, if anything. */
function wearLine(): string {
  if (!wearStatus) return "";
  // The wear verdict is deliberately absent: the device reports "벗음" while
  // worn (battery from the same query is right, so it is the field and not the
  // link). Showing a value known to be wrong is worse than showing none.
  if (
    typeof wearStatus.batteryLevel === "number" &&
    wearStatus.batteryLevel > 0
  ) {
    const chg = wearStatus.isCharging === true ? " · 충전 중" : "";
    return `배터리 ${wearStatus.batteryLevel}%${chg}`;
  }
  return "";
}

/** clampCursor is the app-state view of the pure guard in refresh.ts. */
function clampCursor(): number {
  return clampCursorTo(listCursor, items.length);
}

/** atTop reports whether an up-swipe would fall off the front of everything. */
function atTop(): boolean {
  if (clampCursor() > 0) return false;
  for (let i = pageIndex - 1; i >= 0; i--) {
    if (!isPageEmpty(pages[i])) return false;
  }
  return true;
}

/** onAlertPage reports whether the current page draws the alert cursor. */
function onAlertPage(): boolean {
  return LIST_PAGE_IDS.has(currentPageId()) && items.length > 0;
}

/**
 * showText updates the single text container the app draws everything into.
 *
 * There is exactly one container for the whole app now that alerts are text
 * too, so this is always an in-place upgrade — no rebuild, which is what keeps
 * a background poll from flickering the wearer's view.
 */
/** Last image-push results, reported alongside the next mirrored frame. */
const lastImagePushes: Array<{ name: string; bytes: number; result: string }> =
  [];
function noteImagePush(name: string, bytes: number, result: string): void {
  const at = lastImagePushes.findIndex((i) => i.name === name);
  const entry = { name, bytes, result };
  if (at >= 0) lastImagePushes[at] = entry;
  else lastImagePushes.push(entry);
}

async function showText(content: string): Promise<void> {
  mirrorHud(settings, { screen, text: content, images: lastImagePushes });
  await bridge.textContainerUpgrade(
    new TextContainerUpgrade({
      containerID: 1,
      containerName: "main",
      content,
    }),
  );
}

/**
 * scheduleRefresh drives the background poll as a self-rescheduling timeout
 * rather than a fixed interval, so the delay can grow while the gateway is
 * unreachable and the whole loop can be cancelled with one handle.
 */
function scheduleRefresh(): void {
  if (stopped || paused) return;
  if (refreshTimer !== undefined) clearTimeout(refreshTimer);
  refreshTimer = setTimeout(
    () => {
      refreshTimer = undefined;
      void runScheduledRefresh();
    },
    pollInterval(nextDelayMs(consecutiveFailures), wearStatus),
  );
}

async function runScheduledRefresh(): Promise<void> {
  if (stopped || paused) return;
  // The status screen is a deliberate read; polling under it would swap the
  // wearer's screen out from under them.
  if (!busy && screen !== "status") {
    if (needsSetup(settings)) {
      // Setup polling only re-reads local settings (the QR seed lands in
      // localStorage), so it must not count as a network failure.
      settings = await resolveSettings();
      if (!needsSetup(settings)) {
        screen = "page";
        pageIndex = 0;
        await refreshGlance(false);
      }
    } else {
      await refreshGlance(false, true);
    }
  }
  scheduleRefresh();
}

function isPageEmpty(p: GlancePage | undefined): boolean {
  if (!p) return true;
  return skipPage(p, items.length);
}
