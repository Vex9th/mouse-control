import { expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { parse, compileScript } from '@vue/compiler-sfc'
import { createSSRApp } from 'vue'
import type { Ref } from 'vue'
import { renderToString } from 'vue/server-renderer'
import type { DiagnosticReport, MouseAPI } from '../src/types'

interface SupportState {
  viewerTitle: Ref<string>
  viewerPages: Ref<string[]>
  viewerPage: Ref<number>
  viewerDiagnostic: Ref<boolean>
  supportNotice: Ref<string>
  supportFailure: Ref<boolean>
  supportBusy: Ref<boolean>
  showText(title: string, text: string, support: boolean): void
  supportAction(action: 'copy' | 'issue'): Promise<void>
}

// 执行真实 SFC setup；只替换系统边界，不复制弹窗业务逻辑。
const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const script = compileScript(descriptor, { id: 'support-test' }).content
const javascript = new Bun.Transpiler({ loader: 'ts' }).transformSync(script)
  .replace(/import \{([^}]+)\} from "([^"]+)";/g, (_, names: string, path: string) =>
    `const {${names.replace(/\s+as\s+/g, ':')}} = await loadModule(${JSON.stringify(path)});`)
  .replace('export default', 'return')
const component = await new Function('loadModule', `return (async () => {${javascript}})()`)(
  (path: string) => import(path.startsWith('./') ? new URL('../src/' + path.slice(2), import.meta.url).href : path),
) as { setup(props: { api: MouseAPI; mode: 'native' }, context: { expose(): void }): SupportState }

async function supportState(copyDiagnostics: MouseAPI['copyDiagnostics']): Promise<SupportState> {
  const unexpected = async (): Promise<never> => { throw new Error('不应请求设备或打开外部页面') }
  const api: MouseAPI = {
    scan: unexpected, setDPI: unexpected, setRate: unexpected,
    diagnostics: unexpected, copyDiagnostics, openIssue: unexpected,
  }
  let state!: SupportState
  // SSR 提供真实组件生命周期上下文，且不会运行 onMounted 的设备扫描和定时器。
  await renderToString(createSSRApp({
    setup() {
      state = component.setup({ api, mode: 'native' }, { expose() {} })
      return () => null
    },
  }))
  return state
}

const report: DiagnosticReport = {
  text: '原始设备错误：读取响应格式不匹配。\n' + '完整诊断内容。'.repeat(80) + '\n报告末尾。',
  location: '%LOCALAPPDATA%\\MouseControl\\logs\\error.log',
  saveError: '',
}

test('错误弹窗复制成功但保存失败时，显示完整诊断并保留警告', async () => {
  const saveError = '保存日志失败：拒绝访问；本次报告仅在内存中。'
  const state = await supportState(async () => ({ ...report, saveError }))
  state.showText('完整错误信息', '鼠标设备读取失败', true)
  state.viewerPage.value = 2
  await state.supportAction('copy')

  expect(state.viewerTitle.value).toBe('诊断日志')
  expect(state.viewerDiagnostic.value).toBe(true)
  expect(state.viewerPage.value).toBe(1)
  expect(state.viewerPages.value.length).toBeGreaterThan(1)
  const text = state.viewerPages.value.join('')
  expect(text).toContain(saveError)
  expect(text).toContain(report.text)
  expect(text).toContain(report.location)
  expect(state.supportNotice.value).toContain('已复制')
  expect(state.supportNotice.value).toContain('保存失败')
  expect(state.supportFailure.value).toBe(true)
  expect(state.supportBusy.value).toBe(false)
})

test('复制和保存均成功时保留原错误弹窗，不误报保存失败', async () => {
  const state = await supportState(async () => report)
  state.showText('完整错误信息', '鼠标设备读取失败', true)
  await state.supportAction('copy')

  expect(state.viewerTitle.value).toBe('完整错误信息')
  expect(state.viewerPages.value.join('')).toBe('鼠标设备读取失败')
  expect(state.viewerDiagnostic.value).toBe(false)
  expect(state.supportNotice.value).toContain('已复制')
  expect(state.supportNotice.value).not.toContain('保存失败')
  expect(state.supportFailure.value).toBe(false)
  expect(state.supportBusy.value).toBe(false)
})

test('剪贴板失败保留完整原因，不能显示已复制', async () => {
  const cause = '剪贴板被其他程序占用，未完成复制。' + '错误详情。'.repeat(60)
  const state = await supportState(async () => { throw new Error(cause) })
  state.showText('完整错误信息', '鼠标设备读取失败', true)
  await state.supportAction('copy')

  expect(state.viewerPages.value.join('')).toBe(cause)
  expect(state.supportNotice.value).not.toContain('已复制')
  expect(state.supportFailure.value).toBe(true)
  expect(state.supportBusy.value).toBe(false)
})
