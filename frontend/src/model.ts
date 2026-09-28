import type { DPIRange, MouseDevice } from './types'
export interface Validation<T> { value: T | null; error: string }
export function inDPIRanges(value: number, ranges: DPIRange[]): boolean {
  return ranges.some(r => r.step > 0 && value >= r.min && value <= r.max && (value - r.min) % r.step === 0)
}
function integer(text: string): number | null {
  if (!/^\d+$/.test(text.trim())) return null
  const value = Number(text)
  return Number.isSafeInteger(value) && value >= 1 && value <= 65535 ? value : null
}
export function validateDPIInput(d: MouseDevice, xText: string, yText: string, linked: boolean): Validation<{ x: number; y: number }> {
  const c = d.capabilities
  if (d.status !== 'online') return { value: null, error: '鼠标当前不可用，请刷新连接后再设置。' }
  if (c.dpiReadOnly || c.dpiRanges.length === 0) return { value: null, error: c.dpiReason || '当前设备未提供 DPI 设置。' }
  const x = integer(xText), y = linked || !c.separateAxes ? x : integer(yText)
  if (x === null || y === null) return { value: null, error: '请输入完整的正整数 DPI。' }
  if (!inDPIRanges(x, c.dpiRanges)) return { value: null, error: 'DPI 不在设备允许的范围内，或不符合步长。' }
  // 部分设备的 Y 轴范围独立；原生后端使用它实际读取的 Y 轴能力再次校验。
  if (!linked && c.separateAxes && y < 1) return { value: null, error: '请输入 Y 轴 DPI。' }
  return { value: { x, y }, error: '' }
}
export function validateRateInput(d: MouseDevice, rate: number | null): Validation<number> {
  const c = d.capabilities
  if (d.status !== 'online') return { value: null, error: '鼠标当前不可用，请刷新连接后再设置。' }
  if (c.rateReadOnly || c.pollRates.length === 0) return { value: null, error: c.rateReason || '当前设备未提供回报率设置。' }
  if (rate === null || !c.pollRates.includes(rate)) return { value: null, error: '请选择设备提供的回报率档位。' }
  return { value: rate, error: '' }
}
export function formatDPIRanges(ranges: DPIRange[]): string {
  return ranges.map(r => r.min === r.max ? String(r.min) : `${r.min.toLocaleString('zh-CN')}–${r.max.toLocaleString('zh-CN')}${r.step > 1 ? `，步长 ${r.step}` : ''}`).join(' / ')
}
export function batteryDisplay(b: MouseDevice['battery']): { value: string; unit: string; detail: string; low: boolean; hasValue: boolean } {
  if (b.percent !== null) return { value: String(b.percent), unit: '%', detail: b.chargeText || (b.charging === true ? '正在充电' : b.charging === false ? '未充电' : '充电状态未知'), low: b.percent <= 20, hasValue: true }
  return { value: b.level || (b.voltageMV !== null ? String(b.voltageMV) : '未知'), unit: !b.level && b.voltageMV !== null ? 'mV' : '', detail: [b.level && b.voltageMV !== null ? `${b.voltageMV} mV` : '', b.chargeText || '未提供精确百分比'].filter(Boolean).join(' · '), low: b.level === '低' || b.level === '极低', hasValue: !!b.level || b.voltageMV !== null }
}
export function shortName(d: MouseDevice): string {
  return d.name.replace(/^(Razer |Logitech |Logi |雷蛇\s*|罗技\s*)/i, '')
}
export function statusLabel(d: MouseDevice): string {
  return d.status === 'online' ? '已连接' : d.status === 'identified' ? '仅识别' : '暂不可用'
}
export function timeLabel(value: string | undefined): string {
  if (!value || Number.isNaN(Date.parse(value))) return '尚未读取'
  return new Date(value).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
