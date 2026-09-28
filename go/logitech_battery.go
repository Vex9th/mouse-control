package main

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (m *logitechMouse) discoverBattery() {
	for _, id := range []uint16{0x1004, 0x1000, 0x1001, 0x1F20} {
		idx, err := m.feature(id)
		if errors.Is(err, errHIDPPFeature) {
			continue
		}
		if err != nil {
			m.batteryErr = fmt.Errorf("电量功能 %04X 查询失败：%w", id, err)
			return
		}
		m.batteryFeature, m.batteryIndex = id, idx
		if id == 0x1004 || id == 0x1000 {
			fn := byte(0)
			if id == 0x1000 {
				fn = 0x10
			}
			m.batteryCaps, m.batteryErr = m.call(idx, fn)
		}
		return
	}
	m.batteryErr = fmt.Errorf("电量：%w", errHIDPPFeature)
}

func legacyRegisterUnsupported(err error) bool {
	var pe *hidppProtocolError
	return errors.As(err, &pe) && pe.Legacy && (pe.Code == 1 || pe.Code == 2)
}
func (m *logitechMouse) discoverLegacyBattery() {
	for _, reg := range []byte{0x0D, 7} {
		_, err := hidppExchange(m.transport, m.address, 0x81, reg, nil, nil)
		if legacyRegisterUnsupported(err) {
			continue
		}
		if err != nil {
			m.batteryErr = fmt.Errorf("旧版电量寄存器 %02X：%w", reg, err)
			return
		}
		m.legacyBattery = reg
		return
	}
	m.batteryErr = fmt.Errorf("旧版电量：%w", errHIDPPFeature)
}

func (m *logitechMouse) ReadBattery() (reading, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return reading{}, errors.New("HID++ 通道已关闭")
	}
	if m.batteryErr != nil {
		return reading{}, m.batteryErr
	}
	if m.legacyBattery != 0 {
		p, err := hidppExchange(m.transport, m.address, 0x81, m.legacyBattery, nil, nil)
		if err != nil {
			return reading{}, err
		}
		return decodeLogitechLegacyBattery(m.legacyBattery, p)
	}
	if m.batteryFeature == 0 {
		return reading{}, fmt.Errorf("电量：%w", errHIDPPFeature)
	}
	fn := byte(0)
	if m.batteryFeature == 0x1004 {
		fn = 0x10
	}
	p, err := m.call(m.batteryIndex, fn)
	if err != nil {
		return reading{}, err
	}
	return decodeLogitechBattery(m.batteryFeature, m.batteryCaps, p)
}

func logitechCharge(r *reading, status byte, unified bool) {
	r.ChargeKnown = true
	switch status {
	case 0:
		r.ChargeText = "未充电"
	case 1:
		r.Charging = true
		r.ChargeText = "充电中"
	case 2:
		r.Charging = true
		if unified {
			r.ChargeText = "慢速充电"
		} else {
			r.ChargeText = "充电接近完成"
		}
	case 3:
		r.ChargeText = "已充满"
	case 4:
		if !unified {
			r.Charging = true
			r.ChargeText = "慢速充电"
			return
		}
		fallthrough
	default:
		r.ChargeKnown = false
		r.ChargeError = fmt.Errorf("设备充电状态异常或未知：0x%02X", status)
	}
}
func logitechLevel(percent byte) string {
	switch {
	case percent < 11:
		return "极低"
	case percent < 30:
		return "低"
	case percent < 81:
		return "正常"
	default:
		return "满"
	}
}

func decodeLogitechBattery(feature uint16, caps, p []byte) (reading, error) {
	r := reading{PercentUnknown: true}
	if err := requireHIDPPData(p, 3); err != nil {
		return r, err
	}
	switch feature {
	case 0x1004:
		if err := requireHIDPPData(caps, 2); err != nil {
			return r, err
		}
		if err := requireHIDPPData(p, 4); err != nil {
			return r, err
		}
		if caps[1]&2 != 0 {
			if p[0] > 100 {
				return r, errors.New("统一电量百分比超出 0..100")
			}
			r.Percent = int(p[0])
			r.PercentUnknown = false
		} else {
			level := p[1] & caps[0]
			switch {
			case level&8 != 0:
				r.Level = "满"
			case level&4 != 0:
				r.Level = "正常"
			case level&2 != 0:
				r.Level = "低"
			case level&1 != 0:
				r.Level = "极低"
			}
		}
		logitechCharge(&r, p[2], true)
	case 0x1000:
		if err := requireHIDPPData(caps, 2); err != nil {
			return r, err
		}
		if p[0] > 100 {
			return r, errors.New("电量数值超出 0..100")
		}
		// 充电时的零代表未知，不能显示为电池耗尽；档位设备不冒充精确百分比。
		if p[2] == 0 || p[0] != 0 {
			r.Level = logitechLevel(p[0])
			if caps[0] >= 10 && caps[1]&2 != 0 {
				r.Percent = int(p[0])
				r.PercentUnknown = false
			}
		}
		if p[2] == 3 {
			r.Level = "满"
		}
		logitechCharge(&r, p[2], false)
	case 0x1001:
		r.VoltageMV = int(binary.BigEndian.Uint16(p[:2]))
		flags := p[2]
		r.ChargeKnown = true
		r.ChargeText = "未充电"
		if flags&0x80 != 0 {
			switch flags & 7 {
			case 0:
				r.Charging = true
				r.ChargeText = "充电中"
			case 1:
				r.Level = "满"
				r.ChargeText = "已充满"
			case 2:
				r.ChargeText = "未充电"
			default:
				r.ChargeKnown = false
				r.ChargeText = ""
				r.ChargeError = fmt.Errorf("未知电压电量充电状态：0x%02X", flags)
			}
		}
		if flags&0x20 != 0 {
			r.Level = "极低"
		}
	case 0x1F20:
		r.VoltageMV = int(binary.BigEndian.Uint16(p[:2]))
		switch p[2] {
		case 1:
			r.ChargeKnown = true
			r.ChargeText = "未充电"
		case 3:
			r.ChargeKnown = true
			r.Charging = true
			r.ChargeText = "充电中"
		case 7:
			r.ChargeKnown = true
			r.Level = "满"
			r.ChargeText = "已充满"
		default:
			r.ChargeError = fmt.Errorf("未知 ADC 充电状态：0x%02X", p[2])
		}
	default:
		return r, errHIDPPFeature
	}
	return r, nil
}

func decodeLogitechLegacyBattery(reg byte, p []byte) (reading, error) {
	r := reading{PercentUnknown: true}
	if err := requireHIDPPData(p, 3); err != nil {
		return r, err
	}
	switch reg {
	case 0x0D:
		if p[0] > 100 {
			return r, errors.New("旧版电量百分比超出 0..100")
		}
		r.Percent = int(p[0])
		r.PercentUnknown = false
		switch p[2] & 0xF0 {
		case 0x30:
			r.ChargeKnown = true
			r.ChargeText = "未充电"
		case 0x50:
			r.ChargeKnown = true
			r.Charging = true
			r.ChargeText = "充电中"
		case 0x90:
			r.ChargeKnown = true
			r.ChargeText = "已充满"
		default:
			r.ChargeError = fmt.Errorf("未知旧版充电状态：0x%02X", p[2])
		}
	case 7:
		switch p[0] {
		case 7:
			r.Level = "满"
		case 5:
			r.Level = "正常"
		case 3:
			r.Level = "低"
		case 1:
			r.Level = "极低"
		case 0:
		default:
			return r, fmt.Errorf("未知旧版电量档位：%d", p[0])
		}
		switch {
		case p[1] == 0:
			r.ChargeKnown = true
			r.ChargeText = "未充电"
		case p[1]&0x21 == 0x21:
			r.ChargeKnown = true
			r.Charging = true
			r.ChargeText = "充电中"
		case p[1]&0x22 == 0x22:
			r.ChargeKnown = true
			r.ChargeText = "已充满"
		default:
			r.ChargeError = fmt.Errorf("未知旧版充电状态：0x%02X", p[1])
		}
	default:
		return r, errHIDPPFeature
	}
	return r, nil
}
