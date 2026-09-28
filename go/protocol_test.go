package main

import (
	"errors"
	"testing"
	"time"
)

func response(status, cmd, value byte) [91]byte {
	var b [91]byte
	b[1], b[2], b[6], b[7], b[8], b[10] = status, 0x1f, 2, 7, cmd, value
	for i := 3; i <= 88; i++ {
		b[89] ^= b[i]
	}
	return b
}

func TestBatteryScaleIsContinuous(t *testing.T) {
	for _, tc := range []struct {
		raw  byte
		want int
	}{{0, 0}, {100, 39}, {101, 40}, {128, 50}, {254, 100}, {255, 100}} {
		if got := batteryPercent(tc.raw); got != tc.want {
			t.Errorf("raw=%d: got %d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestFrameRejectsFalseSuccess(t *testing.T) {
	for _, tc := range []struct {
		status byte
		want   error
	}{{3, errRejected}, {4, errAsleep}, {5, errUnsupported}} {
		_, err := decodeResponse(response(tc.status, 0x80, 255), 0x80)
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d accepted/wrong error: %v", tc.status, err)
		}
	}
	b := response(2, 0x80, 255)
	if v, err := decodeResponse(b, 0x80); err != nil || v != 255 {
		t.Fatalf("valid: %d %v", v, err)
	}
	b[89] ^= 1
	if _, err := decodeResponse(b, 0x80); err == nil {
		t.Fatal("corrupt response accepted")
	}
	if _, err := decodeResponse(response(2, 0x84, 1), 0x80); err == nil {
		t.Fatal("wrong command accepted")
	}
	b = response(2, 0x80, 255)
	b[6] = 0
	b[89] ^= 2
	if _, err := decodeResponse(b, 0x80); err == nil {
		t.Fatal("missing argument accepted")
	}
}

func TestRequestLayout(t *testing.T) {
	b := makeRequest(0x9f, 0x80)
	if b[0] != 0 || b[2] != 0x9f || b[6] != 2 || b[7] != 7 || b[8] != 0x80 || b[89] != 0x85 {
		t.Fatalf("wrong request: %x", b)
	}
}

func TestBusyReplyWithValidPayloadMatchesOpenRazer(t *testing.T) {
	v, err := decodeResponse(response(1, 0x80, 128), 0x80)
	if err != nil || v != 128 {
		t.Fatalf("OpenRazer认可带有效载荷的Busy响应：%d %v", v, err)
	}
}

type testChannel struct {
	calls      int
	failCharge bool
	timedOut   bool
	waits      []time.Duration
	requests   [][91]byte
}

func (c *testChannel) Exchange(b [91]byte, wait time.Duration) ([91]byte, error) {
	c.calls++
	c.waits = append(c.waits, wait)
	c.requests = append(c.requests, b)
	if c.timedOut {
		return response(4, b[8], 0), nil
	}
	if c.failCharge && b[8] == 0x84 {
		return response(5, 0x84, 0), nil
	}
	if b[8] == 0x84 {
		return response(2, 0x84, 1), nil
	}
	return response(2, 0x80, 100), nil
}
func (*testChannel) Close() error { return nil }

func TestChargeFailureDoesNotBecomeDischarging(t *testing.T) {
	c := &testChannel{failCharge: true}
	d := device{Info: modelInfo{Battery: true, TID: 0x9f, WaitMillis: 405}, Channels: []featureChannel{c}}
	r, err := readDevice(d)
	if err != nil || r.Percent != 39 || r.ChargeKnown || !errors.Is(r.ChargeError, errUnsupported) {
		t.Fatalf("reading=%+v err=%v", r, err)
	}
	if c.calls != 2 || c.waits[0] != 405*time.Millisecond || c.requests[0][2] != 0x9f {
		t.Fatalf("wrong model protocol: %+v", c)
	}
}
func TestUnsupportedDeviceNeverReceivesQuery(t *testing.T) {
	c := &testChannel{}
	_, err := readDevice(device{Info: modelInfo{Battery: false}, Channels: []featureChannel{c}})
	if !errors.Is(err, errUnsupported) || c.calls != 0 {
		t.Fatalf("unsupported device queried: %d %v", c.calls, err)
	}
}
func TestTimeoutRetriesAreBoundedAndNotZeroBattery(t *testing.T) {
	c := &testChannel{timedOut: true}
	_, err := readDevice(device{Info: modelInfo{Battery: true, TID: 0x1f}, Channels: []featureChannel{c}})
	if !errors.Is(err, errAsleep) || c.calls != 6 {
		t.Fatalf("timeout result: calls=%d err=%v", c.calls, err)
	}
}
func TestNoChargeCommandForExplicitlyUnsupportedModels(t *testing.T) {
	c := &testChannel{}
	r, err := readDevice(device{Info: models[0x0062], Channels: []featureChannel{c}})
	if err != nil || r.Percent != 39 || c.calls != 1 {
		t.Fatalf("unexpected charge query: calls=%d read=%+v err=%v", c.calls, r, err)
	}
}

func TestMultiPacketResponseIsNotAcceptedAsSingleValue(t *testing.T) {
	b := response(2, 0x80, 255)
	b[4] = 1
	b[89] ^= 1
	if _, err := decodeResponse(b, 0x80); err == nil {
		t.Fatal("剩余分包数不匹配的响应被当作电量")
	}
}
