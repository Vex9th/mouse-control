package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

type options struct {
	Watch                                       int
	Help, Supported, Once, Interactive, Details bool
	Device                                      string
	SetDPI                                      *dpiValue
	SetRate                                     int
}

func parseOptions(args []string) (options, error) {
	var o options
	seen := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		if !strings.HasPrefix(key, "-") {
			return o, fmt.Errorf("无法识别“%s”", arg)
		}
		key = strings.TrimPrefix(strings.TrimPrefix(key, "-"), "-")
		if key == "h" {
			key = "help"
		}
		if seen[key] {
			return o, fmt.Errorf("-%s 只能指定一次", key)
		}
		seen[key] = true
		switch key {
		case "help", "supported", "once", "interactive", "details":
			if hasValue {
				return o, fmt.Errorf("-%s 不接受附加值", key)
			}
			switch key {
			case "help":
				o.Help = true
			case "supported":
				o.Supported = true
			case "once":
				o.Once = true
			case "interactive":
				o.Interactive = true
			case "details":
				o.Details = true
			}
		case "watch", "set-dpi", "set-rate", "device":
			if !hasValue {
				i++
				if i >= len(args) {
					return o, fmt.Errorf("-%s 后需要填写值", key)
				}
				value = args[i]
			}
			switch key {
			case "watch":
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 || n > 86400 {
					return o, fmt.Errorf("查询间隔必须为 1–86400 之间的整数秒")
				}
				o.Watch = n
			case "set-dpi":
				dpi, err := parseDPI(value)
				if err != nil {
					return o, err
				}
				o.SetDPI = &dpi
			case "set-rate":
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 || n > 8000 {
					return o, fmt.Errorf("回报率必须为 1–8000 之间的整数；实际档位以鼠标详情为准")
				}
				o.SetRate = n
			case "device":
				o.Device = strings.TrimSpace(value)
				if o.Device == "" {
					return o, fmt.Errorf("-device 后需要完整的设备 ID")
				}
			}
		default:
			return o, fmt.Errorf("无法识别“%s”", arg)
		}
	}
	modes := 0
	for _, enabled := range []bool{o.Help, o.Supported, o.Once, o.Interactive, o.Watch > 0, o.SetDPI != nil, o.SetRate != 0} {
		if enabled {
			modes++
		}
	}
	if modes > 1 {
		return o, fmt.Errorf("帮助、目录、交互、单次查询、循环查询和设置命令不能同时使用；每次只能执行一种操作")
	}
	if (o.Help || o.Supported || o.Interactive) && (o.Device != "" || o.Details) {
		return o, fmt.Errorf("-device 和 -details 只能用于查询或设置命令")
	}
	return o, nil
}

func parseDPI(s string) (dpiValue, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) < 1 || len(parts) > 2 {
		return dpiValue{}, fmt.Errorf("DPI 请填写一个整数，或用英文逗号分隔 X,Y，例如 1600,1200")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || x < 1 || x > 65535 {
		return dpiValue{}, fmt.Errorf("DPI 必须为 1–65535 之间的整数；具体范围与步长以鼠标详情为准")
	}
	y := x
	if len(parts) == 2 {
		y, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || y < 1 || y > 65535 {
			return dpiValue{}, fmt.Errorf("Y 轴 DPI 必须为 1–65535 之间的整数")
		}
	}
	return dpiValue{X: x, Y: y}, nil
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `鼠标工具 · RazerBattery %s

用法
  RazerBattery.exe                    打开交互菜单（输出重定向时查询一次）
  RazerBattery.exe -interactive       打开交互菜单
  RazerBattery.exe -once              查询电量、当前 DPI 和回报率
  RazerBattery.exe -details           查询并显示设备 ID、能力范围
  RazerBattery.exe -watch 30          每 30 秒重新识别并查询
  RazerBattery.exe -set-dpi 1600      设置当前 DPI（X、Y 轴相同）
  RazerBattery.exe -set-dpi 1600,1200 设置当前 DPI（仅限支持分别设置双轴的型号）
  RazerBattery.exe -set-rate 1000     设置回报率，单位 Hz
  RazerBattery.exe -supported         查看鼠标适配目录
  RazerBattery.exe -h                 显示本帮助

多只鼠标同时连接时，设置命令必须附加 -device "完整的设备 ID"。
ID 可从 -details 输出复制；只匹配完整 ID，不按型号或编号猜测。
设置后读取实际值核对；当前值已相同时跳过写入。
每次设置一种参数；实际 DPI 范围与回报率档位以型号详情为准。

支持 Windows 10 / 11 x64；支持雷蛇、罗技及部分迈从鼠标和对应接收器。
罗技通过 HID++ 动态探测电量、DPI、回报率；部分型号或连接方式只提供部分功能。
迈从新协议族为实验性只读支持，尚无真机验证。
USB、无线接收器与可访问的蓝牙接口依型号而定，不代表全系真机认证。
未适配或读取失败会说明原因，不会把未知状态显示为 0。
`, version)
}

func printReport(w io.Writer, rows []deviceResult, at time.Time) {
	fmt.Fprintf(w, "\n  鼠标工具  ·  RazerBattery %s\n  %s\n  %s\n", version, at.Format("2006-01-02 15:04:05"), strings.Repeat("─", 52))
	if len(rows) == 0 {
		fmt.Fprintln(w, "\n  未发现支持的鼠标。\n  请检查 USB、无线接收器或蓝牙连接，并确认鼠标已开启。")
	}
	for i, r := range rows {
		d := r.Device
		if len(rows) > 1 {
			fmt.Fprintf(w, "\n  [%02d] %s\n       实例  %s\n", i+1, deviceName(d), cleanLabel(d.ID))
		} else {
			fmt.Fprintf(w, "\n  %s\n", deviceName(d))
		}
		cap := deviceFeatures(d)
		if !cap.Battery {
			if cap.BatteryReason != "" {
				fmt.Fprintf(w, "       电量    %s\n", cleanLabel(cap.BatteryReason))
			} else {
				fmt.Fprintln(w, "       电量    此型号未适配电量查询")
			}
		} else if r.Err != nil {
			fmt.Fprintf(w, "       电量    读取失败：%s\n", r.Err)
		} else {
			fmt.Fprintf(w, "       电量    %s · %s\n", batteryLabel(r.Reading), chargeLabel(d.Info, r.Reading))
			if r.Reading.ChargeError != nil {
				fmt.Fprintf(w, "       提示    充电状态查询失败：%s\n", r.Reading.ChargeError)
			}
		}
		if len(cap.DPIRanges) == 0 && !cap.DPIReadOnly {
			fmt.Fprintf(w, "       DPI     %s\n", unavailableReason(cap.DPIReason))
		} else if r.DPIErr != nil {
			fmt.Fprintf(w, "       DPI     读取失败：%s\n", r.DPIErr)
		} else if r.DPI.X < 1 || r.DPI.Y < 1 {
			fmt.Fprintln(w, "       DPI     未知")
		} else {
			fmt.Fprintf(w, "       DPI     %s\n", formatDPI(r.DPI))
		}
		if len(cap.PollRates) == 0 && !cap.RateReadOnly {
			fmt.Fprintf(w, "       回报率  %s\n", unavailableReason(cap.RateReason))
		} else if r.RateErr != nil {
			fmt.Fprintf(w, "       回报率  读取失败：%s\n", r.RateErr)
		} else if r.Rate < 1 {
			fmt.Fprintln(w, "       回报率  未知")
		} else {
			fmt.Fprintf(w, "       回报率  %d Hz\n", r.Rate)
		}
	}
	fmt.Fprintf(w, "\n  %s\n", strings.Repeat("─", 52))
}

func chargeLabel(info modelInfo, r reading) string {
	if r.ChargeText != "" {
		return cleanLabel(r.ChargeText)
	}
	if info.NoCharge {
		return "不支持充电状态查询"
	}
	if !r.ChargeKnown {
		return "充电状态未知"
	}
	if r.Charging {
		if !r.PercentUnknown && r.Percent == 100 {
			return "已充满 · 充电连接中"
		}
		return "正在充电"
	}
	if !r.PercentUnknown && r.Percent <= 20 {
		return "电量偏低，建议充电"
	}
	return "未充电"
}

func formatDPI(dpi dpiValue) string {
	if dpi.X == dpi.Y {
		return strconv.Itoa(dpi.X)
	}
	return fmt.Sprintf("X %d / Y %d", dpi.X, dpi.Y)
}

func formatRates(rates []int) string {
	parts := make([]string, len(rates))
	for i, rate := range rates {
		parts[i] = strconv.Itoa(rate)
	}
	return strings.Join(parts, " / ") + " Hz"
}

func printDetails(w io.Writer, rows []deviceResult) {
	for _, r := range rows {
		d := r.Device
		cap := deviceFeatures(d)
		fmt.Fprintf(w, "\n  %s · 详情\n    连接方式  %s\n    设备 ID   %s\n    型号标识  VID %04X / PID %04X\n", deviceName(d), cleanLabel(d.Info.Connection), cleanLabel(d.ID), deviceVID(d), d.PID)
		if cap.BatteryReason != "" {
			fmt.Fprintf(w, "    电量说明  %s\n", cleanLabel(cap.BatteryReason))
		}
		if len(cap.DPIRanges) > 0 {
			fmt.Fprintf(w, "    DPI 范围  %s\n", formatDPIRanges(cap.DPIRanges))
		}
		if cap.DPIReadOnly {
			fmt.Fprintf(w, "    DPI 设置  只读：%s\n", settingRestriction(true, cap.DPIReason))
		} else if len(cap.DPIRanges) == 0 {
			fmt.Fprintf(w, "    DPI 设置  %s\n", unavailableReason(cap.DPIReason))
		} else if cap.SeparateAxes {
			fmt.Fprintln(w, "    DPI 设置  X、Y 轴可分别设置")
		} else {
			fmt.Fprintln(w, "    DPI 设置  统一设置双轴，请输入单个 DPI 值")
		}
		if len(cap.PollRates) > 0 {
			fmt.Fprintf(w, "    回报率档位  %s\n", formatRates(cap.PollRates))
		}
		if cap.RateReadOnly {
			fmt.Fprintf(w, "    回报率设置  只读：%s\n", settingRestriction(true, cap.RateReason))
		} else if len(cap.PollRates) == 0 {
			fmt.Fprintf(w, "    回报率设置  %s\n", unavailableReason(cap.RateReason))
		}
		for _, note := range cap.Notes {
			fmt.Fprintf(w, "    说明  %s\n", cleanLabel(note))
		}
	}
}

func unavailableReason(reason string) string {
	if reason != "" {
		return cleanLabel(reason)
	}
	return "此型号未适配"
}

func settingRestriction(readOnly bool, reason string) string {
	if reason == "" && readOnly {
		return "当前接口仅支持读取"
	}
	return unavailableReason(reason)
}

func formatDPIRanges(ranges []dpiRange) string {
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Min == r.Max {
			parts = append(parts, strconv.Itoa(r.Min))
		} else if r.Step > 1 {
			parts = append(parts, fmt.Sprintf("%d–%d（步长 %d）", r.Min, r.Max, r.Step))
		} else {
			parts = append(parts, fmt.Sprintf("%d–%d", r.Min, r.Max))
		}
	}
	return strings.Join(parts, " / ")
}

func batteryLabel(r reading) string {
	if !r.PercentUnknown {
		return fmt.Sprintf("%s  %d%%", batteryBar(r.Percent), r.Percent)
	}
	if r.Level != "" {
		if r.VoltageMV > 0 {
			return fmt.Sprintf("%s · %d mV", cleanLabel(r.Level), r.VoltageMV)
		}
		return cleanLabel(r.Level)
	}
	if r.VoltageMV > 0 {
		return fmt.Sprintf("%d mV", r.VoltageMV)
	}
	return "未知"
}

func cleanLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

func displayName(s string) string {
	s = strings.NewReplacer("(Wired)", "（有线）", "(Wireless)", "（无线）", "(Receiver)", "（接收器）", "(Bluetooth)", "（蓝牙）").Replace(s)
	for _, brand := range [][2]string{{"Razer ", "雷蛇 "}, {"Logitech ", "罗技 "}, {"Logi ", "罗技 "}, {"MCHOSE ", "迈从 "}} {
		if len(s) >= len(brand[0]) && strings.EqualFold(s[:len(brand[0])], brand[0]) {
			s = brand[1] + s[len(brand[0]):]
			break
		}
	}
	return cleanLabel(s)
}

func deviceName(d device) string {
	s := displayName(d.Info.Name)
	if strings.HasPrefix(s, "雷蛇") || strings.HasPrefix(s, "罗技") || strings.HasPrefix(s, "迈从") {
		return s
	}
	switch deviceVID(d) {
	case 0x1532:
		return "雷蛇 " + s
	case 0x046d:
		return "罗技 " + s
	case mchoseVID:
		return "迈从 " + s
	default:
		return s
	}
}

func batteryBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct + 5) / 10
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", 10-filled) + "]"
}

func reportExitCode(rows []deviceResult) int {
	if len(rows) == 0 {
		return 1
	}
	supported := false
	for _, r := range rows {
		cap := deviceFeatures(r.Device)
		if cap.DiscoveryFailed {
			supported = true
		}
		if cap.Battery {
			supported = true
			if r.Err == nil {
				return 0
			}
		}
		if len(cap.DPIRanges) > 0 || cap.DPIReadOnly {
			supported = true
			if r.DPIErr == nil && r.DPI.X > 0 && r.DPI.Y > 0 {
				return 0
			}
		}
		if len(cap.PollRates) > 0 || cap.RateReadOnly {
			supported = true
			if r.RateErr == nil && r.Rate > 0 {
				return 0
			}
		}
	}
	if supported {
		return 2
	}
	return 0
}

func printSupported(w io.Writer) {
	pids := make([]int, 0, len(models))
	for p := range models {
		if isMousePID(p) {
			pids = append(pids, int(p))
		}
	}
	sort.Ints(pids)
	fmt.Fprintf(w, "雷蛇鼠标适配目录 · %d 个 PID\n\n", len(pids))
	for _, p := range pids {
		pid := uint16(p)
		m := models[pid]
		d := device{PID: pid, Info: m}
		cap := deviceFeatures(d)
		var abilities []string
		if cap.Battery {
			abilities = append(abilities, "电量")
		}
		if len(cap.DPIRanges) > 0 {
			abilities = append(abilities, "DPI "+formatDPIRanges(cap.DPIRanges))
		}
		if len(cap.PollRates) > 0 {
			abilities = append(abilities, "回报率 "+formatRates(cap.PollRates))
		}
		if len(abilities) == 0 {
			abilities = append(abilities, "仅识别")
		}
		fmt.Fprintf(w, "  %04X  %s\n        %s\n", p, deviceName(d), strings.Join(abilities, " · "))
	}
	fmt.Fprintln(w, "\n迈从鼠标 · 实验性只读（VID 3837）")
	mchosePIDs := make([]int, 0, len(mchoseModels))
	for pid := range mchoseModels {
		mchosePIDs = append(mchosePIDs, int(pid))
	}
	sort.Ints(mchosePIDs)
	for _, value := range mchosePIDs {
		m := mchoseModels[uint16(value)]
		fmt.Fprintf(w, "  %04X  %s\n", value, displayName(m.Name))
	}
	fmt.Fprintln(w, "  共用接收器：1014 / 1018 / 1016；须有效响应确认鼠标型号。\n  电量、当前 DPI 与回报率只读；旧协议族未适配，尚无真机验证。")

	fmt.Fprintln(w, "\n罗技鼠标 · HID++ 1.0 / 2.0 动态检测\n  连接：USB / LIGHTSPEED / Unifying / Bolt / 可访问的蓝牙接口\n  按鼠标实际公开的能力查询电量、DPI、回报率；有些功能可能只读或不可用。\n\n目录与协议适配不等于全系真机认证；每台鼠标的详情会列出实际可用能力。")
}

// 只以物理 USB 实例归并。同 PID 的两个实例必须分别保留。
func physicalUSBPID(id string) (uint16, bool) {
	parts := strings.Split(strings.ToLower(id), `\`)
	if len(parts) != 3 || parts[0] != "usb" || !strings.HasPrefix(parts[1], "vid_1532&pid_") || strings.Contains(parts[1], "&mi_") {
		return 0, false
	}
	if len(parts[1]) != 17 || parts[2] == "" {
		return 0, false
	}
	p, err := strconv.ParseUint(parts[1][13:], 16, 16)
	return uint16(p), err == nil
}
