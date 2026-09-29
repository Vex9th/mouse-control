import type { MouseAPI, MouseDevice, MouseRequest, Mutation, Snapshot } from './types'
interface NativeHost { events: EventTarget; submit: (request: MouseRequest) => Promise<boolean> }
interface Pending { resolve: (value: unknown) => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> }
let sequence = 0
function object(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' }
function integer(value: unknown, min: number, max = Number.MAX_SAFE_INTEGER): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= min && value <= max
}
function nullableInteger(value: unknown, min: number, max = Number.MAX_SAFE_INTEGER): boolean { return value === null || integer(value, min, max) }
function strings(value: Record<string, unknown>, keys: string[]): boolean { return keys.every(key => typeof value[key] === 'string') }
function isDevice(value: unknown): value is MouseDevice {
  if (!object(value) || !strings(value, ['id', 'name', 'connection']) || !object(value.battery) || !object(value.dpi) || !object(value.rate) || !object(value.capabilities)) return false
  const b = value.battery, d = value.dpi, r = value.rate, c = value.capabilities
  return (value.vendor === 'razer' || value.vendor === 'logitech' || value.vendor === 'mchose')
    && ['online', 'unavailable', 'identified'].includes(String(value.status))
    && integer(value.vid, 0, 65535) && integer(value.pid, 0, 65535)
    && nullableInteger(b.percent, 0, 100) && nullableInteger(b.voltageMV, 1)
    && (b.charging === null || typeof b.charging === 'boolean') && strings(b, ['level', 'chargeText', 'error'])
    && nullableInteger(d.x, 1, 65535) && nullableInteger(d.y, 1, 65535) && typeof d.error === 'string'
    && nullableInteger(r.hz, 1) && typeof r.error === 'string'
    && ['battery', 'separateAxes', 'dpiReadOnly', 'rateReadOnly'].every(key => typeof c[key] === 'boolean')
    && strings(c, ['batteryReason', 'dpiReason', 'rateReason'])
    && Array.isArray(c.dpiRanges) && c.dpiRanges.every(range => object(range) && integer(range.min, 1, 65535) && integer(range.max, range.min, 65535) && integer(range.step, 1, 65535))
    && Array.isArray(c.pollRates) && c.pollRates.every(rate => integer(rate, 1))
    && Array.isArray(value.notes) && value.notes.every(note => typeof note === 'string')
}
function isSnapshot(value: unknown): value is Snapshot {
  return object(value) && typeof value.version === 'string' && typeof value.scannedAt === 'string' && Array.isArray(value.devices) && value.devices.every(isDevice)
}
function isMutation(value: unknown): value is Mutation {
  return object(value) && typeof value.message === 'string' && typeof value.changed === 'boolean' && isDevice(value.device)
}
export class NativeMouseAPI implements MouseAPI {
  private pending = new Map<string, Pending>()
  private closed = false
  constructor(private host: NativeHost, private timeoutMs = 120_000) { host.events.addEventListener('mouse:response', this.response) }
  private response = (event: Event): void => {
    const detail: unknown = (event as CustomEvent<unknown>).detail
    if (!object(detail) || typeof detail.id !== 'string') return
    const pending = this.pending.get(detail.id)
    if (!pending) return
    clearTimeout(pending.timer)
    this.pending.delete(detail.id)
    if (typeof detail.error === 'string' && detail.error) pending.reject(new Error(detail.error))
    else pending.resolve(detail.result)
  }
  private request(body: Omit<MouseRequest, 'id'> & { deviceId?: string; x?: number; y?: number; rate?: number }): Promise<unknown> {
    if (this.closed) return Promise.reject(new Error('鼠标连接已关闭。'))
    const id = `${Date.now().toString(36)}-${++sequence}`
    return new Promise((resolve, reject) => {
      const fail = (error: unknown): void => {
        const current = this.pending.get(id)
        if (!current) return
        clearTimeout(current.timer)
        this.pending.delete(id)
        reject(error instanceof Error ? error : new Error(String(error)))
      }
      const timer = setTimeout(() => fail(new Error('设备响应超时；设置结果尚未确认，请刷新后再操作。')), this.timeoutMs)
      this.pending.set(id, { resolve, reject, timer })
      try {
        void Promise.resolve(this.host.submit({ ...body, id } as MouseRequest)).then(accepted => { if (!accepted) fail(new Error('本机服务未接受请求，请稍后重试。')) }, fail)
      } catch (error) { fail(error) }
    })
  }
  async scan(): Promise<Snapshot> {
    const value = await this.request({ action: 'scan' })
    if (!isSnapshot(value)) throw new Error('本机服务返回的设备信息格式无效。')
    return value
  }
  async setDPI(deviceId: string, x: number, y: number): Promise<Mutation> {
    const value = await this.request({ action: 'setDPI', deviceId, x, y })
    if (!isMutation(value)) throw new Error('本机服务返回的设置结果格式无效，请刷新确认。')
    return value
  }
  async setRate(deviceId: string, rate: number): Promise<Mutation> {
    const value = await this.request({ action: 'setRate', deviceId, rate })
    if (!isMutation(value)) throw new Error('本机服务返回的设置结果格式无效，请刷新确认。')
    return value
  }
  dispose(): void {
    this.closed = true
    this.host.events.removeEventListener('mouse:response', this.response)
    for (const p of this.pending.values()) { clearTimeout(p.timer); p.reject(new Error('鼠标连接已关闭。')) }
    this.pending.clear()
  }
}
export class UnavailableMouseAPI implements MouseAPI {
  private unavailable(): never { throw new Error('当前页面未连接本机鼠标服务，请使用 Windows 桌面程序打开。') }
  async scan(): Promise<Snapshot> { return this.unavailable() }
  async setDPI(): Promise<Mutation> { return this.unavailable() }
  async setRate(): Promise<Mutation> { return this.unavailable() }
}
export async function createProvider(): Promise<{ api: MouseAPI; mode: 'native' | 'demo' | 'unavailable' }> {
  if (import.meta.env.DEV && new URLSearchParams(window.location.search).get('demo') === '1') {
    const { createDemoAPI } = await import('./demo')
    return { api: createDemoAPI(new URLSearchParams(window.location.search).get('stress') === '1'), mode: 'demo' }
  }
  if (window.mouseNativeSubmit) return { api: new NativeMouseAPI({ events: window, submit: request => window.mouseNativeSubmit!(request) }), mode: 'native' }
  return { api: new UnavailableMouseAPI(), mode: 'unavailable' }
}
