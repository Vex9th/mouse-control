//go:build gui && windows

package main

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestDesktopNativeClientSizeAndMinimum(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPI := desktopIntegrationDPI(t)
	defer restoreDPI()
	previous := desktopActive
	w := &desktopWindow{}
	desktopActive = w
	defer func() {
		if w.hwnd != 0 {
			desktopUser32.NewProc("DestroyWindow").Call(w.hwnd)
		}
		instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
		desktopUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(desktopText("MouseControlWindow"))), instance)
		var message desktopMessage
		for {
			present, _, _ := desktopUser32.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0x12, 0x12, 1)
			if present == 0 {
				break
			}
		}
		desktopActive = previous
	}()
	if err := w.createWindow(); err != nil {
		t.Fatal(err)
	}
	dpi, _, _ := desktopUser32.NewProc("GetDpiForWindow").Call(w.hwnd)
	monitor, _, _ := desktopUser32.NewProc("MonitorFromWindow").Call(w.hwnd, 2)
	var info struct {
		Size          uint32
		Monitor, Work [4]int32
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if result, _, err := desktopUser32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); result == 0 {
		t.Fatal(err)
	}
	// 独立用 Win32 换算预期值；不借生产布局函数计算测试答案。
	outerSize := func(width, height int32) (int32, int32, int32, int32) {
		clientW, clientH := int32((int64(width)*int64(dpi)+48)/96), int32((int64(height)*int64(dpi)+48)/96)
		r := [4]int32{0, 0, clientW, clientH}
		if result, _, err := desktopUser32.NewProc("AdjustWindowRectExForDpi").Call(uintptr(unsafe.Pointer(&r)), 0x00cf0000, 0, 0, dpi); result == 0 {
			t.Fatal(err)
		}
		outerW, outerH := r[2]-r[0], r[3]-r[1]
		fitW, fitH := min(outerW, info.Work[2]-info.Work[0]), min(outerH, info.Work[3]-info.Work[1])
		return fitW, fitH, clientW - (outerW - fitW), clientH - (outerH - fitH)
	}
	var client [4]int32
	if result, _, err := desktopUser32.NewProc("GetClientRect").Call(w.hwnd, uintptr(unsafe.Pointer(&client))); result == 0 {
		t.Fatal(err)
	}
	_, _, wantW, wantH := outerSize(900, 480)
	if client[2] != wantW || client[3] != wantH {
		t.Errorf("默认客户区没有按900×480 DIP与工作区换算：DPI=%d actual=%v want=%dx%d", dpi, client, wantW, wantH)
	}
	var minimum [10]int32
	desktopUser32.NewProc("SendMessageW").Call(w.hwnd, 0x24, 0, uintptr(unsafe.Pointer(&minimum)))
	wantMinW, wantMinH, _, _ := outerSize(760, 440)
	if minimum[6] != wantMinW || minimum[7] != wantMinH {
		t.Errorf("最小外框没有按760×440 DIP设置：DPI=%d actual=%dx%d want=%dx%d", dpi, minimum[6], minimum[7], wantMinW, wantMinH)
	}
	if result, _, err := desktopUser32.NewProc("SetWindowPos").Call(w.hwnd, 0, 0, 0, 100, 100, 0x16); result == 0 {
		t.Fatal(err)
	}
	var minimumClient [4]int32
	if result, _, err := desktopUser32.NewProc("GetClientRect").Call(w.hwnd, uintptr(unsafe.Pointer(&minimumClient))); result == 0 {
		t.Fatal(err)
	}
	_, _, minClientW, minClientH := outerSize(760, 440)
	if minimumClient[2] != minClientW || minimumClient[3] != minClientH {
		t.Errorf("实际窗口被缩到可用客户区以下：got=%v want=%dx%d", minimumClient, minClientW, minClientH)
	}
	if w.layoutErr != nil {
		t.Fatal(w.layoutErr)
	}
	t.Logf("真实窗口尺寸：DPI=%d client=%v work=%v minimum=%dx%d", dpi, client, info.Work, minimum[6], minimum[7])
}
