# MouseControl · 雷蛇 / 罗技 / 迈从鼠标工具

**中文** · [English](README_EN.md) · [下载](https://github.com/Vex9th/mouse-control/releases/latest) · [反馈问题](https://github.com/Vex9th/mouse-control/issues/new?template=bug_report.yml)

轻量的 Windows 鼠标电量查询与设置工具，提供中文 GUI 和 CLI。查看 **Razer 雷蛇 / Logitech 罗技鼠标电量、充电状态、当前 DPI 和回报率**，并根据设备能力调整 DPI 与 polling rate。新增 **MCHOSE 迈从新协议鼠标的实验性只读支持**。支持多设备识别，连接和功能范围见下表。

**Razer & Logitech mouse battery monitor and settings utility, with experimental read-only MCHOSE support.**

![MouseControl 中文桌面界面：鼠标电量、DPI 与回报率设置](docs/images/overview.png)

*截图使用演示数据；实际读数和可用设置由连接的鼠标提供。*

## 下载与使用

面向 **Windows 10 / 11 x64**。在 [Releases](https://github.com/Vex9th/mouse-control/releases/latest) 下载：

| 文件 | 用途 |
| --- | --- |
| `MouseControl-1.5.2-windows-x64.zip` | 桌面版；解压后打开 `MouseControl.exe` |
| `RazerBattery-1.5.2-windows-x64.zip` | 命令行版，包含使用帮助和第三方许可；解压后运行 `RazerBattery.exe` |
| `SHA256SUMS.txt` | 下载文件的 SHA256 校验值 |

桌面版使用系统 **Microsoft Edge WebView2 Runtime**。Windows 11 包含该运行时；缺少时可从 [Microsoft 官方页面](https://developer.microsoft.com/microsoft-edge/webview2/) 安装 Evergreen Runtime。使用程序无需 Node.js、Python 或 Go。

1. 连接鼠标或无线接收器，打开程序。
2. 从顶部选择设备，查看电量、DPI 与回报率。
3. 输入目标 DPI 或选择回报率，再点击对应的“应用”。程序会重新读取数值进行确认。
4. 点“设备详情”查看完整设备身份和能力；多设备与长消息按页显示。

无需安装服务、注册账号或设置自启动。桌面界面和中文字体内嵌在程序中，不启动本地 HTTP 服务，也不依赖在线字体。可手动刷新，或开启每 30 秒刷新；编辑未提交的设置时会暂停自动刷新。

## 功能

- **鼠标电量**：显示百分比、充电状态，或设备实际提供的电量等级与电压。未读到电量不会显示为 0%。
- **DPI 读取与设置**：支持独立 X / Y 轴的设备可分别设置；按鼠标报告的范围、步长和能力校验。
- **回报率设置**：显示设备支持的档位；部分设备可提供 125–8000 Hz，具体以上报能力为准。
- **多设备识别**：按完整设备身份区分同型号鼠标；罗技共享接收器按槽位识别设备。
- **GUI + CLI**：中文桌面界面和命令行查询、交互菜单、定时查看。
- **紧凑界面**：Vue 3 + Naive UI，默认 900×480、最小 760×440 逻辑像素；设备列表、详情和长消息使用分页。
- **设置读回**：当前值相同则跳过写入；修改后再次读取。超时或结果不一致会明确报告，不自动重发设置。
- **诊断反馈**：错误弹窗和底部“诊断日志”提供报告查看与复制；可打开 GitHub 问题模板，由你自行提交。

## 支持范围

| 品牌 / 连接 | 当前实现 | 验证范围 |
| --- | --- | --- |
| 雷蛇 Razer USB / 无线接收器 | 113 个鼠标相关 PID；其中 101 个适配 XY DPI、104 个适配回报率 | DeathAdder V4 Pro 无线连接已有真实读取及设置恢复测试；其余条目主要依据协议资料 |
| 罗技 Logitech HID++ 2.x 鼠标与轨迹球 | 动态发现电量、DPI、回报率能力 | 已有协议模拟测试；尚无本项目罗技真机验证 |
| LIGHTSPEED / POWERPLAY / Unifying / Bolt / Nano 接收器 | 已知接收器识别、槽位查询、鼠标身份区分 | 34 个接收器相关 PID；部分旧协议仅识别 |
| 罗技 USB 直连 / 蓝牙 | Windows 提供可访问的 HID++ 通道时查询 | 蓝牙与具体型号仍需实测 |
| 迈从 MCHOSE 新协议 USB / 2.4 GHz | 14 个鼠标型号 PID、3 个接收器 PID；电量、当前 DPI、回报率只读 | 依据官方网页驱动适配；已测模拟协议，尚无真机验证；不开放设置 |
| HID++ 1.x / 普通 HID / 旧式私有协议 | 按能力识别或读取旧版电量 | 不支持的设置保持不可用 |

**目录条目和协议支持不等于每个型号、连接方式或功能都已通过真机验证。** 雷蛇与罗技的设置接口存在型号差异；键盘、耳机、按键映射、宏、灯光、固件更新和完整板载配置不在本项目范围内。它不是 Razer Synapse、Logitech G HUB、Options+ 或迈从官方驱动的完整替代品。迈从仅适配文档列出的新协议型号，G3 / G7 的其他旧协议家族、固件变体和蓝牙未覆盖。

- [雷蛇鼠标型号与 DPI / 回报率协议](docs/MOUSE_PROTOCOL.md)
- [罗技 HID++ 功能与限制](docs/LOGITECH_PROTOCOL.md)
- [罗技接收器、槽位与蓝牙通道](docs/LOGITECH_RECEIVERS.md)
- [迈从型号、协议与实验性限制](docs/MCHOSE_PROTOCOL.md)
- [兼容性与验证说明](docs/COMPATIBILITY.md)

## 命令行

```powershell
# 查询一次
.\RazerBattery.exe -once

# 显示完整设备身份、能力与错误原因
.\RazerBattery.exe -details

# 每 30 秒查看状态
.\RazerBattery.exe -watch 30

# 设置 DPI；也可分别设置 X、Y 轴
.\RazerBattery.exe -set-dpi 1600
.\RazerBattery.exe -set-dpi 1600,1200

# 设置回报率
.\RazerBattery.exe -set-rate 1000

# 查看型号目录与帮助
.\RazerBattery.exe -supported
.\RazerBattery.exe -h
```

无参数启动进入中文菜单。重定向输入或输出时，默认查询一次后退出。连接多台设备时，设置命令需要通过 `-device` 指定完整设备 ID，从 `-details` 输出中复制：

```powershell
.\RazerBattery.exe -set-dpi 1600 -device '从 -details 复制的完整设备 ID'
```

设备断开后不会把设置自动改发给另一只鼠标。部分雷蛇鼠标会保存当前 DPI；罗技设备若处于板载模式，可能拒绝设置，程序不会自动切换模式或写入板载配置闪存。

## 常见问题

**需要一直运行吗？** 不需要。需要时打开查询或设置，用完可以关闭；没有常驻服务或开机自启。

**为什么显示“未知”或“暂不可用”？** 鼠标可能休眠、关机、断开，或当前连接没有提供相关接口。先移动鼠标并刷新；仍有错误时，可从错误弹窗或底部“诊断日志”查看并复制报告。只有等级或电压时不会换算成虚构百分比。

**和雷云、G HUB 同时运行会怎样？** 多个程序可能争用设备通道或修改同一参数。遇到通信异常时，可先关闭其他鼠标管理软件再读取；本项目没有验证所有共存组合。

**能直接做成网页吗？** 当前版本是 Windows 桌面程序。Vue 界面可以复用，但网页需要单独实现 WebHID 通信、浏览器授权和逐设备验证，不能直接复用 Go 的 Windows HID 后端。见 [网页版说明](docs/WEB_FEASIBILITY.md)。

## 自动编译

推送到 `main`、提交 Pull Request 或在 [Actions](https://github.com/Vex9th/mouse-control/actions/workflows/windows.yml) 手动运行，都会在 GitHub Windows runner 上执行测试并生成 GUI / CLI 便携包、第三方许可和 SHA256。临时构建附件保留 7 天，同一分支的新提交会取消旧构建。

推送与源码版本一致的 `v*` 标签后自动创建 Release；不会覆盖已有 Release。普通 CI 不连接真实鼠标，也不运行设置测试。GitHub 构建不向个人电脑复制文件。

## 从源码构建

需要 Go 1.23+、Node.js 22.12+、Bun 和 Python 3。推荐在 Windows x64 上构建并验证运行。

```powershell
git clone https://github.com/Vex9th/mouse-control.git
cd mouse-control

# 前端测试、类型检查、构建及 Windows GUI 编译
python scripts/build-gui.py

# Go 单元测试与独立 CLI
cd go
go test ./...
go build -trimpath '-ldflags=-s -w' -o ../dist/RazerBattery.exe .
```

GUI 输出为 `dist/MouseControl.exe`。Go 模块和前端依赖使用项目内锁文件；首次构建需要下载依赖。macOS / Linux 可交叉编译 Windows GUI，但跨平台编译不代表通过 Windows 运行验证。开发预览、原生集成测试和打包说明见 [开发文档](docs/DEVELOPMENT.md)。

## 反馈与来源

遇到错误时，在程序错误弹窗或底部“诊断日志”中点击“复制诊断日志”。点击“提交 Issue”会打开固定的 [GitHub 问题模板](https://github.com/Vex9th/mouse-control/issues/new?template=bug_report.yml)；填写鼠标型号、连接方式、程序与 Windows 版本、复现步骤和实际结果，再把报告粘贴到“诊断日志”栏，由你检查后提交。程序不会自动发送日志或创建 Issue。

报告会自动脱敏，发布前仍请检查内容；无需填写账号、序列号或完整设备路径。若程序无法打开或无法复制报告，可在模板中说明情况。

本地错误日志保存在单个 `%LOCALAPPDATA%\MouseControl\logs\error.log` 文件中，最多保留 20 条记录且不超过 128 KiB；连续重复错误合并并累计次数。日志保存失败时会明确提示，仍可复制当前内存中的诊断报告。

维护者：[Vex9th](https://github.com/Vex9th)。协议资料参考 [OpenRazer](https://github.com/openrazer/openrazer)、[Logitech HID++ 文档](https://github.com/Logitech/cpg-docs)、[Solaar](https://github.com/pwr-Solaar/Solaar)、[libratbag](https://github.com/libratbag/libratbag) 、Linux HID 驱动和 [迈从官方网页驱动](https://www.mchose.com.cn/#/connectDevice)；固定版本与具体字段来源列在协议文档中。依赖与字体许可证见 [第三方许可](docs/THIRD_PARTY_NOTICES.txt)。

本项目与 Razer、Logitech、MCHOSE 无隶属关系。品牌和产品名称属于各自权利人。
