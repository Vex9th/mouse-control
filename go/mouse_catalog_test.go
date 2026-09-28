package main

import (
	"reflect"
	"testing"
)

func TestMouseDPIProtocol(t *testing.T) {
	for _, tc := range []struct {
		pid        uint16
		max        int
		tid        byte
		storedRead bool
	}{
		{0x00BF, 45000, 0x1F, false},
		{0x00B7, 35000, 0x1F, false},
		{0x007D, 20000, 0x3F, false},
		{0x007B, 20000, 0xFF, false},
		{0x002F, 6400, 0xFF, true},
		{0x0039, 6400, 0xFF, true},
	} {
		c := mouseCapabilities[tc.pid]
		if c.MaxDPI != tc.max || c.DPITID != tc.tid || c.DPILegacyRead != tc.storedRead {
			t.Errorf("PID %04X DPI 配置错误：%+v", tc.pid, c)
		}
	}
}

func TestMousePollProtocolDifferences(t *testing.T) {
	for _, tc := range []struct {
		pid                 uint16
		read, write, second byte
		v2                  bool
		rates               []int
	}{
		{0x00BF, 0x1F, 0x1F, 0x1F, true, []int{125, 500, 1000, 2000, 4000, 8000}},
		{0x00B7, 0x1F, 0x1F, 0, false, []int{125, 500, 1000}},
		{0x0091, 0x1F, 0x1F, 0xFF, true, []int{125, 500, 1000, 2000, 4000, 8000}},
		{0x00B3, 0x1F, 0x1F, 0xFF, true, []int{125, 500, 1000, 2000, 4000, 8000}},
		{0x009E, 0x1F, 0x1F, 0x1F, true, []int{125, 500, 1000}},
		{0x0044, 0xFF, 0x3F, 0, false, []int{125, 500, 1000}},
		{0x0086, 0xFF, 0x1F, 0, false, []int{125, 500, 1000}},
	} {
		c := mouseCapabilities[tc.pid]
		if c.PollTID != tc.read || c.PollWriteTID != tc.write || c.PollSecondTID != tc.second || c.PollV2 != tc.v2 || c.PollDualWrite != tc.v2 || !reflect.DeepEqual(c.PollRates, tc.rates) {
			t.Errorf("PID %04X 回报率协议错误：%+v", tc.pid, c)
		}
	}
}

func TestMouseUnsafeProtocolsAreDisabled(t *testing.T) {
	// Byte DPI、古老控制报文、缓存模拟值不能进入 04/85、04/05 通道。
	for _, pid := range []uint16{0x0013, 0x0015, 0x0016, 0x001F, 0x0020, 0x0029, 0x002E, 0x0036, 0x0037, 0x0038, 0x0041, 0x0042} {
		c, ok := mouseCapabilities[pid]
		if !ok || c.MaxDPI != 0 || c.DPITID != 0 || c.DPILegacyRead {
			t.Errorf("PID %04X 不应启用现代 DPI：%+v", pid, c)
		}
	}
	// 前五个没有标准真实读写链路；后四个上游宣称250Hz但编码函数会改成500Hz。
	for _, pid := range []uint16{0x0013, 0x0016, 0x0029, 0x0046, 0x004C, 0x00C7, 0x00C8, 0x00D0, 0x00D1} {
		c := mouseCapabilities[pid]
		if c.PollRates != nil || c.PollTID != 0 || c.PollWriteTID != 0 || c.PollSecondTID != 0 || c.PollV2 || c.PollDualWrite {
			t.Errorf("PID %04X 不应启用回报率命令：%+v", pid, c)
		}
	}
}

func TestMouseRecognitionAndCapabilityBounds(t *testing.T) {
	for _, pid := range []uint16{0x0013, 0x0042, 0x00BF, 0x00B3} {
		if !isMousePID(pid) {
			t.Errorf("PID %04X 应保留鼠标或鼠标接收器识别", pid)
		}
	}
	for _, pid := range []uint16{0x0203, 0x0504, 0x0F08, 0xFFFF} {
		if isMousePID(pid) {
			t.Errorf("PID %04X 不能当作鼠标", pid)
		}
	}
	for pid, c := range mouseCapabilities {
		if c.MaxDPI > 0 && (c.MaxDPI < 100 || c.MaxDPI > 45000 || c.DPITID == 0) {
			t.Errorf("PID %04X DPI 范围或 TID 无效：%+v", pid, c)
		}
		if len(c.PollRates) > 0 && (c.PollTID == 0 || c.PollWriteTID == 0) {
			t.Errorf("PID %04X 回报率 TID 缺失：%+v", pid, c)
		}
		for _, rate := range c.PollRates {
			valid := rate == 125 || rate == 500 || rate == 1000 || c.PollV2 && (rate == 2000 || rate == 4000 || rate == 8000)
			if !valid {
				t.Errorf("PID %04X 的 %d Hz 无对应的编码依据", pid, rate)
			}
		}
	}
}
