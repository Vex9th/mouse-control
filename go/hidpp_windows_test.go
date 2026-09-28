//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestWindowsHIDPPDiscardsQueuedReplyBeforeWrite(t *testing.T) {
	server, transport := testPipe(t)
	old := make([]byte, 20)
	copy(old, []byte{0x11, 1, 2, 0x3e, 9})
	var n uint32
	if err := syscall.WriteFile(server, old, &n, nil); err != nil {
		t.Fatal(err)
	}
	flushed := false
	transport.flushInput = func(h syscall.Handle) error {
		p, err := startHIDIO(h, make([]byte, 20), false)
		if err != nil {
			return err
		}
		defer p.close()
		if err := p.wait(time.Now().Add(time.Second)); err != nil {
			return err
		}
		if p.n != 20 || p.buf[4] != 9 {
			return errors.New("旧应答未从队列清除")
		}
		flushed = true
		return nil
	}
	done := make(chan error, 1)
	go func() {
		b := make([]byte, 20)
		var n uint32
		if err := syscall.ReadFile(server, b, &n, nil); err != nil {
			done <- err
			return
		}
		b[4] = 77
		done <- syscall.WriteFile(server, b, &n, nil)
	}()
	r, err := transport.Exchange([]byte{0x10, 1, 2, 0x3e, 0, 0, 0}, func(b []byte) bool { return b[3] == 0x3e }, time.Second)
	if err != nil || !flushed || r[4] != 77 {
		t.Fatalf("接受了旧应答：%X %v", r, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWindowsHIDPPFlushFailurePreventsWrite(t *testing.T) {
	server, transport := testPipe(t)
	want := errors.New("flush failed")
	transport.flushInput = func(syscall.Handle) error { return want }
	_, err := transport.Exchange([]byte{0x10, 1, 2, 0x3e, 0, 0, 0}, func([]byte) bool { return true }, time.Second)
	if !errors.Is(err, want) {
		t.Fatalf("原始flush错误丢失：%v", err)
	}
	var available uint32
	r, _, e := kernel32.NewProc("PeekNamedPipe").Call(uintptr(server), 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
	if r == 0 || available != 0 {
		t.Fatalf("flush失败仍发送了%d字节：%v", available, e)
	}
}

func testPipe(t *testing.T) (syscall.Handle, *windowsHIDPP) {
	t.Helper()
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\MouseTool-%d-%d`, os.Getpid(), time.Now().UnixNano()))
	h, _, e := kernel32.NewProc("CreateNamedPipeW").Call(uintptr(unsafe.Pointer(name)), 3, 0, 1, 4096, 4096, 0, 0)
	if h == uintptr(syscall.InvalidHandle) {
		t.Fatal(e)
	}
	server := syscall.Handle(h)
	client, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		syscall.CloseHandle(server)
		t.Fatal(err)
	}
	transport := &windowsHIDPP{flushInput: func(syscall.Handle) error { return nil }, channels: []hidppWindowsChannel{{handle: client, spec: hidppCollectionSpec{InputLength: 20, OutputLength: 20, OutputIDs: map[byte]bool{0x11: true}}}}}
	t.Cleanup(func() { transport.Close(); syscall.CloseHandle(server) })
	return server, transport
}

func TestWindowsHIDPPMatchesResponseAfterUnrelatedEvent(t *testing.T) {
	server, transport := testPipe(t)
	done := make(chan error, 1)
	go func() {
		b := make([]byte, 20)
		var n uint32
		if e := syscall.ReadFile(server, b, &n, nil); e != nil {
			done <- e
			return
		}
		if n != 20 || b[0] != 0x11 {
			done <- fmt.Errorf("write length %d frame %X", n, b)
			return
		}
		unrelated := append([]byte(nil), b...)
		unrelated[3] = 0
		if e := syscall.WriteFile(server, unrelated, &n, nil); e != nil {
			done <- e
			return
		}
		b[4] = 77
		done <- syscall.WriteFile(server, b, &n, nil)
	}()
	b, err := transport.Exchange([]byte{0x10, 1, 2, 0x3a, 0, 0, 0}, func(b []byte) bool { return len(b) == 20 && b[3] == 0x3a }, time.Second)
	if err != nil || b[4] != 77 {
		t.Fatalf("response %X %v", b, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWindowsHIDPPTimeoutCancelsAndAllowsNextExchange(t *testing.T) {
	_, transport := testPipe(t)
	for i := 0; i < 2; i++ {
		started := time.Now()
		_, err := transport.Exchange([]byte{0x10, 1, 2, 0x3a, 0, 0, 0}, func([]byte) bool { return true }, 25*time.Millisecond)
		if err == nil || time.Since(started) > time.Second {
			t.Fatalf("timeout/cancel failed: %v %s", err, time.Since(started))
		}
	}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Exchange([]byte{0x10, 1, 2, 0x3a, 0, 0, 0}, func([]byte) bool { return true }, time.Second); err == nil {
		t.Fatal("closed transport accepted")
	}
}
