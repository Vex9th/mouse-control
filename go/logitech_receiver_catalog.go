package main

// 目录依据及只读配对寄存器见 docs/LOGITECH_RECEIVERS.md。
// Slots 是路由探测上界，不是最大配对数量；旧协议未适配时为 0，仅识别。
// MaxDevices 是容量默认值，0 表示未知；不得据此截断稀疏槽位枚举。
type receiverInfo struct {
	Family     string
	Name       string
	Slots      byte
	MaxDevices byte
}

// 仅适用于已通过 HidD_GetAttributes 确认为 VID 046D 的物理设备。
// Solaar e7304c4c451cc9bb4f206a914844525e67856a28；
// Linux 72d3fcf802c45d00b300f25b848a93c3a2bd7c7e 补充 C543 及旧协议分类。
var logitechReceivers = map[uint16]receiverInfo{
	0xC52B: {"unifying", "罗技 Unifying 接收器", 7, 6},
	0xC532: {"unifying", "罗技 Unifying 接收器", 7, 6},
	0xC548: {"bolt", "罗技 Bolt 接收器", 6, 6},
	0xC518: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC51A: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC521: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC525: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC526: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC52E: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC52F: {"nano", "罗技 Nano 接收器", 7, 1},
	0xC531: {"nano", "罗技 G700 / G700s 接收器", 7, 1},
	0xC534: {"nano", "罗技 Nano 接收器", 7, 2},
	0xC535: {"nano", "罗技协议 Nano 接收器（Dell）", 7, 1},
	0xC537: {"nano", "罗技 G602 接收器", 7, 1},
	0xC539: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC53A: {"lightspeed", "罗技 POWERPLAY 接收器", 7, 1},
	0xC53D: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC53F: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC541: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC543: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC545: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC547: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	0xC54D: {"lightspeed", "罗技 LIGHTSPEED 接收器", 7, 1},
	// 27 MHz 以及自带 USB 集线器的旧蓝牙桥不能套用普通槽位查询。
	// C51B 在 Solaar 被列为 Nano；采用 Linux 的明确 27 MHz 分类。
	0xC513: {"27mhz", "罗技 MX3000 27 MHz 接收器", 0, 0},
	0xC517: {"27mhz", "罗技 EX100 / S510 27 MHz 接收器", 0, 4},
	0xC51B: {"27mhz", "罗技 27 MHz 鼠标接收器", 0, 0},
	0xC70A: {"legacy-bluetooth", "罗技 MX5000 旧蓝牙桥（鼠标）", 0, 0},
	0xC70E: {"legacy-bluetooth", "罗技 MX5000 旧蓝牙桥（键盘）", 0, 0},
	0xC713: {"legacy-bluetooth", "罗技 diNovo Edge 旧蓝牙桥（键盘）", 0, 0},
	0xC714: {"legacy-bluetooth", "罗技 diNovo Edge 旧蓝牙桥（鼠标）", 0, 0},
	0xC71B: {"legacy-bluetooth", "罗技 MX5500 旧蓝牙桥（键盘）", 0, 0},
	0xC71C: {"legacy-bluetooth", "罗技 MX5500 旧蓝牙桥（鼠标）", 0, 0},
	0xC71E: {"legacy-bluetooth", "罗技 diNovo Mini 旧蓝牙桥（键盘）", 0, 0},
	0xC71F: {"legacy-bluetooth", "罗技 diNovo Mini 旧蓝牙桥（鼠标）", 0, 0},
}

func logitechReceiver(pid uint16) (receiverInfo, bool) {
	info, ok := logitechReceivers[pid]
	return info, ok
}
