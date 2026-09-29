export type Vendor = 'razer' | 'logitech' | 'mchose'
export const vendorNames: Record<Vendor, string> = { razer: '雷蛇', logitech: '罗技', mchose: '迈从' }
export interface DPIRange { min: number; max: number; step: number }
export interface Capabilities {
  battery: boolean
  dpiRanges: DPIRange[]
  separateAxes: boolean
  pollRates: number[]
  dpiReadOnly: boolean
  rateReadOnly: boolean
  batteryReason: string
  dpiReason: string
  rateReason: string
}
export interface MouseDevice {
  id: string
  name: string
  vendor: Vendor
  connection: string
  vid: number
  pid: number
  status: 'online' | 'unavailable' | 'identified'
  battery: {
    percent: number | null
    level: string
    voltageMV: number | null
    charging: boolean | null
    chargeText: string
    error: string
  }
  dpi: { x: number | null; y: number | null; error: string }
  rate: { hz: number | null; error: string }
  capabilities: Capabilities
  notes: string[]
}
export interface Snapshot { version: string; scannedAt: string; devices: MouseDevice[] }
export interface Mutation { message: string; changed: boolean; device: MouseDevice }
export interface MouseAPI {
  scan(): Promise<Snapshot>
  setDPI(deviceId: string, x: number, y: number): Promise<Mutation>
  setRate(deviceId: string, rate: number): Promise<Mutation>
  dispose?(): void
}
export type MouseRequest =
  | { id: string; action: 'scan' }
  | { id: string; action: 'setDPI'; deviceId: string; x: number; y: number }
  | { id: string; action: 'setRate'; deviceId: string; rate: number }
export interface NativeResponse { id: string; result?: unknown; error?: string }
declare global {
  interface Window { mouseNativeSubmit?: (request: MouseRequest) => Promise<boolean> }
}
