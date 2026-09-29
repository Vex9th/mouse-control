import type { MouseAPI, MouseDevice } from './types'
const demoDevice: MouseDevice = {
  id: 'demo:razer:v4', name: '雷蛇 DeathAdder V4 Pro', vendor: 'razer', connection: '无线接收器', vid: 0x1532, pid: 0xbf, status: 'online',
  battery: { percent: 68, level: '', voltageMV: null, charging: false, chargeText: '未充电', error: '' }, dpi: { x: 1600, y: 1600, error: '' }, rate: { hz: 1000, error: '' },
  capabilities: { battery: true, dpiRanges: [{ min: 100, max: 45000, step: 1 }], separateAxes: true, pollRates: [125, 500, 1000, 2000, 4000, 8000], dpiReadOnly: false, rateReadOnly: false, batteryReason: '', dpiReason: '', rateReason: '' }, notes: ['通用鼠标示意图，不代表产品外观。'],
}
export function createDemoAPI(stress = false): MouseAPI {
  const devices: MouseDevice[] = [structuredClone(demoDevice), { ...structuredClone(demoDevice), id: 'demo:logitech:mx', name: '罗技 MX Master 3S', vendor: 'logitech', connection: 'Bolt 接收器', vid: 0x046d, pid: 0xc548, battery: { percent: null, level: '电量充足', voltageMV: 3900, charging: false, chargeText: '未充电', error: '' }, dpi: { x: 800, y: 800, error: '' }, rate: { hz: null, error: '' }, capabilities: { ...structuredClone(demoDevice.capabilities), dpiRanges: [{ min: 200, max: 8000, step: 50 }], separateAxes: false, pollRates: [], rateReadOnly: true, rateReason: '当前连接未提供回报率设置。' } }]
  if (stress) {
    for (let index = 2; index < 12; index++) devices.push({ ...structuredClone(demoDevice), id: `demo:stress:${index}`, name: `雷蛇 第 ${index + 1} 台演示鼠标` })
    devices[0]!.capabilities.pollRates = [125, 250, 500, 1000, 2000, 4000, 8000]
    devices[0]!.name = '雷蛇 DeathAdder V4 Pro 无线接收器与长型号名称排版验证'
    devices[0]!.notes = ['完整详情开始。' + '本段用于检查长消息分页显示，所有信息均需可以访问。'.repeat(35) + '完整详情结束。']
    devices[0]!.id = 'demo:stress:' + 'long-device-identity-'.repeat(18)
    const unavailable = devices[2]!
    const error = '设备未应答，可能休眠、关机或超出接收范围；请移动鼠标或检查电源'
    unavailable.status = 'unavailable'
    unavailable.battery = { ...unavailable.battery, percent: null, charging: null, chargeText: '', error }
    unavailable.dpi = { x: null, y: null, error }
    unavailable.rate = { hz: null, error }
  } else {
    devices.push({ ...structuredClone(demoDevice), id: 'demo:mchose:a7', name: '迈从 A7 V3', vendor: 'mchose', vid: 0x3837, pid: 0x4030,
      capabilities: { ...structuredClone(demoDevice.capabilities), dpiRanges: [{ min: 1, max: 26000, step: 1 }], dpiReadOnly: true, rateReadOnly: true,
        dpiReason: '迈从实验性支持，当前仅提供读取。', rateReason: '迈从实验性支持，当前仅提供读取。' },
      notes: ['仅用于界面预览；迈从协议尚无真机验证。'] })
  }
  let scans = 0
  const wait = (): Promise<void> => new Promise(resolve => setTimeout(resolve, 420))
  return {
    async scan() {
      await wait()
      if (stress && scans++ > 0) throw new Error('压力预览的完整错误开始。' + '设备未响应，保留上次读数；不要重发设置，请检查连接。'.repeat(35) + '压力预览的完整错误结束')
      return { version: '1.5.0', scannedAt: new Date().toISOString(), devices: structuredClone(devices) }
    },
    async setDPI(id, x, y) { await wait(); const d = devices.find(d => d.id === id); if (!d) throw new Error('演示设备已断开'); const changed = d.dpi.x !== x || d.dpi.y !== y; d.dpi = { x, y, error: '' }; return { changed, message: '演示：DPI 读回确认', device: structuredClone(d) } },
    async setRate(id, rate) { await wait(); const d = devices.find(d => d.id === id); if (!d) throw new Error('演示设备已断开'); const changed = d.rate.hz !== rate; d.rate = { hz: rate, error: '' }; return { changed, message: '演示：回报率读回确认', device: structuredClone(d) } },
  }
}
