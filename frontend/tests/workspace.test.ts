import { expect, test } from 'bun:test'
import { createWorkspace } from '../src/workspace'
import type { MouseAPI, Snapshot } from '../src/types'
import { device, snapshot } from './fixtures'
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: Error) => void; const promise = new Promise<T>((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
function apiWithScan(scan: () => Promise<Snapshot>): Pick<MouseAPI, 'scan' | 'setDPI' | 'setRate'> { return { scan, setDPI: async () => { throw Error('unexpected write') }, setRate: async () => { throw Error('unexpected write') } } }

test('设备断开保留选择，不自动切换到替代鼠标', async () => {
  let devices = [device('one')]
  const w = createWorkspace(apiWithScan(async () => snapshot(devices)))
  await w.scan()
  expect(w.state.selectedId).toBe('one')
  devices = [device('replacement')]
  await w.scan()
  expect(w.state.selectedId).toBe('one')
  expect(w.selected()).toBeNull()
  w.select('replacement')
  expect(w.selected()?.id).toBe('replacement')
})

test('忙时拒绝重入，旧请求在dispose后不得覆盖状态', async () => {
  const response = deferred<Snapshot>(); let calls = 0
  const w = createWorkspace(apiWithScan(() => { calls++; return response.promise }))
  const first = w.scan()
  await w.scan()
  expect(calls).toBe(1)
  expect(w.state.busy).toBe('scan')
  w.dispose()
  response.resolve(snapshot())
  await first
  expect(w.state.snapshot).toBeNull()
})

test('刷新失败保留上次读数并明确过期，不捏造空设备', async () => {
  let fail = false
  const w = createWorkspace(apiWithScan(async () => { if (fail) throw Error('接收器访问失败'); return snapshot() }))
  await w.scan(); fail = true; await w.scan()
  expect(w.state.snapshot?.devices.length).toBe(1)
  expect(w.state.stale).toBe(true)
  expect(w.state.error).toBe('接收器访问失败')
})

test('应用结果归属原设备，不能把切换后的鼠标替换成旧结果', async () => {
  const response = deferred<import('../src/types').Mutation>()
  const api = apiWithScan(async () => snapshot([device('one'), device('two')]))
  api.setDPI = () => response.promise
  const w = createWorkspace(api)
  await w.scan(); w.select('one')
  const work = w.applyDPI(1600, 1600)
  w.select('two')
  response.resolve({ changed: true, message: '读回确认', device: { ...device('one'), dpi: { x: 1600, y: 1600, error: '' } } })
  await work
  expect(w.state.selectedId).toBe('two')
  expect(w.selected()?.dpi.x).toBe(800)
  expect(w.state.snapshot?.devices[0]?.dpi.x).toBe(1600)
})

test('设置响应身份不匹配时保留原设备并标记结果待刷新', async () => {
  const api = apiWithScan(async () => snapshot())
  api.setDPI = async () => ({ changed: true, message: '读回确认', device: device('foreign') })
  const w = createWorkspace(api)
  await w.scan()
  expect(await w.applyDPI(1600, 1600)).toBe(false)
  expect(w.state.error).toContain('身份不匹配')
  expect(w.state.stale).toBe(true)
  expect(w.selected()?.id).toBe('usb:one')
})
