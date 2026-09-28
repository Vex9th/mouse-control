package main

import (
	"testing"
	"time"
)

func TestHIDPPOutputSelectionWidensOnlyBeforeSending(t *testing.T) {
	short := []byte{0x10, 1, 2, 0x35, 4, 5, 6}
	caps := []hidppCollectionSpec{{InputLength: 20, OutputLength: 20, OutputIDs: map[byte]bool{0x11: true}}}
	index, b, err := hidppOutput(short, caps)
	if err != nil || index != 0 || len(b) != 20 || b[0] != 0x11 || b[6] != 6 {
		t.Fatalf("长通道适配错误：%d %X %v", index, b, err)
	}
	if short[0] != 0x10 {
		t.Fatal("修改了调用者请求")
	}
	caps = append(caps, hidppCollectionSpec{OutputLength: 7, OutputIDs: map[byte]bool{0x10: true}})
	i, b, err := hidppOutput(short, caps)
	if err != nil || i != 1 || len(b) != 7 {
		t.Fatalf("没有优先同类型报告：%d %X %v", i, b, err)
	}
	if _, _, err := hidppOutput(make([]byte, 20), caps); err == nil {
		t.Fatal("非法reportID被接受")
	}
	if _, _, err := hidppOutput([]byte{0x10, 1}, caps); err == nil {
		t.Fatal("截断报告被接受")
	}
}

func TestHIDPPPacketLengthAndDeadline(t *testing.T) {
	for _, tc := range []struct {
		id   byte
		size int
	}{{0x10, 7}, {0x11, 20}, {0x12, 64}} {
		b := make([]byte, 64)
		b[0] = tc.id
		if got := hidppCanonical(b); len(got) != tc.size {
			t.Fatalf("report %X长%d", tc.id, len(got))
		}
		if hidppCanonical(b[:tc.size-1]) != nil {
			t.Fatal("截断报告被接受")
		}
	}
	if hidppCanonical([]byte{0, 0, 0, 0, 0, 0, 0}) != nil {
		t.Fatal("未知报告被接受")
	}
	if deadlineMillis(time.Now().Add(-time.Second)) != 0 {
		t.Fatal("超时deadline被延长")
	}
	if n := deadlineMillis(time.Now().Add(100 * time.Millisecond)); n < 1 || n > 100 {
		t.Fatalf("deadline %d", n)
	}
}

func TestHIDPPBluetoothAddressNormalizationIsNarrow(t *testing.T) {
	b := []byte{0x10, 0, 2, 0x38, 1, 2, 3}
	got := hidppReply(b, 0xff, true)
	if got[1] != 0xff || b[1] != 0 {
		t.Fatal("蓝牙直连地址未规范化或修改了原始报文")
	}
	for _, tc := range []struct {
		address   byte
		bluetooth bool
	}{{0xff, false}, {1, true}, {1, false}} {
		if got := hidppReply(b, tc.address, tc.bluetooth); got[1] != 0 {
			t.Fatal("接受了 USB 或接收器的错误目标地址")
		}
	}
	if hidppReply(b[:2], 0xff, true) != nil {
		t.Fatal("接受了截断蓝牙报文")
	}
}

func TestHIDPPUsageIncludesDocumentedBluetoothChannel(t *testing.T) {
	for _, tc := range []struct {
		page, usage uint16
		bt, want    bool
	}{
		{0xff00, 1, false, true}, {0xff43, 0x0202, true, true},
		{0xff43, 0x0202, false, false}, {0xff43, 1, true, false}, {0xff01, 0x0202, true, false}, {1, 2, false, false},
	} {
		if got := hidppUsage(tc.page, tc.usage, tc.bt); got != tc.want {
			t.Fatalf("usage %04X/%04X: %t", tc.page, tc.usage, got)
		}
	}
}
