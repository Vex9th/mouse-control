import type { MouseDevice, Snapshot } from '../src/types'
export function device(id = 'usb:one'): MouseDevice {
  return {
    id, name: 'DeathAdder V4 Pro', vendor: 'razer', connection: '无线接收器', vid: 0x1532, pid: 0xbf, status: 'online',
    battery: { percent: 68, level: '', voltageMV: null, charging: false, chargeText: '未充电', error: '' },
    dpi: { x: 800, y: 800, error: '' }, rate: { hz: 1000, error: '' },
    capabilities: { battery: true, dpiRanges: [{ min: 100, max: 45000, step: 1 }], separateAxes: true, pollRates: [125, 500, 1000, 2000, 4000, 8000], dpiReadOnly: false, rateReadOnly: false, batteryReason: '', dpiReason: '', rateReason: '' }, notes: [],
  }
}
export function snapshot(devices = [device()]): Snapshot {
  return { version: '1.4.0', scannedAt: '2026-09-29T10:00:00+08:00', devices }
}
