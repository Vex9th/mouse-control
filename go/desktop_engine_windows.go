//go:build gui && windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2/webviewloader"
)

// 所有方法和回调均由创建窗口的 STA 线程调用。Initialize 只发起创建；
// 调用方泵消息并检查 Ready/Err，负责超时和 CoInitializeEx/CoUninitialize。
type desktopEngine struct {
	hwnd                                  uintptr
	dataPath                              string
	userData                              []uint16
	onMessage                             func(source, raw string)
	environment, controller, core         unsafe.Pointer
	environmentHandler, controllerHandler *desktopEvent
	messageHandler, permissionHandler     *desktopEvent
	messageToken, permissionToken         int64
	messageAdded, permissionAdded         bool
	started, ready, closed                bool
	err                                   error
}

// GUID 与方法序号来自 Microsoft WebView2 SDK 1.0.2739.15 的 WebView2.h。
var (
	desktopEnvironmentCompletedIID = guid{0x4e8a3389, 0xc9d8, 0x4bd2, [8]byte{0xb6, 0xb5, 0x12, 0x4f, 0xee, 0x6c, 0xc1, 0x4d}}
	desktopControllerCompletedIID  = guid{0x6c4819f3, 0xc9b7, 0x4260, [8]byte{0x81, 0x27, 0xc9, 0xf5, 0xbd, 0xe7, 0xf6, 0x8c}}
	desktopMessageReceivedIID      = guid{0x57213f19, 0x00e6, 0x49fa, [8]byte{0x8e, 0x07, 0x89, 0x8e, 0xa0, 0x1e, 0xcb, 0xd2}}
	desktopPermissionRequestedIID  = guid{0x15e1c6a3, 0xc72a, 0x4df3, [8]byte{0x91, 0xd7, 0xd0, 0x97, 0xfb, 0xec, 0x6b, 0xfd}}
)

func newDesktopEngine(hwnd uintptr, dataPath string, onMessage func(source, raw string)) *desktopEngine {
	return &desktopEngine{hwnd: hwnd, dataPath: dataPath, onMessage: onMessage}
}

func (e *desktopEngine) Ready() bool                { return e != nil && e.ready && !e.closed && e.err == nil }
func (e *desktopEngine) Err() error                 { return e.err }
func (e *desktopEngine) Controller() unsafe.Pointer { return e.controller }

func (e *desktopEngine) Initialize() error {
	if e.closed {
		return errors.New("浏览器已经关闭")
	}
	if e.started {
		return errors.New("浏览器已经开始初始化")
	}
	e.started = true
	if runtime.GOARCH != "amd64" {
		return e.fail(errors.New("当前桌面宿主仅支持 Windows x64"))
	}
	if e.hwnd == 0 {
		return e.fail(errors.New("浏览器需要有效的父窗口"))
	}
	var err error
	e.userData, err = syscall.UTF16FromString(e.dataPath)
	if err != nil {
		return e.fail(fmt.Errorf("浏览器缓存目录无效：%w", err))
	}
	e.environmentHandler = newDesktopCallback(desktopEnvironmentCompletedIID, e.environmentCompleted)
	result, err := webviewloader.CreateCoreWebView2EnvironmentWithOptions(nil, &e.userData[0], 0, uintptr(unsafe.Pointer(e.environmentHandler)))
	runtime.KeepAlive(e)
	if err != nil {
		return e.fail(fmt.Errorf("创建浏览器环境失败：%w", err))
	}
	if err := desktopHRESULT("创建浏览器环境", result); err != nil {
		return e.fail(err)
	}
	return e.err
}

func (e *desktopEngine) fail(err error) error {
	if e.err == nil {
		e.err = err
	}
	e.Close()
	return e.err
}

func (e *desktopEngine) environmentCompleted(result uintptr, object unsafe.Pointer) uintptr {
	if e.closed {
		return 0
	}
	if err := desktopHRESULT("初始化浏览器环境", result); err != nil {
		e.fail(err)
		return 0
	}
	if object == nil {
		e.fail(errors.New("浏览器环境创建成功但返回空接口"))
		return 0
	}
	e.environment = object
	desktopCOM(e.environment, 1) // 回调参数是借用引用；保留一份至 Close。
	e.controllerHandler = newDesktopCallback(desktopControllerCompletedIID, e.controllerCompleted)
	if err := desktopHRESULT("创建浏览器控制器", desktopCOM(e.environment, 3, e.hwnd, uintptr(unsafe.Pointer(e.controllerHandler)))); err != nil {
		e.fail(err)
	}
	return 0
}

func (e *desktopEngine) controllerCompleted(result uintptr, object unsafe.Pointer) uintptr {
	if e.closed {
		// 初始化无法取消。超时/关闭后到达的控制器仍须显式关闭；参数是借用引用。
		if object != nil {
			desktopCOM(object, 24)
		}
		return 0
	}
	if err := desktopHRESULT("初始化浏览器控制器", result); err != nil {
		e.fail(err)
		return 0
	}
	if object == nil {
		e.fail(errors.New("浏览器控制器创建成功但返回空接口"))
		return 0
	}
	e.controller = object
	desktopCOM(e.controller, 1)
	if err := desktopHRESULT("获取浏览器页面", desktopCOM(e.controller, 25, uintptr(unsafe.Pointer(&e.core)))); err != nil {
		e.fail(err)
		return 0
	}
	if e.core == nil {
		e.fail(errors.New("浏览器页面接口为空"))
		return 0
	}
	e.messageHandler = newDesktopCallback(desktopMessageReceivedIID, e.messageReceived)
	e.permissionHandler = newDesktopCallback(desktopPermissionRequestedIID, e.permissionRequested)
	if err := desktopHRESULT("注册页面消息", desktopCOM(e.core, 34, uintptr(unsafe.Pointer(e.messageHandler)), uintptr(unsafe.Pointer(&e.messageToken)))); err != nil {
		e.fail(err)
		return 0
	}
	e.messageAdded = true
	if err := desktopHRESULT("限制页面权限", desktopCOM(e.core, 23, uintptr(unsafe.Pointer(e.permissionHandler)), uintptr(unsafe.Pointer(&e.permissionToken)))); err != nil {
		e.fail(err)
		return 0
	}
	e.permissionAdded = true
	e.ready = true
	return 0
}

func (e *desktopEngine) messageReceived(_ uintptr, object unsafe.Pointer) uintptr {
	if e.closed || object == nil {
		return 0
	}
	args := object
	source, err := desktopCOMString(args, 3) // 读取发送消息的文档来源，不能用当前页面代替。
	if err != nil || source != "about:blank" {
		return 0
	}
	raw, err := desktopCOMString(args, 5) // TryGetWebMessageAsString；拒绝非字符串消息。
	if err != nil {
		return 0
	}
	if e.onMessage != nil {
		e.onMessage(source, raw)
	}
	return 0
}

func (e *desktopEngine) permissionRequested(_ uintptr, object unsafe.Pointer) uintptr {
	if object == nil {
		return 0x80004003
	} // E_POINTER
	// 所有权限统一拒绝；无需读取权限种类，避免不必要的输出参数和默认分支。
	return desktopCOM(object, 7, 2) // put_State(DENY)
}

func (e *desktopEngine) Resize() {
	if !e.Ready() {
		return
	}
	var bounds [4]int32
	if result, _, _ := desktopUser32.NewProc("GetClientRect").Call(e.hwnd, uintptr(unsafe.Pointer(&bounds))); result == 0 {
		return
	}
	// Windows x64 ABI 对 16 字节 RECT 值参数传其地址。
	desktopCOM(e.controller, 6, uintptr(unsafe.Pointer(&bounds)))
}

func (e *desktopEngine) Focus() {
	if e.Ready() {
		desktopCOM(e.controller, 12, 0)
	} // PROGRAMMATIC
}

func (e *desktopEngine) NotifyMoved() {
	if e.Ready() {
		desktopCOM(e.controller, 23)
	}
}

func (e *desktopEngine) NavigateHTML(html string) error {
	if !e.Ready() {
		return errors.New("浏览器尚未就绪或已经关闭")
	}
	value, err := syscall.UTF16PtrFromString(html)
	if err != nil {
		return fmt.Errorf("页面内容无效：%w", err)
	}
	return desktopHRESULT("载入内嵌页面", desktopCOM(e.core, 6, uintptr(unsafe.Pointer(value))))
}

func (e *desktopEngine) Eval(script string) error {
	if !e.Ready() {
		return errors.New("浏览器尚未就绪或已经关闭")
	}
	value, err := syscall.UTF16PtrFromString(script)
	if err != nil {
		return fmt.Errorf("界面响应脚本无效：%w", err)
	}
	return desktopHRESULT("传递界面响应", desktopCOM(e.core, 29, uintptr(unsafe.Pointer(value)), 0))
}

func (e *desktopEngine) Close() {
	if e == nil || e.closed {
		return
	}
	e.closed, e.ready = true, false
	if e.core != nil {
		if e.messageAdded {
			desktopCOM(e.core, 35, uintptr(e.messageToken))
		}
		if e.permissionAdded {
			desktopCOM(e.core, 24, uintptr(e.permissionToken))
		}
	}
	if e.controller != nil {
		desktopCOM(e.controller, 24)
	}
	for _, object := range []unsafe.Pointer{e.core, e.controller, e.environment} {
		if object != nil {
			desktopCOM(object, 2)
		}
	}
	e.core, e.controller, e.environment = nil, nil, nil
	// 仅释放自己的初始引用。未完成的异步创建由 COM 持有回调引用，最后一次
	// Release 才解除 pin，防止超时之后原生代码访问已经失效的回调内存。
	for _, handler := range []*desktopEvent{e.messageHandler, e.permissionHandler, e.controllerHandler, e.environmentHandler} {
		if handler != nil {
			handler.Release()
		}
	}
	e.messageHandler, e.permissionHandler, e.controllerHandler, e.environmentHandler = nil, nil, nil, nil
	e.onMessage = nil
}
