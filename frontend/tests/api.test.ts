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
