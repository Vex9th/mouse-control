package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

func (m *logitechMouse) chooseFeature(ids ...uint16) (uint16, byte, error) {
	for _, id := range ids {
		idx, err := m.feature(id)
		if errors.Is(err, errHIDPPFeature) {
			continue
		}
		return id, idx, err
	}
	return 0, 0, errHIDPPFeature
}
func (m *logitechMouse) discoverDPI() {
	var err error
	m.dpiFeature, m.dpiIndex, err = m.chooseFeature(0x2202, 0x2201)
	if err != nil {
		m.dpiErr = fmt.Errorf("DPI 功能查询：%w", err)
		return
	}
	fn := byte(0)
	if m.dpiFeature == 0x2202 {
		fn = 0x10
	}
	p, err := m.call(m.dpiIndex, fn, 0)
	if err != nil {
		m.dpiErr = err
		return
	}
	if m.dpiFeature == 0x2202 {
		m.features.SeparateAxes = p[2]&1 != 0
		m.lod = p[2]&2 != 0
	} else {
		if p[0] == 0 {
			m.dpiErr = errors.New("设备未报告 DPI 传感器")
			return
		}
		if p[0] > 1 {
			m.features.Notes = append(m.features.Notes, "当前 DPI 控制使用传感器 0")
		}
	}
	m.xRanges, err = m.readDPIRanges(0)
	if err != nil {
		m.dpiErr = err
		return
	}
	m.yRanges = m.xRanges
	if m.features.SeparateAxes {
		m.yRanges, err = m.readDPIRanges(1)
		if err != nil {
			m.dpiErr = err
			return
		}
		m.features.Notes = append(m.features.Notes, "DPI 范围显示 X 轴能力，Y 轴提交时按设备报告单独校验")
	}
	m.features.DPIRanges = append([]dpiRange(nil), m.xRanges...)
}

// E000..FFFF 为步长标记，前一数值为起点、下一数值为终点；无该标记是离散档位。
func parseLogitechDPIRanges(data []byte) ([]dpiRange, bool, error) {
	var out []dpiRange
	for i := 0; i+1 < len(data); i += 2 {
		v := int(binary.BigEndian.Uint16(data[i : i+2]))
		if v == 0 {
			if len(out) == 0 {
				return nil, false, errors.New("DPI 档位列表为空")
			}
			return out, true, nil
		}
		if v&0xE000 == 0xE000 {
			if v&0x1FFF == 0 || len(out) == 0 {
				return nil, false, errors.New("DPI 步长标记无效")
			}
			if i+3 >= len(data) {
				return nil, false, nil
			}
			end := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
			last := &out[len(out)-1]
			if last.Min != last.Max || end <= last.Max || end >= 0xE000 {
				return nil, false, errors.New("DPI 范围终点无效")
			}
			last.Step = v & 0x1FFF
			last.Max = end
			i += 2
		} else {
			if len(out) > 0 && v <= out[len(out)-1].Max {
				return nil, false, errors.New("DPI 档位未递增")
			}
			out = append(out, dpiRange{Min: v, Max: v, Step: 1})
		}
	}
	return nil, false, nil
}

func (m *logitechMouse) readDPIRanges(direction byte) ([]dpiRange, error) {
	fn, skip := byte(0x10), 1
	if m.dpiFeature == 0x2202 {
		fn, skip = 0x20, 3
	}
	data := make([]byte, 0, 64)
	for page := 0; page < 32; page++ {
		p, err := m.call(m.dpiIndex, fn, 0, direction, byte(page))
		if err != nil {
			return nil, err
		}
		if len(p) <= skip || p[0] != 0 {
			return nil, fmt.Errorf("%w：DPI 列表传感器或长度无效", errFrame)
		}
		if m.dpiFeature == 0x2201 {
			if err := requireHIDPPData(p, 16); err != nil {
				return nil, err
			}
			// 2201 标准只有一页和七个完整word，末字节是padding。
			data = append(data, p[1:15]...)
			data = append(data, 0, 0)
		} else {
			// 2202 上游只确认三字节header，不猜另两字节的回显语义。
			data = append(data, p[skip:]...)
		}
		ranges, complete, err := parseLogitechDPIRanges(data)
		if err != nil {
			return nil, err
		}
		if complete {
			return ranges, nil
		}
	}
	return nil, errors.New("DPI 列表超过 32 页仍未结束，未启用写入")
}
func logitechDPIAllowed(value int, ranges []dpiRange) bool {
	for _, r := range ranges {
		if r.Step > 0 && value >= r.Min && value <= r.Max && (value-r.Min)%r.Step == 0 {
			return true
		}
	}
	return false
}

func (m *logitechMouse) readDPI() (dpiValue, byte, error) {
	if m.dpiErr != nil {
		return dpiValue{}, 0, m.dpiErr
	}
	if m.dpiIndex == 0 {
		return dpiValue{}, 0, fmt.Errorf("DPI：%w", errHIDPPFeature)
	}
	fn, min := byte(0x20), 5
	if m.dpiFeature == 0x2202 {
		fn, min = 0x50, 10
	}
	p, err := m.call(m.dpiIndex, fn, 0)
	if err != nil {
		return dpiValue{}, 0, err
	}
	if err = requireHIDPPData(p, min); err != nil {
		return dpiValue{}, 0, err
	}
	if p[0] != 0 {
		return dpiValue{}, 0, fmt.Errorf("%w：DPI 传感器不符", errFrame)
	}
	v := dpiValue{X: int(binary.BigEndian.Uint16(p[1:3]))}
	if v.X == 0 {
		v.X = int(binary.BigEndian.Uint16(p[3:5]))
	}
	v.Y = v.X
	if m.features.SeparateAxes {
		v.Y = int(binary.BigEndian.Uint16(p[5:7]))
		if v.Y == 0 {
			v.Y = int(binary.BigEndian.Uint16(p[7:9]))
		}
	}
	if !logitechDPIAllowed(v.X, m.xRanges) || !logitechDPIAllowed(v.Y, m.yRanges) {
		return v, 0, errors.New("当前 DPI 不在设备报告的合法档位内")
	}
	lod := byte(0)
	if m.lod {
		lod = p[9]
		if lod > 2 {
			return v, lod, errors.New("设备 LOD 数值未知，不能安全保留该设置")
		}
	}
	return v, lod, nil
}
func (m *logitechMouse) ReadDPI() (dpiValue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, _, err := m.readDPI()
	return v, err
}

func (m *logitechMouse) verifyControlFeature(id uint16, index byte) error {
	if err := m.verifyWriteIdentity(); err != nil {
		return err
	}
	fresh, err := m.feature(id)
	if err != nil {
		return fmt.Errorf("写入前复核控制功能：%w", err)
	}
	if fresh != index {
		return errors.New("设备功能索引已改变，请重新扫描")
	}
	modeIndex, err := m.feature(0x8100)
	if errors.Is(err, errHIDPPFeature) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("查询板载模式失败：%w", err)
	}
	p, err := m.call(modeIndex, 0x20)
	if err != nil {
		return fmt.Errorf("读取板载模式失败：%w", err)
	}
	if p[0] == 1 {
		return errors.New("设备当前处于板载配置模式；当前功能未改值，也未自动切换模式")
	}
	if p[0] != 2 {
		return fmt.Errorf("未知设备控制模式 %d，未改值", p[0])
	}
	return nil
}
func (m *logitechMouse) SetDPI(want dpiValue) (dpiValue, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dpiErr != nil {
		return dpiValue{}, false, m.dpiErr
	}
	if m.features.DPIReadOnly {
		return dpiValue{}, false, errors.New(m.features.DPIReason)
	}
	if !logitechDPIAllowed(want.X, m.xRanges) || !logitechDPIAllowed(want.Y, m.yRanges) {
		return dpiValue{}, false, errors.New("DPI 不符合设备报告的范围或步长")
	}
	if !m.features.SeparateAxes && want.X != want.Y {
		return dpiValue{}, false, errors.New("此 HID++ DPI 功能只支持 X、Y 使用相同值")
	}
	before, lod, err := m.readDPI()
	if err != nil {
		return before, false, err
	}
	if before == want {
		return before, false, nil
	}
	if err = m.verifyControlFeature(m.dpiFeature, m.dpiIndex); err != nil {
		return before, false, err
	}
	args := []byte{0, byte(want.X >> 8), byte(want.X)}
	fn := byte(0x30)
	if m.dpiFeature == 0x2202 {
		fn = 0x60
		y := 0
		if m.features.SeparateAxes {
			y = want.Y
		}
		args = append(args, byte(y>>8), byte(y), lod)
	}
	if _, err = m.call(m.dpiIndex, fn, args...); err != nil {
		return before, false, fmt.Errorf("DPI 写入结果未确认；未重试，请重新读取：%w", err)
	}
	after, _, err := m.readDPI()
	if err != nil {
		return after, true, fmt.Errorf("DPI 指令已应答，但读回失败：%w", err)
	}
	if after != want {
		return after, true, fmt.Errorf("DPI 读回为 %d/%d，与请求 %d/%d 不符", after.X, after.Y, want.X, want.Y)
	}
	return after, true, nil
}

func (m *logitechMouse) discoverRate() {
	var err error
	m.rateFeature, m.rateIndex, err = m.chooseFeature(0x8061, 0x8060)
	if err != nil {
		m.rateErr = fmt.Errorf("回报率功能查询：%w", err)
		return
	}
	fn := byte(0)
	if m.rateFeature == 0x8061 {
		fn = 0x10
	}
	p, err := m.call(m.rateIndex, fn)
	if err != nil {
		m.rateErr = err
		return
	}
	if m.rateFeature == 0x8061 {
		mask := binary.BigEndian.Uint16(p[:2])
		for code, hz := range []int{125, 250, 500, 1000, 2000, 4000, 8000} {
			if mask&(1<<code) != 0 {
				m.rateCodes[hz] = byte(code)
			}
		}
	} else {
		for period := 1; period <= 8; period++ {
			if p[0]&(1<<(period-1)) != 0 {
				m.rateCodes[1000/period] = byte(period)
				if 1000%period != 0 {
					m.features.Notes = append(m.features.Notes, fmt.Sprintf("%d ms 回报周期在整数 Hz 界面显示为 %d Hz", period, 1000/period))
				}
			}
		}
	}
	for hz := range m.rateCodes {
		m.features.PollRates = append(m.features.PollRates, hz)
	}
	sort.Ints(m.features.PollRates)
	if len(m.rateCodes) == 0 {
		m.rateErr = errors.New("设备未报告已知回报率档位")
	}
}
func (m *logitechMouse) readRate() (int, error) {
	if m.rateErr != nil {
		return 0, m.rateErr
	}
	if m.rateIndex == 0 {
		return 0, fmt.Errorf("回报率：%w", errHIDPPFeature)
	}
	fn := byte(0x10)
	if m.rateFeature == 0x8061 {
		fn = 0x20
	}
	p, err := m.call(m.rateIndex, fn)
	if err != nil {
		return 0, err
	}
	for hz, code := range m.rateCodes {
		if p[0] == code {
			return hz, nil
		}
	}
	return 0, fmt.Errorf("设备当前回报率编码 %d 不在已报告能力中", p[0])
}
func (m *logitechMouse) ReadRate() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readRate()
}
func (m *logitechMouse) SetRate(want int) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rateErr != nil {
		return 0, false, m.rateErr
	}
	if m.features.RateReadOnly {
		return 0, false, errors.New(m.features.RateReason)
	}
	code, ok := m.rateCodes[want]
	if !ok {
		return 0, false, errors.New("回报率不在设备报告的合法档位中")
	}
	before, err := m.readRate()
	if err != nil {
		return before, false, err
	}
	if before == want {
		return before, false, nil
	}
	if err = m.verifyControlFeature(m.rateFeature, m.rateIndex); err != nil {
		return before, false, err
	}
	fn := byte(0x20)
	if m.rateFeature == 0x8061 {
		fn = 0x30
	}
	if _, err = m.call(m.rateIndex, fn, code); err != nil {
		return before, false, fmt.Errorf("回报率写入结果未确认；未重试，请重新读取：%w", err)
	}
	after, err := m.readRate()
	if err != nil {
		return after, true, fmt.Errorf("回报率指令已应答，但读回失败：%w", err)
	}
	if after != want {
		return after, true, fmt.Errorf("回报率读回 %d Hz，与请求 %d Hz 不符", after, want)
	}
	return after, true, nil
}
