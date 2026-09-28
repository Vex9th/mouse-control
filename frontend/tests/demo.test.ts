import { expect, test } from 'bun:test'
import { createDemoAPI } from '../src/demo'

test('压力预览提供可分页设备与完整长消息，刷新失败保留原始原因', async () => {
  const api = createDemoAPI(true)
  const first = await api.scan()
  expect(first.devices.length).toBeGreaterThan(8)
  expect(first.devices[0]!.notes.join('').length).toBeGreaterThan(600)
  expect(new Set(first.devices.map(device => device.id)).size).toBe(first.devices.length)
  await expect(api.scan()).rejects.toThrow('压力预览的完整错误结束')
})
