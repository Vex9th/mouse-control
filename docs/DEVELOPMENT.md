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

前端唯一原生入口是 `window.mouseNativeSubmit(request)`，请求定义在 `frontend/src/types.ts`，返回事件为 `mouse:response`。提交被接受不等于操作成功，前端按请求 ID 等待最终结果。设备接口为 `scan`、`setDPI` 和 `setRate`；诊断接口为 `diagnostics`（读取报告）、`copyDiagnostics`（复制报告）和 `openIssue`（打开固定问题模板）。接口不接受任意命令、文件路径、外部 URL 或原始 HID 报文。

Go 串行执行设备操作，独立校验身份、能力、范围和设置读回。前端状态与未提交输入分别由 `workspace.ts`、`drafts.ts` 管理。原生宿主限制导航、弹窗、外部资源和网页设备权限，关闭时等待已经开始的操作结束。

## 错误日志与问题反馈

用户可从错误弹窗或底部“诊断日志”进入报告，点击“复制诊断日志”，再粘贴到 GitHub Issues。中文优先的问题模板位于 `.github/ISSUE_TEMPLATE/bug_report.yml`，包含型号、连接方式、版本、复现步骤、实际结果和多行诊断日志；日志字段使用 `render: text`，保留报告排版。

“提交 Issue”只打开固定地址 `https://github.com/Vex9th/mouse-control/issues/new?template=bug_report.yml`，由用户自行填写并提交。不得将日志、设备身份或其他动态数据拼入 URL，不自动上传日志或调用 GitHub 创建 Issue。

本地日志使用单个 `%LOCALAPPDATA%\MouseControl\logs\error.log`，同时限制为最多 20 条记录、最多 128 KiB；连续重复错误合并并累计次数。保存失败必须在界面提示，不能伪装成保存成功；当前内存报告仍可复制。报告自动脱敏，但用户发布前仍应检查内容；问题模板不索取账号、序列号或完整设备路径。

变更诊断功能时，应验证错误记录与重复合并、记录数及字节上限、脱敏、保存失败后的内存复制，以及“提交 Issue”只打开固定模板。诊断功能用于收集排查信息，不能把日志功能的测试通过当成迈从或其他设备读取问题已经修复。

## 字体与打包

字体资源已提交，普通构建不需要字体裁剪工具。新增界面文字时，按 [前端字体说明](../frontend/README.md#字体资源) 更新子集；许可证必须同时保留。

验证 GUI 后可生成便携包：

```powershell
python scripts/package-gui.py
```

打包脚本包含中文使用说明和第三方许可证，并检查 ZIP CRC 与包内文件内容。发布源码与二进制应使用同一版本，上传后再核对下载文件的 SHA256。

## 自动构建与版本发布

`.github/workflows/windows.yml` 在 main 提交、PR 和手动触发时运行 Windows x64 构建。它检查 Go 与 `frontend/package.json` 的版本一致性，执行 Python 打包回归、PowerShell 清理保护、前端测试 / 类型检查 / 字体检查、Windows Go 测试与 vet，并生成两个便携 ZIP。普通构建只有读取仓库权限，附件保留 7 天。

发布新版本时，先让 main 构建通过，再推送与源码版本一致的标签，例如 `v1.5.0`。只有标签发布任务拥有 `contents: write` 权限，发布前重新检查下载附件的 SHA256；已存在的 Release 会被拒绝，不能用重跑覆盖资产。

本地完整打包需先生成 `dist/MouseControl.exe`、`dist/RazerBattery.exe` 和 `dist/CLI_HELP.txt`，再执行：

```powershell
python scripts/release-package.py package
```

输出位于 `dist/release/`。`scripts/package-gui.py` 仍可单独打包 GUI，文件名从源码版本读取。

## 编译机只保留最新成功构建

远程源码、临时脚本、日志、缓存和产物统一放入项目固定根目录下 `builds/<任务类型>/<批次>/`，依赖放在 `dependencies/`。新构建成功且产物回传校验完成后，再清理旧批次；失败不能删除最后成功版本。

`scripts/prune-builds.ps1` 默认只预演，使用明确的保留批次和已回传归档清单，不根据目录名猜测所有权。清单格式见脚本顶部说明与 `test-prune-builds.ps1` 中的完整用例。

```powershell
powershell -NoProfile -File scripts/prune-builds.ps1 `
  -ProjectRoot D:\Developer\MouseControl -KeepBatch gui/release-01 `
  -ManifestPath D:\Developer\MouseControl\builds\gui\release-01\receipt.json
# 检查预演输出后，以相同参数添加 -Execute 执行
```

脚本校验项目边界、成功产物哈希、旧批次的完整文件清单与归档回传哈希，拒绝链接、活动进程及源文件变化。清理时不要并行启动构建。个人测试电脑只使用一个固定应用目录，临时验证结果回传后清理。

## English quick reference

With Go 1.23+, Node.js 22.12+, Bun and Python 3 installed, run `python scripts/build-gui.py` from the repository root to install locked frontend dependencies, run frontend tests and type checks, bundle the UI, and cross-compile the Windows x64 GUI. The output is `dist/MouseControl.exe`.

For the CLI, run `go test ./...` and `go build` inside `go/` on Windows. On macOS or Linux, set `GOOS=windows GOARCH=amd64 CGO_ENABLED=0` when building the executable.

The frontend preview is development-only and uses explicit demo data. Native Windows tests require WebView2. Hardware reads and settings tests are opt-in; `RAZER_SETTINGS_TEST=1` performs real, temporary changes and attempts to restore the original values.

Diagnostics are available from an error dialog or “诊断日志” at the bottom. “复制诊断日志” copies the report; “提交 Issue” only opens `https://github.com/Vex9th/mouse-control/issues/new?template=bug_report.yml`, without logs or device data in the URL. Users review, paste and submit reports themselves. Reports are automatically redacted, but should still be checked before posting; account details and serial numbers are not required.

Use one `%LOCALAPPDATA%\MouseControl\logs\error.log` file, capped at 20 records and 128 KiB, merging consecutive duplicate errors with a count. A save failure must remain visible while the current in-memory report is still available to copy. Diagnostic tests do not establish that a device communication failure has been fixed.

GitHub Actions builds Windows x64 GUI/CLI on main pushes, PRs and manual dispatch, retaining artifacts for 7 days. Matching version tags publish new Releases without replacing existing assets. Remote build cleanup requires an explicit verified archive manifest; it defaults to dry-run and preserves the named successful build and shared dependencies.
