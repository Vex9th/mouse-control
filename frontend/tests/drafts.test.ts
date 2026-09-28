import { expect, test } from 'bun:test'
import { effectScope, nextTick, ref } from 'vue'
import { createSettingsDraft } from '../src/drafts'
import type { MouseDevice } from '../src/types'
import { device } from './fixtures'

test('点击当前预设、修改后恢复原 DPI 和回报率不会留下刷新暂停', async () => {
  const scope = effectScope()
  const selected = ref<MouseDevice | null>(device())
  const draft = scope.run(() => createSettingsDraft(selected))!
  try {
    draft.choosePreset(800)
    expect(draft.hasChanges.value).toBe(false)
    draft.dpiX.value = '1600'; draft.editDPI()
    expect(draft.hasChanges.value).toBe(true)
    draft.dpiX.value = '0800'; draft.editDPI()
    expect(draft.hasChanges.value).toBe(false)
    draft.rateValue.value = 500; draft.editRate()
    expect(draft.hasChanges.value).toBe(true)
    draft.rateValue.value = 1000; draft.editRate()
    expect(draft.hasChanges.value).toBe(false)

    selected.value = { ...device(), dpi: { x: 1200, y: 1200, error: '' }, rate: { hz: 500, error: '' } }
    await nextTick()
    expect(draft.dpiX.value).toBe('1200')
    expect(draft.rateValue.value).toBe(500)
    expect(draft.hasChanges.value).toBe(false)
  } finally { scope.stop() }
})

test('刷新保留实际未提交输入，设备读数追上草稿后自动恢复刷新', async () => {
  const scope = effectScope()
  const selected = ref<MouseDevice | null>(device())
  const draft = scope.run(() => createSettingsDraft(selected))!
  try {
    draft.choosePreset(1600)
    selected.value = device()
    await nextTick()
    expect(draft.dpiX.value).toBe('1600')
    expect(draft.hasChanges.value).toBe(true)

    selected.value = { ...device(), dpi: { x: 1600, y: 1600, error: '' } }
    await nextTick()
    expect(draft.hasChanges.value).toBe(false)
    selected.value = { ...device(), dpi: { x: 3200, y: 3200, error: '' } }
    await nextTick()
    expect(draft.dpiX.value).toBe('3200')
    expect(draft.hasChanges.value).toBe(false)
  } finally { scope.stop() }
})

test('无效输入和独立 Y 轴修改暂停刷新，切换设备清除旧草稿', async () => {
  const scope = effectScope()
  const selected = ref<MouseDevice | null>(device())
  const draft = scope.run(() => createSettingsDraft(selected))!
  try {
    draft.dpiX.value = '8e2'; draft.editDPI()
    expect(draft.hasChanges.value).toBe(true)
    draft.dpiX.value = '800'; draft.editDPI()
    draft.linked.value = false; draft.editDPI()
    expect(draft.hasChanges.value).toBe(false)
    draft.dpiY.value = '1600'; draft.editDPI()
    expect(draft.hasChanges.value).toBe(true)
    selected.value = device('second')
    await nextTick()
    expect(draft.dpiY.value).toBe('800')
    expect(draft.hasChanges.value).toBe(false)
  } finally { scope.stop() }
})
