package main

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type guiBackendFixture struct {
	caps                                mouseFeatures
	battery                             reading
	batteryErr, dpiErr, rateErr, setErr error
	dpi                                 dpiValue
	rate                                int
	dpiCalls, rateCalls, closes         int
}

func (b *guiBackendFixture) Features() mouseFeatures       { return b.caps }
func (b *guiBackendFixture) ReadBattery() (reading, error) { return b.battery, b.batteryErr }
func (b *guiBackendFixture) ReadDPI() (dpiValue, error)    { return b.dpi, b.dpiErr }
func (b *guiBackendFixture) ReadRate() (int, error)        { return b.rate, b.rateErr }
func (b *guiBackendFixture) SetDPI(v dpiValue) (dpiValue, bool, error) {
	b.dpiCalls++
	if b.setErr != nil {
		return b.dpi, false, b.setErr
	}
	changed := b.dpi != v
	b.dpi = v
	return v, changed, nil
}
func (b *guiBackendFixture) SetRate(v int) (int, bool, error) {
	b.rateCalls++
	if b.setErr != nil {
		return b.rate, false, b.setErr
	}
	changed := b.rate != v
	b.rate = v
	return v, changed, nil
}
func (b *guiBackendFixture) Close() error { b.closes++; return nil }
func guiFixtureDevice(id string, b *guiBackendFixture) device {
	return device{ID: id, VID: 0x046d, PID: 0xC548, Info: modelInfo{Name: "Logitech GUI Mouse", Connection: "Bolt"}, Backend: b}
}
func guiUsableBackend() *guiBackendFixture {
	return &guiBackendFixture{caps: mouseFeatures{Battery: true, DPIRanges: []dpiRange{{100, 10000, 50}}, PollRates: []int{125, 500, 1000}}, battery: reading{Percent: 50, ChargeKnown: true}, dpi: dpiValue{800, 800}, rate: 1000}
}

func TestGUIScanUnknownValuesAndJSONContract(t *testing.T) {
	b := guiUsableBackend()
	b.battery = reading{PercentUnknown: true, Level: "正常", VoltageMV: 3900}
	b.dpiErr = errors.New("DPI读取超时")
	b.rateErr = errors.New("回报率读取失败")
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("USB/One#slot=2", b)}, nil })
	snapshot, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != version || len(snapshot.Devices) != 1 {
		t.Fatalf("snapshot %+v", snapshot)
	}
	if _, err = time.Parse(time.RFC3339, snapshot.ScannedAt); err != nil {
		t.Fatal(err)
	}
	d := snapshot.Devices[0]
	if d.ID != "USB/One#slot=2" || d.Vendor != "logitech" || d.VID != 0x046d || d.PID != 0xC548 || d.Status != "online" {
		t.Fatalf("device metadata %+v", d)
	}
	if d.Battery.Percent != nil || d.Battery.Charging != nil || d.Battery.VoltageMV == nil || *d.Battery.VoltageMV != 3900 || d.DPI.X != nil || d.Rate.Hz != nil {
		t.Fatalf("unknowns must stay null: %+v", d)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"percent":null`, `"charging":null`, `"voltageMV":3900`, `"x":null`, `"hz":null`, `"dpiRanges":[{"min":100,"max":10000,"step":50}]`, `"notes":[]`} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("missing %s in %s", fragment, data)
		}
	}
	if b.closes != 1 {
		t.Fatalf("handles closed %d times", b.closes)
	}
}

func TestGUIZeroBatteryAndFalseChargingAreValues(t *testing.T) {
	b := guiUsableBackend()
	b.battery = reading{Percent: 0, ChargeKnown: true, Charging: false}
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	r := got.Devices[0].Battery
	if r.Percent == nil || *r.Percent != 0 || r.Charging == nil || *r.Charging || r.VoltageMV != nil {
		t.Fatalf("zero/false lost: %+v", r)
	}
}

func TestGUIChargeFailureKeepsValidBattery(t *testing.T) {
	b := guiUsableBackend()
	b.battery = reading{Percent: 63, ChargeError: errors.New("充电状态未应答")}
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	r := got.Devices[0].Battery
	if r.Percent == nil || *r.Percent != 63 || r.Charging != nil || !strings.Contains(r.Error, "充电状态未应答") {
		t.Fatalf("partial reading lost: %+v", r)
	}
}

func TestGUIEachMutationReenumeratesAndDoesNotReuseDisconnectedSelection(t *testing.T) {
	a, b := guiUsableBackend(), guiUsableBackend()
	calls := 0
	s := newGUIService(func() ([]device, error) {
		calls++
		if calls == 1 {
			return []device{guiFixtureDevice("first", a)}, nil
		}
		return []device{guiFixtureDevice("second", b)}, nil
	})
	if _, err := s.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDPI("first", 1600, 1600); err == nil {
		t.Fatal("disconnected selection reused")
	}
	if calls != 2 || a.dpiCalls+b.dpiCalls != 0 || a.closes != 1 || b.closes != 1 {
		t.Fatalf("enumeration/route/close %d %+v %+v", calls, a, b)
	}
}

func TestGUINilEnumeratorReturnsErrorAndCanClose(t *testing.T) {
	s := newGUIService(nil)
	if _, err := s.Scan(); err == nil {
		t.Fatal("nil enumerator accepted")
	}
	s.Close()
	if _, err := s.Scan(); !errors.Is(err, errGUIClosed) {
		t.Fatalf("closed service: %v", err)
	}
}

func TestGUIReadFailurePreservesCauseAndNull(t *testing.T) {
	b := guiUsableBackend()
	b.batteryErr = errors.New("电池未应答")
	b.dpiErr = errors.New("DPI拒绝")
	b.rateErr = errors.New("频率超时")
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	d := got.Devices[0]
	if d.Status != "unavailable" || d.Battery.Percent != nil || d.Battery.Charging != nil || d.DPI.X != nil || d.Rate.Hz != nil || !strings.Contains(d.Battery.Error, "电池未应答") || !strings.Contains(d.DPI.Error, "DPI拒绝") {
		t.Fatalf("failure hidden: %+v", d)
	}
}

func TestGUIEmptyAndIdentifiedSnapshotsUseEmptyArrays(t *testing.T) {
	s := newGUIService(func() ([]device, error) { return nil, nil })
	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if !strings.Contains(string(data), `"devices":[]`) {
		t.Fatalf("empty devices not []: %s", data)
	}
	b := &guiBackendFixture{batteryErr: errUnsupported, dpiErr: errUnsupported, rateErr: errUnsupported}
	s = newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("unknown", b)}, nil })
	got, err = s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if got.Devices[0].Status != "identified" {
		t.Fatalf("identified status %+v", got.Devices[0])
	}
	data, _ = json.Marshal(got)
	for _, fragment := range []string{`"dpiRanges":[]`, `"pollRates":[]`, `"notes":[]`} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("missing %s in %s", fragment, data)
		}
	}
}

func TestGUIEnumerationFailureClosesPartialHandles(t *testing.T) {
	for _, action := range []string{"scan", "dpi", "rate"} {
		t.Run(action, func(t *testing.T) {
			b := guiUsableBackend()
			cause := errors.New("枚举访问被拒绝")
			s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, cause })
			var err error
			switch action {
			case "scan":
				_, err = s.Scan()
			case "dpi":
				_, err = s.SetDPI("one", 800, 800)
			case "rate":
				_, err = s.SetRate("one", 500)
			}
			if !errors.Is(err, cause) || b.closes != 1 || b.dpiCalls+b.rateCalls != 0 {
				t.Fatalf("enumeration error/close %v %+v", err, b)
			}
		})
	}
}

func TestGUIWriteRequiresExactUnambiguousDeviceID(t *testing.T) {
	for _, id := range []string{"", "one", "USB/ONE", "USB/One ", "USB/One#slot=2"} {
		t.Run(id, func(t *testing.T) {
			b := guiUsableBackend()
			s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("USB/One", b)}, nil })
			if _, err := s.SetDPI(id, 1600, 1600); err == nil {
				t.Fatal("nonexact ID accepted")
			}
			if b.dpiCalls != 0 {
				t.Fatal("unexpected setter call")
			}
			if id != "" && b.closes != 1 {
				t.Fatal("unselected handle not closed")
			}
		})
	}
	a, b := guiUsableBackend(), guiUsableBackend()
	s := newGUIService(func() ([]device, error) {
		return []device{guiFixtureDevice("same", a), guiFixtureDevice("same", b)}, nil
	})
	if _, err := s.SetRate("same", 500); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if a.rateCalls+b.rateCalls != 0 || a.closes != 1 || b.closes != 1 {
		t.Fatal("duplicate selection leaked or mutated")
	}
}

func TestGUIWritesRouteToSelectedDeviceAndRefreshDTO(t *testing.T) {
	for _, action := range []string{"dpi", "rate"} {
		t.Run(action, func(t *testing.T) {
			a, b := guiUsableBackend(), guiUsableBackend()
			s := newGUIService(func() ([]device, error) {
				return []device{guiFixtureDevice("first", a), guiFixtureDevice("second", b)}, nil
			})
			var result guiMutation
			var err error
			if action == "dpi" {
				result, err = s.SetDPI("second", 1600, 1600)
			} else {
				result, err = s.SetRate("second", 500)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed || result.Device.ID != "second" || result.Message == "" || a.dpiCalls+a.rateCalls != 0 || b.dpiCalls+b.rateCalls != 1 {
				t.Fatalf("wrong target/result %+v a=%+v b=%+v", result, a, b)
			}
			if a.closes != 1 || b.closes != 1 {
				t.Fatal("not all enumeration handles closed")
			}
			if action == "dpi" && (result.Device.DPI.X == nil || *result.Device.DPI.X != 1600) {
				t.Fatal("stale DPI in result")
			}
			if action == "rate" && (result.Device.Rate.Hz == nil || *result.Device.Rate.Hz != 500) {
				t.Fatal("stale rate in result")
			}
		})
	}
}

func TestGUISettersRetainErrorsAndNeverRetry(t *testing.T) {
	for _, action := range []string{"dpi", "rate"} {
		t.Run(action, func(t *testing.T) {
			b := guiUsableBackend()
			cause := errors.New("结果未知，不得重发")
			b.setErr = cause
			s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
			var err error
			if action == "dpi" {
				_, err = s.SetDPI("one", 1600, 1600)
			} else {
				_, err = s.SetRate("one", 500)
			}
			if !errors.Is(err, cause) || b.dpiCalls+b.rateCalls != 1 || b.closes != 1 {
				t.Fatalf("setter error/retry %v %+v", err, b)
			}
		})
	}
}

func TestGUIReusesProtocolRangeChecksAndSameValueSkip(t *testing.T) {
	for _, scenario := range []string{"off_step", "outside", "invalid_rate", "same_dpi", "same_rate"} {
		t.Run(scenario, func(t *testing.T) {
			sim := newLogiSimulator(false)
			backend, err := probeLogitech(sim, 1)
			if err != nil {
				t.Fatal(err)
			}
			s := newGUIService(func() ([]device, error) {
				return []device{{ID: "target", VID: 0x046d, PID: 0xC548, Info: modelInfo{Name: backend.Name}, Backend: backend}}, nil
			})
			var result guiMutation
			switch scenario {
			case "off_step":
				result, err = s.SetDPI("target", 825, 825)
			case "outside":
				result, err = s.SetDPI("target", 20000, 20000)
			case "invalid_rate":
				result, err = s.SetRate("target", 123)
			case "same_dpi":
				result, err = s.SetDPI("target", 800, 800)
			case "same_rate":
				result, err = s.SetRate("target", 1000)
			}
			if strings.HasPrefix(scenario, "same") {
				if err != nil || result.Changed {
					t.Fatalf("same setting: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("invalid input accepted")
			}
			if sim.writes != 0 || !backend.closed {
				t.Fatalf("protocol wrote or leaked: %d closed=%v", sim.writes, backend.closed)
			}
		})
	}
}

func TestGUIBusyAndCloseWaitForActiveOperation(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var calls atomic.Int32
	b := guiUsableBackend()
	s := newGUIService(func() ([]device, error) {
		calls.Add(1)
		close(entered)
		<-release
		return []device{guiFixtureDevice("one", b)}, nil
	})
	go func() { _, err := s.Scan(); done <- err }()
	<-entered
	if _, err := s.SetRate("one", 500); !errors.Is(err, errGUIBusy) {
		t.Fatalf("busy mutation not rejected: %v", err)
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	deadline := time.Now().Add(time.Second)
	for {
		_, err := s.Scan()
		if errors.Is(err, errGUIClosed) {
			break
		}
		if !errors.Is(err, errGUIBusy) || time.Now().After(deadline) {
			close(release)
			t.Fatalf("close failed to reject requests: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-closed:
		close(release)
		t.Fatal("Close returned before operation finished")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-closed
	if b.closes != 1 || calls.Load() != 1 {
		t.Fatalf("close/calls: %d/%d", b.closes, calls.Load())
	}
	if _, err := s.SetDPI("one", 800, 800); !errors.Is(err, errGUIClosed) {
		t.Fatalf("closed mutation accepted: %v", err)
	}
	s.Close()
}
