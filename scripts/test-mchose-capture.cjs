// 离线模拟 WebHID；不会连接设备、上传数据或触发真实下载。
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');

const source = readFileSync(join(__dirname, 'mchose-capture.js'), 'utf8');
const tick = () => new Promise(resolve => setImmediate(resolve));
const frame = (command, length = 63) => {
  const bytes = new Uint8Array(length);
  bytes[0] = 1; bytes[1] = 1;
  bytes[3] = command & 255; bytes[4] = command >>> 8;
  return bytes;
};

function fixture(options = {}) {
  const calls = { getDevices: 0, send: [], forbidden: 0, downloads: [], revoked: [] };
  const logs = [];
  const forbidden = () => { calls.forbidden++; throw new Error('采集器不得访问此接口'); };
  class HIDDevice {
    constructor(vendorId = 0x3837, productId = 0x1014) {
      this.vendorId = vendorId; this.productId = productId; this.productName = 'MCHOSE A5';
      this.listeners = new Set();
      this.collections = [{ usagePage: 0xff01, usage: 1, inputReports: [{ reportId: 0x4d, items: [{ reportSize: 8, reportCount: 63 }] }], outputReports: [{ reportId: 0x4d, items: [{ reportSize: 8, reportCount: 63 }] }], children: [] }];
      Object.defineProperty(this, 'serialNumber', { get: forbidden });
    }
    addEventListener(type, listener) { assert.equal(type, 'inputreport'); this.listeners.add(listener); }
    removeEventListener(type, listener) { assert.equal(type, 'inputreport'); this.listeners.delete(listener); }
    emit(reportId, data) { for (const listener of this.listeners) listener({ device: this, reportId, data }); }
    sendReport(...args) {
      calls.send.push({ device: this, args });
      return options.send ? options.send.call(this, ...args) : undefined;
    }
    open() { return forbidden(); }
    sendFeatureReport() { return forbidden(); }
    receiveFeatureReport() { return forbidden(); }
  }
  const device = new HIDDevice();
  const other = new HIDDevice(0x046d);
  const hid = {
    getDevices() { calls.getDevices++; return options.getDevices ? options.getDevices() : Promise.resolve([device, other]); },
    requestDevice: forbidden,
  };
  const blobs = new Map();
  const document = {
    createElement(tag) {
      assert.equal(tag, 'a');
      return { href: '', download: '', click() { calls.downloads.push({ href: this.href, name: this.download, blob: blobs.get(this.href) }); }, remove() {} };
    },
    body: { appendChild() {} },
  };
  Object.defineProperty(document, 'cookie', { get: forbidden });
  const origin = options.origin || 'https://www.mchose.com.cn';
  const context = vm.createContext({
    HIDDevice, navigator: { hid, userAgent: 'Mozilla/5.0 SecretDeviceInfo Chrome/130.0.0.0 Safari/537.36 Edg/130.0.2849.52' },
    location: { origin, href: origin + '/driver?private=DO_NOT_EXPORT#SECRET_FRAGMENT' },
    document, Blob,
    URL: { createObjectURL(blob) { const url = 'blob:' + blobs.size; blobs.set(url, blob); return url; }, revokeObjectURL(url) { calls.revoked.push(url); } },
    setTimeout(callback) { callback(); return 1; },
    console: { info: (...args) => logs.push(args.join(' ')), warn: (...args) => logs.push(args.join(' ')) },
    fetch: forbidden, XMLHttpRequest: forbidden,
  });
  Object.defineProperty(context, 'localStorage', { get: forbidden });
  context.window = context;
  const original = HIDDevice.prototype.sendReport;
  return { context, calls, logs, device, other, HIDDevice, original, install: () => vm.runInContext(source, context) };
}

async function exported(f) {
  f.context.MouseControlCapture.save();
  const download = f.calls.downloads.at(-1);
  assert.equal(download.name, 'mousecontrol-mchose-readonly.json');
  return JSON.parse(await download.blob.text());
}

test('仅精确官网 HTTPS 来源可安装，安装只查询已授权设备', async () => {
  for (const origin of ['http://www.mchose.com.cn', 'https://mchose.com.cn', 'https://www.mchose.com.cn.evil.test', 'https://www.mchose.com.cn:8443']) {
    const f = fixture({ origin });
    assert.throws(f.install, /官网|mchose/);
    assert.equal(f.HIDDevice.prototype.sendReport, f.original);
    assert.equal(f.calls.getDevices, 0);
  }
  const f = fixture(); f.install(); await tick();
  assert.equal(typeof f.context.MouseControlCapture.save, 'function');
  assert.equal(f.calls.getDevices, 1);
  assert.equal(f.calls.send.length, 0);
  assert.equal(f.calls.forbidden, 0);
  assert.equal(f.device.listeners.size, 1);
  assert.equal(f.other.listeners.size, 0);
});

test('BufferSource 子视图按 byteOffset/byteLength 记录，命令位于不含 RID 的偏移 3/4', async () => {
  const marker = Promise.resolve('官网返回值');
  const f = fixture({ send: () => marker }); f.install(); await tick();
  const backing = new Uint8Array(90).fill(0xee);
  backing.set(frame(0x0900), 11);
  const outgoing = new Uint8Array(backing.buffer, 11, 63);
  assert.equal(f.device.sendReport(0x4d, outgoing), marker);
  assert.equal(f.calls.send[0].device, f.device);
  assert.equal(f.calls.send[0].args[1], outgoing);
  const incoming = new DataView(backing.buffer, 11, 63);
  f.device.emit(0x4d, incoming);
  backing.fill(0xaa);
  const result = await exported(f);
  assert.equal(result.rawFrames.length, 2);
  for (const record of result.rawFrames) {
    assert.equal(record.command, '0900');
    assert.equal(record.byteLength, 63);
    assert.equal(record.hex, Array.from(frame(0x0900), x => x.toString(16).padStart(2, '0').toUpperCase()).join(' '));
    assert.equal(record.reportId, 0x4d);
    assert.ok(!Number.isNaN(Date.parse(record.timestamp)));
  }
  assert.deepEqual(result.rawFrames.map(x => x.direction), ['out', 'in']);
});

test('只保留指定 VID/RID 的三条读取命令，其余仅汇总且不保留 payload', async () => {
  const f = fixture(); f.install(); await tick();
  for (const command of [0x0900, 0x0002, 0x0003]) f.device.sendReport(0x4d, frame(command));
  for (const command of [0x0001, 0x0200, 0x8003]) {
    const bytes = frame(command); bytes.set(Buffer.from('SECRET_PAYLOAD'), 9);
    f.device.sendReport(0x4d, bytes);
    f.device.emit(0x4d, new DataView(bytes.buffer));
  }
  f.device.emit(1, new DataView(frame(0x0900).buffer));
  f.other.sendReport(0x4d, frame(0x0900));
  const result = await exported(f);
  assert.equal(result.rawFrames.length, 3);
  assert.equal(result.summary.reduce((n, item) => n + item.count, 0), 7);
  for (const item of result.summary) { assert.equal(item.hex, undefined); assert.equal(item.payload, undefined); }
  assert.equal(JSON.stringify(result).includes(Buffer.from('SECRET_PAYLOAD').toString('hex').toUpperCase()), false);
  assert.equal(result.devices.length, 1);
});

test('键盘 collection 即使碰巧匹配 RID/命令也不采集输入 payload', async () => {
  const f = fixture();
  f.device.collections = [{ usagePage: 1, usage: 6, inputReports: [{ reportId: 0x4d, items: [] }], outputReports: [], children: [] }];
  f.install(); await tick();
  f.device.emit(0x4d, new DataView(frame(0x0900).buffer));
  const result = await exported(f);
  assert.equal(result.rawFrames.length, 0);
  assert.equal(result.summary[0].count, 1);
});

test('官网首次发送前绑定新设备；原始返回值和同步异常均不被包装', async () => {
  const failure = new Error('官网错误');
  const marker = {};
  let fail = false;
  const f = fixture({ getDevices: () => Promise.resolve([]), send(reportId, data) {
    if (fail) throw failure;
    this.emit(reportId, new DataView(data.buffer, data.byteOffset, data.byteLength));
    return marker;
  } });
  f.install(); await tick();
  assert.equal(f.device.sendReport(0x4d, frame(2)), marker);
  fail = true;
  assert.throws(() => f.device.sendReport(0x4d, frame(2)), error => error === failure);
  const result = await exported(f);
  assert.equal(result.rawFrames.length, 3);
  assert.deepEqual(result.rawFrames.map(x => x.direction), ['out', 'in', 'out']);
});

test('采集元数据异常不阻止官网 I/O，Promise 对象与拒绝原因保持不变', async () => {
  const reason = new Error('官网 Promise 拒绝');
  const result = Promise.reject(reason); result.catch(() => {});
  const f = fixture({ send: () => result });
  Object.defineProperty(f.device, 'productName', { get() { throw Error('元数据读取失败'); } });
  f.install(); await tick();
  assert.equal(f.device.sendReport(0x4d, frame(3)), result);
  await assert.rejects(result, error => error === reason);
  assert.equal(f.calls.send.length, 1);
  assert.equal(f.calls.forbidden, 0);
});

test('重复安装不叠加 hook，stop 解绑并仅恢复自己仍持有的 hook', async () => {
  const f = fixture(); f.install(); await tick();
  const api = f.context.MouseControlCapture, hook = f.HIDDevice.prototype.sendReport;
  f.install();
  assert.equal(f.context.MouseControlCapture, api);
  assert.equal(f.HIDDevice.prototype.sendReport, hook);
  assert.equal(f.calls.getDevices, 1);
  api.stop(); api.stop();
  assert.equal(f.HIDDevice.prototype.sendReport, f.original);
  assert.equal(f.device.listeners.size, 0);
  hook.call(f.device, 0x4d, frame(2));
  assert.equal(api.status().capturedFrames, 0);
  const g = fixture(); g.install(); await tick();
  const later = function () {};
  g.HIDDevice.prototype.sendReport = later;
  g.context.MouseControlCapture.stop();
  assert.equal(g.HIDDevice.prototype.sendReport, later);
  assert.equal(g.device.listeners.size, 0);
});

test('原始帧最多 512 条并统计丢弃，超长帧和汇总种类同样受限', async () => {
  const f = fixture(); f.install(); await tick();
  for (let i = 0; i < 520; i++) f.device.sendReport(0x4d, frame(3));
  f.device.sendReport(0x4d, frame(3, 4096));
  for (let i = 0; i < 200; i++) f.device.sendReport(i, frame(0x8000, 10 + i));
  const result = await exported(f);
  assert.equal(result.rawFrames.length, 512);
  assert.equal(result.statistics.droppedFrames, 8);
  assert.equal(result.statistics.oversizeReports, 1);
  assert.ok(result.summary.length <= 128);
  assert.ok(result.statistics.droppedSummary > 0);
});

test('save 停止后导出固定 schema/站点/浏览器版本和安全描述符，无有效帧仍明确警告', async () => {
  const f = fixture(); f.install(); await tick();
  assert.ok(f.context.MouseControlCapture.status().warnings.some(x => /未捕获/.test(x)));
  const result = await exported(f);
  assert.equal(result.schema, 'mousecontrol.mchose.readonly.v1');
  assert.equal(result.site, 'https://www.mchose.com.cn/');
  assert.equal(result.browser.name, 'Edge');
  assert.equal(result.browser.version, '130.0.2849.52');
  assert.equal(result.payloadIncludesReportId, false);
  assert.ok(result.warnings.some(x => /未捕获/.test(x)));
  assert.equal(f.context.MouseControlCapture.status().active, false);
  assert.equal(f.device.listeners.size, 0);
  assert.equal(f.HIDDevice.prototype.sendReport, f.original);
  assert.equal(result.devices[0].vendorId, 0x3837);
  assert.equal(result.devices[0].productId, 0x1014);
  assert.deepEqual(result.devices[0].collections[0].inputReports[0], { reportId: 0x4d, items: [{ reportSize: 8, reportCount: 63 }] });
  assert.ok(!/DO_NOT_EXPORT|SECRET_FRAGMENT|SecretDeviceInfo|serialNumber|cookie|localStorage/.test(JSON.stringify(result)));
  assert.equal(f.calls.forbidden, 0);
  assert.equal(f.calls.revoked.length, 1);
});

test('停止后晚到的授权枚举不能重新绑定；枚举失败有摘要警告', async () => {
  let resolve;
  const f = fixture({ getDevices: () => new Promise(r => { resolve = r; }) });
  f.install(); f.context.MouseControlCapture.stop(); resolve([f.device]); await tick();
  assert.equal(f.device.listeners.size, 0);
  const g = fixture({ getDevices: () => Promise.reject(Error('内部身份不得导出')) });
  g.install(); await tick();
  assert.ok(g.context.MouseControlCapture.status().warnings.some(x => /授权设备/.test(x)));
  const result = await exported(g);
  assert.ok(!JSON.stringify(result).includes('内部身份不得导出'));
});

test('仅有发送记录时明确未收到读取响应，收到响应后才清除此警告', async () => {
  const f = fixture(); f.install(); await tick();
  f.device.sendReport(0x4d, frame(0x0900));
  const outgoing = f.context.MouseControlCapture.status();
  assert.equal(outgoing.sentFrames, 1);
  assert.equal(outgoing.receivedFrames, 0);
  assert.ok(outgoing.warnings.some(x => /未捕获设备读取响应/.test(x)));
  f.device.emit(0x4d, new DataView(frame(0x0900).buffer));
  const incoming = f.context.MouseControlCapture.status();
  assert.equal(incoming.sentFrames, 1);
  assert.equal(incoming.receivedFrames, 1);
  assert.ok(!incoming.warnings.some(x => /未捕获设备读取响应/.test(x)));
  const result = await exported(f);
  assert.deepEqual(result.rawFrames.map(x => x.direction), ['out', 'in']);
});
