//go:build gui && windows

package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/webviewloader"
)

//go:embed gui_assets/index.html
var desktopHTML string

var (
	desktopUser32 = systemDLL("user32.dll")
	desktopActive *desktopWindow
)

type desktopWindow struct {
	hwnd         uintptr
	view         *desktopEngine
	boundary     *desktopBoundary
	service      *guiService
	token        string
	busy         atomic.Bool
	closing      atomic.Bool
	replies      chan desktopReply
	shutdownDone chan struct{}
	initializing bool
	layoutErr    error
}

type desktopWindowClass struct {
	Size, Style                                                        uint32
	WndProc                                                            uintptr
	ClassExtra, WindowExtra                                            int32
	Instance, Icon, Cursor, Background, MenuName, ClassName, SmallIcon uintptr
}

type desktopMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
	Private        uint32
}

func desktopText(value string) *uint16 { return syscall.StringToUTF16Ptr(value) }

func desktopError(err error) {
	desktopUser32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(desktopText(err.Error()))), uintptr(unsafe.Pointer(desktopText("鼠标工具 · 无法启动"))), 0x10)
}

func main() {
	runtime.LockOSThread()
	if err := runDesktop(); err != nil {
		desktopError(err)
	}
}

func runDesktop() (resultErr error) {
	dpi, err := beginDesktopDPI()
	if err != nil {
		return err
	}
	// 最先登记，确保窗口和 COM 都已清理后才恢复调用线程的 DPI。
	defer func() { resultErr = errors.Join(resultErr, dpi.Close()) }()
	ole32 := systemDLL("ole32.dll")
	hr, _, _ := ole32.NewProc("CoInitializeEx").Call(0, 2)
	if err := desktopHRESULT("初始化窗口组件", hr); err != nil {
		return err
	}
	defer ole32.NewProc("CoUninitialize").Call()
	// 仅更改本进程的 DLL 搜索范围，避免加载下载目录内的同名 Loader。
	if r, _, err := kernel32.NewProc("SetDefaultDllDirectories").Call(0x800); r == 0 {
		return windowsError("设置系统组件搜索范围", err)
	}
	runtimeVersion, err := webviewloader.GetInstalledVersion()
	if err != nil || runtimeVersion == "" {
		return fmt.Errorf("需要 Microsoft Edge WebView2 Runtime 才能显示界面。\n\n请从 Microsoft 官网安装 Evergreen Runtime 后重试：\nhttps://developer.microsoft.com/microsoft-edge/webview2/\n\n检测信息：%v", err)
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return fmt.Errorf("创建界面会话失败：%w", err)
	}
	w := &desktopWindow{service: newGUIService(discoverDevices), token: hex.EncodeToString(secret[:]), replies: make(chan desktopReply, 1), shutdownDone: make(chan struct{}), initializing: true}
	desktopActive = w
	defer func() { w.closing.Store(true); w.service.Close() }()
	document, err := desktopDocument(desktopHTML, w.token)
	if err != nil {
		return err
	}
	if err := w.createWindow(); err != nil {
		return err
	}
	defer desktopUser32.NewProc("DestroyWindow").Call(w.hwnd)

	dataRoot, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("无法获取用户缓存目录：%w", err)
	}
	w.view = newDesktopEngine(w.hwnd, filepath.Join(dataRoot, "MouseControl", "WebView2"), w.receive)
	defer w.view.Close()
	if err := w.view.Initialize(); err != nil {
		return err
	}
	if err := w.startTimer(); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for !w.view.Ready() {
		if err := w.view.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("WebView2 启动超时，请关闭其他运行中的本程序后重试")
		}
		running, err := desktopNextMessage()
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
	}
	w.initializing = false
	w.boundary, err = newDesktopBoundary(w.view.Controller(), document)
	if err != nil {
		return err
	}
	defer w.boundary.Close()
	var settings unsafe.Pointer
	if err := desktopHRESULT("获取页面设置", desktopCOM(w.boundary.core, 3, uintptr(unsafe.Pointer(&settings)))); err != nil {
		return err
	}
	if settings == nil {
		return fmt.Errorf("页面设置接口不可用")
	}
	// 禁止脚本对话框、状态栏、开发者工具、默认菜单、HostObjects 和页面缩放快捷键。
	for _, slot := range []uintptr{8, 10, 12, 14, 16, 18} {
		if err := desktopHRESULT("设置页面权限", desktopCOM(settings, slot, 0)); err != nil {
			desktopCOM(settings, 2)
			return err
		}
	}
	desktopCOM(settings, 2)
	if err := w.view.NavigateHTML(document); err != nil {
		return err
	}
	w.view.Resize()
	desktopUser32.NewProc("ShowWindow").Call(w.hwnd, 5)
	if err := desktopHRESULT("显示浏览器界面", desktopCOM(w.view.Controller(), 4, 1)); err != nil {
		return err
	}
	desktopUser32.NewProc("UpdateWindow").Call(w.hwnd)
	for {
		running, err := desktopNextMessage()
		if err != nil {
			return err
		}
		if !running {
			break
		}
	}
	w.closing.Store(true)
	return nil
}

func desktopNextMessage() (bool, error) {
	var message desktopMessage
	result, _, e := desktopUser32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
	if int32(result) == -1 {
		return false, windowsError("读取窗口消息", e)
	}
	if result == 0 {
		return false, nil
	}
	desktopUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
	desktopUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	if desktopActive != nil && desktopActive.layoutErr != nil {
		return false, desktopActive.layoutErr
	}
	return true, nil
}

func (w *desktopWindow) startTimer() error {
	if result, _, err := desktopUser32.NewProc("SetTimer").Call(w.hwnd, 1, 100, 0); result == 0 {
		return windowsError("启动窗口任务通知", err)
	}
	return nil
}

// 只在启动、设备操作和关闭期间轮询完成队列，空闲时不保留定时器。
func (w *desktopWindow) tick() {
	if w.initializing {
		return
	}
	if w.closing.Load() {
		select {
		case <-w.shutdownDone:
			desktopUser32.NewProc("DestroyWindow").Call(w.hwnd)
		default:
		}
		return
	}
	select {
	case reply := <-w.replies:
		w.busy.Store(false)
		w.deliver(reply)
	default:
	}
	if !w.busy.Load() {
		desktopUser32.NewProc("KillTimer").Call(w.hwnd, 1)
	}
}

func (w *desktopWindow) createWindow() error {
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	cursor, _, _ := desktopUser32.NewProc("LoadCursorW").Call(0, 32512)
	icon, _, _ := desktopUser32.NewProc("LoadIconW").Call(0, 32512)
	name := desktopText("MouseControlWindow")
	class := desktopWindowClass{WndProc: syscall.NewCallback(desktopWindowProc), Instance: instance, Cursor: cursor, Icon: icon, SmallIcon: icon, Background: 6, ClassName: uintptr(unsafe.Pointer(name))}
	class.Size = uint32(unsafe.Sizeof(class))
	if r, _, e := desktopUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); r == 0 {
		return windowsError("注册窗口", e)
	}
	var work [4]int32
	if r, _, e := desktopUser32.NewProc("SystemParametersInfoW").Call(0x30, 0, uintptr(unsafe.Pointer(&work)), 0); r == 0 {
		return windowsError("读取桌面工作区", e)
	}
	dpi, _, _ := desktopUser32.NewProc("GetDpiForSystem").Call()
	size, err := desktopOuterSize(desktopDefaultWidth, desktopDefaultHeight, uint32(dpi))
	if err != nil {
		return err
	}
	x, y := work[0]+(work[2]-work[0]-size[0])/2, work[1]+(work[3]-work[1]-size[1])/2
	r, err := desktopFitRect([4]int32{x, y, x + size[0], y + size[1]}, work)
	if err != nil {
		return err
	}
	hwnd, _, e := desktopUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(desktopText("鼠标工具"))), desktopWindowStyle, uintptr(r[0]), uintptr(r[1]), uintptr(r[2]-r[0]), uintptr(r[3]-r[1]), 0, 0, instance, 0)
	if hwnd == 0 {
		return windowsError("创建窗口", e)
	}
	w.hwnd = hwnd
	// 创建后使用窗口实际 DPI，避免系统 DPI 与所在显示器 DPI 不同时尺寸偏差。
	if w.layoutErr == nil {
		if err := desktopSetClientSize(hwnd, desktopDefaultWidth, desktopDefaultHeight); err != nil {
			w.layoutErr = err
		}
	}
	if w.layoutErr != nil {
		desktopUser32.NewProc("DestroyWindow").Call(hwnd)
		w.hwnd = 0
		return w.layoutErr
	}
	return nil
}

func desktopWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	w := desktopActive
	switch message {
	case 0x24: // WM_GETMINMAXINFO
		if err := desktopSetMinimumSize(hwnd, lparam); err != nil {
			if w != nil {
				w.layoutErr = err
			}
			break
		}
		return 0
	case 5: // WM_SIZE
		if w != nil && w.view != nil {
			w.view.Resize()
		}
	case 3: // WM_MOVE
		if w != nil && w.view != nil {
			w.view.NotifyMoved()
		}
	case 7: // WM_SETFOCUS
		if w != nil && w.view != nil {
			w.view.Focus()
		}
	case 0x2e0: // WM_DPICHANGED
		var r [4]int32
		// lparam 是系统提供的 RECT 地址，用系统内存复制保留 native 指针语义。
		kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&r)), lparam, unsafe.Sizeof(r))
		monitor, _, _ := desktopUser32.NewProc("MonitorFromRect").Call(uintptr(unsafe.Pointer(&r)), 2)
		work, err := desktopMonitorWork(monitor)
		if err == nil {
			r, err = desktopFitRect(r, work)
		}
		if err == nil {
			err = desktopSetWindowRect(hwnd, r)
		}
		if err != nil && w != nil {
			w.layoutErr = err
		}
		return 0
	case 0x113: // WM_TIMER
		if w != nil {
			w.tick()
		}
		return 0
	case 0x10: // WM_CLOSE：停止接收请求，等设备句柄释放后关闭。
		if w != nil && w.closing.CompareAndSwap(false, true) {
			w.initializing = false
			if err := w.startTimer(); err != nil {
				w.service.Close()
				desktopUser32.NewProc("DestroyWindow").Call(hwnd)
				return 0
			}
			desktopUser32.NewProc("ShowWindow").Call(hwnd, 0)
			go func() { w.service.Close(); close(w.shutdownDone) }()
		}
		return 0
	case 2: // WM_DESTROY
		desktopUser32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	result, _, _ := desktopUser32.NewProc("DefWindowProcW").Call(hwnd, uintptr(message), wparam, lparam)
	return result
}

func (w *desktopWindow) receive(source, raw string) {
	if source != "about:blank" || w.closing.Load() || !w.boundary.Trusted() {
		return
	}
	request, err := decodeDesktopRequest(raw, w.token)
	if err != nil {
		if request.Token == w.token && request.ID != "" {
			w.deliver(desktopReply{ID: request.ID, Error: err.Error()})
		}
		return
	}
	if !w.busy.CompareAndSwap(false, true) {
		w.deliver(desktopReply{ID: request.ID, Error: "正在与鼠标通信，请稍候再试"})
		return
	}
	if err := w.startTimer(); err != nil {
		w.busy.Store(false)
		w.deliver(desktopReply{ID: request.ID, Error: err.Error()})
		return
	}
	go func() {
		reply := executeDesktopRequest(w.service, request)
		if w.closing.Load() {
			return
		}
		w.replies <- reply
	}()
}

func (w *desktopWindow) deliver(reply desktopReply) {
	if w.closing.Load() || !w.boundary.Trusted() {
		return
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return
	} // DTO 仅含可序列化类型。
	if err := w.view.Eval("window.dispatchEvent(new CustomEvent('mouse:response',{detail:" + string(data) + "}))"); err != nil {
		desktopError(fmt.Errorf("界面无法显示操作结果，请重新打开后刷新确认：%w", err))
	}
}
