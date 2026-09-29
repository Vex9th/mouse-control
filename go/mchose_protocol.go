package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

type mchoseTransport interface {
	Exchange([]byte, func([]byte) bool, time.Duration) ([]byte, error)
	Close() error
}

const mchoseReadOnlyReason = "迈从实验性只读支持：写入确认与设备状态语义尚未完成验证"

// 只允许三条读取命令；不能通过这个入口发送设置、配对或固件指令。
func mchoseReadRequest(command uint16, payload []byte) ([]byte, error) {
	switch command {
	case 0x0900, 0x0002:
		if len(payload) != 0 {
			return nil, errors.New("迈从读取命令不接受参数")
		}
	case 0x0003:
		if len(payload) != 2 || payload[0] > 2 || payload[1] > 1 {
			return nil, errors.New("迈从 DPI 查询参数无效")
		}
	default:
		return nil, fmt.Errorf("禁止发送未适配的迈从命令 %04X", command)
	}
	b := make([]byte, 64)
	b[0], b[1], b[2], b[3] = 0x4d, 1, 1, byte(len(payload))
	binary.LittleEndian.PutUint16(b[4:6], command)
	copy(b[8:], payload)
	for _, v := range b[2 : 8+len(payload)] {
		b[8+len(payload)] ^= v
	}
	return b, nil
}
func mchoseReplyMatches(command uint16, b []byte) bool {
	return len(b) >= 6 && b[0] == 0x4d && binary.LittleEndian.Uint16(b[4:6]) == command
}
func decodeMchoseReply(command uint16, b []byte) ([]byte, error) {
	if len(b) != 64 || !mchoseReplyMatches(command, b) {
		return nil, fmt.Errorf("%w：迈从报告长度、ID 或命令不符", errFrame)
	}
	// 官网接收路径未说明其他标志的含义；实验版只接受与公开编码器一致的完整包。
	if b[1] != 1 || b[2] != 1 || b[6] != 0 || b[7] != 0 {
		return nil, fmt.Errorf("%w：迈从回复头 %02X/%02X/%02X/%02X 尚未适配", errFrame, b[1], b[2], b[6], b[7])
	}
	n := int(b[3])
	if n > 55 {
		return nil, fmt.Errorf("%w：迈从载荷长度越界", errFrame)
	}
	var checksum byte
	for _, v := range b[2 : 8+n] {
		checksum ^= v
	}
	if checksum != b[8+n] {
		return nil, fmt.Errorf("%w：迈从 XOR 校验失败", errFrame)
	}
	p := b[8 : 8+n]
	if len(p) > 0 && p[0] == 0xff {
		return nil, errors.New("迈从设备返回忙或数据未就绪，请稍后重新读取")
	}
	return p, nil
}
func mchoseQuery(t mchoseTransport, command uint16, args []byte) ([]byte, error) {
	q, e := mchoseReadRequest(command, args)
	if e != nil {
		return nil, e
	}
	r, e := t.Exchange(q, func(b []byte) bool { return mchoseReplyMatches(command, b) }, 3*time.Second)
	if e != nil {
		return nil, fmt.Errorf("迈从命令 %04X：%w", command, e)
	}
	return decodeMchoseReply(command, r)
}

type mchoseMouse struct {
	Name                string
	mu                  sync.Mutex
	transport           mchoseTransport
	usbPID, reportedPID uint16
	model               mchoseModel
	closed              bool
}

func probeMchose(t mchoseTransport, usbPID uint16) (*mchoseMouse, error) {
	if t == nil || !mchoseUSBSupported(mchoseVID, usbPID) {
		return nil, errors.New("未收录此迈从协议型号")
	}
	m := &mchoseMouse{transport: t, usbPID: usbPID}
	p, e := m.readIdentity()
	if e != nil {
		return nil, e
	}
	m.reportedPID = binary.LittleEndian.Uint16(p[2:4])
	m.model = mchoseModels[m.reportedPID]
	m.Name = m.model.Name
	return m, nil
}
func (m *mchoseMouse) readIdentity() ([]byte, error) {
	if m.closed {
		return nil, errors.New("迈从通道已关闭")
	}
	p, e := mchoseQuery(m.transport, 0x0900, nil)
	if e != nil {
		return nil, e
	}
	if len(p) < 14 {
		return nil, fmt.Errorf("%w：迈从设备信息不足 14 字节", errFrame)
	}
	vid, pid := binary.LittleEndian.Uint16(p[:2]), binary.LittleEndian.Uint16(p[2:4])
	if vid != mchoseVID || !mchoseIdentityMatches(m.usbPID, pid) || (m.reportedPID != 0 && m.reportedPID != pid) {
		return nil, fmt.Errorf("迈从设备身份不符：USB PID %04X，响应 %04X:%04X；请重新扫描", m.usbPID, vid, pid)
	}
	if p[4] == 0 || p[4] > 3 {
		return nil, fmt.Errorf("%w：迈从配置数量 %d 尚未适配", errFrame, p[4])
	}
	return p, nil
}
func (m *mchoseMouse) Features() mouseFeatures {
	m.mu.Lock()
	defer m.mu.Unlock()
	return mouseFeatures{Battery: true, SeparateAxes: true, DPIRanges: []dpiRange{{1, m.model.MaxDPI, 1}}, PollRates: mchoseModelRates(m.model), DPIReadOnly: true, RateReadOnly: true, DPIReason: mchoseReadOnlyReason, RateReason: mchoseReadOnlyReason, Notes: []string{"迈从新协议实验性只读支持；尚无真机验证", "接收器响应仅能核对型号，不能区分同型号的重新配对设备", "接收器连接状态字段含义和读数新鲜度尚未完成实物验证"}}
}
func (m *mchoseMouse) ReadBattery() (reading, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, e := m.readIdentity()
	if e != nil {
		return reading{}, e
	}
	if p[12] > 100 {
		return reading{}, fmt.Errorf("%w：迈从电量值 %d 不在 0–100", errFrame, p[12])
	}
	r := reading{Percent: int(p[12])}
	switch p[11] {
	case 0:
		r.ChargeKnown = true
	case 1:
		r.ChargeKnown = true
		r.Charging = true
	default:
		r.ChargeError = fmt.Errorf("未知迈从充电状态 0x%02X", p[11])
	}
	return r, nil
}

type mchosePerformance struct{ profile, index, rateCode byte }

func (m *mchoseMouse) readPerformance() (mchosePerformance, error) {
	p, e := mchoseQuery(m.transport, 2, nil)
	if e != nil {
		return mchosePerformance{}, e
	}
	if len(p) < 9 || p[0] > 2 || p[1]&15 > 5 {
		return mchosePerformance{}, fmt.Errorf("%w：迈从性能字段无效", errFrame)
	}
	return mchosePerformance{p[0], p[1] & 15, p[1] >> 4}, nil
}
func (m *mchoseMouse) readAxis(perf mchosePerformance, direction byte) (int, bool, error) {
	p, e := mchoseQuery(m.transport, 3, []byte{perf.profile, direction})
	if e != nil {
		return 0, false, e
	}
	if len(p) < 17 || p[0] != perf.profile || p[1] != direction || p[2] < 1 || p[2] > 6 || p[3] != perf.index || p[3] >= p[2] || p[4] > 1 {
		return 0, false, fmt.Errorf("%w：迈从 DPI 档案、方向或索引不符", errFrame)
	}
	value := int(binary.LittleEndian.Uint16(p[5+2*int(p[3]):]))
	if value < 1 || value > m.model.MaxDPI {
		return 0, false, fmt.Errorf("%w：迈从 DPI %d 超出已知型号范围", errFrame, value)
	}
	return value, p[4] == 1, nil
}
func (m *mchoseMouse) ReadDPI() (dpiValue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, e := m.readIdentity(); e != nil {
		return dpiValue{}, e
	}
	perf, e := m.readPerformance()
	if e != nil {
		return dpiValue{}, e
	}
	x, separate, e := m.readAxis(perf, 0)
	if e != nil {
		return dpiValue{}, e
	}
	y := x
	if separate {
		var hasY bool
		y, hasY, e = m.readAxis(perf, 1)
		if e != nil {
			return dpiValue{}, e
		}
		if !hasY {
			return dpiValue{}, fmt.Errorf("%w：迈从读取期间双轴设置发生变化", errFrame)
		}
	}
	end, e := m.readPerformance()
	if e != nil {
		return dpiValue{}, e
	}
	if end != perf {
		return dpiValue{}, errors.New("迈从读取期间当前档案或 DPI 档位发生变化，请重读")
	}
	if _, e = m.readIdentity(); e != nil {
		return dpiValue{}, e
	}
	return dpiValue{X: x, Y: y}, nil
}
func (m *mchoseMouse) ReadRate() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, e := m.readIdentity(); e != nil {
		return 0, e
	}
	p, e := m.readPerformance()
	if e != nil {
		return 0, e
	}
	// 官网写编码跳过 1；不要照抄 UI 将异常代码夹到最近档位的行为。
	rates := map[byte]int{0: 125, 2: 500, 3: 1000, 4: 2000, 5: 4000, 6: 8000}
	rate, ok := rates[p.rateCode]
	if !ok || (m.model.Receiver1K && rate > 1000) {
		return 0, fmt.Errorf("%w：未知或不适用的迈从回报率代码 %d", errFrame, p.rateCode)
	}
	if _, e = m.readIdentity(); e != nil {
		return 0, e
	}
	return rate, nil
}
func (m *mchoseMouse) SetDPI(dpiValue) (dpiValue, bool, error) {
	return dpiValue{}, false, errors.New(mchoseReadOnlyReason)
}
func (m *mchoseMouse) SetRate(int) (int, bool, error) {
	return 0, false, errors.New(mchoseReadOnlyReason)
}
func (m *mchoseMouse) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	return m.transport.Close()
}

func mchoseWireRequest(q []byte) bool {
	if len(q) != 64 || q[3] > 55 {
		return false
	}
	expected, e := mchoseReadRequest(binary.LittleEndian.Uint16(q[4:6]), q[8:8+int(q[3])])
	return e == nil && bytes.Equal(q, expected)
}
