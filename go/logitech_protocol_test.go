package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

// 模拟器按公开 feature ID 解释请求，特性索引故意不等于功能编号。
type logiSimulator struct {
	features    map[uint16]byte
	kind        byte
	unit        byte
	dpi         dpiValue
	lod         byte
	rate        byte
	extended    bool
	writes      int
	requests    [][]byte
	failFeature uint16
	failWrite   bool
	ignoreWrite bool
	legacy      bool
	onboard     byte
}

func newLogiSimulator(extended bool) *logiSimulator {
	s := &logiSimulator{features: map[uint16]byte{3: 5, 5: 8, 0x1004: 11}, kind: 3, unit: 7, dpi: dpiValue{800, 800}, rate: 1, lod: 2, extended: extended}
	if extended {
		s.features[0x2202], s.features[0x8061] = 17, 21
	} else {
		s.features[0x2201], s.features[0x8060] = 17, 21
	}
	return s
}
func (s *logiSimulator) Close() error { return nil }
func (s *logiSimulator) Exchange(q []byte, match func([]byte) bool, _ time.Duration) ([]byte, error) {
	s.requests = append(s.requests, append([]byte(nil), q...))
	r := make([]byte, 20)
	r[0], r[1], r[2], r[3] = 0x11, q[1], q[2], q[3]
	p := r[4:]
	fn := q[3] & 0xF0
	fail := func(legacy bool, code byte) ([]byte, error) {
		e := []byte{0x10, q[1], 0xFF, q[2], q[3], code, 0}
		if legacy {
			e[2] = 0x8F
		}
		if !match(e) {
			return nil, errors.New("error response rejected")
		}
		return e, nil
	}
	if s.legacy {
		if q[2] == 0 {
			return fail(true, 1)
		}
		if q[2] != 0x81 {
			return nil, errors.New("unexpected legacy mutation")
		}
		if q[3] == 0x0D {
			return fail(true, 2)
		}
		if q[3] != 7 {
			return nil, errors.New("unexpected register")
		}
		p[0], p[1] = 3, 0
	} else if q[2] == 0 {
		if fn == 0x10 {
			p[0], p[1], p[2] = 4, 5, q[6]
		} else {
			id := binary.BigEndian.Uint16(q[4:6])
			if id == s.failFeature {
				return nil, errors.New("simulated timeout")
			}
			p[0], p[2] = s.features[id], 1
		}
	} else {
		var id uint16
		for k, v := range s.features {
			if v == q[2] {
				id = k
			}
		}
		switch id {
		case 3:
			p[0], p[1], p[6], p[7], p[8] = 1, s.unit, 8, 0xC0, 0xAA
		case 5:
			name := "Logitech test mouse"
			switch fn {
			case 0:
				p[0] = byte(len(name))
			case 0x10:
				copy(p, name[int(q[4]):])
			case 0x20:
				p[0] = s.kind
			}
		case 0x1004:
			if fn == 0 {
				p[0], p[1] = 15, 2
			} else {
				p[0], p[1], p[2] = 0, 1, 0
			}
		case 0x2201, 0x2202:
			if (id == 0x2201 && fn == 0) || (id == 0x2202 && fn == 0x10) {
				p[0], p[2] = 1, 3
			} else if (id == 0x2201 && fn == 0x10) || (id == 0x2202 && fn == 0x20) {
				start := 1
				if id == 0x2202 {
					start = 3
					p[1], p[2] = q[5], q[6]
				}
				copy(p[start:], []byte{0, 100, 0xE0, 50, 0x27, 0x10, 0, 0})
			} else if (id == 0x2201 && fn == 0x20) || (id == 0x2202 && fn == 0x50) {
				binary.BigEndian.PutUint16(p[1:3], uint16(s.dpi.X))
				binary.BigEndian.PutUint16(p[5:7], uint16(s.dpi.Y))
				p[9] = s.lod
			} else if (id == 0x2201 && fn == 0x30) || (id == 0x2202 && fn == 0x60) {
				s.writes++
				if s.failWrite {
					return nil, errors.New("write timeout")
				}
				if !s.ignoreWrite {
					s.dpi.X = int(binary.BigEndian.Uint16(q[5:7]))
					s.dpi.Y = s.dpi.X
					if id == 0x2202 {
						s.dpi.Y = int(binary.BigEndian.Uint16(q[7:9]))
						s.lod = q[9]
					}
				}
			} else {
				return nil, errors.New("unexpected DPI function")
			}
		case 0x8060, 0x8061:
			capfn, readfn, writefn := byte(0), byte(0x10), byte(0x20)
			if id == 0x8061 {
				capfn, readfn, writefn = 0x10, 0x20, 0x30
			}
			switch fn {
			case capfn:
				if id == 0x8061 {
					p[1] = 0x7F
				} else {
					p[0] = 0x8B
				}
			case readfn:
				p[0] = s.rate
			case writefn:
				s.writes++
				if s.failWrite {
					return nil, errors.New("write timeout")
				}
				if !s.ignoreWrite {
					s.rate = q[4]
				}
			default:
				return nil, errors.New("unexpected rate function")
			}
		case 0x8100:
			if fn != 0x20 {
				return nil, errors.New("attempted onboard mode mutation")
			}
			p[0] = s.onboard
		default:
			return fail(false, 6)
		}
	}
	if !match(r) {
		return nil, errors.New("valid response rejected")
	}
	return r, nil
}

func TestLogitechReplyMatching(t *testing.T) {
	q := []byte{0x10, 2, 9, 0x27, 0, 0, 0}
	good := []byte{0x10, 2, 9, 0x27, 0, 0, 0}
	if !hidppReplyMatches(q, good) {
		t.Fatal("valid short reply")
	}
	for _, i := range []int{1, 2, 3} {
		bad := bytes.Clone(good)
		bad[i]++
		if hidppReplyMatches(q, bad) {
			t.Errorf("accepted wrong byte %d", i)
		}
	}
	if hidppReplyMatches(q, good[:6]) {
		t.Fatal("accepted truncated frame")
	}
	long := make([]byte, 20)
	copy(long, good)
	long[0] = 0x11
	if !hidppReplyMatches(q, long) {
		t.Fatal("short request may receive long response")
	}
	for _, id := range []byte{0x8F, 0xFF} {
		e := []byte{0x10, 2, id, 9, 0x27, 2, 0}
		if !hidppReplyMatches(q, e) {
			t.Fatal("matching protocol error rejected")
		}
		e[4]++
		if hidppReplyMatches(q, e) {
			t.Fatal("wrong software ID accepted")
		}
	}
	good[3] &= 0xF0
	if hidppReplyMatches(q, good) {
		t.Fatal("event accepted as response")
	}
}

func TestLogitechProbeAndControl(t *testing.T) {
	for _, extended := range []bool{false, true} {
		t.Run(map[bool]string{false: "2201_8060", true: "2202_8061"}[extended], func(t *testing.T) {
			s := newLogiSimulator(extended)
			m, err := probeLogitech(s, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !m.IsMouse || m.Name != "Logitech test mouse" || m.Identity == "" || m.Protocol != "HID++ 4.5" {
				t.Fatalf("identity: %+v", m)
			}
			f := m.Features()
			if !f.Battery || len(f.DPIRanges) == 0 || f.SeparateAxes != extended || f.DPIReadOnly {
				t.Fatalf("features: %+v", f)
			}
			r, err := m.ReadBattery()
			if err != nil || r.Percent != 0 || r.PercentUnknown {
				t.Fatalf("zero percent must remain valid: %+v %v", r, err)
			}
			if _, changed, err := m.SetDPI(dpiValue{800, 800}); err != nil || changed || s.writes != 0 {
				t.Fatalf("same DPI: %v %v %d", changed, err, s.writes)
			}
			if _, _, err := m.SetDPI(dpiValue{825, 825}); err == nil || s.writes != 0 {
				t.Fatal("off-step DPI accepted")
			}
			want := dpiValue{1200, 1200}
			if extended {
				want.Y = 1600
			}
			got, changed, err := m.SetDPI(want)
			if err != nil || !changed || got != want || s.lod != 2 {
				t.Fatalf("set DPI: %v %v %v LOD=%d", got, changed, err, s.lod)
			}
			writes := s.writes
			if _, _, err := m.SetRate(123); err == nil || s.writes != writes {
				t.Fatal("invalid rate accepted")
			}
			rate := 125
			if extended {
				rate = 8000
			}
			actual, changed, err := m.SetRate(rate)
			if err != nil || !changed || actual != rate {
				t.Fatalf("rate: %d %v %v", actual, changed, err)
			}
			writes = s.writes
			if _, changed, err := m.SetRate(rate); err != nil || changed || s.writes != writes {
				t.Fatal("same rate wrote")
			}
		})
	}
}

func TestLogitechWriteSafety(t *testing.T) {
	for _, scenario := range []string{"identity_changed", "no_identity", "timeout", "readback", "keyboard"} {
		t.Run(scenario, func(t *testing.T) {
			s := newLogiSimulator(false)
			if scenario == "no_identity" {
				delete(s.features, 3)
			}
			if scenario == "keyboard" {
				s.kind = 0
			}
			m, err := probeLogitech(s, 1)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "identity_changed":
				s.unit++
			case "timeout":
				s.failWrite = true
			case "readback":
				s.ignoreWrite = true
			}
			_, _, err = m.SetDPI(dpiValue{1200, 1200})
			if err == nil {
				t.Fatal("unsafe write reported success")
			}
			wantWrites := 0
			if scenario == "timeout" || scenario == "readback" {
				wantWrites = 1
			}
			if s.writes != wantWrites {
				t.Fatalf("writes=%d want=%d", s.writes, wantWrites)
			}
		})
	}
}

func TestLogitechPreservesDiscoveryError(t *testing.T) {
	s := newLogiSimulator(false)
	s.failFeature = 0x2202
	m, err := probeLogitech(s, 0xFF)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Features().DPIReason, "simulated timeout") {
		t.Fatalf("lost discovery cause: %+v", m.Features())
	}
	if _, err = m.ReadDPI(); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatal("timeout became unsupported", err)
	}
}

func TestLogitechLegacyBatteryAndIdentity(t *testing.T) {
	s := newLogiSimulator(false)
	s.legacy = true
	m, err := probeLogitech(s, 2)
	if err != nil {
		t.Fatal(err)
	}
	m.SetReceiverIdentity("Legacy Mouse", "鼠标", "paired:1234")
	if !m.IsMouse || m.Protocol != "HID++ 1.0" {
		t.Fatalf("legacy: %+v", m)
	}
	r, err := m.ReadBattery()
	if err != nil || !r.PercentUnknown || r.Level != "低" || !r.ChargeKnown {
		t.Fatalf("legacy battery: %+v %v", r, err)
	}
	if _, _, err = m.SetRate(500); err == nil || s.writes != 0 {
		t.Fatal("legacy write accepted")
	}
}

func TestLogitechBatteryDecoding(t *testing.T) {
	tests := []struct {
		name              string
		feature           uint16
		caps, data        []byte
		percent           int
		unknown, charging bool
		voltage           int
		level             string
	}{
		{"unified zero", 0x1004, []byte{15, 2}, []byte{0, 1, 0, 0}, 0, false, false, 0, ""},
		{"unified level", 0x1004, []byte{15, 0}, []byte{0, 2, 1, 0}, 0, true, true, 0, "低"},
		{"old coarse", 0x1000, []byte{4, 0}, []byte{30, 10, 0}, 0, true, false, 0, "正常"},
		{"old percentage", 0x1000, []byte{100, 2}, []byte{55, 20, 0}, 55, false, false, 0, "正常"},
		{"charging unknown", 0x1000, []byte{100, 2}, []byte{0, 0, 1}, 0, true, true, 0, ""},
		{"voltage", 0x1001, nil, []byte{0x0F, 0xA0, 0x80}, 0, true, true, 4000, ""},
		{"adc", 0x1F20, nil, []byte{0x0F, 0xA0, 1}, 0, true, false, 4000, ""},
		{"adc charging", 0x1F20, nil, []byte{0x0F, 0xA0, 3}, 0, true, true, 4000, ""},
		{"adc full", 0x1F20, nil, []byte{0x0F, 0xA0, 7}, 0, true, false, 4000, "满"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := decodeLogitechBattery(tt.feature, tt.caps, tt.data)
			if err != nil || r.Percent != tt.percent || r.PercentUnknown != tt.unknown || r.Charging != tt.charging || r.VoltageMV != tt.voltage || r.Level != tt.level {
				t.Fatalf("got %+v err %v", r, err)
			}
		})
	}
	if _, err := decodeLogitechBattery(0x1004, []byte{15, 2}, []byte{101, 0, 0, 0}); err == nil {
		t.Fatal("invalid percentage accepted")
	}
	for _, flags := range []byte{0, 0x0F, 0x83} {
		r, err := decodeLogitechBattery(0x1F20, nil, []byte{15, 160, flags})
		if err != nil || r.ChargeKnown || r.ChargeError == nil || r.VoltageMV != 4000 {
			t.Fatalf("unknown ADC flags misrepresented: %+v %v", r, err)
		}
	}
}

func TestLogitechADCFeatureDiscovery(t *testing.T) {
	s := newLogiSimulator(false)
	delete(s.features, 0x1004)
	s.features[0x1F20] = 11
	m, err := probeLogitech(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.batteryFeature != 0x1F20 || !m.Features().Battery {
		t.Fatalf("ADC feature missing: %04X %+v", m.batteryFeature, m.Features())
	}
}

func TestLogitechUnknownVoltageChargeIsNotShownAsDischarging(t *testing.T) {
	r, err := decodeLogitechBattery(0x1001, nil, []byte{15, 160, 0x83})
	if err != nil || r.ChargeKnown || r.ChargeError == nil || r.ChargeText != "" {
		t.Fatalf("unknown voltage charge misrepresented: %+v %v", r, err)
	}
}

func TestLogitechUsesDedicatedSoftwareID(t *testing.T) {
	s := newLogiSimulator(false)
	if _, err := probeLogitech(s, 1); err != nil {
		t.Fatal(err)
	}
	for _, q := range s.requests {
		if q[3]&15 != 0x0C {
			t.Fatalf("request used non-dedicated software ID: % X", q)
		}
	}
}

func TestLogitechOnboardModeNeverAutoChanges(t *testing.T) {
	s := newLogiSimulator(false)
	s.features[0x8100] = 25
	s.onboard = 1
	m, err := probeLogitech(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = m.SetDPI(dpiValue{1200, 1200}); err == nil || !strings.Contains(err.Error(), "板载") {
		t.Fatalf("onboard mode not explained: %v", err)
	}
	if s.writes != 0 {
		t.Fatal("onboard mode received control write")
	}
	s.onboard = 2
	if _, _, err = m.SetDPI(dpiValue{1200, 1200}); err != nil {
		t.Fatal(err)
	}
}

func TestLogitechFeature8FLongReply(t *testing.T) {
	q := []byte{0x10, 1, 0x8F, 0x28, 0, 0, 0}
	r := make([]byte, 20)
	copy(r, []byte{0x11, 1, 0x8F, 0x28})
	if !hidppReplyMatches(q, r) {
		t.Fatal("long feature 8F is not HID++1 short error")
	}
}

func TestLogitechDPIListBoundaries(t *testing.T) {
	for _, data := range [][]byte{{0xE0, 50, 0, 100, 0, 0}, {0, 100, 0xE0, 0, 1, 0, 0, 0}, {0, 100, 0xE0, 50, 0, 90, 0, 0}, {0, 100, 0, 90, 0, 0}} {
		if _, _, err := parseLogitechDPIRanges(data); err == nil {
			t.Fatalf("invalid list accepted % X", data)
		}
	}
	// 2201 最多七个显式值，可填满16字节响应，不要求存在终止word。
	s := newLogiSimulator(false)
	listReads := 0
	wrapper := logiTransportFunc(func(q []byte, match func([]byte) bool, d time.Duration) ([]byte, error) {
		if q[2] == 17 && q[3]&0xF0 == 0x10 {
			listReads++
			r := make([]byte, 20)
			copy(r, []byte{0x11, q[1], q[2], q[3]})
			for i, v := range []uint16{100, 200, 400, 800, 1600, 3200, 6400} {
				binary.BigEndian.PutUint16(r[5+i*2:], v)
			}
			return r, nil
		}
		return s.Exchange(q, match, d)
	})
	m, err := probeLogitech(wrapper, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Features().DPIRanges) != 7 {
		t.Fatalf("full DPI list rejected: %+v", m.Features())
	}
	if listReads != 1 {
		t.Fatalf("2201 invented pagination: %d reads", listReads)
	}
}

func TestLogitechBatteryDiscoveryFailureIsVisible(t *testing.T) {
	s := newLogiSimulator(false)
	s.failFeature = 0x1004
	m, err := probeLogitech(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(m.Features().Notes, " "), "simulated timeout") {
		t.Fatalf("battery discovery error hidden: %+v", m.Features())
	}
	if !m.Features().DiscoveryFailed {
		t.Fatal("battery discovery failure was not retained")
	}
	if _, err = m.ReadBattery(); err == nil || !strings.Contains(err.Error(), "simulated timeout") {
		t.Fatal("lost battery timeout", err)
	}
}

func TestLogitechExtendedDPIPageBoundary(t *testing.T) {
	s := newLogiSimulator(true)
	values := []uint16{100, 200, 400, 800, 1200, 1600, 2400, 3200, 6400, 10000, 0}
	data := make([]byte, len(values)*2)
	for i, v := range values {
		binary.BigEndian.PutUint16(data[i*2:], v)
	}
	tr := logiTransportFunc(func(q []byte, match func([]byte) bool, d time.Duration) ([]byte, error) {
		if q[2] == 17 && q[3]&0xF0 == 0x20 {
			r := make([]byte, 20)
			copy(r, []byte{0x11, q[1], q[2], q[3]})
			r[5], r[6] = 0xA5, 0x5A
			offset := int(q[6]) * 13
			if offset < len(data) {
				copy(r[7:], data[offset:])
			}
			return r, nil
		}
		return s.Exchange(q, match, d)
	})
	m, err := probeLogitech(tr, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Features().DPIRanges) != 10 {
		t.Fatalf("DPI across odd-byte page boundary: %+v", m.Features())
	}
}

func TestLogitechMetadataDoesNotOverrideKeyboardAndClose(t *testing.T) {
	s := newLogiSimulator(false)
	s.kind = 0
	m, err := probeLogitech(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	m.SetReceiverIdentity("Mouse", "鼠标", "old-identity")
	if m.IsMouse || m.Kind != "键盘" {
		t.Fatal("pairing hint replaced authoritative type")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	n := len(s.requests)
	if _, err = m.ReadDPI(); err == nil {
		t.Fatal("closed backend read succeeded")
	}
	if len(s.requests) != n {
		t.Fatal("closed backend sent I/O")
	}
}
