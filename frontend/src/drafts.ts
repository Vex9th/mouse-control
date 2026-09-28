import { computed, ref, watch, type Ref } from 'vue'
import type { MouseDevice } from './types'

function matchesDPI(text: string, current: number | null): boolean {
  const value = text.trim()
  if (value === '') return current === null
  return /^\d+$/.test(value) && Number(value) === current
}

export function createSettingsDraft(selected: Readonly<Ref<MouseDevice | null>>) {
  const dpiX = ref(''), dpiY = ref(''), linked = ref(true), dpiDirty = ref(false)
  const rateValue = ref<number | null>(null), rateDirty = ref(false)
  const dpiChanged = computed(() => {
    const d = selected.value
    if (!d) return false
    const y = linked.value || !d.capabilities.separateAxes ? dpiX.value : dpiY.value
    return !matchesDPI(dpiX.value, d.dpi.x) || !matchesDPI(y, d.dpi.y)
  })
  const rateChanged = computed(() => selected.value !== null && rateValue.value !== selected.value.rate.hz)

  function resetDPI(): void {
    const d = selected.value
    dpiX.value = d?.dpi.x === null || d?.dpi.x === undefined ? '' : String(d.dpi.x)
    dpiY.value = d?.dpi.y === null || d?.dpi.y === undefined ? '' : String(d.dpi.y)
    linked.value = !d?.capabilities.separateAxes || d.dpi.x === d.dpi.y
    dpiDirty.value = false
  }
  function resetRate(): void { rateValue.value = selected.value?.rate.hz ?? null; rateDirty.value = false }
  function editDPI(): void { dpiDirty.value = dpiChanged.value }
  function editRate(): void { rateDirty.value = rateChanged.value }
  function choosePreset(value: number): void {
    dpiX.value = String(value)
    if (linked.value) dpiY.value = String(value)
    editDPI()
  }
  watch(() => selected.value?.id, () => { resetDPI(); resetRate() }, { immediate: true })
  // 同值输入不再阻止后续读数更新；手动刷新也可能确认设备已经达到草稿值。
  watch(() => [selected.value?.dpi.x, selected.value?.dpi.y], () => { if (!dpiDirty.value || !dpiChanged.value) resetDPI() })
  watch(() => selected.value?.rate.hz, () => { if (!rateDirty.value || !rateChanged.value) resetRate() })

  return {
    dpiX, dpiY, linked, dpiDirty, rateValue, rateDirty,
    hasChanges: computed(() => dpiChanged.value || rateChanged.value),
    resetDPI, resetRate, editDPI, editRate, choosePreset,
  }
}
