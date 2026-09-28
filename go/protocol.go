package main

import (
	"errors"
	"fmt"
	"time"
)

var (
	errRejected    = errors.New("设备拒绝了指令")
	errAsleep      = errors.New("设备未应答，可能休眠、关机或超出接收范围；请移动鼠标或检查电源")
	errUnsupported = errors.New("设备未提供已适配的电量接口")
	errFrame       = errors.New("设备响应格式不匹配")
)

type featureChannel interface {
	Exchange([91]byte, time.Duration) ([91]byte, error)
	Close() error
}

type reading struct {
	Percent        int
	PercentUnknown bool
	Level          string
	VoltageMV      int
	ChargeText     string
	ChargeKnown    bool
	Charging       bool
	ChargeError    error
}

type device struct {
	ID          string
	VID         uint16
	PID         uint16
	Info        modelInfo
	Channels    []featureChannel
	AccessError error
	Backend     mouseBackend
	Caps        *mouseFeatures
}

type deviceResult struct {
	Device  device
	Reading reading
	Err     error
	DPI     dpiValue
	DPIErr  error
	Rate    int
	RateErr error
}

func makeRequest(tid, command byte) [91]byte {
	var b [91]byte
	b[2], b[6], b[7], b[8] = tid, 2, 7, command
	for i := 3; i <= 88; i++ {
		b[89] ^= b[i]
	}
	return b
}

func decodeResponse(b [91]byte, command byte) (byte, error) {
	if b[0] != 0 || b[3] != 0 || b[4] != 0 || b[7] != 7 || b[8] != command {
		return 0, errFrame
	}
	var crc byte
	for i := 3; i <= 88; i++ {
		crc ^= b[i]
	}
	if crc != b[89] {
		return 0, fmt.Errorf("%w：校验失败", errFrame)
	}
	switch b[1] {
	case 3:
		return 0, errRejected
	case 4:
		return 0, errAsleep
	case 5:
		return 0, errUnsupported
	case 1, 2: // OpenRazer 明确认可部分带有效载荷的 Busy 响应。
	default:
		return 0, fmt.Errorf("设备返回未知状态 0x%02X", b[1])
	}
	if b[6] < 2 || b[6] > 80 {
		return 0, errFrame
	}
	return b[10], nil
}

// OpenRazer 的 charge_level 使用固定 0..255 刻度，不能按数值大小切换单位。
func batteryPercent(raw byte) int { return (int(raw)*100 + 127) / 255 }

func query(c featureChannel, info modelInfo, command byte) (byte, error) {
	wait := time.Duration(info.WaitMillis) * time.Millisecond
	if wait < 35*time.Millisecond {
		wait = 35 * time.Millisecond
	}
	var last error
	for attempt := 0; attempt < 6; attempt++ {
		b, err := c.Exchange(makeRequest(info.TID, command), wait)
		if err != nil {
			return 0, err
		}
		value, err := decodeResponse(b, command)
		if err == nil {
			return value, nil
		}
		last = err
		if !errors.Is(err, errAsleep) && !errors.Is(err, errFrame) {
			break
		}
	}
	return 0, last
}

func readDevice(d device) (reading, error) {
	if d.Backend != nil {
		return d.Backend.ReadBattery()
	}
	if deviceVID(d) != 0x1532 {
		return reading{}, errUnsupported
	}
	if !d.Info.Battery {
		return reading{}, errUnsupported
	}
	if len(d.Channels) == 0 {
		if d.AccessError != nil {
			return reading{}, d.AccessError
		}
		return reading{}, errors.New("未找到可用的 HID 电量通道，请确认使用 USB 连接或无线接收器")
	}
	var last error
	for _, c := range d.Channels {
		raw, err := query(c, d.Info, 0x80)
		if err != nil {
			last = err
			continue
		}
		r := reading{Percent: batteryPercent(raw)}
		if d.Info.NoCharge {
			r.ChargeKnown = true
			return r, nil
		}
		charging, err := query(c, d.Info, 0x84)
		if err != nil {
			r.ChargeError = err
			return r, nil
		}
		if charging > 1 {
			r.ChargeError = fmt.Errorf("充电响应数值无效：%d", charging)
			return r, nil
		}
		r.ChargeKnown = true
		r.Charging = charging == 1
		return r, nil
	}
	return reading{}, last
}

func closeDevices(devices []device) {
	for _, d := range devices {
		if d.Backend != nil {
			d.Backend.Close()
		}
		for _, c := range d.Channels {
			c.Close()
		}
	}
}
