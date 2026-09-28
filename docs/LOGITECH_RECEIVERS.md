# 罗技接收器目录与只读路由

核对日期：2026-09-29。本文件记录源码依据及实现边界，不是 Windows 真机兼容名单。只读请求也通过 HID Output 写报告，但不修改配对、通知配置、板载模式或设备设置。

## 固定来源

- Solaar：`e7304c4c451cc9bb4f206a914844525e67856a28`。[接收器目录](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/base_usb.py)、[配对信息实现](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/receiver.py)、[HID++ 1 寄存器与电量](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/hidpp10.py)、[常量](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/hidpp10_constants.py)、[帧和响应匹配](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/base.py)。
- Linux：`72d3fcf802c45d00b300f25b848a93c3a2bd7c7e`。[接收器类型与路由](https://github.com/torvalds/linux/blob/72d3fcf802c45d00b300f25b848a93c3a2bd7c7e/drivers/hid/hid-logitech-dj.c)、[USB PID 常量](https://github.com/torvalds/linux/blob/72d3fcf802c45d00b300f25b848a93c3a2bd7c7e/drivers/hid/hid-ids.h)。
- libratbag：`b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21`。[设备数据目录](https://github.com/libratbag/libratbag/tree/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/data/devices)、[quirk 定义](https://github.com/libratbag/libratbag/blob/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/src/hidpp20.h)。

## 静态目录

`go/logitech_receiver_catalog.go` 的 `logitechReceiver(pid)` 仅用于已经确认 `VID=046D` 的物理设备。共 34 个已知接收器相关 PID，其中 23 个允许普通只读槽位探测，11 个旧协议条目仅识别。

`Slots` 是本程序有界扫描的最后一个槽位编号；`MaxDevices` 是目录容量默认值。两者不能等同：Solaar 的普通接收器遍历 `1..7`，明确说明有些设备槽位超过最大配对数；Linux 同样使用 `DJ_DEVICE_INDEX_MAX=7`。不能因为 LIGHTSPEED 默认容量为 1 就忽略槽位 2..7。容量读取成功也不证明槽位连续。

| Family | PID（十六进制） | Slots | MaxDevices | 依据/例外 |
| --- | --- | --- | --- | --- |
| unifying | C52B、C532 | 7 | 6 | Solaar 与 Linux；容量 6 见 [Logitech 产品说明](https://www.logitech.com/en-us/shop/p/unifying-receiver-usb) |
| bolt | C548 | 6 | 6 | Solaar 明确最大 6；使用 Bolt 配对寄存器 |
| nano | C518、C51A、C521、C525、C526、C52E、C52F、C531、C535、C537 | 7 | 1 | Solaar 默认值；C531 是 G700/G700s，C537 是 G602，C535 为 Dell 品牌 |
| nano | C534 | 7 | 2 | Solaar 明确默认容量 2；可能无接收器序列号 |
| lightspeed | C539、C53A、C53D、C53F、C541、C545、C547、C54D | 7 | 1 | Solaar；C53A 是 POWERPLAY |
| lightspeed | C543 | 7 | 1 | Linux 补充；容量 1 仅为保守默认值，不能用于限制扫描 |
| 27mhz | C513、C517、C51B | 0 | C517=4，其余未知 | Linux 明确旧协议；C51B 与 Solaar Nano 分类冲突，采用 Linux 特殊分类 |
| legacy-bluetooth | C70A、C70E、C713、C714、C71B、C71C、C71E、C71F | 0 | 未知 | Linux 的 MX5000/MX5500/diNovo USB 蓝牙桥；不能按普通接收器归并与查询 |

Solaar 另有 `17EF:6042` 的 Lenovo 接收器；它不是 `046D:6042`，故没有混入本目录。目录之外的 `C5xx` 不能根据编号范围自动认作接收器。27 MHz 的鼠标可能位于槽位 1/2，键盘为 3、小键盘为 4；Solaar 依赖 Linux 子设备信息获取其 WPID，目前 Windows 实现没有这条证据链，因此 `Slots=0`。

## Windows 识别与安全探测边界

1. 枚举 HID collection，用零访问权限句柄读取 `HidD_GetAttributes` 和 HID caps，先确认真实 VID/PID。按物理 USB 实例归并；同 PID 的多个实例必须分开。
2. 已知普通接收器允许在其 vendor 通道查询配对表和指定槽位；非接收器只有同一物理组含 Generic Desktop / Mouse（UsagePage `01`，Usage `02`）时才探测 HID++。纯键盘、摄像头、耳机组不因 VID=046D 而被主动探测。
3. 通道应确认为 vendor UsagePage `FF00`，并有对应 report `10`/7 字节、`11`/20 字节或 `12`/64 字节的 Input/Output。不同 collections 的长短报告可能分离，不能仅使用第一个。Windows 的 caps 长度包括 Report ID；不要额外前置零。
4. 配对表只能证明存在配对记录，不能证明鼠标在线。成功获取鼠标/轨迹球类型后才进入其后端；键盘记录不进入鼠标设置。配对记录缺失不能在所有 LIGHTSPEED/Nano 上直接当作空槽，仍可有界只读 ping，并保留未确定类型的边界。
5. 逻辑身份至少包含物理父设备 ID 和槽位；有可靠 WPID/serial 时一并记录。重新配对可把同一槽位换成另一只设备，不能单靠 `PID+slot` 沿用旧能力缓存或设置目标。

[Microsoft HidD_GetAttributes](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_getattributes) 明确返回 collection 的属性；[HIDAPI Windows 实现](https://github.com/libusb/hidapi/blob/master/windows/hid.c) 使用该函数取 VID/PID，并用父节点 CompatibleIds 判断总线类型。

蓝牙路径可能出现 `VID&0002046D`，不能把它当作普通 USB 的四位 `VID_046D` 直接截取。[Microsoft 蓝牙 PnP 规则](https://learn.microsoft.com/en-us/windows-hardware/drivers/bluetooth/installing-a-bluetooth-device)规定 `VID&` 后为八位、`PID&` 后为四位；这些字符串用于初筛，最终仍核对 `HidD_GetAttributes`。沿 HID 节点父级的 CompatibleIds 区分 `BTHENUM` 和 `BTHLEDEVICE`，在蓝牙设备父节点/ContainerId 归并，不能继续上溯至本机 USB 蓝牙适配器。仅出现 `LOCALMFG` 表示本机无线电厂商，不能用作远端鼠标品牌。

## 只读配对请求

下表 `p` 一律表示完整响应去掉前四字节后的 payload，即 `frame[4:]`；`n` 为槽位编号。HID++ 1 没有 HID++ 2 的 softwareID 编排：寄存器读命令为 `81`（短）或 `83`（长），不能改写低四位。

长寄存器读请求仍可使用 7 字节 `10 FF 83 reg a b c`；成功响应一般为 20 字节 `11 FF 83 reg ...`。输入长度、设备地址、sub-ID、寄存器必须匹配；`83/B5` 还须检查 `p[0]` 等于所请求 selector。HID++ 1 错误为 `10 addr 8F originalSubID originalRegister error ...`，不能把错误码当数据。所有无关通知使用固定截止时间忽略，不能无限延长超时。

| 查询 | 请求（十六进制） | 成功响应布局 |
| --- | --- | --- |
| 普通接收器信息 | `10 FF 83 B5 03 00 00` | `p[1:5]` 为四字节 receiver serial；`p[6]` 为容量，只接受 1..6 |
| 已连接数量 | `10 FF 81 02 00 00 00` | `p[1]` 为数量；不能据此假定槽位连续 |
| 普通配对信息 | `10 FF 83 B5 (20+n-1) 00 00` | `p[3:5]` 为大端 WPID；`p[7]&0F` 为 HID++ 1 kind；`p[2]` 为间隔毫秒 |
| 普通扩展身份 | `10 FF 83 B5 (30+n-1) 00 00` | `p[1:5]` 为四字节设备 serial；`p[9]&0F` 为电源开关位置 |
| 普通名称 | `10 FF 83 B5 (40+n-1) 00 00` | `p[1]` 为名称长度，名称从 `p[2]` 起；读取前检查剩余长度 |
| Bolt 接收器 ID | `10 FF 83 FB 00 00 00` | payload 为 receiver unique ID，不能按普通信息布局切片 |
| Bolt 配对信息 | `10 FF 83 B5 (50+n) 00 00` | `p[1]&0F` 为 kind；`p[2:4]` 为小端 WPID；`p[4:8]` 为四字节设备身份 |
| Bolt 名称首片段 | `10 FF 83 B5 (60+n) 01 00` | `p[2]` 为名称长度，内容从 `p[3]` 起；Solaar 最多取 14，实际还受 payload 长度约束，不能越界或宣称片段必然是完整名称 |

Bolt 的 selector 使用 `base+n`，普通配对使用 `base+n-1`，不能共享一个算式。HID++ 1 的类别 `02`=鼠标、`08`=轨迹球；HID++ 2 `0005` 的 `03`=鼠标、`05`=轨迹球，两套值不能混用。serial/unique ID 只作为身份数据，不能视为通信地址。

普通 Nano 的 Solaar 后备路径会读 `83/B5 selector04` 获取 `p[3:5]` 的 WPID，并以 kind=unknown 返回；还会读 `83/D5` 取 `p[1:5]` serial。上游自己标记这两个接口为未公开/存疑。若采用，只限明确 Nano 的只读后备，不可把它复制给每个槽位并捏造出多只设备；失败保留未知。首版可以不依赖这条后备。

不要照搬 `Receiver.__init__` 的 configuration pending 写入、`notify_devices()` 的 `80/02` 写入、配对/取消配对命令或通知配置。Linux 的 DJ 模式切换、伪子设备和写通知流程也不是 Windows 查询前置条件。

## HID++ 1 电量

只对已确认 HID++ 1 的在线鼠标读取。无型号寄存器信息时，Solaar 先尝试 `0D`，不支持再试 `07`。请求中的地址是鼠标槽位，不能固定用接收器 `FF`。

| 寄存器 | 请求 | 电量 | 充电状态 |
| --- | --- | --- | --- |
| `0D` | `10 n 81 0D 00 00 00` | `p[0]` 百分比；本实现应拒绝 >100，不能把 255 当 100% | `p[2]&F0`：30 放电、50 充电、90 满；其他未知 |
| `07` | `10 n 81 07 00 00 00` | `p[0]`：7 满、5 正常、3 低、1 极低；这是档位，不能当精确百分比 | `p[1]==0` 放电；`(p[1]&21)==21` 充电；否则 `(p[1]&22)==22` 满；其他未知 |

`07` 中 `p[0]==0` 且 `p[1]&03!=0` 是没有电量数据的充电通知，不得显示“0%”。其他未定义档位也应保持未知。状态可以单独已知而电量未知，UI 数据模型应允许这种组合。以上来自固定版本 `hidpp10.py::parse_battery_status`，没有转换电压或猜测 AA 电池类型。

## 直连地址例外与蓝牙响应

固定版本的 libratbag 包含 76 个 Logitech 设备数据文件。真正的路由字段是 `DeviceIndex`；`Quirk=INDEX_OFFSET` 只修正 onboard profile 的 1 起始编号，与 HID++ 设备地址无关。

- [G Pro Wireless](https://github.com/libratbag/libratbag/blob/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/data/devices/logitech-g-pro-wireless.device)：`USB C088` 明确 `DeviceIndex=1`；同文件 `4079` 是无线 WPID，不能当 USB 接收器 PID。
- [G602](https://github.com/libratbag/libratbag/blob/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/data/devices/logitech-g602.device) 的 `402C` 和 [G700 wireless](https://github.com/libratbag/libratbag/blob/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/data/devices/logitech-g700-wireless.device) 的 `C531` 使用 index 1，分别是无线子设备/已知接收器路由，不是另两个直连例外。
- [POWERPLAY](https://github.com/libratbag/libratbag/blob/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/data/devices/logitech-g-powerplay.device) 的 `405F` 使用 index 7，说明高编号可能是配件，必须再检查设备类型，不能全部当鼠标。
- G915/G915 TKL 的 index 1 是键盘范围，本鼠标工具不据此主动探测键盘。

其余直连鼠标先用 `FF`，不要把所有设备都改成 1。Solaar 允许 Bluetooth 的 `FF` 请求以 `00` 地址响应；Windows 层应只在已确认的蓝牙直连通道启用该例外，并继续匹配 nonce、feature/function、softwareID。接收器槽位不能普遍放宽为 `addr XOR FF`。

## 目录验证

本文件和静态接收器目录不声明任何 transport/backend；纯协议查询由 HID++ 后端统一负责。目录测试覆盖 34 个条目数量、普通/特殊分类、槽位与容量边界及非接收器 PID 拒绝。此检查不证明 Windows HID 收发、接收器实际配对表或蓝牙真机可用。

## Windows 传输补充

标准 vendor page `FF00` 之外，已确认蓝牙物理组也接纳 `FF43/0202`；仍须从 HID descriptor 验证输入/输出报告 ID 为 `10/11/12` 及长度。该通道有 [MX Master 3S 蓝牙 Windows 实测资料](https://github.com/marcelhoffs/input-switcher/blob/master/README.md#usagepage-and-usage)；本项目没有该鼠标的实测。

每次发送前在连接锁内对所有打开的 vendor collection 调用 [HidD_FlushQueue](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/hidsdi/nf-hidsdi-hidd_flushqueue)，清理已经积压的输入；失败保留 Windows 原因，不继续发送。之后才启动重叠读取和单次写入。它不能消除清理之后才到达、且字段恰好相同的迟到包，因此仍不能保证多实例或其它工具并行请求完全无干扰。

Windows 输入事件、OVERLAPPED 和缓冲区保持有效直到完成或取消完成。命名管道测试验证异步收发、无关事件过滤、超时回收、清理顺序和错误传播；管道中的清队列操作使用测试替身，不代表真实 HID 驱动或罗技鼠标已验证。
