//go:build gui && windows

package main

import (
	"testing"
	"unsafe"
)

func TestDesktopWindowsLayouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("桌面发行目标为 Windows x64")
	}
	if got := unsafe.Sizeof(desktopWindowClass{}); got != 80 {
		t.Fatalf("WNDCLASSEXW 大小 = %d，预期 80", got)
	}
	if got := unsafe.Sizeof(desktopMessage{}); got != 48 {
		t.Fatalf("MSG 大小 = %d，预期 48", got)
	}
	if got := unsafe.Offsetof(desktopMessage{}.WParam); got != 16 {
		t.Fatalf("MSG.wParam 偏移 = %d，预期 16", got)
	}
	if got := unsafe.Sizeof(guid{}); got != 16 {
		t.Fatalf("GUID 大小 = %d，预期 16", got)
	}
}
