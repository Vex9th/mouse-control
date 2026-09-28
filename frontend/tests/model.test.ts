import { describe, expect, test } from 'bun:test'
import { batteryDisplay, validateDPIInput, validateRateInput } from '../src/model'
import { device } from './fixtures'

describe('设备报告的设置边界', () => {
  test('连续范围、离散档位、步长与双轴分别校验，不擅自舍入', () => {
    const d = device()
    d.capabilities.dpiRanges = [{ min: 200, max: 12000, step: 50 }, { min: 16000, max: 16000, step: 1 }]
    expect(validateDPIInput(d, '850', '1200', false)).toEqual({ value: { x: 850, y: 1200 }, error: '' })
    expect(validateDPIInput(d, '825', '1200', false).error).toContain('步长')
    expect(validateDPIInput(d, '16000', '', true).value).toEqual({ x: 16000, y: 16000 })
    for (const invalid of ['', '8e2', '800.5', '0', '-800', '65536']) expect(validateDPIInput(d, invalid, '800', false).value).toBeNull()
  })
  test('只读、设备离线、不可用档位均不能触发设置', () => {
    const d = device()
    d.capabilities.dpiReadOnly = true
    d.capabilities.dpiReason = '当前模式仅支持读取'
    expect(validateDPIInput(d, '1600', '1600', true).error).toContain('仅支持读取')
    expect(validateRateInput(d, 250).error).toContain('档位')
    d.status = 'unavailable'
    expect(validateRateInput(d, 1000).value).toBeNull()
  })
  test('百分比未知保留等级或电压，不显示0%', () => {
    const b = device().battery
    b.percent = null; b.level = '电量充足'; b.voltageMV = 3900
    expect(batteryDisplay(b).value).toBe('电量充足')
    expect(batteryDisplay(b).detail).toContain('3900 mV')
    b.level = ''; b.voltageMV = null
    expect(batteryDisplay(b).value).toBe('未知')
    b.percent = 0
    expect(batteryDisplay(b).value).toBe('0')
    expect(batteryDisplay(b).unit).toBe('%')
  })
})

test('充电查询失败时保留已读到的百分比或电压', () => {
  const b = device().battery
  b.error = '充电状态查询失败'
  expect(batteryDisplay(b).hasValue).toBe(true)
  expect(batteryDisplay(b).value).toBe('68')
  b.percent = null; b.voltageMV = 3800
  expect(batteryDisplay(b).hasValue).toBe(true)
  b.voltageMV = null
  expect(batteryDisplay(b).hasValue).toBe(false)
})
