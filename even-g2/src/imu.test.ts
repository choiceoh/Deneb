import { describe, expect, it } from 'vitest'
import { IMU_DATA_REPORT, readImuSample } from './imu'

const sys = (imuData: unknown, eventType: unknown = IMU_DATA_REPORT) => ({
  sysEvent: { eventType, imuData },
})

describe('readImuSample', () => {
  it('reads the sys channel the SDK actually uses', () => {
    // Documented in the SDK README and NOWHERE in the type definitions: IMU
    // rides sysEvent with eventType IMU_DATA_REPORT. Reading the .d.ts alone
    // gets the channel wrong and silently records nothing.
    expect(readImuSample(sys({ x: 1, y: -2, z: 0.5 }))).toEqual({ x: 1, y: -2, z: 0.5 })
  })

  it('ignores the touch and lifecycle events that share the channel', () => {
    // Ordinals 0-7 are click/scroll/lifecycle; only 8 carries motion. Treating
    // them alike would turn a walk into a stream of phantom gestures.
    for (const type of [0, 1, 2, 3, 4, 5, 6, 7]) {
      expect(readImuSample(sys({ x: 1, y: 1, z: 1 }, type))).toBeUndefined()
    }
  })

  it('drops a partial reading rather than recording a fake zero', () => {
    // A missing axis logged as 0 looks exactly like stillness, which is the one
    // thing a gesture detector must not be lied to about.
    expect(readImuSample(sys({ x: 1, y: 2 }))).toBeUndefined()
    expect(readImuSample(sys({ x: 1, y: 2, z: Number.NaN }))).toBeUndefined()
    expect(readImuSample(sys({ x: 1, y: 2, z: '3' }))).toBeUndefined()
  })

  it('survives anything else the host sends', () => {
    expect(readImuSample(undefined)).toBeUndefined()
    expect(readImuSample({})).toBeUndefined()
    expect(readImuSample({ listEvent: {} })).toBeUndefined()
    expect(readImuSample(sys(undefined))).toBeUndefined()
  })
})
