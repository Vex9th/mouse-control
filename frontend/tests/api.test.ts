import { expect, test } from 'bun:test'
import { NativeMouseAPI } from '../src/api'
import { snapshot } from './fixtures'
import type { MouseRequest } from '../src/types'

test('原生接收不等于完成，忽略不匹配ID并等待最终事件', async () => {
  const target = new EventTarget(); let request!: MouseRequest
  const api = new NativeMouseAPI({ events: target, submit: async q => { request = q; return true } }, 100)
  let done = false
  const result = api.scan().then(v => { done = true; return v })
  await Promise.resolve(); await Promise.resolve()
  expect(done).toBe(false)
  target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: 'foreign', result: snapshot() } }))
  expect(done).toBe(false)
  target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, result: snapshot() } }))
  expect((await result).devices.length).toBe(1)
  api.dispose()
})

test('原生拒绝、超时与错误字段均成为明确失败', async () => {
  const target = new EventTarget()
  const rejected = new NativeMouseAPI({ events: target, submit: async () => false }, 20)
  await expect(rejected.scan()).rejects.toThrow('未接受')
  rejected.dispose()
  const timeout = new NativeMouseAPI({ events: target, submit: async () => true }, 10)
  await expect(timeout.scan()).rejects.toThrow('超时')
  timeout.dispose()
})

test('畸形设备结果不能进入页面冒充有效读数', async () => {
  const target = new EventTarget(); let request!: MouseRequest
  const api = new NativeMouseAPI({ events: target, submit: async q => { request = q; return true } }, 100)
  const result = api.scan()
  await Promise.resolve()
  const malformed = snapshot()
  malformed.devices[0]!.battery = {} as never
  target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, result: malformed } }))
  await expect(result).rejects.toThrow('格式无效')
  api.dispose()
})

test('匹配请求的错误事件保留服务原始原因', async () => {
  const target = new EventTarget(); let request!: MouseRequest
  const api = new NativeMouseAPI({ events: target, submit: async q => { request = q; return true } }, 100)
  const result = api.setDPI('usb:one', 1600, 1600)
  target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, error: '所选设备身份已经改变' } }))
  await expect(result).rejects.toThrow('身份已经改变')
  api.dispose()
})

test('迈从只读结果通过桥接校验，未支持品牌仍被拒绝', async () => {
  const target = new EventTarget(); let request!: MouseRequest
  const api = new NativeMouseAPI({ events: target, submit: async q => { request = q; return true } }, 100)
  for (const vendor of ['mchose', 'unknown']) {
    const result = api.scan()
    const value = snapshot()
    Object.assign(value.devices[0]!, { vendor, id: 'mchose:3837:4026', name: '迈从 A7' })
    value.devices[0]!.capabilities.dpiReadOnly = true
    value.devices[0]!.capabilities.rateReadOnly = true
    target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, result: value } }))
    if (vendor === 'mchose') expect((await result).devices[0]!.vendor).toBe('mchose')
    else await expect(result).rejects.toThrow('格式无效')
  }
  api.dispose()
})

test('诊断日志校验完整字段与字节上限，复制只在原生成功后确认', async () => {
  const target = new EventTarget(); let request!: MouseRequest
  const api = new NativeMouseAPI({ events: target, submit: async q => { request = q; return true } }, 100)
  const report = { text: '鼠标工具 1.5.1\n设备响应格式不匹配', location: '%LOCALAPPDATA%\\MouseControl\\logs\\error.log', saveError: '' }
  for (const value of [report, { ...report, text: null }, { ...report, text: '错'.repeat(50_000) }]) {
    const result = api.diagnostics()
    expect(request.action).toBe('diagnostics')
    target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, result: value } }))
    if (value === report) expect((await result).text).toContain('响应格式不匹配')
    else await expect(result).rejects.toThrow('日志格式无效')
  }
  const copying = api.copyDiagnostics()
  expect(request.action).toBe('copyDiagnostics')
  target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, error: '剪贴板正被其他程序使用' } }))
  await expect(copying).rejects.toThrow('剪贴板')
  api.dispose()
})

test('提交问题必须等待打开页面的成功回执', async () => {
  const target = new EventTarget(); let request!: MouseRequest
  const api = new NativeMouseAPI({ events: target, submit: async q => { request = q; return true } }, 100)
  for (const value of [{ opened: true }, { opened: false }, {}]) {
    const result = api.openIssue()
    expect(request.action).toBe('openIssue')
    target.dispatchEvent(new CustomEvent('mouse:response', { detail: { id: request.id, result: value } }))
    if ('opened' in value && value.opened) await result
    else await expect(result).rejects.toThrow('未能确认')
  }
  api.dispose()
})
