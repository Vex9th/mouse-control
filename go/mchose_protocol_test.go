package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// 只模拟公开报文边界，不调用任何 HID API。
type mchoseSimulator struct {
	mu                       sync.Mutex
	pid                      uint16
	battery, charge          byte
	maxConfigNum             byte
	profile, index, rateCode byte
	separate                 bool
	x, y                     uint16
	calls                    [][]byte
	closed                   int
	failure                  error
	mutate                   func([]byte) []byte
	changePIDAfter           int
}

func newMchoseSimulator() *mchoseSimulator {
	return &mchoseSimulator{pid: 0x4035, battery: 76, charge: 1, maxConfigNum: 3, profile: 1, index: 2, rateCode: 6, separate: true, x: 1600, y: 1800}
}
func mchoseFixture(command uint16, data []byte) []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x4d, 1, 1, byte(len(data)), byte(command), byte(command >> 8), 0, 0})
	copy(b[8:], data)
	for _, v := range b[2 : 8+len(data)] {
		b[8+len(data)] ^= v
	}
	return b
}
func (s *mchoseSimulator) Exchange(q []byte, match func([]byte) bool, timeout time.Duration) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]byte(nil), q...))
	if s.closed > 0 {
		return nil, errors.New("模拟通道已关闭")
	}
	if s.failure != nil {
		return nil, s.failure
	}
	if timeout <= 0 || timeout > 3*time.Second {
		return nil, fmt.Errorf("错误的超时 %v", timeout)
	}
	if len(q) != 64 || q[0] != 0x4d {
		return nil, errors.New("无效请求")
	}
	cmd := binary.LittleEndian.Uint16(q[4:6])
	var p []byte
	switch cmd {
	case 0x0900:
		pid := s.pid
		if s.changePIDAfter > 0 && len(s.calls) >= s.changePIDAfter {
			pid = 0x4038
		}
		p = make([]byte, 14)
		binary.LittleEndian.PutUint16(p, 0x3837)
		binary.LittleEndian.PutUint16(p[2:], pid)
		p[4] = s.maxConfigNum
		p[10] = 1
		p[11] = s.charge
		p[12] = s.battery
	case 0x0002:
		p = []byte{s.profile, s.rateCode<<4 | s.index, s.rateCode<<4 | s.index, 3, 0, 0, 0, 1, 1}
	case 0x0003:
		if q[3] != 2 || q[8] != s.profile || q[9] > 1 {
			return nil, errors.New("DPI 查询参数不正确")
		}
		p = make([]byte, 17)
		p[0], p[1], p[2], p[3] = s.profile, q[9], 6, s.index
		if s.separate {
			p[4] = 1
		}
		for i := 0; i < 6; i++ {
			value := s.x
			if q[9] == 1 {
				value = s.y
			}
			binary.LittleEndian.PutUint16(p[5+2*i:], value)
		}
	default:
		return nil, fmt.Errorf("禁止发送非只读指令 %04X", cmd)
	}
	r := mchoseFixture(cmd, p)
	if s.mutate != nil {
		r = s.mutate(r)
	}
	if match(mchoseFixture(0x0600, []byte{1, 2})) {
		return nil, errors.New("异步通知被误认响应")
	}
	if s.mutate == nil && !match(r) {
		return nil, errors.New("正确回复被拒绝")
	}
	return r, nil
}
func (s *mchoseSimulator) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed++; return nil }

func TestMchoseReadRequestsAreExactAndBounded(t *testing.T) {
	q, err := mchoseReadRequest(0x0900, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 64)
	copy(want, []byte{0x4d, 1, 1, 0, 0, 9, 0, 0, 8})
	if !bytes.Equal(q, want) {
		t.Fatalf("设备信息报文 %x", q)
	}
	q, err = mchoseReadRequest(3, []byte{2, 1})
	if err != nil {
		t.Fatal(err)
	}
	want = make([]byte, 64)
	copy(want, []byte{0x4d, 1, 1, 2, 3, 0, 0, 0, 2, 1, 3})
	if !bytes.Equal(q, want) {
		t.Fatalf("DPI 报文 %x", q)
	}
	for _, cmd := range []uint16{0x0102, 0x0104, 0x090b, 0x09f0, 0x0906} {
		if _, e := mchoseReadRequest(cmd, nil); e == nil {
			t.Fatalf("允许写命令 %04X", cmd)
		}
	}
	if _, err := mchoseReadRequest(3, []byte{0, 2}); err == nil {
		t.Fatal("接受未知 DPI 方向")
	}
}
func TestMchoseCatalogDoesNotProbeBroadVID(t *testing.T) {
	if len(mchoseModels) != 14 {
		t.Fatalf("型号数 %d", len(mchoseModels))
	}
	for _, pid := range []uint16{0x4026, 0x4027, 0x4028, 0x4029, 0x402a, 0x4030, 0x4031, 0x4032, 0x4033, 0x4034, 0x4035, 0x4036, 0x4037, 0x4038, 0x1014, 0x1016, 0x1018} {
		if !mchoseUSBSupported(0x3837, pid) {
			t.Fatalf("遗漏 %04X", pid)
		}
	}
	for _, pair := range [][2]uint16{{0x3837, 0x4010}, {0x5253, 0x4035}, {0x046d, 0x1014}, {0x3837, 0x100a}} {
		if mchoseUSBSupported(pair[0], pair[1]) {
			t.Fatalf("扩大白名单 %x", pair)
		}
	}
	s := newMchoseSimulator()
	if _, e := probeMchose(s, 0x4010); e == nil || len(s.calls) > 0 {
		t.Fatal("未知 PID 被探测")
	}
}
func TestMchoseReadBatteryDPIAndRate(t *testing.T) {
	s := newMchoseSimulator()
	m, e := probeMchose(s, 0x1014)
	if e != nil {
		t.Fatal(e)
	}
	if m.Name != "MCHOSE A5 V3 Pro" {
		t.Fatal(m.Name)
	}
	r, e := m.ReadBattery()
	if e != nil || r.Percent != 76 || !r.ChargeKnown || !r.Charging {
		t.Fatalf("电量 %+v %v", r, e)
	}
	dpi, e := m.ReadDPI()
	if e != nil || dpi != (dpiValue{X: 1600, Y: 1800}) {
		t.Fatalf("DPI %+v %v", dpi, e)
	}
	rate, e := m.ReadRate()
	if e != nil || rate != 8000 {
		t.Fatalf("回报率 %d %v", rate, e)
	}
	f := m.Features()
	if !f.Battery || !f.DPIReadOnly || !f.RateReadOnly || f.DPIReason == "" || f.RateReason == "" {
		t.Fatalf("能力 %+v", f)
	}
	before := len(s.calls)
	if _, changed, e := m.SetDPI(dpi); e == nil || changed {
		t.Fatal("只读 DPI 允许设置")
	}
	if _, changed, e := m.SetRate(8000); e == nil || changed {
		t.Fatal("只读回报率允许设置")
	}
	if len(s.calls) != before {
		t.Fatal("设置调用发送了 HID 报文")
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	m.Close()
	if s.closed != 1 {
		t.Fatalf("关闭 %d 次", s.closed)
	}
	if _, e := m.ReadBattery(); e == nil {
		t.Fatal("已关闭还允许查询")
	}
}
func TestMchoseFrameCorruptionIsNotAReading(t *testing.T) {
	tests := []struct {
		name string
		edit func([]byte) []byte
	}{
		{"short", func(b []byte) []byte { return b[:20] }}, {"report", func(b []byte) []byte { b[0] = 0x4e; return b }}, {"command", func(b []byte) []byte { b[4] = 3; return b }}, {"checksum", func(b []byte) []byte { b[22] ^= 1; return b }}, {"length", func(b []byte) []byte { b[3] = 63; return b }}, {"version", func(b []byte) []byte { b[1] = 2; return b }}, {"flags", func(b []byte) []byte { b[2] = 0; return b }}, {"sequence", func(b []byte) []byte { b[7] = 1; return b }}, {"business", func(b []byte) []byte { b[6] = 1; return b }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newMchoseSimulator()
			s.mutate = tt.edit
			if _, e := probeMchose(s, 0x4035); !errors.Is(e, errFrame) {
				t.Fatalf("损坏回复未保留错误 %v", e)
			}
		})
	}
}
func TestMchoseTimeoutRetainsCauseWithoutResending(t *testing.T) {
	s := newMchoseSimulator()
	cause := errors.New("测试超时")
	s.failure = cause
	if _, e := probeMchose(s, 0x4035); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	if len(s.calls) != 1 {
		t.Fatalf("重复请求 %d", len(s.calls))
	}
}
func TestMchoseReplyIdentityCannotCrossDevices(t *testing.T) {
	s := newMchoseSimulator()
	if _, e := probeMchose(s, 0x4038); e == nil {
		t.Fatal("直连身份错配被接受")
	}
	s = newMchoseSimulator()
	if _, e := probeMchose(s, 0x1016); e == nil {
		t.Fatal("不同接收器族身份被接受")
	}
	s = newMchoseSimulator()
	m, e := probeMchose(s, 0x1014)
	if e != nil {
		t.Fatal(e)
	}
	s.pid = 0x4038
	if _, e = m.ReadDPI(); e == nil || !strings.Contains(e.Error(), "身份") {
		t.Fatalf("重配对变化未拒绝 %v", e)
	}
	s1, s2 := newMchoseSimulator(), newMchoseSimulator()
	s2.battery = 23
	m1, _ := probeMchose(s1, 0x1014)
	m2, _ := probeMchose(s2, 0x1014)
	r1, _ := m1.ReadBattery()
	r2, _ := m2.ReadBattery()
	if r1.Percent != 76 || r2.Percent != 23 {
		t.Fatal("多设备读数串线")
	}
}
func TestMchoseDPIRejectsMidReadIdentityAndProfileChanges(t *testing.T) {
	s := newMchoseSimulator()
	m, e := probeMchose(s, 0x1014)
	if e != nil {
		t.Fatal(e)
	}
	s.changePIDAfter = 4
	if _, e := m.ReadDPI(); e == nil {
		t.Fatal("读取中身份变化仍返回 DPI")
	}
	s = newMchoseSimulator()
	m, _ = probeMchose(s, 0x1014)
	count := 0
	s.mutate = func(b []byte) []byte {
		if binary.LittleEndian.Uint16(b[4:6]) == 2 {
			count++
			if count == 2 {
				p := append([]byte(nil), b[8:17]...)
				p[0] = 2
				return mchoseFixture(2, p)
			}
		}
		return b
	}
	if _, e := m.ReadDPI(); e == nil {
		t.Fatal("读取中档案变化仍返回 DPI")
	}
}
func TestMchoseUnknownValuesNeverBecomeFabricatedReadings(t *testing.T) {
	s := newMchoseSimulator()
	m, _ := probeMchose(s, 0x1014)
	s.battery = 255
	if _, e := m.ReadBattery(); e == nil {
		t.Fatal("电量 255 被当作有效百分比")
	}
	s.battery = 0
	s.charge = 7
	r, e := m.ReadBattery()
	if e != nil || r.Percent != 0 || r.ChargeKnown || r.ChargeError == nil {
		t.Fatalf("未知状态 %+v %v", r, e)
	}
	s.rateCode = 1
	if _, e := m.ReadRate(); e == nil {
		t.Fatal("保留回报率代码被静默映射")
	}
	s.rateCode = 6
	s.index = 6
	if _, e := m.ReadDPI(); e == nil {
		t.Fatal("DPI 索引越界未拒绝")
	}
	s.index = 2
	s.x = 0
	if _, e := m.ReadDPI(); e == nil {
		t.Fatal("DPI 零被接受")
	}
}

func TestMchosePayloadBoundariesAndAxisEcho(t *testing.T) {
	cases := []struct {
		name    string
		command uint16
		edit    func([]byte) []byte
		read    string
	}{
		{"device-short", 0x0900, func(p []byte) []byte { return p[:13] }, "probe"},
		{"busy", 0x0900, func(p []byte) []byte { p[0] = 0xff; return p }, "probe"},
		{"foreign-vendor", 0x0900, func(p []byte) []byte { p[0] = 0x53; p[1] = 0x52; return p }, "probe"},
		{"performance-short", 2, func(p []byte) []byte { return p[:8] }, "dpi"},
		{"profile-echo", 3, func(p []byte) []byte { p[0] = 2; return p }, "dpi"},
		{"axis-echo", 3, func(p []byte) []byte { p[1] ^= 1; return p }, "dpi"},
		{"axis-flag", 3, func(p []byte) []byte { p[4] = 2; return p }, "dpi"},
		{"dpi-too-high", 3, func(p []byte) []byte { binary.LittleEndian.PutUint16(p[9:11], 50000); return p }, "dpi"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := newMchoseSimulator()
			var m *mchoseMouse
			if tt.read != "probe" {
				m, _ = probeMchose(s, 0x1014)
			}
			s.mutate = func(b []byte) []byte {
				if binary.LittleEndian.Uint16(b[4:6]) != tt.command {
					return b
				}
				return mchoseFixture(tt.command, tt.edit(append([]byte(nil), b[8:8+int(b[3])]...)))
			}
			var e error
			if tt.read == "probe" {
				_, e = probeMchose(s, 0x1014)
			} else {
				_, e = m.ReadDPI()
			}
			if e == nil {
				t.Fatal("错误载荷被接受")
			}
		})
	}
}
func TestMchoseOneAxisAndOneKFamily(t *testing.T) {
	s := newMchoseSimulator()
	s.pid = 0x4029
	s.separate = false
	s.rateCode = 3
	m, e := probeMchose(s, 0x1016)
	if e != nil {
		t.Fatal(e)
	}
	v, e := m.ReadDPI()
	if e != nil || v.X != 1600 || v.Y != 1600 {
		t.Fatalf("单轴 %+v %v", v, e)
	}
	r, e := m.ReadRate()
	if e != nil || r != 1000 {
		t.Fatalf("回报率 %d %v", r, e)
	}
	s.rateCode = 6
	if _, e := m.ReadRate(); e == nil {
		t.Fatal("1K 型号接受 8K")
	}
}

func TestMchoseReportedConfigCountDoesNotRejectIdentityOrBattery(t *testing.T) {
	for _, count := range []byte{0, 4, 255} {
		t.Run(fmt.Sprintf("count=%d", count), func(t *testing.T) {
			s := newMchoseSimulator()
			s.maxConfigNum = count
			m, err := probeMchose(s, 0x1018)
			if err != nil {
				t.Fatalf("数量元数据阻止了已匹配的鼠标身份：%v", err)
			}
			defer m.Close()
			value, err := m.ReadBattery()
			if err != nil || value.Percent != 76 || !value.Charging {
				t.Fatalf("电量：%+v %v", value, err)
			}
			for _, q := range s.calls {
				if binary.LittleEndian.Uint16(q[4:6]) != 0x0900 {
					t.Fatal("电量查询发送了其他命令")
				}
			}
		})
	}
}

func TestMchoseFourConfigMetadataReadsOnlyConfirmedProfileIndices(t *testing.T) {
	for _, profile := range []byte{0, 1, 2} {
		t.Run(fmt.Sprintf("profile=%d", profile), func(t *testing.T) {
			s := newMchoseSimulator()
			s.maxConfigNum = 4
			s.profile = profile
			m, err := probeMchose(s, 0x1018)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			dpi, err := m.ReadDPI()
			if err != nil || dpi != (dpiValue{1600, 1800}) {
				t.Fatalf("DPI：%+v %v", dpi, err)
			}
			rate, err := m.ReadRate()
			if err != nil || rate != 8000 {
				t.Fatalf("回报率：%d %v", rate, err)
			}
		})
	}
}

func TestMchoseUnconfirmedProfileKeepsBatteryAndNeverQueriesAnAxis(t *testing.T) {
	s := newMchoseSimulator()
	s.maxConfigNum = 4
	s.profile = 3
	m, err := probeMchose(s, 0x1018)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	value, err := m.ReadBattery()
	if err != nil || value.Percent != 76 {
		t.Fatalf("未知当前配置不应阻止电量：%+v %v", value, err)
	}
	if _, err = m.ReadDPI(); !errors.Is(err, errFrame) || !strings.Contains(err.Error(), "配置索引 3") {
		t.Fatalf("未保留当前配置原值：%v", err)
	}
	if _, err = m.ReadRate(); !errors.Is(err, errFrame) {
		t.Fatalf("未知配置的回报率被接受：%v", err)
	}
	for _, q := range s.calls {
		if binary.LittleEndian.Uint16(q[4:6]) == 3 {
			t.Fatalf("未知配置仍发送 DPI 查询：%x", q)
		}
	}
	if _, err = mchoseReadRequest(3, []byte{3, 0}); err == nil {
		t.Fatal("不应开放编号3的DPI查询")
	}
}
