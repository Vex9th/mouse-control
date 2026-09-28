import { expect, test } from 'bun:test'
import { deviceReport, paginateText } from '../src/display'
import { device } from './fixtures'

test('长原文按字符宽度与行数分页，不丢失空白或拆开 Unicode 字符', () => {
  for (const source of ['设备身份错误。'.repeat(90), 'usb:'.repeat(500), '\n'.repeat(60), '🖱️鼠标\r\n'.repeat(50)]) {
    const pages = paginateText(source, 40, 5)
    expect(pages.length).toBeGreaterThan(1)
    expect(pages.join('')).toBe(source)
    expect(pages.every(page => Array.from(page).length <= 40 * 5)).toBe(true)
    expect(pages.every(page => !/^[\uDC00-\uDFFF]|[\uD800-\uDBFF]$/.test(page))).toBe(true)
  }
})

test('设备报告保留完整 ID、未知电量以及所有错误与备注', () => {
  const d = device('receiver/'.repeat(120))
  d.battery.percent = null
  d.battery.error = '充电状态未应答'
  d.dpi.error = 'DPI 原始错误'
  d.notes = ['说明一', '说明二']
  const text = deviceReport(d, '12:30:00')
  for (const value of [d.id, '百分比未知', '充电状态未应答', 'DPI 原始错误', '说明一', '说明二']) expect(text).toContain(value)
  expect(paginateText(text).join('')).toBe(text)
})

test('设备报告明确列出只读能力，即使设备没有提供原因', () => {
  const d = device('readonly')
  d.capabilities.dpiReadOnly = true
  d.capabilities.rateReadOnly = true
  d.capabilities.dpiReason = ''
  d.capabilities.rateReason = ''
  const text = deviceReport(d, '12:30:00')
  expect(text).toContain('DPI 设置：只读')
  expect(text).toContain('回报率设置：只读')
})
