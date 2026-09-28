import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

// 内嵌页面不依赖虚拟主机或本地HTTP服务；构建失败时不能留下未内联资源。
function singleDocument(): Plugin {
  return {
    name: 'mouse-single-document', enforce: 'post',
    generateBundle(_options, bundle) {
      const html = bundle['index.html']
      if (!html || html.type !== 'asset') throw new Error('缺少 index.html')
      let content = String(html.source)
      for (const [file, asset] of Object.entries(bundle)) {
        if (file === 'index.html') continue
        if (asset.type === 'chunk') {
          const escaped = file.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
          const tag = new RegExp(`<script[^>]*src="(?:\\./|/)?${escaped}"[^>]*><\\/script>`)
          if (!tag.test(content)) throw new Error(`发现未链接的脚本资源：${file}`)
          content = content.replace(tag, () => `<script type="module">${asset.code.replace(/<\/script/gi, '<\\/script')}</script>`)
        } else if (file.endsWith('.css')) {
          const escaped = file.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
          const tag = new RegExp(`<link[^>]*href="(?:\\./|/)?${escaped}"[^>]*>`)
          if (!tag.test(content)) throw new Error(`发现未链接的样式资源：${file}`)
          content = content.replace(tag, () => `<style>${String(asset.source).replace(/<\/style/gi, '<\\/style')}</style>`)
        } else throw new Error(`构建包含无法内嵌的资源：${file}`)
        delete bundle[file]
      }
      html.source = content
    },
  }
}
export default defineConfig({
  base: './', plugins: [vue(), singleDocument()],
  build: { target: 'es2020', assetsInlineLimit: 100_000_000, cssCodeSplit: false, modulePreload: false, rollupOptions: { output: { inlineDynamicImports: true } } },
  server: { host: '127.0.0.1', port: 5173, strictPort: true },
})
