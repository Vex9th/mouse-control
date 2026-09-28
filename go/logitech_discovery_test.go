package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type logiReceiverSimulator struct {
	slots   map[byte]*logiSimulator
	visited map[byte]bool
	closes  int
}

func (s *logiReceiverSimulator) Close() error { s.closes++; return nil }
func (s *logiReceiverSimulator) Exchange(q []byte, match func([]byte) bool, timeout time.Duration) ([]byte, error) {
	// 接收器配对寄存器不可用，在线槽位必须仍由只读 ping 发现。
	if q[1] == 0xff {
		r := []byte{0x10, 0xff, 0x8f, q[2], q[3], 2, 0}
		if !match(r) {
			return nil, fmt.Errorf("配对错误帧被拒绝")
		}
		return r, nil
	}
	s.visited[q[1]] = true
	if slot := s.slots[q[1]]; slot != nil {
		return slot.Exchange(q, match, timeout)
	}
	r := []byte{0x10, q[1], 0x8f, q[2], q[3], 8, 0}
	if !match(r) {
		return nil, fmt.Errorf("空槽错误帧被拒绝")
	}
	return r, nil
}

func TestLogitechDiscoveryMultipleMiceSparseSlotsAndKeyboard(t *testing.T) {
	a, b, key := newLogiSimulator(false), newLogiSimulator(true), newLogiSimulator(false)
	b.unit = 9
	key.kind = 0
	s := &logiReceiverSimulator{slots: map[byte]*logiSimulator{1: a, 3: key, 7: b}, visited: map[byte]bool{}}
	g := &logitechHIDGroup{id: "usb:receiverA", pid: 0xc539, connection: "lightspeed 接收器"}
	r, _ := logitechReceiver(g.pid)
	devices := probeLogitechGroup(g, r, true, s)
	if len(devices) != 2 || !strings.Contains(devices[0].ID, "#slot=1#id=unit:") || !strings.Contains(devices[1].ID, "#slot=7#id=unit:") {
		t.Fatalf("多设备/槽位7/键盘筛选错误：%+v", devices)
	}
	if len(s.visited) != 7 || s.closes != 0 {
		t.Fatalf("扫描范围或句柄生命周期错误：%+v", s)
	}
	for _, d := range devices {
		if v, _, err := readMouseDPI(d); err != nil || v.X != 800 {
			t.Fatalf("槽位路由读取失败：%+v %v", v, err)
		}
	}
	if _, _, err := setMouseDPI(devices[1], dpiValue{1600, 1600}); err != nil {
		t.Fatal(err)
	}
	if a.writes != 0 || b.writes != 1 || key.writes != 0 {
		t.Fatal("设置发送到了错误的子设备")
	}
	closeDevices(devices)
}

func TestLogitechDiscoveryNoOnlineMouseClosesTransport(t *testing.T) {
	s := &logiReceiverSimulator{slots: map[byte]*logiSimulator{}, visited: map[byte]bool{}}
	g := &logitechHIDGroup{id: "usb:receiverB", pid: 0xc548, name: "Bolt", connection: "bolt 接收器"}
	r, _ := logitechReceiver(g.pid)
	devices := probeLogitechGroup(g, r, true, s)
	if len(devices) != 1 || devices[0].Backend != nil || s.closes != 1 || len(s.visited) != 6 {
		t.Fatalf("空接收器/释放错误：%+v %+v", devices, s)
	}
}

func TestLogitechReceiverCatalogProtocolBoundaries(t *testing.T) {
	for _, pid := range []uint16{0xc513, 0xc517, 0xc51b, 0xc70a, 0xc71f} {
		if r, ok := logitechReceiver(pid); !ok || r.Slots != 0 {
			t.Fatalf("旧协议不应扫描现代槽位：%04X %+v", pid, r)
		}
	}
	if _, ok := logitechReceiver(0xc088); ok {
		t.Fatal("把直连 G Pro Wireless 当成接收器")
	}
	for pid, r := range logitechReceivers {
		if r.Slots > 7 || r.Name == "" || r.Family == "" {
			t.Fatalf("接收器目录错误：%04X %+v", pid, r)
		}
	}
}
