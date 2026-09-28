//go:build gui && windows

package main

import (
	"fmt"
	"unsafe"
)

const desktopWindowStyle = 0x00cf0000

func desktopOuterSize(width, height int32, dpi uint32) ([2]int32, error) {
	w, h, err := desktopClientPixels(width, height, dpi)
	if err != nil {
		return [2]int32{}, err
	}
	r := [4]int32{0, 0, w, h}
	if result, _, err := desktopUser32.NewProc("AdjustWindowRectExForDpi").Call(uintptr(unsafe.Pointer(&r)), desktopWindowStyle, 0, 0, uintptr(dpi)); result == 0 {
		return [2]int32{}, windowsError("计算窗口边框尺寸", err)
	}
	return [2]int32{r[2] - r[0], r[3] - r[1]}, nil
}

func desktopWindowDPI(hwnd uintptr) (uint32, error) {
	dpi, _, _ := desktopUser32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		return 0, fmt.Errorf("无法读取窗口 DPI：hwnd=0x%x", hwnd)
	}
	return uint32(dpi), nil
}

func desktopMonitorWork(monitor uintptr) ([4]int32, error) {
	var info struct {
		Size          uint32
		Monitor, Work [4]int32
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if result, _, err := desktopUser32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); result == 0 {
		return [4]int32{}, windowsError("读取窗口所在显示器工作区", err)
	}
	return info.Work, nil
}

func desktopWindowWork(hwnd uintptr) ([4]int32, error) {
	monitor, _, _ := desktopUser32.NewProc("MonitorFromWindow").Call(hwnd, 2)
	return desktopMonitorWork(monitor)
}

func desktopSetClientSize(hwnd uintptr, width, height int32) error {
	dpi, err := desktopWindowDPI(hwnd)
	if err != nil {
		return err
	}
	size, err := desktopOuterSize(width, height, dpi)
	if err != nil {
		return err
	}
	work, err := desktopWindowWork(hwnd)
	if err != nil {
		return err
	}
	x, y := work[0]+(work[2]-work[0]-size[0])/2, work[1]+(work[3]-work[1]-size[1])/2
	r, err := desktopFitRect([4]int32{x, y, x + size[0], y + size[1]}, work)
	if err != nil {
		return err
	}
	return desktopSetWindowRect(hwnd, r)
}

func desktopSetWindowRect(hwnd uintptr, r [4]int32) error {
	if result, _, err := desktopUser32.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(r[0]), uintptr(r[1]), uintptr(r[2]-r[0]), uintptr(r[3]-r[1]), 0x14); result == 0 {
		return windowsError("调整窗口尺寸", err)
	}
	return nil
}

func desktopSetMinimumSize(hwnd, target uintptr) error {
	dpi, err := desktopWindowDPI(hwnd)
	if err != nil {
		return err
	}
	size, err := desktopOuterSize(desktopMinimumWidth, desktopMinimumHeight, dpi)
	if err != nil {
		return err
	}
	work, err := desktopWindowWork(hwnd)
	if err != nil {
		return err
	}
	// 极小工作区优先保证窗口仍可见，不能保证其 CSS 空间仍达到设计最小值。
	size[0], size[1] = min(size[0], work[2]-work[0]), min(size[1], work[3]-work[1])
	var info [10]int32 // MINMAXINFO：5 个 POINT；只改 ptMinTrackSize。
	kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&info)), target, unsafe.Sizeof(info))
	info[6], info[7] = size[0], size[1]
	kernel32.NewProc("RtlMoveMemory").Call(target, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	return nil
}
