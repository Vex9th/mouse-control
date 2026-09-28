package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

type dpiValue struct{ X, Y int }

func mouseRequest(tid, class, command byte, args ...byte) [91]byte {
	var b [91]byte
	b[2], b[6], b[7], b[8] = tid, byte(len(args)), class, command
	copy(b[9:89], args)
	for i := 3; i <= 88; i++ {
		b[89] ^= b[i]
	}
	return b
}

func mouseResponse(b, req [91]byte, minSize byte) error {
	if b[0] != 0 || b[3] != 0 || b[4] != 0 || b[5] != 0 || b[7] != req[7] || b[8] != req[8] {
		return errFrame
	}
	var crc byte
	for i := 3; i <= 88; i++ {
		crc ^= b[i]
	}
	if crc != b[89] {
		return fmt.Errorf("%w：校验失败", errFrame)
	}
	switch b[1] {
	case 1, 2: // 与上游一致；写操作必须另行读回核对。
	case 3:
		return errRejected
	case 4:
		return errAsleep
	case 5:
		return errors.New("该设备或当前连接方式不支持此功能")
	default:
		return fmt.Errorf("设备返回未知状态 0x%02X", b[1])
	}
	if b[6] < minSize || b[6] > 80 {
		return errFrame
	}
	return nil
}

func exchangeMouse(c featureChannel, info modelInfo, req [91]byte, minSize byte, readOnly bool) ([91]byte, error) {
	wait := time.Duration(info.WaitMillis) * time.Millisecond
	if wait < 35*time.Millisecond {
		wait = 35 * time.Millisecond
	}
	attempts := 1
	if readOnly {
		attempts = 6
	}
	var reply [91]byte
	var err error
	for i := 0; i < attempts; i++ {
		reply, err = c.Exchange(req, wait)
		if err != nil {
			return reply, err
		}
		err = mouseResponse(reply, req, minSize)
		if err == nil || (!errors.Is(err, errAsleep) && !errors.Is(err, errFrame)) {
			break
		}
	}
	return reply, err
}

func mouseChannelError(d device) error {
	if d.AccessError != nil {
		return d.AccessError
	}
	return errors.New("未找到可用的 HID 控制通道，请检查 USB 连接或无线接收器")
}

func validateDPI(cap mouseCapability, v dpiValue) error {
	if cap.MaxDPI == 0 {
		return errors.New("该型号暂未适配 DPI 读写")
	}
	if v.X < 100 || v.Y < 100 || v.X > cap.MaxDPI || v.Y > cap.MaxDPI {
		return fmt.Errorf("此型号的 X、Y DPI 均须为 100–%d 之间的整数", cap.MaxDPI)
	}
	return nil
}

func readDPIChannel(d device, c featureChannel) (dpiValue, error) {
	cap := mouseCapabilities[d.PID]
	if cap.MaxDPI == 0 {
		return dpiValue{}, errors.New("该型号暂未适配 DPI 读写")
	}
	storage := byte(0)
	if cap.DPILegacyRead {
		storage = 1
	}
	req := mouseRequest(cap.DPITID, 4, 0x85, storage, 0, 0, 0, 0, 0, 0)
	b, err := exchangeMouse(c, d.Info, req, 5, true)
	if err != nil {
		return dpiValue{}, err
	}
	v := dpiValue{int(binary.BigEndian.Uint16(b[10:12])), int(binary.BigEndian.Uint16(b[12:14]))}
	if err := validateDPI(cap, v); err != nil {
		return dpiValue{}, fmt.Errorf("DPI 响应超出已适配范围（X=%d，Y=%d）：%w", v.X, v.Y, err)
	}
	return v, nil
}

func readMouseDPI(d device) (dpiValue, featureChannel, error) {
	if d.Backend != nil {
		v, e := d.Backend.ReadDPI()
		return v, nil, e
	}
	if deviceVID(d) != 0x1532 {
		return dpiValue{}, nil, errors.New("此设备尚无 DPI 接口")
	}
	if mouseCapabilities[d.PID].MaxDPI == 0 {
		return dpiValue{}, nil, errors.New("该型号暂未适配 DPI 读写")
	}
	last := mouseChannelError(d)
	for _, c := range d.Channels {
		v, err := readDPIChannel(d, c)
		if err == nil {
			return v, c, nil
		}
		last = err
	}
	return dpiValue{}, nil, last
}

func setMouseDPI(d device, want dpiValue) (dpiValue, bool, error) {
	if d.Backend != nil {
		return d.Backend.SetDPI(want)
	}
	if deviceVID(d) != 0x1532 {
		return dpiValue{}, false, errors.New("此设备尚无 DPI 设置接口")
	}
	cap := mouseCapabilities[d.PID]
	if err := validateDPI(cap, want); err != nil {
		return dpiValue{}, false, err
	}
	before, c, err := readMouseDPI(d)
	if err != nil {
		return dpiValue{}, false, fmt.Errorf("读取当前 DPI 失败，未发送设置：%w", err)
	}
	if before == want {
		return before, false, nil
	}
	// 上游 set_dpi_xy 实际固定使用 VARSTORE(1)，不能假定这是易失性设置。
	req := mouseRequest(cap.DPITID, 4, 5, 1, byte(want.X>>8), byte(want.X), byte(want.Y>>8), byte(want.Y), 0, 0)
	if _, err = exchangeMouse(c, d.Info, req, 0, false); err != nil {
		return dpiValue{}, true, fmt.Errorf("DPI 设置未能确认，当前设置可能已改变；请刷新核对：%w", err)
	}
	actual, err := readDPIChannel(d, c)
	if err != nil {
		return dpiValue{}, true, fmt.Errorf("设置后无法读回 DPI，当前设置可能已改变；请刷新核对：%w", err)
	}
	if actual != want {
		return actual, true, fmt.Errorf("DPI 读回核对不一致：请求 X=%d / Y=%d，实际 X=%d / Y=%d", want.X, want.Y, actual.X, actual.Y)
	}
	return actual, true, nil
}

func rateCode(v int, v2 bool) (byte, bool) {
	if v2 {
		c, ok := map[int]byte{125: 0x40, 250: 0x20, 500: 0x10, 1000: 8, 2000: 4, 4000: 2, 8000: 1}[v]
		return c, ok
	}
	c, ok := map[int]byte{125: 8, 500: 2, 1000: 1}[v]
	return c, ok
}

func readRateChannel(d device, c featureChannel) (int, error) {
	cap := mouseCapabilities[d.PID]
	if len(cap.PollRates) == 0 {
		return 0, errors.New("该型号暂未适配回报率读写")
	}
	cmd, minSize := byte(0x85), byte(1)
	if cap.PollV2 {
		// 新协议沿用请求size=1，实际值位于arg1；00BF真机帧已验证。
		// 完整91字节、CRC和频率编码仍必须有效，不能仅依赖size字段。
		cmd = 0xc0
	}
	req := mouseRequest(cap.PollTID, 0, cmd, 0)
	b, err := exchangeMouse(c, d.Info, req, minSize, true)
	if err != nil {
		return 0, err
	}
	code := b[9]
	if cap.PollV2 {
		code = b[10]
	}
	for _, v := range cap.PollRates {
		if known, ok := rateCode(v, cap.PollV2); ok && known == code {
			return v, nil
		}
	}
	return 0, fmt.Errorf("回报率响应超出已适配档位（代码 0x%02X）", code)
}

func readMouseRate(d device) (int, featureChannel, error) {
	if d.Backend != nil {
		v, e := d.Backend.ReadRate()
		return v, nil, e
	}
	if deviceVID(d) != 0x1532 {
		return 0, nil, errors.New("此设备尚无回报率接口")
	}
	if len(mouseCapabilities[d.PID].PollRates) == 0 {
		return 0, nil, errors.New("该型号暂未适配回报率读写")
	}
	last := mouseChannelError(d)
	for _, c := range d.Channels {
		v, err := readRateChannel(d, c)
		if err == nil {
			return v, c, nil
		}
		last = err
	}
	return 0, nil, last
}

func setMouseRate(d device, want int) (int, bool, error) {
	if d.Backend != nil {
		return d.Backend.SetRate(want)
	}
	if deviceVID(d) != 0x1532 {
		return 0, false, errors.New("此设备尚无回报率设置接口")
	}
	cap := mouseCapabilities[d.PID]
	allowed := false
	for _, v := range cap.PollRates {
		if v == want {
			allowed = true
		}
	}
	code, encodable := rateCode(want, cap.PollV2)
	if !allowed || !encodable {
		return 0, false, fmt.Errorf("此型号未适配 %d Hz 回报率，请查看设备详情中的可选档位", want)
	}
	before, c, err := readMouseRate(d)
	if err != nil {
		return 0, false, fmt.Errorf("读取当前回报率失败，未发送设置：%w", err)
	}
	if before == want {
		return before, false, nil
	}
	req := mouseRequest(cap.PollWriteTID, 0, 5, code)
	if cap.PollV2 {
		req = mouseRequest(cap.PollWriteTID, 0, 0x40, 0, code)
	}
	if _, err = exchangeMouse(c, d.Info, req, 0, false); err != nil {
		return 0, true, fmt.Errorf("回报率设置未能确认，当前设置可能已改变；请刷新核对：%w", err)
	}
	if cap.PollDualWrite {
		req = mouseRequest(cap.PollSecondTID, 0, 0x40, 1, code)
		if _, err = exchangeMouse(c, d.Info, req, 0, false); err != nil {
			return 0, true, fmt.Errorf("回报率第二步设置未能确认，当前设置可能已改变；请刷新核对：%w", err)
		}
	}
	actual, err := readRateChannel(d, c)
	if err != nil {
		return 0, true, fmt.Errorf("设置后无法读回回报率，当前设置可能已改变；请刷新核对：%w", err)
	}
	if actual != want {
		return actual, true, fmt.Errorf("回报率读回核对不一致：请求 %d Hz，实际 %d Hz", want, actual)
	}
	return actual, true, nil
}

func readMouse(d device) deviceResult {
	r := deviceResult{Device: d}
	r.Reading, r.Err = readDevice(d)
	r.DPI, _, r.DPIErr = readMouseDPI(d)
	r.Rate, _, r.RateErr = readMouseRate(d)
	return r
}

func hasMouseCommands(pid uint16) bool {
	cap := mouseCapabilities[pid]
	return models[pid].Battery || cap.MaxDPI > 0 || len(cap.PollRates) > 0
}
