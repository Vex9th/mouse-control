//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	createHIDEvent   = kernel32.NewProc("CreateEventW")
	getHIDOverlapped = kernel32.NewProc("GetOverlappedResult")
	waitHIDEvents    = kernel32.NewProc("WaitForMultipleObjects")
	flushHIDQueue    = hidDLL.NewProc("HidD_FlushQueue")
)

type hidppWindowsChannel struct {
	handle syscall.Handle
	spec   hidppCollectionSpec
}
type windowsHIDPP struct {
	mu         sync.Mutex
	channels   []hidppWindowsChannel
	closed     bool
	bluetooth  bool
	flushInput func(syscall.Handle) error
}

func flushWindowsHIDInput(h syscall.Handle) error {
	r, _, e := flushHIDQueue.Call(uintptr(h))
	if r == 0 {
		return windowsError("清除 HID 输入队列", e)
	}
	return nil
}

// 内核可能在ReadFile返回后继续使用缓冲区与OVERLAPPED；直到取消完成前都固定在堆上。
type hidPending struct {
	handle   syscall.Handle
	ov       syscall.Overlapped
	buf      []byte
	n        uint32
	complete bool
	pinner   runtime.Pinner
}

func startHIDIO(h syscall.Handle, b []byte, write bool) (*hidPending, error) {
	ev, _, e := createHIDEvent.Call(0, 1, 0, 0)
	if ev == 0 {
		return nil, windowsError("创建 HID 事件", e)
	}
	p := &hidPending{handle: h, buf: b, ov: syscall.Overlapped{HEvent: syscall.Handle(ev)}}
	p.pinner.Pin(&p.ov)
	p.pinner.Pin(&p.buf[0])
	p.pinner.Pin(&p.n)
	var err error
	if write {
		err = syscall.WriteFile(h, b, &p.n, &p.ov)
	} else {
		err = syscall.ReadFile(h, b, &p.n, &p.ov)
	}
	if err == nil {
		p.complete = true
		return p, nil
	}
	if err == syscall.ERROR_IO_PENDING {
		return p, nil
	}
	p.complete = true
	p.close()
	return nil, windowsError("启动 HID 异步通信", err)
}

func (p *hidPending) finish(wait bool) error {
	if p.complete {
		return nil
	}
	w := uintptr(0)
	if wait {
		w = 1
	}
	r, _, e := getHIDOverlapped.Call(uintptr(p.handle), uintptr(unsafe.Pointer(&p.ov)), uintptr(unsafe.Pointer(&p.n)), w)
	if r == 0 {
		if e == syscall.Errno(996) {
			return e
		}
		p.complete = true
		return windowsError("完成 HID 异步通信", e)
	}
	p.complete = true
	return nil
}

func (p *hidPending) close() {
	if p == nil {
		return
	}
	if !p.complete {
		// CancelIoEx只是请求取消，必须等待完成后才能释放事件和缓冲区。
		syscall.CancelIoEx(p.handle, &p.ov)
		_ = p.finish(true)
	}
	if p.ov.HEvent != 0 {
		syscall.CloseHandle(p.ov.HEvent)
		p.ov.HEvent = 0
		p.pinner.Unpin()
	}
}

func (p *hidPending) wait(deadline time.Time) error {
	if p.complete {
		return nil
	}
	ms := deadlineMillis(deadline)
	if ms == 0 {
		return errors.New("HID++ 通信超时，设备可能休眠或离线")
	}
	r, err := syscall.WaitForSingleObject(p.ov.HEvent, ms)
	if err != nil {
		return windowsError("等待 HID 响应", err)
	}
	if r == syscall.WAIT_TIMEOUT {
		return errors.New("HID++ 通信超时，设备可能休眠或离线")
	}
	if r != 0 {
		return fmt.Errorf("等待 HID 响应返回异常状态 %d", r)
	}
	return p.finish(false)
}

func (t *windowsHIDPP) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	var errs []error
	for _, c := range t.channels {
		if e := syscall.CloseHandle(c.handle); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

func (t *windowsHIDPP) Exchange(request []byte, match func([]byte) bool, timeout time.Duration) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errors.New("HID++ 通道已关闭")
	}
	if timeout <= 0 || timeout > 30*time.Second || match == nil || len(t.channels) == 0 || len(t.channels) > 8 {
		return nil, errors.New("HID++ 通信参数无效")
	}
	deadline := time.Now().Add(timeout)
	specs := make([]hidppCollectionSpec, len(t.channels))
	for i, c := range t.channels {
		specs[i] = c.spec
	}
	writer, wire, err := hidppOutput(request, specs)
	if err != nil {
		return nil, err
	}
	flush := t.flushInput
	if flush == nil {
		flush = flushWindowsHIDInput
	}
	// 丢弃本次发送前已经排队的旧应答；不能在发送之后清队列。
	for _, c := range t.channels {
		if err := flush(c.handle); err != nil {
			return nil, err
		}
	}
	reads := make([]*hidPending, len(t.channels))
	defer func() {
		for _, p := range reads {
			p.close()
		}
	}()
	for i, c := range t.channels {
		if c.spec.InputLength < 7 || c.spec.InputLength > 4096 {
			return nil, errors.New("HID++ 输入报告长度无效")
		}
		reads[i], err = startHIDIO(c.handle, make([]byte, c.spec.InputLength), false)
		if err != nil {
			return nil, err
		}
	}
	write, err := startHIDIO(t.channels[writer].handle, wire, true)
	if err != nil {
		return nil, err
	}
	defer write.close()
	if err := write.wait(deadline); err != nil {
		return nil, err
	}
	if int(write.n) != len(wire) {
		return nil, errors.New("HID++ 指令未完整发送")
	}
	for {
		if deadlineMillis(deadline) == 0 {
			return nil, errors.New("HID++ 通信超时，设备可能休眠或离线")
		}
		ready := -1
		for i, p := range reads {
			if p.complete {
				ready = i
				break
			}
		}
		if ready < 0 {
			handles := make([]syscall.Handle, len(reads))
			for i, p := range reads {
				handles[i] = p.ov.HEvent
			}
			r, _, e := waitHIDEvents.Call(uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])), 0, uintptr(deadlineMillis(deadline)))
			if r == uintptr(syscall.WAIT_TIMEOUT) {
				return nil, errors.New("HID++ 通信超时，设备可能休眠或离线")
			}
			if r >= uintptr(len(handles)) {
				return nil, windowsError("等待 HID 通道", e)
			}
			ready = int(r)
			if err := reads[ready].finish(false); err != nil {
				return nil, err
			}
		}
		p := reads[ready]
		if p.n > uint32(len(p.buf)) {
			return nil, errors.New("HID++ 接收长度越界")
		}
		b := hidppReply(p.buf[:p.n], request[1], t.bluetooth)
		if b != nil && match(b) {
			return append([]byte(nil), b...), nil
		}
		p.close()
		c := t.channels[ready]
		reads[ready], err = startHIDIO(c.handle, make([]byte, c.spec.InputLength), false)
		if err != nil {
			return nil, err
		}
	}
}
