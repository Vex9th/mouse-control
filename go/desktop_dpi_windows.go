//go:build gui && windows

package main

import (
	"errors"
	"fmt"
	"syscall"
)

var (
	desktopSetThreadDPI = desktopUser32.NewProc("SetThreadDpiAwarenessContext")
	desktopGetThreadDPI = desktopUser32.NewProc("GetThreadDpiAwarenessContext")
	desktopDPIEqual     = desktopUser32.NewProc("AreDpiAwarenessContextsEqual")
)

type desktopDPIContext struct {
	previous uintptr
	threadID uintptr
}

// 调用方须锁定 OS 线程，并在全部窗口、WebView2 和 COM 资源释放后 Close。
// 进程 DPI 可能已由启动环境设定；只切换 UI 线程，不修改进程或系统设置。
func beginDesktopDPI() (*desktopDPIContext, error) {
	for _, proc := range []*syscall.LazyProc{desktopSetThreadDPI, desktopGetThreadDPI, desktopDPIEqual} {
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("系统缺少界面 DPI API %s：%w", proc.Name, err)
		}
	}
	threadID, _, _ := kernel32.NewProc("GetCurrentThreadId").Call()
	previous, _, err := desktopSetThreadDPI.Call(^uintptr(3)) // PMv2，完整指针位宽的 -4。
	if previous == 0 {
		return nil, windowsError("设置界面线程 PMv2 DPI", err)
	}
	context := &desktopDPIContext{previous: previous, threadID: threadID}
	current, _, _ := desktopGetThreadDPI.Call()
	equal, _, _ := desktopDPIEqual.Call(current, ^uintptr(3))
	if equal == 0 {
		return nil, errors.Join(fmt.Errorf("界面线程未进入 PMv2 DPI：context=0x%x", current), context.Close())
	}
	return context, nil
}

func (c *desktopDPIContext) Close() error {
	if c == nil || c.previous == 0 {
		return nil
	}
	threadID, _, _ := kernel32.NewProc("GetCurrentThreadId").Call()
	if threadID != c.threadID {
		return fmt.Errorf("必须在原界面线程恢复 DPI：current=%d owner=%d", threadID, c.threadID)
	}
	if result, _, err := desktopSetThreadDPI.Call(c.previous); result == 0 {
		return windowsError("恢复界面线程 DPI", err)
	}
	current, _, _ := desktopGetThreadDPI.Call()
	equal, _, _ := desktopDPIEqual.Call(current, c.previous)
	if equal == 0 {
		return fmt.Errorf("界面线程 DPI 恢复后不匹配：current=0x%x previous=0x%x", current, c.previous)
	}
	c.previous = 0
	return nil
}
