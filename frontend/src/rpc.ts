// rpc.ts — thin client for the gateway's /api/v1/miniapp/rpc endpoint.
//
// The gateway protocol wraps every call in a RequestFrame / ResponseFrame
// pair. We expose a single `call<T>()` function that handles framing,
// the "Authorization: tma <raw>" header, and OK/error unwrapping so view
// code can treat the call like a typed promise.

const RPC_PATH = '/api/v1/miniapp/rpc';

let counter = 0;
function nextRequestId(): string {
  counter += 1;
  return `m-${Date.now().toString(36)}-${counter.toString(36)}`;
}

export class RpcError extends Error {
  readonly code: string;
  readonly httpStatus: number;
  constructor(code: string, message: string, httpStatus: number) {
    super(message);
    this.code = code;
    this.httpStatus = httpStatus;
  }
}

interface ResponseFrame<T> {
  type: 'res';
  id: string;
  ok: boolean;
  payload?: T;
  error?: { code: string; message: string };
}

/**
 * Call a miniapp.* method and return the typed payload on success.
 *
 * On HTTP-level failure (401, 403, 503, 5xx) throws RpcError with the
 * server-supplied message; on protocol-level failure (frame.ok === false)
 * throws with the frame's error code/message.
 */
export async function call<T>(method: string, params: unknown, initData: string): Promise<T> {
  const frame = {
    type: 'req',
    id: nextRequestId(),
    method,
    params: params === undefined ? null : params,
  };

  const res = await fetch(RPC_PATH, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `tma ${initData}`,
    },
    body: JSON.stringify(frame),
  });

  if (!res.ok) {
    let body: { error?: string } = {};
    try {
      body = (await res.json()) as { error?: string };
    } catch {
      // Body wasn't JSON — fall through to HTTP status text.
    }
    throw new RpcError(
      `HTTP_${res.status}`,
      body.error ?? res.statusText ?? `HTTP ${res.status}`,
      res.status,
    );
  }

  const decoded = (await res.json()) as ResponseFrame<T>;
  if (!decoded.ok) {
    throw new RpcError(
      decoded.error?.code ?? 'UNKNOWN',
      decoded.error?.message ?? 'rpc call failed',
      res.status,
    );
  }
  if (decoded.payload === undefined) {
    throw new RpcError('EMPTY_PAYLOAD', 'rpc returned no payload', res.status);
  }
  return decoded.payload;
}

// --- Typed convenience wrappers ------------------------------------------

export interface PingResult {
  ok: boolean;
  version: string;
  tsMs: number;
  model?: string;
}

export interface WhoamiResult {
  id: number;
  firstName: string;
  lastName?: string;
  username?: string;
  languageCode?: string;
  isPremium?: boolean;
  authDateMs: number;
}

export const ping = (initData: string) => call<PingResult>('miniapp.ping', null, initData);
export const whoami = (initData: string) => call<WhoamiResult>('miniapp.whoami', null, initData);

// --- Model quick-change --------------------------------------------------

export interface MiniappModelOption {
  id: string;
  label: string;
  provider?: string;
  display?: string;
  health?: 'online' | 'offline' | 'unknown';
  current: boolean;
}

export interface MiniappModelSection {
  title: string;
  models: MiniappModelOption[];
}

export interface MiniappRoleModel {
  role: string;
  model: string;
}

export interface MiniappModelsResult {
  current: string;
  roles: MiniappRoleModel[];
  sections: MiniappModelSection[];
}

export interface MiniappModelSetResult {
  ok: boolean;
  role?: string;
  current: string;
}

export interface MiniappModelAddResult {
  ok: boolean;
  id: string;
  provider: string;
  endpoint: string;
  model: string;
  added: boolean;
}

export const listMiniappModels = (initData: string) =>
  call<MiniappModelsResult>('miniapp.models.list', null, initData);

export const setMiniappModel = (initData: string, id: string, role?: string) =>
  call<MiniappModelSetResult>('miniapp.models.set', { id, role }, initData);

export const addMiniappModel = (initData: string, endpoint: string, model: string) =>
  call<MiniappModelAddResult>('miniapp.models.add_custom', { endpoint, model }, initData);

// --- Calendar -----------------------------------------------------------

export interface CalendarAttendee {
  email?: string;
  displayName?: string;
  responseStatus?: string;
  self?: boolean;
  organizer?: boolean;
}

// Shared shape: fields the backend emits in BOTH list and detail
// responses. List adds `hasMeet`; detail adds `description` and
// `conference`. Splitting them prevents readers from relying on a
// field that doesn't exist in their response path.
interface CalendarEventBase {
  id: string;
  summary: string;
  location?: string;
  start: string; // RFC3339, "" for missing
  end: string;
  allDay?: boolean;
  status?: string;
  htmlLink?: string;
  organizer?: CalendarAttendee;
  attendees?: CalendarAttendee[];
}

export interface CalendarEventSummary extends CalendarEventBase {
  hasMeet?: boolean;
}

export interface CalendarEventDetail extends CalendarEventBase {
  description?: string;
  conference?: { solution?: string; uri?: string };
}

export const calendarListUpcoming = (
  initData: string,
  params?: { hoursAhead?: number; limit?: number },
) =>
  call<{ events: CalendarEventSummary[] }>(
    'miniapp.calendar.list_upcoming',
    params ?? null,
    initData,
  );

export const calendarGet = (initData: string, id: string) =>
  call<CalendarEventDetail>('miniapp.calendar.get', { id }, initData);
