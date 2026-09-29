import type { MouseDevice } from './types'
import { vendorNames } from './types'

export function paginateText(text: string, columns = 26, rows = 7): string[] {
  if (!Number.isInteger(columns) || columns < 2 || !Number.isInteger(rows) || rows < 1) throw new Error('分页尺寸无效')
  const pages: string[] = []
  let page = '', line = 1, column = 0
  for (const char of text.match(/\r\n|./gsu) ?? []) {
    const newline = char === '\n' || char === '\r' || char === '\r\n'
    // 按全格保守预算，不能假定比例字体里的 ASCII 都只有半个汉字宽。
    const width = char === '\t' ? 4 : 1
    const nextLine = newline || column + width > columns ? line + 1 : line
    if (page && nextLine > rows) { pages.push(page); page = ''; line = 1; column = 0 }
    page += char
    if (newline) { line++; column = 0 }
    else { if (column + width > columns) { line++; column = 0 }; column += width }
  }
  if (page || !pages.length) pages.push(page)
  return pages
}

export function deviceReport(d: MouseDevice, updatedAt: string): string {
  return [
    `设备名称：${d.name}`,
    `品牌：${vendorNames[d.vendor]}`,
    `连接方式：${d.connection}`,
    `设备 ID：${d.id}`,
    `VID / PID：${d.vid.toString(16).toUpperCase().padStart(4, '0')} / ${d.pid.toString(16).toUpperCase().padStart(4, '0')}`,
    `最后读取：${updatedAt}`,
    `电量：${d.battery.percent === null ? '百分比未知' : `${d.battery.percent}%`}`,
    d.battery.level && `电量等级：${d.battery.level}`,
    d.battery.voltageMV !== null && `电池电压：${d.battery.voltageMV} mV`,
    `充电状态：${d.battery.chargeText || (d.battery.charging === null ? '未知' : d.battery.charging ? '正在充电' : '未充电')}`,
    d.battery.error && `电量查询：${d.battery.error}`,
    d.capabilities.batteryReason && `电量能力：${d.capabilities.batteryReason}`,
    `当前 DPI：${d.dpi.x ?? '未知'} / ${d.dpi.y ?? '未知'}`,
    `DPI 设置：${d.capabilities.dpiReadOnly ? '只读' : d.capabilities.dpiRanges.length ? '可设置' : '未提供'}`,
    `双轴独立：${d.capabilities.separateAxes ? '支持' : '不支持'}`,
    `DPI 范围：${d.capabilities.dpiRanges.map(r => `${r.min}–${r.max}，步长 ${r.step}`).join('；') || '未提供'}`,
    d.dpi.error && `DPI 查询：${d.dpi.error}`,
    d.capabilities.dpiReason && `DPI 能力：${d.capabilities.dpiReason}`,
    `当前回报率：${d.rate.hz === null ? '未知' : `${d.rate.hz} Hz`}`,
    `回报率设置：${d.capabilities.rateReadOnly ? '只读' : d.capabilities.pollRates.length ? '可设置' : '未提供'}`,
    `回报率档位：${d.capabilities.pollRates.join(' / ') || '未提供'}`,
    d.rate.error && `回报率查询：${d.rate.error}`,
    d.capabilities.rateReason && `回报率能力：${d.capabilities.rateReason}`,
    ...d.notes.map(note => `设备说明：${note}`),
  ].filter(v => v !== false && v !== '').join('\n')
}
