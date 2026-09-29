// 在迈从官网 Console / Snippet 运行。仅旁听网页通信，不主动打开或读写设备。
// WebHID 的 data 不含 reportId：https://hid.spec.whatwg.org/#dom-hidinputreportevent-data
(() => {
  'use strict';
  const site = 'https://www.mchose.com.cn/';
  if (window.location.origin !== site.slice(0, -1)) {
    throw new Error('此采集脚本仅允许在 https://www.mchose.com.cn 官网运行。');
  }
  if (window.MouseControlCapture) {
    console.warn('采集接口已存在，请使用现有 MouseControlCapture；重新采集请先保存并刷新页面。');
    return window.MouseControlCapture;
  }
  if (!navigator.hid || typeof HIDDevice === 'undefined') {
    throw new Error('当前浏览器未提供 WebHID，请使用 Windows Chrome 或 Edge 打开官网。');
  }
  const prototype = HIDDevice.prototype;
  const originalDescriptor = Object.getOwnPropertyDescriptor(prototype, 'sendReport');
  if (!originalDescriptor || typeof originalDescriptor.value !== 'function') {
    throw new Error('未找到可包装的 WebHID sendReport，未启动采集。');
  }
  const originalSend = originalDescriptor.value;
  const limits = { frames: 512, frameBytes: 256, devices: 16, collections: 32, reports: 64, items: 8, summary: 128 };
  const startedAt = new Date().toISOString();
  let active = true, stoppedAt = null, enumerationPending = true;
  const bindings = new Map(), devices = [], rawFrames = [], summary = new Map();
  const statistics = { observedReports: 0, droppedFrames: 0, oversizeReports: 0, droppedSummary: 0, droppedDevices: 0, truncatedDescriptors: 0, captureErrors: 0, enumerationErrors: 0 };
  const readCommands = new Set([0x0900, 0x0002, 0x0003]);
  const boundedNumber = (value, max) => Number.isInteger(value) && value >= 0 && value <= max ? value : null;
  const safely = action => { try { action(); } catch { statistics.captureErrors++; } };

  function describe(device, index) {
    const inputKeyboardIDs = new Set(), outputKeyboardIDs = new Set();
    let truncated = false, reportCount = 0;
    const collections = [], seen = new Set();
    const queue = Array.from(device.collections || [], collection => ({ collection, keyboard: false }));
    const reports = (list, keyboard, blocked) => {
      const result = [];
      for (const report of list || []) {
        if (++reportCount > limits.reports) { truncated = true; break; }
        const reportId = boundedNumber(report.reportId, 255);
        if (reportId === null) continue;
        if (keyboard) blocked.add(reportId);
        const items = [];
        for (const item of report.items || []) {
          if (items.length >= limits.items) { truncated = true; break; }
          if (item.usagePage === 7 || (item.usages || []).some(usage => usage >>> 16 === 7)) blocked.add(reportId);
          items.push({ reportSize: boundedNumber(item.reportSize, 65535), reportCount: boundedNumber(item.reportCount, 65535) });
        }
        result.push({ reportId, items });
      }
      return result;
    };
    while (queue.length) {
      if (collections.length >= limits.collections) { truncated = true; break; }
      const { collection, keyboard: inherited } = queue.shift();
      if (seen.has(collection)) { truncated = true; continue; }
      seen.add(collection);
      const usagePage = boundedNumber(collection.usagePage, 65535), usage = boundedNumber(collection.usage, 65535);
      const keyboard = inherited || usagePage === 7 || (usagePage === 1 && (usage === 6 || usage === 7));
      collections.push({ usagePage, usage,
        inputReports: reports(collection.inputReports, keyboard, inputKeyboardIDs),
        outputReports: reports(collection.outputReports, keyboard, outputKeyboardIDs) });
      for (const child of collection.children || []) {
        if (queue.length + collections.length >= limits.collections) { truncated = true; break; }
        queue.push({ collection: child, keyboard });
      }
    }
    if (truncated) statistics.truncatedDescriptors++;
    const productName = typeof device.productName === 'string' ? device.productName.replace(/[\x00-\x1f\x7f]/g, ' ').slice(0, 120) : '';
    return { descriptor: { index, vendorId: 0x3837, productId: boundedNumber(device.productId, 65535), productName, collections, truncated }, inputKeyboardIDs, outputKeyboardIDs, truncated };
  }

  function bind(device) {
    if (!active || device.vendorId !== 0x3837) return null;
    if (bindings.has(device)) return bindings.get(device);
    if (devices.length >= limits.devices) { statistics.droppedDevices++; return null; }
    const entry = describe(device, devices.length + 1);
    entry.listener = event => safely(() => observe(entry, 'in', event.reportId, event.data));
    device.addEventListener('inputreport', entry.listener);
    bindings.set(device, entry);
    devices.push(entry.descriptor);
    return entry;
  }

  function dataView(data) {
    if (ArrayBuffer.isView(data)) return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
    if (Object.prototype.toString.call(data) === '[object ArrayBuffer]') return new Uint8Array(data);
    throw new Error('非 BufferSource 报文');
  }

  function observe(entry, direction, reportId, data) {
    if (!active || !entry) return;
    const bytes = dataView(data), length = bytes.byteLength;
    statistics.observedReports++;
    const blocked = entry.truncated || (direction === 'in' ? entry.inputKeyboardIDs : entry.outputKeyboardIDs).has(reportId);
    // 命令为小端序；只读 data 的偏移 3/4，不能把独立的 reportId 再算一次。
    const command = reportId === 0x4d && length >= 5 && !blocked ? bytes[3] | bytes[4] << 8 : null;
    const commandText = command === null ? null : command.toString(16).padStart(4, '0').toUpperCase();
    if (reportId === 0x4d && readCommands.has(command) && !blocked && length <= limits.frameBytes) {
      if (rawFrames.length >= limits.frames) { statistics.droppedFrames++; return; }
      rawFrames.push({ device: entry.descriptor.index, direction, timestamp: new Date().toISOString(), reportId, command: commandText,
        byteLength: length, hex: Array.from(bytes, value => value.toString(16).padStart(2, '0').toUpperCase()).join(' ') });
      return;
    }
    if (length > limits.frameBytes) statistics.oversizeReports++;
    const id = boundedNumber(reportId, 255);
    const key = [entry.descriptor.index, direction, id, commandText, length].join(':');
    if (summary.has(key)) { summary.get(key).count++; return; }
    if (summary.size >= limits.summary) { statistics.droppedSummary++; return; }
    summary.set(key, { device: entry.descriptor.index, direction, reportId: id, command: commandText, byteLength: length, count: 1 });
  }

  // 不使用 async、不处理官网 Promise；保持返回对象、this、参数和异常完全不变。
  function captureSendReport(reportId, data) {
    if (active) safely(() => observe(bind(this), 'out', reportId, data));
    return Reflect.apply(originalSend, this, arguments);
  }

  function warnings() {
    const result = [];
    if (!rawFrames.length) result.push('未捕获有效读取报文；仍可导出摘要，但不算采集成功。请确认官网已显示读数。');
    else if (!rawFrames.some(frame => frame.direction === 'in')) result.push('未捕获设备读取响应；只有发送记录不能确认读数。');
    if (enumerationPending) result.push('已授权设备列表尚未返回。');
    if (statistics.enumerationErrors) result.push('查询已授权设备列表失败；之后由官网发送报告的设备仍可自动绑定。');
    if (statistics.droppedFrames) result.push('原始读取报文达到 512 条上限，后续报文已计入丢弃数量。');
    if (statistics.truncatedDescriptors || statistics.droppedDevices || statistics.droppedSummary || statistics.oversizeReports) result.push('部分数据超过采集边界，仅保留摘要或丢弃计数。');
    if (statistics.captureErrors) result.push('部分记录失败；官网原始通信仍原样转发。');
    return result;
  }

  function status() {
    const receivedFrames = rawFrames.filter(frame => frame.direction === 'in').length;
    const result = { active, startedAt, stoppedAt, enumerationPending, deviceCount: devices.length, capturedFrames: rawFrames.length,
      receivedFrames, sentFrames: rawFrames.length - receivedFrames,
      droppedFrames: statistics.droppedFrames, statistics: { ...statistics }, warnings: warnings() };
    if (!receivedFrames) console.warn(result.warnings[0]);
    return result;
  }

  function stop() {
    if (active) {
      active = false;
      stoppedAt = new Date().toISOString();
      // 不覆盖采集启动后其他工具安装的包装函数。
      safely(() => { if (prototype.sendReport === captureSendReport) Object.defineProperty(prototype, 'sendReport', originalDescriptor); });
      for (const [device, entry] of bindings) safely(() => device.removeEventListener('inputreport', entry.listener));
      bindings.clear();
    }
    return status();
  }

  function browserVersion() {
    const agent = navigator.userAgent || '';
    for (const [token, name] of [['Edg', 'Edge'], ['Chrome', 'Chrome'], ['Chromium', 'Chromium']]) {
      const match = agent.match(new RegExp('\\b' + token + '/([0-9.]{1,32})'));
      if (match) return { name, version: match[1] };
    }
    return { name: '未知', version: '未知' };
  }

  function save() {
    stop();
    const report = { schema: 'mousecontrol.mchose.readonly.v1', site, startedAt, stoppedAt, exportedAt: new Date().toISOString(), browser: browserVersion(),
      payloadIncludesReportId: false, outgoingMeaning: '官网 sendReport 调用时的原始参数，不代表发送成功或设备确认。',
      limits: { ...limits }, devices, statistics: { ...statistics }, summary: Array.from(summary.values()), rawFrames, warnings: warnings() };
    const text = JSON.stringify(report, null, 2);
    const url = URL.createObjectURL(new Blob([text], { type: 'application/json;charset=utf-8' }));
    const anchor = document.createElement('a');
    try {
      anchor.href = url;
      anchor.download = 'mousecontrol-mchose-readonly.json';
      document.body.appendChild(anchor);
      anchor.click();
    } finally {
      anchor.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    }
    console.info('已请求下载 mousecontrol-mchose-readonly.json，采集已停止；请确认下载完成后手动发送文件。');
    return JSON.parse(text);
  }

  const api = Object.freeze({ save, stop, status });
  Object.defineProperty(prototype, 'sendReport', { ...originalDescriptor, value: captureSendReport });
  try {
    Object.defineProperty(window, 'MouseControlCapture', { value: api, configurable: true });
  } catch (error) {
    Object.defineProperty(prototype, 'sendReport', originalDescriptor);
    throw error;
  }
  try {
    Promise.resolve(navigator.hid.getDevices()).then(list => {
      enumerationPending = false;
      if (active) for (const device of list) safely(() => bind(device));
    }).catch(() => { enumerationPending = false; statistics.enumerationErrors++; });
  } catch {
    enumerationPending = false;
    statistics.enumerationErrors++;
  }
  console.info('迈从只读采集已启动。请在官网连接设备并查看读数；完成后运行 MouseControlCapture.save()。');
  return api;
})();
