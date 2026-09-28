package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
)

type hidppTransport interface {
	Exchange(request []byte, match func([]byte) bool, timeout time.Duration) ([]byte, error)
	Close() error
}

var errHIDPPFeature = errors.New("设备未提供该 HID++ 功能")

// 避开 Linux(1)、OpenRGB(7)、libratbag(8)、LGSTrayEx(A)、Solaar(B)、G HUB(D)、固件(F)。
// Software ID 只有四位，不保证两个本程序实例或迟到响应之间绝对隔离。
const hidppSoftwareID byte = 0x0C

type hidppProtocolError struct {
	Legacy bool
	Code   byte
}

func (e *hidppProtocolError) Error() string {
	version := "2"
	if e.Legacy {
		version = "1"
	}
	return fmt.Sprintf("HID++ %s 设备错误 0x%02X", version, e.Code)
}

func hidppFrameLength(id byte) int {
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

// 不强制回复 report ID 与请求相同：短请求允许由长 collection 回复。
func hidppReplyMatches(request, reply []byte) bool {
	if len(request) < 4 || len(reply) < 7 || hidppFrameLength(reply[0]) != len(reply) || reply[1] != request[1] {
		return false
	}
	if hidppIsError(reply) {
		return reply[3] == request[2] && reply[4] == request[3]
	}
	return reply[2] == request[2] && reply[3] == request[3]
}

func hidppIsError(reply []byte) bool {
	return len(reply) >= 7 && (reply[2] == 0xFF || (reply[0] == 0x10 && reply[2] == 0x8F))
}

func hidppExchange(t hidppTransport, address, index, function byte, args []byte, nonce *byte) ([]byte, error) {
	length, id := 7, byte(0x10)
	if len(args) > 3 {
		length, id = 20, 0x11
	}
	if len(args) > 16 {
		length, id = 64, 0x12
	}
	if len(args) > 60 {
		return nil, errors.New("HID++ 参数过长")
	}
	q := make([]byte, length)
	q[0], q[1], q[2], q[3] = id, address, index, function
	copy(q[4:], args)
	match := func(r []byte) bool {
		if !hidppReplyMatches(q, r) {
			return false
		}
		return nonce == nil || hidppIsError(r) || r[6] == *nonce
	}
	r, err := t.Exchange(q, match, 800*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !match(r) {
		return nil, fmt.Errorf("%w：HID++ 回复与请求不符", errFrame)
	}
	if hidppIsError(r) {
		return nil, &hidppProtocolError{Legacy: r[2] == 0x8F, Code: r[5]}
	}
	return r[4:], nil
}

type logitechMouse struct {
	Name, Kind, Identity, Protocol          string
	IsMouse                                 bool
	mu                                      sync.Mutex
	transport                               hidppTransport
	address                                 byte
	legacy, kindKnown, direct               bool
	features                                mouseFeatures
	identity                                string
	identityErr                             error
	dpiIndex, rateIndex, batteryIndex       byte
	dpiFeature, rateFeature, batteryFeature uint16
	dpiErr, rateErr, batteryErr             error
	xRanges, yRanges                        []dpiRange
	lod                                     bool
	rateCodes                               map[int]byte
	batteryCaps                             []byte
	legacyBattery                           byte
	closed                                  bool
}

func (m *logitechMouse) call(index, fn byte, args ...byte) ([]byte, error) {
	if m.closed {
		return nil, errors.New("HID++ 通道已关闭")
	}
	p, err := hidppExchange(m.transport, m.address, index, fn|hidppSoftwareID, args, nil)
	if err != nil {
		return nil, fmt.Errorf("功能索引 %02X / 函数 %02X：%w", index, fn, err)
	}
	return p, nil
}
func (m *logitechMouse) feature(id uint16) (byte, error) {
	p, err := m.call(0, 0, byte(id>>8), byte(id), 0)
	if err != nil {
		return 0, err
	}
	if p[0] == 0 {
		return 0, errHIDPPFeature
	}
	return p[0], nil
}
func requireHIDPPData(p []byte, n int) error {
	if len(p) < n {
		return fmt.Errorf("%w：HID++ 载荷只有 %d 字节，至少需要 %d", errFrame, len(p), n)
	}
	return nil
}

func probeLogitech(t hidppTransport, address byte) (*logitechMouse, error) {
	if t == nil || (address != 0xFF && (address < 1 || address > 7)) {
		return nil, errors.New("无效 HID++ 地址或传输")
	}
	m := &logitechMouse{transport: t, address: address, direct: address == 0xFF, Name: "Logitech HID++ 设备", Kind: "未知", rateCodes: make(map[int]byte)}
	var token [1]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, fmt.Errorf("生成 ping 校验值：%w", err)
	}
	p, err := hidppExchange(t, address, 0, 0x10|hidppSoftwareID, []byte{0, 0, token[0]}, &token[0])
	if err != nil {
		var pe *hidppProtocolError
		if !errors.As(err, &pe) || !pe.Legacy || pe.Code != 1 {
			return nil, fmt.Errorf("HID++ ping：%w", err)
		}
		m.legacy = true
		m.Protocol = "HID++ 1.0"
		m.dpiErr = errors.New("HID++ 1.0 暂只支持读取电量")
		m.rateErr = m.dpiErr
		m.discoverLegacyBattery()
		m.applyReasons()
		return m, nil
	}
	if p[0] < 2 {
		return nil, fmt.Errorf("未知 HID++ 协议版本 %d.%d", p[0], p[1])
	}
	m.Protocol = fmt.Sprintf("HID++ %d.%d", p[0], p[1])
	m.readNameAndKind()
	m.identity, m.identityErr = m.readIdentity()
	m.Identity = m.identity
	if m.identityErr != nil && !errors.Is(m.identityErr, errHIDPPFeature) {
		m.features.Notes = append(m.features.Notes, "唯一身份读取失败："+m.identityErr.Error())
	}
	// 明确的键盘、接收器等只返回身份，不向它们发鼠标控制功能请求。
	if !m.kindKnown || m.IsMouse {
		m.discoverBattery()
		m.discoverDPI()
		m.discoverRate()
	}
	m.applyReasons()
	return m, nil
}

func cleanLogitechText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
}
func (m *logitechMouse) readNameAndKind() {
	idx, err := m.feature(5)
	if err != nil {
		if !errors.Is(err, errHIDPPFeature) {
			m.features.Notes = append(m.features.Notes, "设备名称/类型："+err.Error())
		}
		return
	}
	if p, err := m.call(idx, 0x20); err == nil {
		m.kindKnown = true
		switch p[0] {
		case 0:
			m.Kind = "键盘"
		case 3:
			m.Kind = "鼠标"
			m.IsMouse = true
		case 5:
			m.Kind = "轨迹球"
			m.IsMouse = true
		case 7:
			m.Kind = "接收器"
		default:
			m.Kind = fmt.Sprintf("其他设备（类型 %d）", p[0])
		}
	} else {
		m.features.Notes = append(m.features.Notes, "设备类型："+err.Error())
	}
	p, err := m.call(idx, 0)
	if err != nil {
		m.features.Notes = append(m.features.Notes, "设备名称："+err.Error())
		return
	}
	length := int(p[0])
	name := make([]byte, 0, length)
	for len(name) < length {
		fragment, e := m.call(idx, 0x10, byte(len(name)))
		if e != nil {
			m.features.Notes = append(m.features.Notes, "设备名称："+e.Error())
			return
		}
		n := length - len(name)
		if n > len(fragment) {
			n = len(fragment)
		}
		name = append(name, fragment[:n]...)
	}
	if value := cleanLogitechText(string(name)); value != "" {
		m.Name = value
	}
}

func (m *logitechMouse) readIdentity() (string, error) {
	idx, err := m.feature(3)
	if err != nil {
		return "", err
	}
	p, err := m.call(idx, 0)
	if err != nil {
		return "", err
	}
	if err = requireHIDPPData(p, 13); err != nil {
		return "", err
	}
	unit := p[1:5]
	zero, ones := true, true
	for _, b := range unit {
		zero = zero && b == 0
		ones = ones && b == 0xFF
	}
	if zero || ones {
		return "", errors.New("设备 0003 未提供有效唯一 unit ID")
	}
	return "unit:" + hex.EncodeToString(unit) + "/model:" + hex.EncodeToString(p[7:13]), nil
}

func (m *logitechMouse) SetReceiverIdentity(name, kind, identity string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Name == "Logitech HID++ 设备" && name != "" {
		m.Name = cleanLogitechText(name)
	}
	if !m.kindKnown && kind != "" {
		m.Kind = cleanLogitechText(kind)
		m.IsMouse = kind == "鼠标" || kind == "轨迹球"
		m.kindKnown = true
	}
	if m.Identity == "" {
		m.Identity = cleanLogitechText(identity)
	}
	m.applyReasons()
}

// 少数有线设备以地址 1 工作；此标记由物理 USB 枚举确定，不能按名称推断。
func (m *logitechMouse) SetDirectConnection() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.direct = true
	m.applyReasons()
}
func (m *logitechMouse) applyReasons() {
	m.features.Battery = m.batteryFeature != 0 || m.legacyBattery != 0
	m.features.DiscoveryFailed = m.batteryErr != nil && !errors.Is(m.batteryErr, errHIDPPFeature)
	if !m.legacy {
		for _, err := range []error{m.dpiErr, m.rateErr} {
			if err != nil && !errors.Is(err, errHIDPPFeature) {
				m.features.DiscoveryFailed = true
			}
		}
	}
	m.features.BatteryReason = ""
	if m.batteryErr != nil {
		m.features.BatteryReason = m.batteryErr.Error()
	}
	if m.batteryErr != nil && !errors.Is(m.batteryErr, errHIDPPFeature) {
		note := "电量读取能力未确认：" + m.batteryErr.Error()
		found := false
		for _, existing := range m.features.Notes {
			found = found || existing == note
		}
		if !found {
			m.features.Notes = append(m.features.Notes, note)
		}
	}
	m.features.DPIReason = ""
	m.features.RateReason = ""
	m.features.DPIReadOnly = false
	m.features.RateReadOnly = false
	if m.dpiErr != nil {
		m.features.DPIReason = m.dpiErr.Error()
	}
	if m.rateErr != nil {
		m.features.RateReason = m.rateErr.Error()
	}
	reason := ""
	if !m.IsMouse {
		reason = "未确认设备类型为鼠标或轨迹球"
	} else if m.legacy {
		reason = "HID++ 1.0 仅支持只读电量"
	} else if !m.direct && m.identity == "" {
		reason = "接收器设备缺少可复核的唯一身份，禁止改值"
		if m.identityErr != nil {
			reason += "：" + m.identityErr.Error()
		}
	}
	if reason != "" {
		m.features.DPIReadOnly = true
		m.features.RateReadOnly = true
		if m.features.DPIReason == "" {
			m.features.DPIReason = reason
		}
		if m.features.RateReason == "" {
			m.features.RateReason = reason
		}
	}
}

func (m *logitechMouse) verifyWriteIdentity() error {
	if !m.IsMouse {
		return errors.New("未确认鼠标身份，禁止写入")
	}
	if m.identity == "" {
		if m.direct {
			return nil
		}
		return errors.New("接收器设备缺少可复核的唯一身份，禁止改值")
	}
	current, err := m.readIdentity()
	if err != nil {
		return fmt.Errorf("写入前复核设备身份：%w", err)
	}
	if current != m.identity {
		return errors.New("接收器槽位的设备身份已改变，请重新扫描后再设置")
	}
	return nil
}

func (m *logitechMouse) Features() mouseFeatures {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.features
	f.DPIRanges = append([]dpiRange(nil), f.DPIRanges...)
	f.PollRates = append([]int(nil), f.PollRates...)
	f.Notes = append([]string(nil), f.Notes...)
	return f
}
func (m *logitechMouse) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	return m.transport.Close()
}

var _ mouseBackend = (*logitechMouse)(nil)
