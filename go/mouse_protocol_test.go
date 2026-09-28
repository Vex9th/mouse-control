package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

type capturedMouseChannel struct{ reply [91]byte }

func (c *capturedMouseChannel) Close() error { return nil }
func (c *capturedMouseChannel) Exchange(_ [91]byte, _ time.Duration) ([91]byte, error) {
	return c.reply, nil
}

func TestV4CapturedPollReplyUsesSizeOneAndArgumentOne(t *testing.T) {
	// V4 Pro 无线00BF真实响应：size=1，但arg1=02表示4000Hz；CRC=C3。
	raw, err := hex.DecodeString("00021F0000000100C00002000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000C300")
	if err != nil || len(raw) != 91 {
		t.Fatalf("fixture: %d %v", len(raw), err)
	}
	c := &capturedMouseChannel{}
	copy(c.reply[:], raw)
	v, _, err := readMouseRate(simulatedMouse(0x00bf, c))
	if err != nil || v != 4000 {
		t.Fatalf("真实有效回报率被拒绝: %d %v", v, err)
	}
	c.reply[10] = 0
	c.reply[89] ^= 2
	if _, _, err = readMouseRate(simulatedMouse(0x00bf, c)); err == nil {
		t.Fatal("未知频率编码仍应拒绝")
	}
}

// 用有状态设备模拟器验证设置前读、协议写和读回，而非只断言调用次数。
type mouseSimulator struct {
	dpi                                 dpiValue
	rate                                int
	requests                            [][91]byte
	ignoreWrite, failWrite, corruptRead bool
}

func (c *mouseSimulator) Close() error { return nil }
func (c *mouseSimulator) Exchange(req [91]byte, _ time.Duration) ([91]byte, error) {
	c.requests = append(c.requests, req)
	b := req
	b[1] = 2
	switch {
	case req[7] == 4 && req[8] == 0x85:
		binary.BigEndian.PutUint16(b[10:12], uint16(c.dpi.X))
		binary.BigEndian.PutUint16(b[12:14], uint16(c.dpi.Y))
	case req[7] == 4 && req[8] == 5:
		if req[9] != 1 {
			return b, errors.New("DPI storage 应为1")
		}
		if !c.ignoreWrite {
			c.dpi = dpiValue{int(binary.BigEndian.Uint16(req[10:12])), int(binary.BigEndian.Uint16(req[12:14]))}
		}
		if c.failWrite {
			return b, errors.New("写入后连接中断")
		}
	case req[7] == 0 && req[8] == 0xc0:
		b[6] = 2
		b[10] = map[int]byte{125: 64, 500: 16, 1000: 8, 2000: 4, 4000: 2, 8000: 1}[c.rate]
	case req[7] == 0 && req[8] == 0x85:
		b[9] = map[int]byte{125: 8, 500: 2, 1000: 1}[c.rate]
	case req[7] == 0 && req[8] == 0x40:
		if !c.ignoreWrite {
			c.rate = map[byte]int{64: 125, 16: 500, 8: 1000, 4: 2000, 2: 4000, 1: 8000}[req[10]]
		}
		if c.failWrite {
			return b, errors.New("写入后连接中断")
		}
	case req[7] == 0 && req[8] == 5:
		if !c.ignoreWrite {
			c.rate = map[byte]int{8: 125, 2: 500, 1: 1000}[req[9]]
		}
	default:
		return b, errors.New("模拟器收到非预期命令")
	}
	b[89] = 0
	for i := 3; i <= 88; i++ {
		b[89] ^= b[i]
	}
	if c.corruptRead && req[8]&0x80 != 0 {
		b[89] ^= 1
	}
	return b, nil
}
func simulatedMouse(pid uint16, c featureChannel) device {
	return device{PID: pid, Info: models[pid], Channels: []featureChannel{c}}
}

func TestDPIReadWriteAndNoop(t *testing.T) {
	c := &mouseSimulator{dpi: dpiValue{800, 900}}
	d := simulatedMouse(0x00bf, c)
	actual, changed, err := setMouseDPI(d, dpiValue{1600, 1200})
	if err != nil || !changed || actual != (dpiValue{1600, 1200}) {
		t.Fatalf("%+v %t %v", actual, changed, err)
	}
	if len(c.requests) != 3 {
		t.Fatalf("必须先读再写再核对：%d", len(c.requests))
	}
	b := c.requests[1]
	if b[2] != 0x1f || b[6] != 7 || b[7] != 4 || b[8] != 5 || b[9] != 1 || b[10] != 6 || b[11] != 0x40 || b[12] != 4 || b[13] != 0xb0 || b[14] != 0 || b[15] != 0 {
		t.Fatalf("DPI写帧错误：%X", b)
	}
	c.requests = nil
	_, changed, err = setMouseDPI(d, actual)
	if err != nil || changed || len(c.requests) != 1 {
		t.Fatalf("相同值不应写入：%t %v %d", changed, err, len(c.requests))
	}
}

func TestDPIRejectsInvalidOrUnsupportedWithoutIO(t *testing.T) {
	for _, tc := range []struct {
		pid   uint16
		value dpiValue
	}{{0x00bf, dpiValue{99, 800}}, {0x00bf, dpiValue{45001, 800}}, {0x00b7, dpiValue{36000, 800}}, {0x0015, dpiValue{800, 800}}, {0xffff, dpiValue{800, 800}}} {
		c := &mouseSimulator{}
		if _, _, err := setMouseDPI(simulatedMouse(tc.pid, c), tc.value); err == nil || len(c.requests) != 0 {
			t.Fatalf("未拒绝%+v: %v", tc, err)
		}
	}
}

func TestWriteFailureIsNotRetriedOrHiddenByAnotherChannel(t *testing.T) {
	first := &mouseSimulator{dpi: dpiValue{800, 800}, failWrite: true}
	second := &mouseSimulator{dpi: dpiValue{800, 800}}
	d := simulatedMouse(0x00bf, first)
	d.Channels = append(d.Channels, second)
	_, _, err := setMouseDPI(d, dpiValue{1600, 1600})
	if err == nil || !strings.Contains(err.Error(), "可能已改变") || len(first.requests) != 2 || len(second.requests) != 0 {
		t.Fatalf("设置结果被隐藏或重试：%v %d %d", err, len(first.requests), len(second.requests))
	}
}

func TestDPIReadbackMismatchIsFailure(t *testing.T) {
	c := &mouseSimulator{dpi: dpiValue{800, 800}, ignoreWrite: true}
	actual, _, err := setMouseDPI(simulatedMouse(0x00bf, c), dpiValue{1600, 1600})
	if err == nil || actual != (dpiValue{800, 800}) || !strings.Contains(err.Error(), "核对不一致") {
		t.Fatalf("写应答不能代替读回: %+v %v", actual, err)
	}
}

func TestRateProtocolsAndAllowedValues(t *testing.T) {
	for _, tc := range []struct {
		pid          uint16
		want, frames int
	}{{0x00bf, 4000, 4}, {0x00b7, 500, 3}} {
		c := &mouseSimulator{rate: 1000}
		actual, changed, err := setMouseRate(simulatedMouse(tc.pid, c), tc.want)
		if err != nil || !changed || actual != tc.want || len(c.requests) != tc.frames {
			t.Fatalf("PID%X actual%d changed%t %v frames%d", tc.pid, actual, changed, err, len(c.requests))
		}
		if tc.pid == 0x00bf && (c.requests[1][9] != 0 || c.requests[2][9] != 1 || c.requests[1][8] != 0x40 || c.requests[2][8] != 0x40) {
			t.Fatal("V4需要两次明确写入")
		}
		c.requests = nil
		if _, changed, err := setMouseRate(simulatedMouse(tc.pid, c), tc.want); err != nil || changed || len(c.requests) != 1 {
			t.Fatal("回报率同值不应写入")
		}
	}
	for _, tc := range []struct {
		pid  uint16
		rate int
	}{{0x00bf, 250}, {0x00b7, 8000}, {0x00c7, 500}, {0xffff, 1000}} {
		c := &mouseSimulator{}
		if _, _, err := setMouseRate(simulatedMouse(tc.pid, c), tc.rate); err == nil || len(c.requests) != 0 {
			t.Fatalf("未拒绝未支持档位: %+v %v", tc, err)
		}
	}
}

func TestCorruptDPIReadPreventsWrite(t *testing.T) {
	c := &mouseSimulator{dpi: dpiValue{800, 800}, corruptRead: true}
	_, _, err := setMouseDPI(simulatedMouse(0x00bf, c), dpiValue{1600, 1600})
	if err == nil || len(c.requests) > 6 {
		t.Fatalf("损坏帧未受限: %v %d", err, len(c.requests))
	}
	for _, r := range c.requests {
		if r[8] == 5 {
			t.Fatal("无法读取当前值仍发送设置")
		}
	}
}
