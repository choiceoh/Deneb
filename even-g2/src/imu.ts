import type { GlanceSettings } from './settings'
import { REQUEST_TIMEOUT_MS } from './refresh'

// IMU capture — an instrument, not a feature.
//
// `IMU_Report_Data` is three doubles with no stated units, no axis convention
// and no per-sample timestamp, reported at 100–1000ms. A nod/shake detector
// written against that would encode a GUESS, and its unit tests would pin the
// guess rather than the device — the exact "looks verified" failure this
// plugin has been bitten by repeatedly.
//
// So: record labelled windows on the operator's actual head first, then build
// the detector against those as fixtures.

export type ImuSample = { x: number; y: number; z: number }

/** OsEventTypeList.IMU_DATA_REPORT — the sys eventType that carries imuData. */
export const IMU_DATA_REPORT = 8

/** Fastest the device reports (ImuReportPace.P100). A nod is ~0.5–1s ≈ 5–10 of these. */
export const IMU_PACE_MS = 100

/** Long enough to contain a deliberate gesture with quiet either side. */
export const CAPTURE_MS = 3_000

/** The motions worth labelling: two candidate commands, two false-positive sources. */
export const IMU_LABELS = ['정지', '끄덕임', '도리도리', '걷기'] as const
export type ImuLabel = (typeof IMU_LABELS)[number]

/**
 * readImuSample pulls a usable sample out of a host event.
 *
 * IMU arrives on the SYS channel — `sysEvent.eventType === IMU_DATA_REPORT`
 * (ordinal 8) with `sysEvent.imuData` — which is documented in the SDK README
 * and nowhere in the type definitions. Reading the types alone, as this file
 * originally did, gets the channel wrong and records nothing at all.
 *
 * Axes are optional on the wire, so a partial reading is a real possibility;
 * dropping it beats recording a zero that looks like stillness.
 */
export function readImuSample(event: unknown): ImuSample | undefined {
  if (!event || typeof event !== 'object') return undefined
  const sys = (event as { sysEvent?: { eventType?: unknown; imuData?: unknown } }).sysEvent
  if (!sys || sys.eventType !== IMU_DATA_REPORT) return undefined
  const raw = sys.imuData
  if (!raw || typeof raw !== 'object') return undefined
  const r = raw as { x?: unknown; y?: unknown; z?: unknown }
  const nums = [r.x, r.y, r.z].map((v) => (typeof v === 'number' && Number.isFinite(v) ? v : undefined))
  if (nums.some((v) => v === undefined)) return undefined
  return { x: nums[0]!, y: nums[1]!, z: nums[2]! }
}

/** postImuRecording ships one labelled window to the gateway for later analysis. */
export async function postImuRecording(
  settings: GlanceSettings,
  label: string,
  samples: ImuSample[],
): Promise<number> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  try {
    const res = await fetch(`${settings.baseUrl}/api/even/imu-sample`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${settings.token}`,
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify({ label, paceMs: IMU_PACE_MS, samples }),
      signal: controller.signal,
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return samples.length
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') throw new Error('시간 초과')
    throw err
  } finally {
    clearTimeout(timer)
  }
}
