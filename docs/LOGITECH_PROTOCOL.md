# 罗技 HID++ 鼠标后端

核对日期：2026-09-29。实现按设备报告的 feature 和能力工作，不以单个鼠标型号白名单决定是否支持。当前完成的是通用协议实现与离线测试；没有罗技实机验证，不能把它当作全型号硬件兼容承诺。

本文件描述现有实现。Windows collection、蓝牙与接收器目录见 [LOGITECH_RECEIVERS.md](LOGITECH_RECEIVERS.md)。

## 来源与文件

- Logitech [官方 Root 协议](https://github.com/Logitech/cpg-docs/blob/master/hidpp20/features/0x0000-IRoot.rst)：Root 索引、feature 查询与 ping。
- Solaar `e7304c4c451cc9bb4f206a914844525e67856a28`：[身份与电量](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/hidpp20.py)、[功能常量](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/hidpp20_constants.py)、[DPI/回报率设置](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/settings_templates.py)、[帧与 Software ID](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/base.py)、[旧版电量](https://github.com/pwr-Solaar/Solaar/blob/e7304c4c451cc9bb4f206a914844525e67856a28/lib/logitech_receiver/hidpp10.py)。
- Linux `72d3fcf802c45d00b300f25b848a93c3a2bd7c7e`：[feature 专用电量解析](https://github.com/torvalds/linux/blob/72d3fcf802c45d00b300f25b848a93c3a2bd7c7e/drivers/hid/hid-logitech-hidpp.c)。`1000` 在约 1190–1320 行，`1001` 约 1392–1440 行，`1004` 约 1567–1675 行，`1F20` 约 1857–1935 行。
- libratbag `b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21`：[DPI、回报率与板载模式](https://github.com/libratbag/libratbag/blob/b8d4d3ca1f4d6b23c664ffee2888b8eb669bee21/src/hidpp20.c)，`2201` 约 1394–1560 行、`8100` 约 1665–1690 / 1913–1935 行。

实现分为 `logitech_protocol.go`（帧、探测、身份）、`logitech_battery.go`（电量）、`logitech_controls.go`（DPI/回报率）、`logitech_pairing.go`（只读配对信息）。Go 实现按上述协议资料组织，引用用于追溯报文字段和能力边界。

## 请求与能力发现

`hidppTransport.Exchange` 单次发送请求并等待匹配回复。报告 ID `10/11/12` 的总长度分别为 `7/20/64` 字节，包括 Report ID。字段为 `[reportID, address, featureIndex, function|softwareID, payload...]`。请求参数多于 3/16 字节时分别选长/超长报告。只有长输出 collection 的设备由 Windows 层在首次发送前扩大报告，不在超时后重发。

协议层严格匹配报告长度、地址、feature、function 和 Software ID；允许短请求返回长报告。`8F` 只有在短报告时才表示 HID++ 1 错误，长报告的 feature index `8F` 仍可合法；`FF` 是 HID++ 2 错误。错误携带的原 feature/function 也必须匹配。每次调用使用 800 ms 固定期限；无关事件不延长期限，读写都不自动重试。

固定 Software ID 为 `C`，避开已知的 Linux `1`、OpenRGB `7`、libratbag `8`、LGSTrayEx `A`、Solaar `B`、G HUB `D` 与固件 `F`。四位 Software ID 不提供进程级唯一身份，两个本工具实例及迟到响应仍需传输层隔离；不能据此宣称完全消除干扰。

Root 始终为索引 `0`：`fn10` 发送 `[0,0,随机 nonce]`，回复验证 major/minor/nonce；`fn00` 发送 feature ID 的大端字节。非 Root feature 返回索引 `0` 才表示缺失。超时、协议错误、短报文各保留原始原因，不转成“不支持”，也不因此尝试另一种协议。只有 ping 明确收到 HID++ 1 `INVALID_SUB_ID_COMMAND=1` 才降级到旧版。

每个后端持有独立 feature 缓存与串行锁；写入前重新查询相关 feature 索引。地址支持直连 `FF` 和槽位 `1..7`。G Pro Wireless 的直连地址例外由 Windows 枚举调用 `SetDirectConnection()` 标识。蓝牙 `FF→00` 例外仅由确认同一蓝牙物理设备的传输层规范化，接收器地址不放宽。

## 名称、类型与身份

`0005 fn00` 获取名称长度，`fn10(offset)` 读取片段，`fn20` 获取类型。类型 `3` 鼠标、`5` 轨迹球允许进入鼠标控制；键盘、接收器及其他已知非鼠标类型不启用控制。名称移除控制字符。接收器元数据只补充缺失信息，不覆盖设备返回的权威类型。

`0003 fn00` 返回 `payload[1:5]` 四字节 unit ID 和 `payload[7:13]` 六字节 model ID；两者共同构成稳定身份。全零或全 `FF` unit ID 不用于写入验证。接收器上的改值操作每次重新用 Root 查询 `0003` 并比较身份，身份缺失、读取失败或改变均停止写入。配对表 serial 仅补展示信息，不代替此检查。直连设备由 Windows 物理实例锁定；有有效 `0003` 时同样复核。

没有找到 `0004` 足够可靠的当前 wire 依据，因此不猜测其函数或偏移。只提供 `0004`、没有有效 `0003` 的接收器鼠标仍可读取已知功能，但禁止改值。该约束降低重配对误写风险，并非对检查与发送之间发生硬件变化的绝对保证。

## 电量

依次发现 `1004→1000→1001→1F20`；前一功能明确缺失才查询下一种，通信失败保留原因。各功能独立解码，不共用百分比或充电枚举。

| Feature | 查询 | 字段与行为 |
| --- | --- | --- |
| `1004` Unified Battery | 能力 `fn00`、状态 `fn10` | 能力 byte1 bit1 表示 SOC；有该位时状态 byte0 是 0..100%，其中 0 合法。无该位时用状态 byte1 与能力 byte0 相交后的档位位图。 |
| `1000` Battery Level | 能力 `fn10`、状态 `fn00` | 能力 levelCount≥10 且 flags bit1 才显示百分比；其他设备显示档位。充电时 level=0 为未知，不能显示 0%。 |
| `1001` Battery Voltage | `fn00` | 状态 byte0..1 大端毫伏；byte2 bit7 有效时低三位 0=充电、1=满、2=未充电；未知值保留错误。 |
| `1F20` ADC Measurement | `fn00` | byte0..1 大端毫伏；完整 flags `01/03/07` 分别为放电/充电/满，`0F` 及其他值为未知。ADC 不是 `1005`，也不能只用 bit1 判断充电。 |

`1004` 充电状态 `0/1/2/3/4` 为未充电/充电/慢充/完成/错误。`1000` 的 `2` 为充电末期、`4` 为慢充，不能套用 `1004`。档位 `1004` 的 `01/02/04/08` 分别为极低/低/正常/满；`1000` 使用 Linux 的 `<11/<30/<81` 阈值。电压始终单独报告 mV，不套通用电池曲线制造百分比。未知充电状态不丢掉有效电量或电压。

HID++ 1 仅读 `81/0D` 和 `81/07`；只有明确非法寄存器/命令错误才切换后备寄存器。`0D` byte0 为百分比，byte2 高四位 `30/50/90` 为放电/充电/满；`07` byte0 的 `7/5/3/1` 是档位，未知档位不转换百分比。配对格式与旧版状态掩码见接收器文档。HID++ 1 不启用 DPI 或回报率写入。

## DPI 与回报率

以下函数编号已经左移四位，尚未加 Software ID；所有 DPI 为大端 uint16，使用传感器 `0`。只接受设备实际报告的离散值或 `Min+n×Step`，不猜范围、步长或自动舍入。

| Feature | 读取能力/当前值 | 改值请求 |
| --- | --- | --- |
| `2201` | `fn00` 传感器数量；`fn10([sensor,0,0])` 返回单页七个完整 DPI word，忽略末 padding；`fn20(sensor)` 返回 sensor/current/default。 | `fn30([sensor,Xhi,Xlo])`；X=Y。current=0 时使用 default。 |
| `2202` | `fn10(sensor)` byte2 bit0=独立Y、bit1=LOD；`fn20([sensor,direction,page])` 的三字节 header 后为 DPI 字节流；`fn50(sensor)` 读当前/默认 X、当前/默认 Y、LOD。 | `fn60([sensor,Xhi,Xlo,Yhi,Ylo,LOD])`；不支持 Y/LOD 的字段置0，支持 LOD 时先读并原样保留。 |
| `8060` | `fn00` byte0 bit0..7 对应 1..8 ms；`fn10` byte0 为当前周期。 | `fn20(period_ms)`。3/6/7 ms 在整数 Hz 界面分别显示333/166/142，并附周期说明，写回仍使用原周期。 |
| `8061` | `fn10` 大端两字节能力位图；`fn20` 当前索引。索引0..6依次为125/250/500/1000/2000/4000/8000 Hz。 | `fn30(index)`，不把索引当毫秒。 |

DPI word 的 `E000|step` 是范围标记，前一个显式值为起点、后一个值为终点。步长0、倒序、缺终点等不启用控制。`2202` 最多32页，按上游累积字节流，允许 word 跨页；只检查已经确认的 sensor 字段，不猜 header 另外两字节的语义。`mouseFeatures.DPIRanges` 展示 X 轴范围，后端对 Y 轴用其独立报告的范围校验。

设置流程：校验能力/范围→读取当前值→同值直接返回→复核唯一身份及 feature 索引→检查模式→单次写入→独立读取并比较。设备有 `8100` 时仅读 `fn20`；mode=2 host 才改值，mode=1 板载或未知模式会说明原因并停止。没有 `8100` 时无需模式检查。没有自动模式切换、配对、按键重映射、profile/flash 或固件写入。

写入超时/协议错误返回“结果未确认”，不重复写。写入已应答但读回失败或不一致也返回错误；`changed=true` 只表示指令已应答，不表示物理效果已验证。用户可重新读取或重新扫描，程序不执行猜测性恢复。

## 验证范围与已知限制

离线模拟器使用独立的动态 feature 索引和设备状态，覆盖帧匹配、ping nonce、固定 Software ID、旧版降级、名称分片、身份变化、配对布局、电量语义、DPI步长与分页、两代回报率、同值跳过、单次写入、读回失败和板载模式。验证命令为 `go test ./...`、`go test -race ./...`、`go vet ./...`；构建或测试通过不能证明 Windows HID 或鼠标本体正常工作。

尚未实现旧式非标准 DPI（例如 libratbag 的 G602 专用布局）、`0004`、太阳能事件订阅或新型非 HID++ 协议。此类设备可保留识别与其它独立功能，未确认布局不猜测写入。

公开实测指出 [PRO X2 SUPERSTRIKE](https://github.com/mclol0/linux-superstrike/blob/main/REVERSE_ENGINEERING.md) 的 live DPI setter 可能无效，回报率 getter 也可能缓存。因此本工具只能证明命令和读回一致；真实 DPI 要按相同物理距离比较位移计数，真实回报率要在持续移动时测量输入报告频率。尚未取得本项目罗技真机样本，所有写入仍属于未经硬件验证的实现。
