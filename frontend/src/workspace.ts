import { reactive } from 'vue'
import type { MouseAPI, MouseDevice, Mutation, Snapshot } from './types'
export interface WorkspaceState {
  snapshot: Snapshot | null
  selectedId: string | null
  busy: '' | 'scan' | 'dpi' | 'rate'
  error: string
  notice: string
  stale: boolean
  deviceUpdatedAt: Record<string, string>
}
export function createWorkspace(api: Pick<MouseAPI, 'scan' | 'setDPI' | 'setRate' | 'dispose'>) {
  const state = reactive<WorkspaceState>({ snapshot: null, selectedId: null, busy: '', error: '', notice: '', stale: false, deviceUpdatedAt: {} })
  let generation = 0, closed = false
  const selected = (): MouseDevice | null => state.snapshot?.devices.find(d => d.id === state.selectedId) ?? null
  const errorMessage = (error: unknown): string => error instanceof Error ? error.message : '设备操作未完成，请刷新后重试。'
  async function scan(): Promise<void> {
    if (closed || state.busy) return
    const current = ++generation
    state.busy = 'scan'; state.error = ''; state.notice = ''
    try {
      const snapshot = await api.scan()
      if (closed || current !== generation) return
      state.snapshot = snapshot
      state.stale = false
      state.deviceUpdatedAt = {}
      if (state.selectedId === null && snapshot.devices[0]) state.selectedId = snapshot.devices[0].id
    } catch (error) {
      if (closed || current !== generation) return
      state.error = errorMessage(error); state.stale = state.snapshot !== null
    } finally { if (!closed && current === generation) state.busy = '' }
  }
  async function mutate(kind: 'dpi' | 'rate', operation: (id: string) => Promise<Mutation>): Promise<boolean> {
    const device = selected()
    if (closed || state.busy || !device || device.status !== 'online' || state.stale) return false
    const id = device.id, current = ++generation
    state.busy = kind; state.error = ''; state.notice = ''
    try {
      const result = await operation(id)
      if (closed || current !== generation) return false
      if (result.device.id !== id) throw new Error('服务返回的设备身份不匹配，请重新刷新。')
      if (state.snapshot) state.snapshot.devices = state.snapshot.devices.map(d => d.id === id ? result.device : d)
      state.deviceUpdatedAt[id] = new Date().toISOString()
      state.notice = `${device.name}：${result.message}`
      return true
    } catch (error) {
      if (!closed && current === generation) { state.error = `${device.name}：${errorMessage(error)}`; state.stale = true }
      return false
    } finally { if (!closed && current === generation) state.busy = '' }
  }
  return {
    state, selected, scan,
    select(id: string): void { if (state.snapshot?.devices.some(d => d.id === id)) state.selectedId = id },
    applyDPI(x: number, y: number): Promise<boolean> { return mutate('dpi', id => api.setDPI(id, x, y)) },
    applyRate(rate: number): Promise<boolean> { return mutate('rate', id => api.setRate(id, rate)) },
    dispose(): void { closed = true; generation++; api.dispose?.() },
  }
}
