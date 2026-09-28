# 鼠标 DPI 与回报率协议

能力目录固定于 OpenRazer `6820f9da169d354bc7e6e93a0aa8683a6bb75792`。`mouseCapabilities` 包含 **113 个鼠标相关 PID**（112 个鼠标条目和 HyperPolling 接收器 `00B3`），其中 **101 个支持本实现的 16 位 XY DPI 协议**，**104 个支持回报率读写**。这些数字代表源码依据，不代表全部经过 Windows 真机验证。型号识别继续来自 `models`；未知 PID 不猜测写入命令。

## DPI 报文和边界

- 读取：class `04`、command `85`、data size `07`。`arguments[0]` 通常为 `00`，仅 `002F` Imperator 和 `0039` Orochi 2013 为 `01`。对应 `DPILegacyRead`，这个名字不表示使用旧版 byte-DPI 协议。
- 响应：`arguments[1:3]` 是 X、`arguments[3:5]` 是 Y，均为 16 位大端。去除 report ID 的 90 字节载荷中为 `[9:11]` 和 `[11:13]`；完整 91 字节 HID 报文中为 `[10:12]` 和 `[12:14]`。
- 写入：class `04`、command `05`、data size `07`，参数为 `[01, X高, X低, Y高, Y低, 00, 00]`。**上游 `set_dpi_xy(variable_storage, ...)` 实际忽略 storage 入参，固定写 `VARSTORE=01`**。不能根据调用处 `NOSTORE` 宣称写入不会保存。
- `DPITID` 同时用于读写。每个 PID 的 TID 与上限见下表；最大 DPI 取 daemon 类及其继承关系中的 `DPI_MAX`，下限 100 来自上游报文构造函数。只接受范围内整数，不静默截断；源码没有通用 50 DPI 等步长限制，也不能据此保证每个传感器的原生步长为 1。
- 保持设置不变：先读当前 X/Y，只有两轴都等于请求时直接跳过写入。发送“同值”仍可能保存设备配置，不能当成零副作用验证。实际修改后重新读取两轴，设备返回值不同则明确报告，不伪造成功。若 CLI 使用单值 DPI，显式将 X/Y 都设为该值；不能为了验证而抹平原有不相等的两轴设置。
- 12 个 PID 不启用该 DPI 协议：`0013 0015 0016 001F 0020 0029 002E 0036 0037 0038 0041 0042`。其中旧式 byte-DPI 使用 `04/81` 与 `04/01`；Orochi 2011 和 DeathAdder 3.5G 使用其他命令或缓存；Abyssus `0042` 没有 dpi 属性。

证据：[DPI 读取分支与响应解析](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L2620-L2808)、[DPI 写入分支](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L2385-L2612)、[实际 DPI 报文构造](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razerchromacommon.c#L1208-L1264)、[daemon 上限校验](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/dbus_services/dbus_methods/mamba.py#L155-L203)。

## 回报率协议

| 协议 | 读取 | 读取结果 | 写入 | 编码 |
| --- | --- | --- | --- | --- |
| 旧版 | `00/85`，size `01`，参数全零 | `arguments[0]` | `00/05`，size `01`，`arguments[0]=编码` | `01=1000`、`02=500`、`08=125` Hz |
| 新版 | `00/C0`，size `01`，`arguments[0]=00` | `arguments[1]` | `00/40`，size `02`，参数 `[选择字节, 编码]` | `01=8000`、`02=4000`、`04=2000`、`08=1000`、`10=500`、`40=125` Hz |

新协议共有 8 个 PID：`0091 009E 009F 00B2 00B3 00BE 00BF 00C1`。这些条目都必须连续写两次，选择字节先 `00` 后 `01`；任何一步失败均保留错误并提示刷新核对，不自动重发；仅两步均应答成功后读回，不能把部分成功当成完成。不能把第二条命令自动省略。

- `PollTID` 专用于读取；`PollWriteTID` 用于唯一写入或首写；`PollSecondTID` 只用于第二次写入。
- `0044/0045`（Mamba）读取 TID `FF`、写入 `3F`；`0086/0088`（Basilisk Ultimate）读取 `FF`、写入 `1F`。
- 新版 `0091/00B3` 读取和首写 TID `1F`，第二次写入 `FF`；其他新版两次写入都为 `1F`。
- 新协议并不意味着所有高频档位都可用：`009E` Viper Mini SE 有线只允许 125/500/1000 Hz；其余频率按对应 daemon `POLL_RATES`。未单独声明时，上游默认 125/500/1000 Hz。
- 禁用 9 个 PID 的回报率功能：`0013 0016 0029` 依赖旧式状态/缓存；`0046 004C` 未绑定 poll_rate 属性；`00C7 00C8 00D0 00D1` 的 daemon 允许 250 Hz，但驱动选择旧版编码函数，250 会落入默认分支变成 500 Hz。该矛盾未独立解决前，`PollRates=nil`，不发送回报率命令。
- 对合法列表之外的值直接拒绝，不能照抄上游构造函数“默认降为 500 Hz”的行为。读回未知编码也应报错，不能显示 0 Hz。

证据：[回报率读取与每 PID TID](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L1838-L2040)、[回报率写入与双写](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L2048-L2229)、[两版报文与编码](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razerchromacommon.c#L1083-L1185)、[默认频率](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/device_base.py#L114-L116)、[Pro Click V2 冲突声明](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2081-L2135)。

## 优先验证的两款鼠标

| PID | 型号 | DPI | 回报率 |
| --- | --- | --- | --- |
| `00BF` | DeathAdder V4 Pro 接收器 | 100..45000；读 storage=0，写 storage=1；TID `1F` | 新协议；125/500/1000/2000/4000/8000；两次写入 TID 均 `1F` |
| `00B7` | DeathAdder V3 Pro 原接收器 | 100..35000（当前上游值）；读 storage=0，写 storage=1；TID `1F` | 旧协议；125/500/1000；读写 TID `1F` |

`00B7` 不因为鼠标产品支持另配 HyperPolling 接收器就启用高频协议。HyperPolling `00B3` 的上游 DPI 上限为 30000；它代表接收器条目的限制，不能据此推断所配鼠标传感器的实际规格。`0095` 仍沿用上游 USB 表里的 Bluetooth 型号名称，不证明本程序支持直接蓝牙连接。

证据：[V3 Pro 型号与上限](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1620-L1656)、[V4 Pro 型号与上限](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2000-L2022)、[属性实际绑定](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L7199-L7228)。

## 序列号与固件研究边界

本能力目录不启用这两项功能。上游序列号为 `00/82`、size `16`（十六进制，22 字节），取 `arguments[0:22]`；固件为 `00/81`、size `02`，取 `arguments[0]`、`arguments[1]` 组成主次版本。V4 Pro/V3 Pro 均使用 TID `1F`，但其他型号不能复用 DPI TID：例如 Mamba Elite `006C` 序列号用 `FF`，Naga X `0096` 用 `08`。早期设备还返回模拟序列号或固定固件版本，不能在本工具中当作硬件实读值。

证据：[只读报文定义](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razerchromacommon.c#L56-L70)、[序列号分支及偏移](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L1373-L1524)、[固件分支及偏移](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/driver/razermouse_driver.c#L765-L912)。

## 逐 PID 能力表

所有 DPI 行中的写 storage 均为 1；读 storage 仅标记“1”的两项例外。回报率 TID 按“读 / 首写 / 第二次写”排列，`—` 表示本版不启用。类名和行号指向同一版本的 `daemon/openrazer_daemon/hardware/mouse.py`，属性可能继承自父类。

| PID | daemon 型号类 | DPI 上限 | DPI TID / 读 storage | 回报率协议 | 回报率 TID | Hz 档位 |
| --- | --- | --- | --- | --- | --- | --- |
| `0013` | [RazerOrochi2011](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L741) | — | — | — | — | — |
| `0015` | [RazerNaga](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L421) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0016` | [RazerDeathAdder3_5G](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L625) | — | — | — | — | — |
| `001F` | [RazerNagaEpic](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1523) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0020` | [RazerAbyssus1800](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L773) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0024` | [RazerMamba2012Wired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L674) | 6400 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0025` | [RazerMamba2012Wireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L658) | 6400 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0029` | [RazerDeathAdder3_5GBlack](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L642) | — | — | — | — | — |
| `002E` | [RazerNaga2012](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L438) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `002F` | [RazerImperator](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L269) | 6400 | `FF` / 1 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0032` | [RazerOuroboros](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L283) | 8200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0034` | [RazerTaipan](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L566) | 8200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0036` | [RazerNagaHexRed](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L549) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0037` | [RazerDeathAdder2013](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L363) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0038` | [RazerDeathAdder1800](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1046) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0039` | [RazerOrochi2013](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L298) | 6400 | `FF` / 1 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `003E` | [RazerNagaEpicChromaWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1451) | 8200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `003F` | [RazerNagaEpicChromaWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1469) | 8200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0040` | [RazerNaga2014](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L724) | 8200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0041` | [RazerNagaHex](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L532) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0042` | [RazerAbyssus](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L258) | — | — | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0043` | [RazerDeathAdderChroma](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L331) | 10000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0044` | [RazerMambaChromaWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L221) | 16000 | `FF` / 0 | 旧版 | `FF` / `3F` / — | 125/500/1000 |
| `0045` | [RazerMambaChromaWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L201) | 16000 | `FF` / 0 | 旧版 | `FF` / `3F` / — | 125/500/1000 |
| `0046` | [RazerMambaTE](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L240) | 16000 | `FF` / 0 | — | — | — |
| `0048` | [RazerOrochiWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L312) | 8200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `004C` | [RazerDiamondbackChroma](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L607) | 16000 | `FF` / 0 | — | — | — |
| `004F` | [RazerDeathAdder2000](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L347) | 2000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0050` | [RazerNagaHexV2](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L378) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0053` | [RazerNagaChroma](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L455) | 16000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0054` | [RazerDeathAdder3500](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L801) | 3500 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0059` | [RazerLanceheadWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L71) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `005A` | [RazerLanceheadWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L102) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `005B` | [RazerAbyssusV2](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L757) | 5000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `005C` | [RazerDeathAdderElite](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L583) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `005E` | [RazerAbyssus2000](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L787) | 2000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0060` | [RazerLanceheadTE](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L172) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0062` | [RazerAtherisReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1283) | 7200 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0064` | [RazerBasilisk](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1060) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0065` | [RazerBasiliskEssential](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1084) | 6400 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0067` | [RazerNagaTrinity](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L500) | 16000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `006A` | [RazerAbyssusEliteDVaEdition](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L132) | 7200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `006B` | [RazerAbyssusEssential](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L152) | 7200 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `006C` | [RazerMambaElite](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L908) | 16000 | `1F` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `006E` | [RazerDeathAdderEssential](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L873) | 6400 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `006F` | [RazerLanceheadWirelessReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L63) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0070` | [RazerLanceheadWirelessWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L32) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0071` | [RazerDeathAdderEssentialWhiteEdition](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L110) | 6400 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0072` | [RazerMambaWirelessReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L715) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0073` | [RazerMambaWirelessWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L691) | 16000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0077` | [RazerProClickReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1478) | 16000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0078` | [RazerViper](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L851) | 16000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `007A` | [RazerViperUltimateWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L817) | 20000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `007B` | [RazerViperUltimateWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L841) | 20000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `007C` | [RazerDeathAdderV2ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1249) | 20000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `007D` | [RazerDeathAdderV2ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1273) | 20000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0080` | [RazerProClickWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1499) | 16000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0083` | [RazerBasiliskXHyperSpeed](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1300) | 16000 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0084` | [RazerDeathAdderV2](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1226) | 20000 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `0085` | [RazerBasiliskV2](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1168) | 20000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0086` | [RazerBasiliskUltimateWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1106) | 20000 | `1F` / 0 | 旧版 | `FF` / `1F` / — | 125/500/1000 |
| `0088` | [RazerBasiliskUltimateReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1161) | 20000 | `1F` / 0 | 旧版 | `FF` / `1F` / — | 125/500/1000 |
| `008A` | [RazerViperMini](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L10) | 8500 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `008C` | [RazerDeathAdderV2Mini](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1375) | 8500 | `3F` / 0 | 旧版 | `3F` / `3F` / — | 125/500/1000 |
| `008D` | [RazerNagaLeftHanded2020](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L939) | 20000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `008F` | [RazerNagaProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L971) | 20000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0090` | [RazerNagaProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1003) | 20000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0091` | [RazerViper8KHz](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1399) | 20000 | `FF` / 0 | 新版双写 | `1F` / `1F` / `FF` | 125/500/1000/2000/4000/8000 |
| `0094` | [RazerOrochiV2Receiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1319) | 18000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0095` | [RazerOrochiV2Bluetooth](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1338) | 18000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0096` | [RazerNagaX](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1345) | 18000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `0098` | [RazerDeathAdderEssential2021](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L892) | 6400 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `0099` | [RazerBasiliskV3](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1192) | 26000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `009A` | [RazerProClickMiniReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1846) | 12000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `009C` | [RazerDeathAdderV2XHyperSpeed](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1507) | 14000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `009E` | [RazerViperMiniSEWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1420) | 30000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `1F` | 125/500/1000 |
| `009F` | [RazerViperMiniSEWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1439) | 30000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `1F` | 125/500/1000/2000/4000/8000 |
| `00A1` | [RazerDeathAdderV2Lite](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1866) | 8500 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00A3` | [RazerCobra](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1890) | 8500 | `FF` / 0 | 旧版 | `FF` / `FF` / — | 125/500/1000 |
| `00A5` | [RazerViperV2ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1541) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00A6` | [RazerViperV2ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1558) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00A7` | [RazerNagaV2ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1011) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00A8` | [RazerNagaV2ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1038) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00AA` | [RazerBasiliskV3ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1659) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00AB` | [RazerBasiliskV3ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1695) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00AF` | [RazerCobraProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1566) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00B0` | [RazerCobraProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1595) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00B2` | [RazerDeathAdderV3](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1602) | 30000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `1F` | 125/500/1000/2000/4000/8000 |
| `00B3` | [RazerHyperPollingWirelessDongle](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1821) | 30000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `FF` | 125/500/1000/2000/4000/8000 |
| `00B4` | [RazerNagaV2HyperSpeedReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1913) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00B6` | [RazerDeathAdderV3ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1620) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00B7` | [RazerDeathAdderV3ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1637) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00B8` | [RazerViperV3HyperSpeed](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1931) | 30000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00B9` | [RazerBasiliskV3XHyperSpeed](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1948) | 18000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00BE` | [RazerDeathAdderV4ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2000) | 45000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `1F` | 125/500/1000/2000/4000/8000 |
| `00BF` | [RazerDeathAdderV4ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2018) | 45000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `1F` | 125/500/1000/2000/4000/8000 |
| `00C0` | [RazerViperV3ProWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2025) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00C1` | [RazerViperV3ProWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2043) | 35000 | `1F` / 0 | 新版双写 | `1F` / `1F` / `1F` | 125/500/1000/2000/4000/8000 |
| `00C2` | [RazerDeathAdderV3ProWired_Alternate](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1645) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00C3` | [RazerDeathAdderV3ProWireless_Alternate](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1652) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00C4` | [RazerDeathAdderV3HyperSpeedWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2055) | 26000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00C5` | [RazerDeathAdderV3HyperSpeedWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2073) | 26000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00C7` | [RazerProClickV2VerticalEditionWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2081) | 30000 | `1F` / 0 | — | — | — |
| `00C8` | [RazerProClickV2VerticalEditionWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2102) | 30000 | `1F` / 0 | — | — | — |
| `00CB` | [RazerBasiliskV3_35K](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1786) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00CC` | [RazerBasiliskV3Pro35KWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1703) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00CD` | [RazerBasiliskV3Pro35KWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1739) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00D0` | [RazerProClickV2Wired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2109) | 30000 | `1F` / 0 | — | — | — |
| `00D1` | [RazerProClickV2Wireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L2130) | 30000 | `1F` / 0 | — | — | — |
| `00D3` | [RazerBasiliskMobileWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1970) | 18000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00D4` | [RazerBasiliskMobileReceiver](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1991) | 18000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00D6` | [RazerBasiliskV3Pro35KPhantomGreenEditionWired](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1747) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
| `00D7` | [RazerBasiliskV3Pro35KPhantomGreenEditionWireless](https://github.com/openrazer/openrazer/blob/6820f9da169d354bc7e6e93a0aa8683a6bb75792/daemon/openrazer_daemon/hardware/mouse.py#L1778) | 35000 | `1F` / 0 | 旧版 | `1F` / `1F` / — | 125/500/1000 |
