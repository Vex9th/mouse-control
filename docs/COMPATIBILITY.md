# 兼容性与验证范围 / Compatibility

当前版本：GUI 1.4.2。源码目录中的条目表示已实现的协议能力，不是逐型号实机认证。

## 已有硬件验证

| 设备 | 连接 | 已验证 |
| --- | --- | --- |
| Razer DeathAdder V4 Pro，PID `00BF` | 无线接收器 | 电量、充电状态、DPI、回报率读取；CLI 中短暂修改 DPI/回报率后恢复并读回 |
| Logitech | — | 尚无本项目真机样本 |

DPI 与回报率的协议读回只能证明返回值一致，不能替代传感器位移或输入报告频率测量。同一产品的有线、无线和蓝牙路径需要分别验证。

## GUI 1.4.2 验证

- 前端：20 项测试、92 个断言，类型检查和构建通过。
- Windows 原生测试：111 项顶层测试通过；真实 GUI 集成另行启用并连续通过两次。
- WebView2 实际窗口：900×440 默认客户区和 760×440 最小客户区无滚动溢出、无控件越界；内嵌字体加载完成。
- 浏览器演示：多设备分页与切换、双轴大数值、回报率选项、未知电量、只读能力、长详情和错误分页。
- 真实 GUI 验证时接收器可枚举，但鼠标未应答。界面正确显示不可用及原始错误；该次运行没有新的成功电量/DPI/回报率读数。

上述记录不表示所有显示器缩放、设备固件或厂商软件共存组合已验证。Windows 编译成功、模拟协议测试或演示界面操作不能代替真实设备测试。

## 适配反馈

请提供型号、VID/PID、连接方式、Windows 版本、程序版本及错误信息。若反馈参数设置问题，请说明原值、目标值、返回信息及刷新后的实际读数。公开日志前请遮盖设备序列号；不要提供账号、密钥或无关系统资料。

## English

The device catalog describes implemented protocol support, not a hardware certification list. DeathAdder V4 Pro wireless (`00BF`) has real battery/DPI/polling-rate reads and CLI settings/restore tests. This project has not yet verified Logitech hardware.

GUI 1.4.2 passed native WebView2 font, layout, bridge and shutdown checks at 900×440 and 760×440 logical pixels. During that run the receiver was detected but the mouse did not respond; the UI correctly reported unavailable values. No fresh successful mouse readings or hardware writes are claimed for that GUI run.
