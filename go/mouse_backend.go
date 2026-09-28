package main

// 单值用Min=Max表示；范围只接受Min+n*Step，不能静默舍入用户输入。
type dpiRange struct{ Min, Max, Step int }

type mouseFeatures struct {
	Battery                   bool
	BatteryReason             string
	DiscoveryFailed           bool
	DPIRanges                 []dpiRange
	SeparateAxes              bool
	PollRates                 []int
	DPIReadOnly, RateReadOnly bool
	DPIReason, RateReason     string
	Notes                     []string
}

type mouseBackend interface {
	Features() mouseFeatures
	ReadBattery() (reading, error)
	ReadDPI() (dpiValue, error)
	ReadRate() (int, error)
	SetDPI(dpiValue) (dpiValue, bool, error)
	SetRate(int) (int, bool, error)
	Close() error
}

func deviceVID(d device) uint16 {
	if d.VID == 0 {
		return 0x1532
	}
	return d.VID
}

func deviceFeatures(d device) mouseFeatures {
	if d.Caps != nil {
		return *d.Caps
	}
	if d.Backend != nil {
		return d.Backend.Features()
	}
	if deviceVID(d) != 0x1532 {
		return mouseFeatures{}
	}
	c := mouseCapabilities[d.PID]
	f := mouseFeatures{Battery: d.Info.Battery, SeparateAxes: true, PollRates: c.PollRates}
	if c.MaxDPI > 0 {
		f.DPIRanges = []dpiRange{{100, c.MaxDPI, 1}}
	}
	return f
}
