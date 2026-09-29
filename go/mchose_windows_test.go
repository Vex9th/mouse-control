//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// named pipe 验证异步 I/O 生命周期，不是鼠标固件或 HID 描述符兼容证据。
func testMchosePipe(t *testing.T) (syscall.Handle, *mchoseWindows) {
	t.Helper()
	server, original := testPipe(t)
	h := original.channels[0].handle
	original.channels = nil
	transport := &mchoseWindows{handle: h, flushInput: func(syscall.Handle) error { return nil }}
	t.Cleanup(func() { transport.Close() })
	return server, transport
}
func TestWindowsMchoseTimeoutCancelsAndAllowsNextExchange(t *testing.T) {
	_, tr := testMchosePipe(t)
	q, _ := mchoseReadRequest(0x0900, nil)
	for i := 0; i < 2; i++ {
		start := time.Now()
		_, e := tr.Exchange(q, func([]byte) bool { return true }, 25*time.Millisecond)
		if e == nil || time.Since(start) > time.Second {
			t.Fatalf("超时取消失效 %v %s", e, time.Since(start))
		}
	}
	if e := tr.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := tr.Exchange(q, func([]byte) bool { return true }, time.Second); e == nil {
		t.Fatal("已关闭仍允许读取")
	}
}
func TestWindowsMchoseShortReadReachesProtocolValidation(t *testing.T) {
	server, tr := testMchosePipe(t)
	done := make(chan error, 1)
	go func() {
		q := make([]byte, 64)
		var n uint32
		if e := syscall.ReadFile(server, q, &n, nil); e != nil {
			done <- e
			return
		}
		if n != 64 || binary.LittleEndian.Uint16(q[4:6]) != 0x0900 {
			done <- fmt.Errorf("错误请求 %x", q)
			return
		}
		r := mchoseFixture(0x0900, make([]byte, 14))
		done <- syscall.WriteFile(server, r[:8], &n, nil)
	}()
	_, e := mchoseQuery(tr, 0x0900, nil)
	if !errors.Is(e, errFrame) {
		t.Fatalf("短读未保留格式错误 %v", e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestWindowsMchoseUnrelatedEventsDoNotExtendDeadline(t *testing.T) {
	server, tr := testMchosePipe(t)
	done := make(chan error, 1)
	go func() {
		q := make([]byte, 64)
		var n uint32
		if e := syscall.ReadFile(server, q, &n, nil); e != nil {
			done <- e
			return
		}
		for i := 0; i < 50; i++ {
			if e := syscall.WriteFile(server, mchoseFixture(0x0600, []byte{1, 2}), &n, nil); e != nil {
				done <- e
				return
			}
			time.Sleep(time.Millisecond)
		}
		done <- nil
	}()
	q, _ := mchoseReadRequest(0x0900, nil)
	start := time.Now()
	_, e := tr.Exchange(q, func(b []byte) bool { return mchoseReplyMatches(0x0900, b) }, 25*time.Millisecond)
	if e == nil || time.Since(start) > time.Second {
		t.Fatalf("无关事件延长截止时间 %v %s", e, time.Since(start))
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestWindowsMchoseFlushFailurePreventsSending(t *testing.T) {
	server, tr := testMchosePipe(t)
	cause := errors.New("测试 flush 失败")
	tr.flushInput = func(syscall.Handle) error { return cause }
	q, _ := mchoseReadRequest(0x0900, nil)
	if _, e := tr.Exchange(q, func([]byte) bool { return true }, time.Second); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	var available uint32
	r, _, e := kernel32.NewProc("PeekNamedPipe").Call(uintptr(server), 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
	if r == 0 || available != 0 {
		t.Fatalf("flush 失败仍发送 %d 字节 %v", available, e)
	}
}
