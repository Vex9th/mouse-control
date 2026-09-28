# 开发与构建 / Development

## 环境

- Go 1.23 或更新版本。
- Node.js 22.12+：运行 TypeScript 检查和 Vite。
- Bun：安装前端依赖、运行测试和构建。
- Python 3：执行构建、字体检查与打包脚本；不进入最终程序。
- Windows 10 / 11 x64 与 WebView2 Runtime：原生 GUI 和硬件验证。

源码分为 `go/`（HID 协议、设备身份、CLI 与桌面宿主）和 `frontend/`（Vue 3、TypeScript、Naive UI）。GUI 静态资源内嵌在 `go/gui_assets/index.html`，通过 WebView2 加载，不运行本地 HTTP 服务。

## GUI 构建

在仓库根目录执行：

```powershell
python scripts/build-gui.py
```

脚本检查已保存的字体资源，执行 `bun install --frozen-lockfile`、前端测试、TypeScript 检查与 Vite 构建，再编译 Windows x64 程序到 `dist/MouseControl.exe`。依赖使用 `frontend/bun.lock` 和 `go/go.sum` 锁定；首次构建需要联网。也可以指定输出位置：

```powershell
python scripts/build-gui.py --output dist/MouseControl.exe
```

Windows 上构建 CLI：

```powershell
cd go
go test ./...
go vet ./...
go build -trimpath '-ldflags=-s -w' -o ../dist/RazerBattery.exe .
```

macOS / Linux 可以交叉编译 Windows CLI：

```sh
cd go
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath '-ldflags=-s -w' -o ../dist/RazerBattery.exe .
```

## 前端预览

```sh
cd frontend
bun install --frozen-lockfile
bun test
bun run build
bun run dev
```

访问 `http://127.0.0.1:5173/?demo=1` 使用明确标注的演示数据。添加 `&stress=1` 可检查多设备分页、长消息与不可用设备。演示仅存在于开发模式；普通网页没有原生桥接时显示不可用，不连接真实鼠标。

## 原生测试

Windows 上执行：

```powershell
cd go
go test -tags gui ./...
go vet -tags gui ./...
```

普通测试不访问鼠标。以下环境变量单独启用对应验证：

| 变量 | 验证内容 |
| --- | --- |
| `RAZER_HARDWARE_TEST=1` | 真实设备电量、DPI、回报率只读查询 |
| `RAZER_GUI_TEST=1` | 真实 WebView2、Vue 页面、原生桥接、设备读取、字体与布局 |
| `RAZER_SETTINGS_TEST=1` | 限定单只 DeathAdder V4 Pro 无线鼠标；短暂更改参数，结束时恢复并读回 |

设置测试确实会改变设备参数，仅在专门的硬件验证中开启。Go 模拟协议测试、Windows 命名管道测试和真实硬件测试的证明范围不同，见 [兼容性说明](COMPATIBILITY.md)。

## 前后端契约

前端唯一原生入口是 `window.mouseNativeSubmit(request)`，请求定义在 `frontend/src/types.ts`，返回事件为 `mouse:response`。提交被接受不等于操作成功，前端按请求 ID 等待最终结果。接口仅允许 `scan`、`setDPI` 和 `setRate`；没有任意命令、文件系统或原始 HID 报文接口。

Go 串行执行设备操作，独立校验身份、能力、范围和设置读回。前端状态与未提交输入分别由 `workspace.ts`、`drafts.ts` 管理。原生宿主限制导航、弹窗、外部资源和网页设备权限，关闭时等待已经开始的操作结束。

## 字体与打包

字体资源已提交，普通构建不需要字体裁剪工具。新增界面文字时，按 [前端字体说明](../frontend/README.md#字体资源) 更新子集；许可证必须同时保留。

验证 GUI 后可生成便携包：

```powershell
python scripts/package-gui.py
```

打包脚本包含中文使用说明和第三方许可证，并检查 ZIP CRC 与包内文件内容。发布源码与二进制应使用同一版本，上传后再核对下载文件的 SHA256。

## English quick reference

With Go 1.23+, Node.js 22.12+, Bun and Python 3 installed, run `python scripts/build-gui.py` from the repository root to install locked frontend dependencies, run frontend tests and type checks, bundle the UI, and cross-compile the Windows x64 GUI. The output is `dist/MouseControl.exe`.

For the CLI, run `go test ./...` and `go build` inside `go/` on Windows. On macOS or Linux, set `GOOS=windows GOARCH=amd64 CGO_ENABLED=0` when building the executable.

The frontend preview is development-only and uses explicit demo data. Native Windows tests require WebView2. Hardware reads and settings tests are opt-in; `RAZER_SETTINGS_TEST=1` performs real, temporary changes and attempts to restore the original values.
