//go:build gui && windows

package main

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestDesktopDPIContextRestoresOriginal(t *testing.T) {
	for _, initial := range []struct {
		name    string
		context uintptr
	}{{"unaware", ^uintptr(0)}, {"per-monitor-v1", ^uintptr(2)}, {"already-per-monitor-v2", ^uintptr(3)}} {
		t.Run(initial.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			set := desktopUser32.NewProc("SetThreadDpiAwarenessContext")
			original, _, setErr := set.Call(initial.context)
			if original == 0 {
				t.Fatalf("设置测试初始线程 DPI：%v", setErr)
			}
			defer func() {
				if result, _, err := set.Call(original); result == 0 {
					t.Errorf("还原测试线程原始 DPI：%v", err)
				}
			}()
			dpi, err := beginDesktopDPI()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := dpi.Close(); err != nil {
					t.Error(err)
				}
			}()
			current, _, _ := desktopUser32.NewProc("GetThreadDpiAwarenessContext").Call()
			equal, _, _ := desktopUser32.NewProc("AreDpiAwarenessContextsEqual").Call(current, ^uintptr(3))
			if equal == 0 {
				t.Fatalf("共享 DPI 初始化未进入 PMv2：context=0x%x", current)
			}
			// 使用系统 STATIC 窗口类，验证真实 HWND 继承调用线程的 PMv2。
			hwnd, _, createErr := desktopUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(desktopText("STATIC"))), uintptr(unsafe.Pointer(desktopText("DPI test"))), 0, 0, 0, 320, 180, 0, 0, 0, 0)
			if hwnd == 0 {
				t.Fatalf("创建隐藏 DPI 测试窗口：%v", createErr)
			}
			windowContext, _, _ := desktopUser32.NewProc("GetWindowDpiAwarenessContext").Call(hwnd)
			windowEqual, _, _ := desktopUser32.NewProc("AreDpiAwarenessContextsEqual").Call(windowContext, ^uintptr(3))
			windowDPI, _, _ := desktopUser32.NewProc("GetDpiForWindow").Call(hwnd)
			destroyed, _, destroyErr := desktopUser32.NewProc("DestroyWindow").Call(hwnd)
			if destroyed == 0 {
				t.Errorf("关闭隐藏 DPI 测试窗口：%v", destroyErr)
			}
			if windowEqual == 0 || windowDPI == 0 {
				t.Errorf("窗口没有继承 PMv2：context=0x%x equalPMv2=%d DPI=%d", windowContext, windowEqual, windowDPI)
			}
			if err := dpi.Close(); err != nil {
				t.Fatal(err)
			}
			restored, _, _ := desktopUser32.NewProc("GetThreadDpiAwarenessContext").Call()
			restoredEqual, _, _ := desktopUser32.NewProc("AreDpiAwarenessContextsEqual").Call(restored, initial.context)
			if restoredEqual == 0 {
				t.Fatalf("UI 资源关闭后未恢复原线程 DPI：context=0x%x initial=0x%x", restored, initial.context)
			}
			t.Logf("真实 DPI 生命周期：initial=%s thread=0x%x window=0x%x windowDPI=%d restored=0x%x", initial.name, current, windowContext, windowDPI, restored)
		})
	}
}

func TestDesktopDPIContextRejectsRestoreOnAnotherThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dpi, err := beginDesktopDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := dpi.Close(); err != nil {
			t.Error(err)
		}
	}()
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- dpi.Close()
	}()
	if err := <-done; err == nil {
		t.Fatal("跨线程恢复被错误地接受")
	}
}
