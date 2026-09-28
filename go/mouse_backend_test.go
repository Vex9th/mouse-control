package main

import "testing"

type backendStub struct {
	closed bool
	dpi    dpiValue
	rate   int
}

func (b *backendStub) Features() mouseFeatures {
	return mouseFeatures{Battery: true, DPIRanges: []dpiRange{{200, 32000, 50}}, PollRates: []int{125, 500, 1000}}
}
func (*backendStub) ReadBattery() (reading, error) {
	return reading{PercentUnknown: true, Level: "正常", VoltageMV: 3900}, nil
}
func (b *backendStub) ReadDPI() (dpiValue, error)                { return b.dpi, nil }
func (b *backendStub) ReadRate() (int, error)                    { return b.rate, nil }
func (b *backendStub) SetDPI(v dpiValue) (dpiValue, bool, error) { b.dpi = v; return v, true, nil }
func (b *backendStub) SetRate(v int) (int, bool, error)          { b.rate = v; return v, true, nil }
func (b *backendStub) Close() error                              { b.closed = true; return nil }

func TestBackendRoutingPreservesUnknownBatteryAndCloses(t *testing.T) {
	b := &backendStub{dpi: dpiValue{800, 800}, rate: 1000}
	d := device{VID: 0x046d, PID: 0x00bf, Backend: b} // 与雷蛇PID重合也不能复用雷蛇协议。
	r := readMouse(d)
	if !r.Reading.PercentUnknown || r.Reading.Level != "正常" || r.DPI.X != 800 || r.Rate != 1000 || r.Err != nil {
		t.Fatalf("后端读数丢失：%+v", r)
	}
	if v, _, e := setMouseDPI(d, dpiValue{1600, 1600}); e != nil || v.X != 1600 {
		t.Fatalf("设置未路由：%+v %v", v, e)
	}
	if v, _, e := setMouseRate(d, 500); e != nil || v != 500 {
		t.Fatalf("回报率未路由：%d %v", v, e)
	}
	closeDevices([]device{d})
	if !b.closed {
		t.Fatal("未关闭后端")
	}
}

func TestFeaturesAreVendorScopedAndSnapshotSurvivesClose(t *testing.T) {
	if got := deviceFeatures(device{VID: 0x046d, PID: 0x00bf}); got.Battery || len(got.DPIRanges) > 0 {
		t.Fatal("罗技误用雷蛇PID能力")
	}
	if got := deviceFeatures(device{PID: 0x00bf, Info: models[0x00bf]}); !got.Battery || len(got.DPIRanges) != 1 || got.DPIRanges[0].Max != 45000 || !got.SeparateAxes {
		t.Fatalf("雷蛇回归：%+v", got)
	}
	cap := (&backendStub{}).Features()
	if got := deviceFeatures(device{VID: 0x046d, Caps: &cap}); len(got.DPIRanges) != 1 || got.DPIRanges[0].Step != 50 {
		t.Fatal("能力快照丢失")
	}
}
