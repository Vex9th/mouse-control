package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type logiTransportFunc func([]byte, func([]byte) bool, time.Duration) ([]byte, error)

func (f logiTransportFunc) Exchange(q []byte, match func([]byte) bool, d time.Duration) ([]byte, error) {
	return f(q, match, d)
}
func (f logiTransportFunc) Close() error { return nil }

func TestLogitechPairingLayouts(t *testing.T) {
	for _, family := range []string{"unifying", "bolt"} {
		t.Run(family, func(t *testing.T) {
			calls := 0
			transport := logiTransportFunc(func(q []byte, match func([]byte) bool, _ time.Duration) ([]byte, error) {
				calls++
				if len(q) != 7 || q[0] != 0x10 || q[1] != 0xFF || q[2] != 0x83 || q[3] != 0xB5 {
					t.Fatalf("invalid read request % X", q)
				}
				r := make([]byte, 20)
				copy(r, []byte{0x11, 0xFF, 0x83, 0xB5})
				p := r[4:]
				p[0] = q[4]
				switch q[4] {
				case 0x22:
					p[3], p[4], p[7] = 0x40, 0x74, 2
				case 0x32:
					copy(p[1:], []byte{1, 2, 3, 4})
				case 0x42:
					p[1] = 5
					copy(p[2:], "Mouse")
				case 0x53:
					p[1], p[2], p[3] = 8, 0x74, 0x40
					copy(p[4:], []byte{1, 2, 3, 4})
				case 0x63:
					if q[5] != 1 {
						t.Fatal("Bolt name block must be 1")
					}
					p[2] = 5
					copy(p[3:], "Mouse")
				default:
					t.Fatalf("wrong selector %02X", q[4])
				}
				wrong := append([]byte(nil), r...)
				wrong[4]++
				if match(wrong) {
					t.Fatal("accepted other slot selector")
				}
				if !match(r) {
					t.Fatal("rejected correct selector")
				}
				return r, nil
			})
			p, err := readLogitechPairing(transport, 3, family)
			if err != nil || !p.Paired || p.Name != "Mouse" || !strings.Contains(p.Identity, "01020304") {
				t.Fatalf("pairing: %+v %v", p, err)
			}
			want := "鼠标"
			if family == "bolt" {
				want = "轨迹球"
			}
			if p.Kind != want {
				t.Fatalf("kind=%s", p.Kind)
			}
			if calls < 2 {
				t.Fatal("name not queried")
			}
		})
	}
}

func TestLogitechPairingErrorsStayUnknown(t *testing.T) {
	errTimeout := errors.New("pairing timeout")
	tr := logiTransportFunc(func([]byte, func([]byte) bool, time.Duration) ([]byte, error) { return nil, errTimeout })
	_, err := readLogitechPairing(tr, 1, "nano")
	if !errors.Is(err, errTimeout) {
		t.Fatalf("lost timeout %v", err)
	}
}

func TestLogitechPingNonceAndNoTimeoutFallback(t *testing.T) {
	calls := 0
	tr := logiTransportFunc(func(q []byte, match func([]byte) bool, _ time.Duration) ([]byte, error) {
		calls++
		r := []byte{0x10, q[1], 0, q[3], 2, 0, q[6] ^ 0x80}
		if match(r) {
			t.Fatal("wrong ping nonce accepted")
		}
		return nil, errors.New("ping timeout")
	})
	_, err := probeLogitech(tr, 1)
	if err == nil || !strings.Contains(err.Error(), "ping timeout") || calls != 1 {
		t.Fatalf("timeout fallback: %v calls=%d", err, calls)
	}
}
