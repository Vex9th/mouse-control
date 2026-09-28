package main

import (
	"errors"
	"time"
)

type hidppCollectionSpec struct {
	InputLength, OutputLength int
	OutputIDs                 map[byte]bool
}

func hidppUsage(page, usage uint16, bluetooth bool) bool {
	return page == 0xff00 || (bluetooth && page == 0xff43 && usage == 0x0202)
}

func hidppReportSize(id byte) int {
	switch id {
	case 0x10:
		return 7
	case 0x11:
		return 20
	case 0x12:
		return 64
	}
	return 0
}

func hidppCanonical(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	n := hidppReportSize(b[0])
	if n == 0 || len(b) < n {
		return nil
	}
	return b[:n]
}

func hidppReply(b []byte, address byte, bluetooth bool) []byte {
	b = hidppCanonical(b)
	// 部分蓝牙设备对 FF 直连地址以 00 应答（Solaar base.py）。
	// 仅在系统拓扑已确认蓝牙时适配，USB 接收器仍严格匹配槽位。
	if b != nil && bluetooth && address == 0xff && b[1] == 0 {
		b = append([]byte(nil), b...)
		b[1] = 0xff
	}
	return b
}

func hidppOutput(req []byte, channels []hidppCollectionSpec) (int, []byte, error) {
	if len(req) == 0 || hidppReportSize(req[0]) != len(req) {
		return -1, nil, errors.New("HID++ 请求长度或报告类型无效")
	}
	for _, id := range []byte{req[0], 0x11, 0x12} {
		if hidppReportSize(id) < len(req) {
			continue
		}
		for i, c := range channels {
			if c.OutputIDs[id] && c.OutputLength >= hidppReportSize(id) && c.OutputLength <= 4096 {
				b := make([]byte, c.OutputLength)
				copy(b, req)
				b[0] = id
				return i, b, nil
			}
		}
	}
	return -1, nil, errors.New("设备没有可用的 HID++ 输出报告通道")
}

func deadlineMillis(deadline time.Time) uint32 {
	d := time.Until(deadline)
	if d <= 0 {
		return 0
	}
	ms := (d + time.Millisecond - 1) / time.Millisecond
	if ms > 0xfffffffe {
		return 0xfffffffe
	}
	return uint32(ms)
}
