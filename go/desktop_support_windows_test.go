//go:build gui && windows

package main

import (
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

type desktopClipboardFixture struct {
	steps                      []string
	text                       []uint16
	fail                       map[string]error
	owned, transferred, opened bool
}

func (f *desktopClipboardFixture) step(name string) error {
	f.steps = append(f.steps, name)
	return f.fail[name]
}
func (f *desktopClipboardFixture) allocate(text []uint16) (uintptr, error) {
	if err := f.step("allocate"); err != nil {
		return 0, err
	}
	f.text = append([]uint16(nil), text...)
	f.owned = true
	return 17, nil
}
func (f *desktopClipboardFixture) free(handle uintptr) error {
	if handle != 17 || !f.owned || f.transferred {
		panic("释放了不属于应用的内存")
	}
	f.owned = false
	return f.step("free")
}
func (f *desktopClipboardFixture) open(hwnd uintptr) error {
	if hwnd != 23 {
		panic("窗口句柄没有传给 OpenClipboard")
	}
	if err := f.step("open"); err != nil {
		return err
	}
	f.opened = true
	return nil
}
func (f *desktopClipboardFixture) empty() error {
	if !f.opened {
		panic("未打开剪贴板")
	}
	return f.step("empty")
}
func (f *desktopClipboardFixture) set(handle uintptr) error {
	if !f.opened || !f.owned || handle != 17 {
		panic("提交无效剪贴板内存")
	}
	if err := f.step("set"); err != nil {
		return err
	}
	f.transferred, f.owned = true, false
	return nil
}
func (f *desktopClipboardFixture) close() error {
	if !f.opened {
		panic("关闭未打开的剪贴板")
	}
	f.opened = false
	return f.step("close")
}

func TestDesktopClipboardRejectsBeforeNativeOperations(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		hwnd       uintptr
	}{
		{"zero owner", "text", 0},
		{"NUL", "prefix\x00suffix", 23},
		{"invalid UTF8", string([]byte{0xff}), 23},
		{"byte limit", strings.Repeat("x", 128*1024+1), 23},
		{"unicode byte limit", strings.Repeat("中", 43691), 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &desktopClipboardFixture{}
			if err := desktopCopyTextWithAPI(tc.hwnd, tc.text, f); err == nil {
				t.Fatal("接受无效复制请求")
			}
			if len(f.steps) != 0 {
				t.Fatalf("校验前访问了系统剪贴板：%v", f.steps)
			}
		})
	}
}

func TestDesktopClipboardEncodesUTF16AndTransfersOwnership(t *testing.T) {
	f := &desktopClipboardFixture{}
	if err := desktopCopyTextWithAPI(23, "错误 😀\r\n", f); err != nil {
		t.Fatal(err)
	}
	if want := []uint16{0x9519, 0x8bef, 0x20, 0xd83d, 0xde00, 0x0d, 0x0a, 0}; !reflect.DeepEqual(f.text, want) {
		t.Fatalf("UTF16/NUL 不完整：%x", f.text)
	}
	if !f.transferred || f.owned || f.opened {
		t.Fatal("系统接管成功后所有权或关闭状态错误")
	}
	if want := []string{"allocate", "open", "empty", "set", "close"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("剪贴板事务顺序错误：%v", f.steps)
	}
	for _, text := range []string{"", strings.Repeat("x", 128*1024)} {
		if err := desktopCopyTextWithAPI(23, text, &desktopClipboardFixture{}); err != nil {
			t.Fatalf("拒绝有效边界：%v", err)
		}
	}
}

func TestDesktopClipboardFailuresPreserveCauseAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		failed      string
		steps       []string
		transferred bool
	}{
		{"allocate", []string{"allocate"}, false},
		{"open", []string{"allocate", "open", "free"}, false},
		{"empty", []string{"allocate", "open", "empty", "close", "free"}, false},
		{"set", []string{"allocate", "open", "empty", "set", "close", "free"}, false},
		{"close", []string{"allocate", "open", "empty", "set", "close"}, true},
	} {
		t.Run(tc.failed, func(t *testing.T) {
			cause := errors.New("native " + tc.failed)
			f := &desktopClipboardFixture{fail: map[string]error{tc.failed: cause}}
			if err := desktopCopyTextWithAPI(23, "text", f); !errors.Is(err, cause) {
				t.Fatalf("失败原因丢失：%v", err)
			}
			if !reflect.DeepEqual(f.steps, tc.steps) || f.owned || f.transferred != tc.transferred {
				t.Fatalf("失败路径泄漏或错误移交：%+v", f)
			}
		})
	}
	setErr, closeErr, freeErr := errors.New("set"), errors.New("close"), errors.New("free")
	f := &desktopClipboardFixture{fail: map[string]error{"set": setErr, "close": closeErr, "free": freeErr}}
	err := desktopCopyTextWithAPI(23, "text", f)
	if !errors.Is(err, setErr) || !errors.Is(err, closeErr) || !errors.Is(err, freeErr) {
		t.Fatalf("清理错误覆盖了原始失败：%v", err)
	}
}

func TestDesktopIssueTargetAndShellResult(t *testing.T) {
	if desktopIssueURL != "https://github.com/Vex9th/mouse-control/issues/new?template=bug_report.yml" {
		t.Fatal("Issues 目标不再是固定官方入口")
	}
	for _, result := range []uintptr{0, 2, 5, 31, 32, ^uintptr(0)} {
		cause := errors.New("shell failure")
		if err := desktopShellResult(result, cause); !errors.Is(err, cause) {
			t.Fatalf("ShellExecute 失败或原因丢失：result=%d err=%v", result, err)
		}
	}
	if err := desktopShellResult(0, nil); err == nil {
		t.Fatal("没有 last error 的 ShellExecute 失败被当作成功")
	}
	if err := desktopShellResult(33, errors.New("stale last error")); err != nil {
		t.Fatalf("成功结果被过期 last error 覆盖：%v", err)
	}
}

// 普通 CI 只分配自己的内存，不打开系统剪贴板。
func TestDesktopClipboardNativeMemory(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	want := []uint16{0x9519, 0x8bef, 0x20, 0xd83d, 0xde00, 0}
	api := desktopClipboard{}
	handle, err := api.allocate(want)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := api.free(handle); err != nil {
			t.Error(err)
		}
	}()
	got := desktopClipboardReadMemory(t, handle)
	if len(got) < len(want) || !reflect.DeepEqual(got[:len(want)], want) {
		t.Fatalf("原生全局内存内容不符：%x", got)
	}
	if err := desktopSupportError("测试", syscall.Errno(5)); !errors.Is(err, syscall.Errno(5)) {
		t.Fatal("Windows 原始错误被丢失")
	}
}

func desktopClipboardReadMemory(t *testing.T, handle uintptr) []uint16 {
	t.Helper()
	size, _, err := kernel32.NewProc("GlobalSize").Call(handle)
	if size < 2 || size > 2*(desktopClipboardMaxBytes+1) {
		t.Fatalf("全局内存尺寸无效：%d err=%v", size, err)
	}
	address, _, err := kernel32.NewProc("GlobalLock").Call(handle)
	if address == 0 {
		t.Fatal(desktopSupportError("读取测试内存", err))
	}
	defer func() {
		if err := desktopGlobalUnlock(handle); err != nil {
			t.Error(err)
		}
	}()
	result := make([]uint16, size/2)
	kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&result[0])), address, uintptr(len(result))*2)
	return result
}

func TestDesktopClipboardRoundTripNoninteractive(t *testing.T) {
	if os.Getenv("RAZER_GUI_TEST") != "1" {
		t.Skip("真实剪贴板测试须显式启用 RAZER_GUI_TEST=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var session uint32
	if result, _, err := kernel32.NewProc("ProcessIdToSessionId").Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&session))); result == 0 {
		t.Fatal(desktopSupportError("读取测试会话", err))
	}
	if session != 0 {
		t.Skip("禁止在交互用户会话读写剪贴板")
	}
	station, _, err := desktopUser32.NewProc("GetProcessWindowStation").Call()
	if station == 0 {
		t.Fatal(desktopSupportError("读取测试窗口站", err))
	}
	var flags [3]uint32
	var required uint32
	if result, _, err := desktopUser32.NewProc("GetUserObjectInformationW").Call(station, 1, uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), uintptr(unsafe.Pointer(&required))); result == 0 {
		t.Fatal(desktopSupportError("读取窗口站标志", err))
	}
	var name [256]uint16
	if result, _, err := desktopUser32.NewProc("GetUserObjectInformationW").Call(station, 2, uintptr(unsafe.Pointer(&name)), unsafe.Sizeof(name), uintptr(unsafe.Pointer(&required))); result == 0 {
		t.Fatal(desktopSupportError("读取窗口站名称", err))
	}
	if flags[2]&1 != 0 || strings.EqualFold(syscall.UTF16ToString(name[:]), "WinSta0") {
		t.Skip("禁止访问交互窗口站剪贴板")
	}
	hwnd, _, err := desktopUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(desktopText("STATIC"))), 0, 0, 0, 0, 1, 1, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(desktopSupportError("创建剪贴板测试窗口", err))
	}
	defer desktopUser32.NewProc("DestroyWindow").Call(hwnd)
	const text = "MouseControl 测试错误\r\n中文 😀"
	if err := desktopCopyText(hwnd, text); err != nil {
		t.Fatal(err)
	}
	api := desktopClipboard{}
	if err := api.open(hwnd); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := api.close(); err != nil {
			t.Error(err)
		}
	}()
	owner, _, _ := desktopUser32.NewProc("GetClipboardOwner").Call()
	if owner != hwnd {
		t.Fatal("剪贴板已被其他进程替换，拒绝读取")
	}
	defer func() {
		if err := api.empty(); err != nil {
			t.Error(err)
		}
	}()
	handle, _, err := desktopUser32.NewProc("GetClipboardData").Call(13)
	if handle == 0 {
		t.Fatal(desktopSupportError("读回复制内容", err))
	}
	if got := syscall.UTF16ToString(desktopClipboardReadMemory(t, handle)); got != text {
		t.Fatalf("剪贴板读回不一致：%q", got)
	}
}
