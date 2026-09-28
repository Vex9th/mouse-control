# 鼠标工具界面

Vue 3 + TypeScript strict + Vite + Naive UI 2.45.3。按钮、输入框、开关、回报率选项、分页和弹窗使用组件库；图标来自 @vicons/tabler。外观由 `src/theme.ts` 的设计变量统一配置，`style.css` 负责页面布局。组件、图标与 Noto Sans SC 字体一起内嵌，不请求在线资源。

## 开发与构建

构建需要 Node.js 22.12+ 和 Bun。在本目录执行：

```sh
bun install --frozen-lockfile
bun test
bun run build
bun run dev
```

开发服务只监听 `127.0.0.1:5173`。显式打开 `http://127.0.0.1:5173/?demo=1` 才启用演示数据，页面会一直显示演示标记。追加 `&stress=1` 可检查 12 台设备、长型号/ID/详情分页，以及第 3 台设备的读取失败状态；第二次刷新会返回长错误以检查失败展示。生产构建移除演示分支，无原生桥接时显示不可用。

`dist/index.html` 是唯一构建产物，JS、CSS 和 SVG 均在该文件内，供 Go 桌面程序内嵌。构建脚本先执行 `vue-tsc --noEmit`；发现不能内嵌的输出资源时直接失败。

## 接口与状态

`src/types.ts` 定义 `MouseAPI` 和 JSON 类型；`src/api.ts` 通过 `window.mouseNativeSubmit` 提交请求，按请求 ID 接收 `mouse:response` 事件。提交被接收不代表操作成功，最终结果有 120 秒等待上限；超时会提示结果未确认，要求刷新，不自动重试设置。原生接口与构建说明见 [开发文档](../docs/DEVELOPMENT.md)。

`src/workspace.ts` 管理设备选择、忙状态、陈旧读数和响应归属。选择按完整设备 ID 保留，设备断开后不会把写入目标自动改为另一只鼠标。修改输入不直接写设备，DPI 和回报率分别应用。可选的 30 秒刷新在页面不可见、输入尚未提交或服务忙时暂停。

`src/drafts.ts` 按输入与当前读数的实际差异判断未提交状态。点击当前预设或把参数恢复为当前值不会暂停刷新；确有差异时，底部状态显示“存在未提交输入，自动刷新已暂停”。

`src/model.ts` 负责输入提示和显示；原生后端仍需独立校验范围、双轴能力、只读模式和设备身份。协议读回不等于对硬件物理效果的测量。未知电量、等级、电压及部分查询失败各自保留真实含义。

## 字体资源

`src/fonts.css` 引用本地 `MouseUISans.woff2`，Vite 将其编码进单一 HTML。中文界面、后端消息和英文型号共用 400–700 可变字重；额外的设备字符回退到系统中文字体。正文与表单至少 14 px，次要说明至少 12 px，不对文字容器进行 transform 缩放。

普通构建运行 `python3 scripts/update-ui-font.py` 检查资源哈希与项目字符覆盖（在仓库根目录）。新增文字后，维护者可从 Google Fonts 官方 `ofl/notosanssc` 下载完整 `NotoSansSC[wght].ttf` 和 `OFL.txt`，分别存为 `.cache/fonts/NotoSansSC.ttf`、`.cache/fonts/OFL.txt`，再运行 `python3 scripts/update-ui-font.py --refresh`。裁剪完全在本地进行，使用项目开发缓存中的 `fonttools==4.60.1` 与 `brotli==1.1.0`；普通构建和用户运行不需要这些工具。新资源与 `font-manifest.json`、许可一同保存。原始字体源码与许可：https://github.com/google/fonts/tree/main/ofl/notosanssc
