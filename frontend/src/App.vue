<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { darkTheme, zhCN, NButton, NCard, NConfigProvider, NGlobalStyle, NIcon, NInput, NModal, NPagination, NPopover, NProgress, NRadio, NRadioGroup, NSwitch, NTag, NText } from 'naive-ui'
import { Activity, AlertCircle, Check, ChevronDown, InfoCircle, Mouse, Refresh, Settings } from '@vicons/tabler'
import type { MouseAPI } from './types'
import { createWorkspace } from './workspace'
import { createSettingsDraft } from './drafts'
import { batteryDisplay, formatDPIRanges, inDPIRanges, shortName, statusLabel, timeLabel, validateDPIInput, validateRateInput } from './model'
import { deviceReport, paginateText } from './display'
import { mouseTheme } from './theme'

const props = defineProps<{ api: MouseAPI; mode: 'native' | 'demo' | 'unavailable' }>()
const workspace = createWorkspace(props.api), state = workspace.state
const selected = computed(() => workspace.selected())
const devices = computed(() => state.snapshot?.devices ?? [])
const battery = computed(() => selected.value ? batteryDisplay(selected.value.battery) : null)
const autoRefresh = ref(false)
const { dpiX, dpiY, linked, dpiDirty, rateValue, rateDirty, hasChanges: draftsPending, resetDPI, resetRate, editDPI, editRate, choosePreset } = createSettingsDraft(selected)
const busy = computed(() => state.busy !== '')
const noWrite = computed(() => busy.value || state.stale || selected.value?.status !== 'online')
const dpiValidation = computed(() => selected.value ? validateDPIInput(selected.value, dpiX.value, dpiY.value, linked.value) : { value: null, error: '' })
const rateValidation = computed(() => selected.value ? validateRateInput(selected.value, rateValue.value) : { value: null, error: '' })
const dpiSame = computed(() => dpiValidation.value.value?.x === selected.value?.dpi.x && dpiValidation.value.value?.y === selected.value?.dpi.y)
const rateSame = computed(() => rateValidation.value.value !== null && rateValidation.value.value === selected.value?.rate.hz)
const dpiBlocked = computed(() => {
  const c = selected.value?.capabilities
  return !c ? '' : c.dpiReadOnly || c.dpiRanges.length === 0 ? c.dpiReason || '当前设备未提供 DPI 设置。' : ''
})
const rateBlocked = computed(() => {
  const c = selected.value?.capabilities
  return !c ? '' : c.rateReadOnly || c.pollRates.length === 0 ? c.rateReason || '当前设备未提供回报率设置。' : ''
})
const presets = computed(() => [400, 800, 1600, 3200].filter(v => selected.value && inDPIRanges(v, selected.value.capabilities.dpiRanges)))
const updatedAt = computed(() => timeLabel(selected.value ? state.deviceUpdatedAt[selected.value.id] || state.snapshot?.scannedAt : state.snapshot?.scannedAt))
async function applyDPI(): Promise<void> {
  const value = dpiValidation.value.value
  if (!value || noWrite.value || dpiSame.value) return
  if (await workspace.applyDPI(value.x, value.y)) resetDPI()
}
async function applyRate(): Promise<void> {
  const value = rateValidation.value.value
  if (value === null || noWrite.value || rateSame.value) return
  if (await workspace.applyRate(value)) resetRate()
}
function updateX(value: string): void { dpiX.value = value; editDPI() }
function updateY(value: string): void { dpiY.value = value; editDPI() }
function updateLinked(value: boolean): void { linked.value = value; editDPI() }
function updateRate(value: string | number): void { if (typeof value === 'number') { rateValue.value = value; editRate() } }

const devicePicker = ref(false), devicePage = ref(1)
const devicePageCount = computed(() => Math.max(1, Math.ceil(devices.value.length / 4)))
const visibleDevices = computed(() => devices.value.slice((devicePage.value - 1) * 4, devicePage.value * 4))
watch(devicePageCount, count => { devicePage.value = Math.min(devicePage.value, count) })
function chooseDevice(id: string): void { workspace.select(id); devicePicker.value = false }

const viewerOpen = ref(false), viewerTitle = ref(''), viewerPages = ref<string[]>(['']), viewerPage = ref(1)
function showText(title: string, text: string): void {
  viewerTitle.value = title
  viewerPages.value = paginateText(text || '未提供', 26, 7)
  viewerPage.value = 1
  viewerOpen.value = true
}
function showDeviceDetails(): void { if (selected.value) showText('设备详情', deviceReport(selected.value, updatedAt.value)) }
const dpiIssue = computed(() => dpiDirty.value && dpiValidation.value.error || dpiBlocked.value || selected.value?.dpi.error || '')
const rateIssue = computed(() => rateDirty.value && rateValidation.value.error || rateBlocked.value || selected.value?.rate.error || '')
const batteryIssue = computed(() => selected.value?.battery.error || selected.value?.capabilities.batteryReason || '')
const feedback = computed(() => state.error || state.notice)
function showFeedback(): void { showText(state.error ? '完整错误信息' : '操作结果', feedback.value + (state.stale ? '\n\n保留的是上次读数，请先刷新再设置。' : '')) }
function showDPIRanges(): void { if (selected.value) showText('DPI 范围', formatDPIRanges(selected.value.capabilities.dpiRanges) + (!linked.value ? '\nY 轴提交时按设备报告单独校验' : '')) }
const quietStatus = computed(() => {
  if (busy.value) return state.busy === 'scan' ? '正在读取设备…' : '正在设置并读回确认…'
  if (autoRefresh.value && draftsPending.value) return '存在未提交输入，自动刷新已暂停'
  if (state.stale) return '读数待刷新'
  return state.snapshot ? updatedAt.value + ' 已更新' + (autoRefresh.value ? ' · 每 30 秒自动刷新' : '') : '等待设备'
})
let refreshTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void workspace.scan()
  refreshTimer = setInterval(() => { if (autoRefresh.value && !document.hidden && !draftsPending.value) void workspace.scan() }, 30_000)
})
onBeforeUnmount(() => { clearInterval(refreshTimer); workspace.dispose() })
</script>

<template>
  <NConfigProvider :theme="darkTheme" :theme-overrides="mouseTheme" :locale="zhCN" class="app-provider">
    <NGlobalStyle/>
    <main class="app-frame" :aria-busy="busy" :data-selected-id="state.selectedId" :data-state="selected ? 'ready' : busy ? 'loading' : mode === 'unavailable' ? 'unavailable' : 'empty'">
      <header class="toolbar">
        <div class="brand"><NIcon :component="Mouse" :size="21"/><strong>鼠标工具</strong></div>
        <NTag v-if="mode === 'demo'" type="warning" size="small" :bordered="false" title="未连接真实鼠标，所有操作仅为界面演示">演示</NTag>
        <NPopover v-model:show="devicePicker" trigger="click" placement="bottom-start" :show-arrow="false" :width="340">
          <template #trigger>
            <NButton secondary size="small" class="device-trigger" :disabled="!devices.length" aria-label="选择鼠标">
              <span class="trigger-label">{{ selected ? shortName(selected) : '选择设备' }}</span>
              <template #icon><NIcon :component="ChevronDown"/></template>
            </NButton>
          </template>
          <div class="device-picker">
            <div class="picker-heading"><NText>选择设备</NText><NText depth="3">{{ devices.length }} 台</NText></div>
            <div class="device-options" aria-label="鼠标设备列表">
              <NButton v-for="d in visibleDevices" :key="d.id" block secondary size="large" :type="state.selectedId === d.id ? 'primary' : 'default'" :aria-pressed="state.selectedId === d.id" @click="chooseDevice(d.id)">
                <div class="device-option-label"><span :title="d.name">{{ d.name }}</span><span class="secondary-text" :title="d.id">{{ d.connection }} · {{ d.id.length > 12 ? '…' : '' }}{{ d.id.slice(-12) }}</span></div>
              </NButton>
            </div>
            <NPagination v-model:page="devicePage" :page-count="devicePageCount" simple size="small" aria-label="设备列表分页"/>
          </div>
        </NPopover>
        <div class="toolbar-spacer"/>
        <div class="auto-control"><NSwitch v-model:value="autoRefresh" size="small" :disabled="mode === 'unavailable'" aria-label="每 30 秒自动刷新"/><NText class="secondary-text">自动刷新</NText></div>
        <NButton size="small" :loading="state.busy === 'scan'" :disabled="busy || mode === 'unavailable'" @click="workspace.scan"><template #icon><NIcon :component="Refresh"/></template>刷新</NButton>
      </header>

      <NCard v-if="selected" class="device-summary" content-class="summary-content" content-style="padding: var(--summary-padding)" size="small" :bordered="false">
        <div class="summary-identity">
          <h1 :title="selected.name">{{ shortName(selected) }}</h1>
          <div class="summary-meta"><NTag size="small" :type="selected.status === 'online' && !state.stale ? 'success' : 'warning'" :bordered="false">{{ state.stale ? '待读取' : statusLabel(selected) }}</NTag><NText depth="3" class="connection-label">{{ selected.vendor === 'razer' ? '雷蛇' : '罗技' }} · {{ selected.connection }}</NText></div>
        </div>
        <div class="summary-battery">
          <div class="battery-heading"><NText depth="3" class="secondary-text">电量</NText><NText class="battery-value">{{ selected.capabilities.battery && battery?.hasValue ? battery.value : '未知' }}<span v-if="selected.capabilities.battery && battery?.hasValue">{{ battery.unit }}</span></NText></div>
          <NProgress v-if="selected.battery.percent !== null" type="line" :percentage="selected.battery.percent" :show-indicator="false" :height="4" :status="battery?.low ? 'warning' : 'default'"/>
          <NButton v-if="batteryIssue" text type="warning" size="tiny" class="single-line-button" @click="showText('电量信息', batteryIssue)"><span class="ellipsis">{{ batteryIssue }}</span><template #icon><NIcon :component="AlertCircle"/></template></NButton>
          <NText v-else depth="3" class="secondary-text ellipsis">{{ battery?.detail || '设备未提供电量信息' }}</NText>
        </div>
      </NCard>
      <NCard v-else class="device-summary no-device-summary" content-class="summary-content" content-style="padding: var(--summary-padding)" size="small" :bordered="false" role="status"><NIcon :component="Mouse" :size="28"/><NText>{{ busy ? '正在查找鼠标…' : state.selectedId ? '所选鼠标已断开，设备选择已保留' : '未发现可用鼠标' }}</NText></NCard>

      <div v-if="selected" class="settings-grid">
        <NCard class="control-panel" content-class="control-panel-content" content-style="padding: var(--control-padding)" size="small" :bordered="false">
          <div class="panel-heading"><h2><NIcon :component="Settings" :size="18"/>灵敏度</h2><div class="reading"><NText depth="3" class="secondary-text">当前</NText><NText class="readout">{{ selected.dpi.error ? '—' : selected.dpi.x?.toLocaleString('zh-CN') ?? '—' }}<span v-if="!selected.dpi.error && selected.dpi.y !== null && selected.dpi.y !== selected.dpi.x"> / {{ selected.dpi.y.toLocaleString('zh-CN') }}</span></NText><NText depth="3" class="secondary-text">DPI</NText></div></div>
          <form class="control-form" @submit.prevent="applyDPI">
            <div class="form-content">
              <div class="field-heading"><label for="dpi-x">目标 DPI</label><div v-if="selected.capabilities.separateAxes" class="axis-control"><NText class="secondary-text">双轴同步</NText><NSwitch :value="linked" size="small" :disabled="noWrite || !!dpiBlocked" aria-label="双轴同步" @update:value="updateLinked"/></div></div>
              <div class="dpi-fields">
                <NInput :value="dpiX" :disabled="noWrite || !!dpiBlocked" :maxlength="5" :status="dpiDirty && dpiValidation.error ? 'error' : undefined" :input-props="{ id: 'dpi-x', inputmode: 'numeric', 'aria-label': linked ? '目标 DPI' : 'X 轴 DPI', 'aria-describedby': 'dpi-range' }" placeholder="输入 DPI" @update:value="updateX"><template v-if="!linked" #prefix>X</template></NInput>
                <NInput v-if="!linked" :value="dpiY" :disabled="noWrite || !!dpiBlocked" :maxlength="5" :status="dpiDirty && dpiValidation.error ? 'error' : undefined" :input-props="{ id: 'dpi-y', inputmode: 'numeric', 'aria-label': 'Y 轴 DPI', 'aria-describedby': 'dpi-range' }" placeholder="Y 轴 DPI" @update:value="updateY"><template #prefix>Y</template></NInput>
              </div>
              <div class="preset-row" aria-label="常用 DPI"><NButton v-for="preset in presets" :key="preset" size="tiny" :type="dpiX === String(preset) ? 'primary' : 'default'" :secondary="dpiX === String(preset)" :disabled="noWrite || !!dpiBlocked" @click="choosePreset(preset)">{{ preset }}</NButton></div>
              <div class="range-row"><NText id="dpi-range" depth="3" class="secondary-text ellipsis">{{ selected.capabilities.dpiRanges.length ? formatDPIRanges(selected.capabilities.dpiRanges) : '未提供 DPI 范围' }}{{ !linked ? ' · Y 轴单独校验' : '' }}</NText><NButton v-if="selected.capabilities.dpiRanges.length" text size="tiny" aria-label="查看完整 DPI 范围" @click="showDPIRanges"><NIcon :component="InfoCircle" :size="15"/></NButton></div>
            </div>
            <div class="panel-footer"><NButton v-if="dpiIssue" text type="warning" size="tiny" class="single-line-button" @click="showText('DPI 状态', dpiIssue)"><span class="ellipsis">{{ dpiIssue }}</span><template #icon><NIcon :component="AlertCircle"/></template></NButton><NText v-else depth="3" class="secondary-text">{{ dpiSame ? '已是当前值' : '设置后读回确认' }}</NText><NButton attr-type="submit" type="primary" size="small" :loading="state.busy === 'dpi'" :disabled="noWrite || !dpiValidation.value || dpiSame">应用 DPI</NButton></div>
          </form>
        </NCard>
        <NCard class="control-panel" content-class="control-panel-content" content-style="padding: var(--control-padding)" size="small" :bordered="false">
          <div class="panel-heading"><h2><NIcon :component="Activity" :size="18"/>回报率</h2><div class="reading"><NText depth="3" class="secondary-text">当前</NText><NText class="readout">{{ selected.rate.error ? '—' : selected.rate.hz?.toLocaleString('zh-CN') ?? '—' }}</NText><NText depth="3" class="secondary-text">Hz</NText></div></div>
          <form class="control-form" @submit.prevent="applyRate">
            <div class="form-content">
              <div class="field-heading"><span>目标回报率</span><NText depth="3" class="secondary-text">Hz</NText></div>
              <NRadioGroup :value="rateValue" :disabled="noWrite || !!rateBlocked" size="small" class="rate-options" aria-label="目标回报率" @update:value="updateRate"><NRadio v-for="rate in selected.capabilities.pollRates" :key="rate" :value="rate">{{ rate.toLocaleString('zh-CN') }}</NRadio></NRadioGroup>
              <NText v-if="!selected.capabilities.pollRates.length" depth="3" class="secondary-text">设备未提供可设置档位</NText>
              <NText depth="3" class="secondary-text rate-note">更高的回报率通常会增加耗电。</NText>
            </div>
            <div class="panel-footer"><NButton v-if="rateIssue" text type="warning" size="tiny" class="single-line-button" @click="showText('回报率状态', rateIssue)"><span class="ellipsis">{{ rateIssue }}</span><template #icon><NIcon :component="AlertCircle"/></template></NButton><NText v-else depth="3" class="secondary-text">{{ rateSame ? '已是当前值' : '设置后读回确认' }}</NText><NButton attr-type="submit" type="primary" size="small" :loading="state.busy === 'rate'" :disabled="noWrite || rateValidation.value === null || rateSame">应用回报率</NButton></div>
          </form>
        </NCard>
      </div>
      <NCard v-else class="empty-panel" content-class="empty-panel-content" content-style="padding: 16px" size="small" :bordered="false" role="status">
        <NIcon :component="mode === 'unavailable' ? AlertCircle : Mouse" :size="32"/>
        <h2>{{ mode === 'unavailable' ? '请运行 Windows 桌面程序' : state.selectedId ? '等待原来的鼠标重新连接' : busy ? '正在检查设备连接' : '连接鼠标后刷新' }}</h2>
        <NText depth="3">{{ mode === 'unavailable' ? '此页面没有连接本机服务。' : state.selectedId ? '也可从顶部明确选择另一只鼠标。' : '支持可访问的 USB、无线接收器与蓝牙鼠标。' }}</NText>
        <NButton v-if="mode !== 'unavailable'" size="small" :disabled="busy" @click="workspace.scan"><template #icon><NIcon :component="Refresh"/></template>重新查找</NButton>
      </NCard>
      <footer class="status-bar" :role="state.error ? 'alert' : 'status'">
        <NButton v-if="feedback" text size="tiny" :type="state.error ? 'warning' : 'success'" class="feedback-button" :aria-label="state.error ? '查看完整错误信息' : '查看操作结果'" @click="showFeedback"><template #icon><NIcon :component="state.error ? AlertCircle : Check"/></template><span class="ellipsis">{{ feedback }}</span><span class="feedback-more">查看</span></NButton>
        <NText v-else depth="3" class="status-copy secondary-text">{{ quietStatus }}</NText>
        <NButton text size="small" :disabled="!selected" @click="showDeviceDetails"><template #icon><NIcon :component="InfoCircle" :size="17"/></template>详情</NButton>
      </footer>
    </main>
    <NModal v-model:show="viewerOpen" preset="card" size="small" :title="viewerTitle" :bordered="false" class="text-viewer" :mask-closable="true" :segmented="{ content: true, footer: 'soft' }">
      <div class="document-page">{{ viewerPages[viewerPage - 1] }}</div>
      <template #footer><div class="viewer-footer"><NText depth="3" class="secondary-text">原文完整保留，可逐页查看</NText><NPagination v-model:page="viewerPage" :page-count="viewerPages.length" simple size="small" aria-label="详情内容分页"/></div></template>
    </NModal>
  </NConfigProvider>
</template>
