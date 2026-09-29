//go:build gui && windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

const desktopClipboardMaxBytes = 128 * 1024
const desktopIssueURL = "https://github.com/Vex9th/mouse-control/issues/new?template=bug_report.yml"

// hwnd 必须是仍然有效的宿主窗口。函数锁定线程直到 CloseClipboard 完成，
// 可以从已 LockOSThread 的 UI 线程调用，不会解除调用方持有的线程锁。
func desktopCopyText(hwnd uintptr, text string) error {
	return desktopCopyTextWithAPI(hwnd, text, desktopClipboard{})
}

// 仅隔离有外部副作用的 API，使普通测试无需访问用户剪贴板。
type desktopClipboardAPI interface {
	allocate([]uint16) (uintptr, error)
	free(uintptr) error
	open(uintptr) error
	empty() error
	set(uintptr) error
	close() error
}

func desktopCopyTextWithAPI(hwnd uintptr, text string, api desktopClipboardAPI) (resultErr error) {
	if hwnd == 0 {
		return errors.New("复制失败：窗口句柄无效")
	}
	if len(text) > desktopClipboardMaxBytes {
		return errors.New("复制失败：文本超过 128 KiB")
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return errors.New("复制失败：文本包含无效 UTF-8 或 NUL 字符")
	}
	data := append(utf16.Encode([]rune(text)), 0)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// 在清空剪贴板之前准备内存；分配失败不会破坏原有内容。
	handle, err := api.allocate(data)
	if err != nil {
		return err
	}
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, api.free(handle))
		}
	}()
	if err := api.open(hwnd); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, api.close()) }()
	if err := api.empty(); err != nil {
		return err
	}
	if err := api.set(handle); err != nil {
		return err
	}
	// SetClipboardData 成功后系统接管 HGLOBAL，即使 CloseClipboard 失败也不能释放。
	transferred = true
	return nil
}

type desktopClipboard struct{}

func desktopSupportError(action string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s失败，Windows 未提供错误码", action)
	}
	return fmt.Errorf("%s失败：%w", action, err)
}

func (c desktopClipboard) allocate(text []uint16) (_ uintptr, resultErr error) {
	const gmemMoveable = 0x0002
	handle, _, err := kernel32.NewProc("GlobalAlloc").Call(gmemMoveable, uintptr(len(text))*2)
	if handle == 0 {
		return 0, desktopSupportError("分配剪贴板内存", err)
	}
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, c.free(handle))
		}
	}()
	address, _, err := kernel32.NewProc("GlobalLock").Call(handle)
	if address == 0 {
		return 0, desktopSupportError("锁定剪贴板内存", err)
	}
	// 使用原生复制避免把 Windows 返回的 uintptr 转成 Go 指针。
	kernel32.NewProc("RtlMoveMemory").Call(address, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text))*2)
	runtime.KeepAlive(text)
	if err := desktopGlobalUnlock(handle); err != nil {
		return 0, err
	}
	complete = true
	return handle, nil
}

// GlobalUnlock 返回零既可能成功也可能失败，必须同时检查 GetLastError。
// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalunlock
func desktopGlobalUnlock(handle uintptr) error {
	kernel32.NewProc("SetLastError").Call(0)
	result, _, err := kernel32.NewProc("GlobalUnlock").Call(handle)
	if result == 0 && err != syscall.Errno(0) && err != nil {
		return desktopSupportError("解锁剪贴板内存", err)
	}
	return nil
}

func (desktopClipboard) free(handle uintptr) error {
	if result, _, err := kernel32.NewProc("GlobalFree").Call(handle); result != 0 {
		return desktopSupportError("释放剪贴板内存", err)
	}
	return nil
}

func (desktopClipboard) open(hwnd uintptr) error {
	if result, _, err := desktopUser32.NewProc("OpenClipboard").Call(hwnd); result == 0 {
		return desktopSupportError("打开剪贴板", err)
	}
	return nil
}

func (desktopClipboard) empty() error {
	if result, _, err := desktopUser32.NewProc("EmptyClipboard").Call(); result == 0 {
		return desktopSupportError("清空剪贴板", err)
	}
	return nil
}

// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
func (desktopClipboard) set(handle uintptr) error {
	const cfUnicodeText = 13
	if result, _, err := desktopUser32.NewProc("SetClipboardData").Call(cfUnicodeText, handle); result == 0 {
		return desktopSupportError("写入剪贴板", err)
	}
	return nil
}

func (desktopClipboard) close() error {
	if result, _, err := desktopUser32.NewProc("CloseClipboard").Call(); result == 0 {
		return desktopSupportError("关闭剪贴板", err)
	}
	return nil
}

// 在宿主已初始化 COM STA 的 UI 线程调用；固定 URL 不接收文本、路径或查询参数。
func desktopOpenIssue() error {
	operation := desktopText("open")
	target := desktopText(desktopIssueURL)
	result, _, err := systemDLL("shell32.dll").NewProc("ShellExecuteW").Call(0,
		uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	return desktopShellResult(result, err)
}

func desktopShellResult(result uintptr, err error) error {
	// 官方返回值按 INT_PTR 比较，不能把负值转换成巨大无符号数后当作成功。
	if int(result) > 32 {
		return nil
	}
	action := fmt.Sprintf("打开问题反馈页面（ShellExecuteW 错误码 %d）", int(result))
	return desktopSupportError(action, err)
}
