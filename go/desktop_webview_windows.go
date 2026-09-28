//go:build gui && windows

package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// go-webview2 未公开这些方法；序号来自 Microsoft WebView2 SDK 1.0.2739.15。
// 仅适配稳定的 ICoreWebView2 COM ABI，不访问绑定库的私有 Go 字段。
//
//go:uintptrescapes
func desktopCOM(object unsafe.Pointer, slot uintptr, args ...uintptr) uintptr {
	table := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(table, slot*unsafe.Sizeof(uintptr(0))))
	call := append([]uintptr{uintptr(object)}, args...)
	result, _, _ := syscall.SyscallN(method, call...)
	runtime.KeepAlive(object)
	return result
}

func desktopHRESULT(action string, result uintptr) error {
	if int32(uint32(result)) < 0 {
		return fmt.Errorf("%s失败（HRESULT 0x%08X）", action, uint32(result))
	}
	return nil
}

func desktopCOMString(object unsafe.Pointer, slot uintptr) (string, error) {
	var value *uint16
	result := desktopCOM(object, slot, uintptr(unsafe.Pointer(&value)))
	if value != nil {
		defer windows.CoTaskMemFree(unsafe.Pointer(value))
	}
	if err := desktopHRESULT("读取页面地址", result); err != nil {
		return "", err
	}
	return windows.UTF16PtrToString(value), nil
}

var (
	desktopIUnknown      = guid{0, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	desktopNavigationIID = guid{0x9adbe429, 0xf36d, 0x432b, [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3}}
	desktopNewWindowIID  = guid{0xd4c185fe, 0xc81c, 0x4989, [8]byte{0x97, 0xaf, 0x2d, 0x3f, 0xa7, 0xab, 0x56, 0x51}}
)

type desktopEvent struct {
	vtable *[4]uintptr
	iid    guid
	refs   atomic.Uint32
	invoke func(uintptr, unsafe.Pointer) uintptr
	pin    runtime.Pinner
}

func newDesktopEvent(iid guid, invoke func(unsafe.Pointer) uintptr) *desktopEvent {
	return newDesktopCallback(iid, func(_ uintptr, args unsafe.Pointer) uintptr { return invoke(args) })
}

func newDesktopCallback(iid guid, invoke func(uintptr, unsafe.Pointer) uintptr) *desktopEvent {
	h := &desktopEvent{iid: iid, invoke: invoke}
	h.vtable = &[4]uintptr{
		syscall.NewCallback(func(this *desktopEvent, riid *guid, out *unsafe.Pointer) uintptr {
			if out == nil || riid == nil {
				return 0x80004003
			}
			*out = nil
			requested := *riid
			if requested != desktopIUnknown && requested != h.iid {
				return 0x80004002
			}
			*out = unsafe.Pointer(this)
			h.refs.Add(1)
			return 0
		}),
		syscall.NewCallback(func(uintptr) uintptr { return uintptr(h.refs.Add(1)) }),
		syscall.NewCallback(func(uintptr) uintptr {
			n := h.refs.Add(^uint32(0))
			if n == 0 {
				h.pin.Unpin()
			}
			return uintptr(n)
		}),
		syscall.NewCallback(func(_ uintptr, first uintptr, second unsafe.Pointer) uintptr { return h.invoke(first, second) }),
	}
	h.refs.Store(1)
	h.pin.Pin(h)
	h.pin.Pin(h.vtable)
	return h
}

func (h *desktopEvent) Release() { desktopCOM(unsafe.Pointer(h), 2) }

type desktopBoundary struct {
	desktopNavigationGate
	core                            unsafe.Pointer
	navigation, newWindow           *desktopEvent
	navigationToken, newWindowToken int64
	navigationAdded, newWindowAdded bool
}

func newDesktopBoundary(controller unsafe.Pointer, document string) (*desktopBoundary, error) {
	b := &desktopBoundary{desktopNavigationGate: newDesktopNavigationGate(document)}
	if err := desktopHRESULT("获取浏览器接口", desktopCOM(controller, 25, uintptr(unsafe.Pointer(&b.core)))); err != nil {
		return nil, err
	}
	if b.core == nil {
		return nil, fmt.Errorf("浏览器接口不可用")
	}
	b.navigation = newDesktopEvent(desktopNavigationIID, func(args unsafe.Pointer) uintptr {
		uri, err := desktopCOMString(args, 3)
		if err == nil && b.Allow(uri) {
			return 0
		}
		return desktopCOM(args, 8, 1)
	})
	b.newWindow = newDesktopEvent(desktopNewWindowIID, func(args unsafe.Pointer) uintptr {
		return desktopCOM(args, 6, 1)
	})
	if err := desktopHRESULT("限制页面导航", desktopCOM(b.core, 7, uintptr(unsafe.Pointer(b.navigation)), uintptr(unsafe.Pointer(&b.navigationToken)))); err != nil {
		b.Close()
		return nil, err
	}
	b.navigationAdded = true
	if err := desktopHRESULT("限制新窗口", desktopCOM(b.core, 44, uintptr(unsafe.Pointer(b.newWindow)), uintptr(unsafe.Pointer(&b.newWindowToken)))); err != nil {
		b.Close()
		return nil, err
	}
	b.newWindowAdded = true
	return b, nil
}

func (b *desktopBoundary) Trusted() bool {
	if b == nil || b.core == nil || b.initialNavigation {
		return false
	}
	source, err := desktopCOMString(b.core, 4)
	return err == nil && source == "about:blank"
}

func (b *desktopBoundary) Close() {
	if b == nil || b.core == nil {
		return
	}
	if b.navigationAdded {
		desktopCOM(b.core, 8, uintptr(b.navigationToken))
	}
	if b.newWindowAdded {
		desktopCOM(b.core, 45, uintptr(b.newWindowToken))
	}
	desktopCOM(b.core, 2)
	b.core = nil
	if b.navigation != nil {
		b.navigation.Release()
	}
	if b.newWindow != nil {
		b.newWindow.Release()
	}
}
