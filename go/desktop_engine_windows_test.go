//go:build gui && windows

package main

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// 使用真实 SyscallN 调用 Go COM 回调，覆盖参数个数、输出指针和引用计数。
type desktopEngineCOMFixture struct {
	vtable              *[61]uintptr
	refs, closes        int
	message, permission *desktopEvent
	pin                 runtime.Pinner
}

func newDesktopEngineCOMFixture(t *testing.T) *desktopEngineCOMFixture {
	t.Helper()
	f := &desktopEngineCOMFixture{vtable: new([61]uintptr), refs: 1}
	f.vtable[1] = syscall.NewCallback(func(uintptr) uintptr { f.refs++; return uintptr(f.refs) })
	f.vtable[2] = syscall.NewCallback(func(uintptr) uintptr { f.refs--; return uintptr(f.refs) })
	f.vtable[24] = syscall.NewCallback(func(uintptr) uintptr { f.closes++; return 0 })
	f.pin.Pin(f)
	f.pin.Pin(f.vtable)
	t.Cleanup(f.pin.Unpin)
	return f
}

func TestDesktopEngineMethodsBeforeReadyAndAfterClose(t *testing.T) {
	e := newDesktopEngine(0, "", nil)
	e.Resize()
	e.Focus()
	e.NotifyMoved()
	if e.Ready() || e.Controller() != nil {
		t.Fatal("未初始化引擎被视为可用")
	}
	if e.NavigateHTML("<html></html>") == nil || e.Eval("1") == nil {
		t.Fatal("未初始化时执行了页面操作")
	}
	e.Close()
	e.Close()
	if e.Initialize() == nil {
		t.Fatal("关闭后的引擎重新初始化")
	}
}

func TestDesktopEngineRejectsFailedOrNilCreation(t *testing.T) {
	for _, hr := range []uintptr{0, 0x80004005} {
		e := newDesktopEngine(1, "", nil)
		e.environmentCompleted(hr, nil)
		if e.Err() == nil || e.Ready() {
			t.Fatalf("接受无效环境：0x%X", hr)
		}
		e.Close()
		e = newDesktopEngine(1, "", nil)
		e.controllerCompleted(hr, nil)
		if e.Err() == nil || e.Ready() {
			t.Fatalf("接受无效控制器：0x%X", hr)
		}
		e.Close()
	}
}

func TestDesktopEngineClosesLateController(t *testing.T) {
	f := newDesktopEngineCOMFixture(t)
	e := newDesktopEngine(1, "", nil)
	e.controllerHandler = newDesktopCallback(desktopControllerCompletedIID, e.controllerCompleted)
	handler := e.controllerHandler
	desktopCOM(unsafe.Pointer(handler), 1) // 模拟异步创建持有的引用。
	e.Close()
	if handler.refs.Load() != 1 {
		t.Fatal("提前释放仍由 COM 持有的回调")
	}
	desktopCOM(unsafe.Pointer(handler), 3, 0, uintptr(unsafe.Pointer(f)))
	handler.Release()
	if f.closes != 1 || f.refs != 1 {
		t.Fatalf("迟到控制器未关闭或错误释放借用引用：close=%d refs=%d", f.closes, f.refs)
	}
	if handler.refs.Load() != 0 {
		t.Fatal("迟到回调结束后仍持有引用")
	}
}

func TestDesktopEngineOwnsAndReleasesControllerCoreAndHandlers(t *testing.T) {
	controller, core := newDesktopEngineCOMFixture(t), newDesktopEngineCOMFixture(t)
	controller.vtable[25] = syscall.NewCallback(func(_ uintptr, out *unsafe.Pointer) uintptr {
		core.refs++
		*out = unsafe.Pointer(core)
		return 0
	})
	add := func(target **desktopEvent, token int64) uintptr {
		return syscall.NewCallback(func(_ uintptr, handler *desktopEvent, out *int64) uintptr {
			*target = handler
			desktopCOM(unsafe.Pointer(*target), 1)
			*out = token
			return 0
		})
	}
	remove := func(target **desktopEvent, want int64) uintptr {
		return syscall.NewCallback(func(_, token uintptr) uintptr {
			if int64(token) != want {
				t.Errorf("错误事件标识：%d != %d", token, want)
			}
			(*target).Release()
			return 0
		})
	}
	core.vtable[34], core.vtable[35] = add(&core.message, 11), remove(&core.message, 11)
	core.vtable[23], core.vtable[24] = add(&core.permission, 12), remove(&core.permission, 12)
	e := newDesktopEngine(1, "", nil)
	e.controllerCompleted(0, unsafe.Pointer(controller))
	if e.Err() != nil || !e.Ready() || controller.refs != 2 || core.refs != 2 {
		t.Fatalf("初始化所有权错误：%v ready=%v refs=%d/%d", e.Err(), e.Ready(), controller.refs, core.refs)
	}
	message, permission := core.message, core.permission
	e.Close()
	e.Close()
	if controller.closes != 1 || controller.refs != 1 || core.refs != 1 || message.refs.Load() != 0 || permission.refs.Load() != 0 {
		t.Fatalf("关闭未配平所有权：close=%d refs=%d/%d handlers=%d/%d", controller.closes, controller.refs, core.refs, message.refs.Load(), permission.refs.Load())
	}
}

func TestDesktopEngineCreationFailureReleasesPartialObjects(t *testing.T) {
	controller, core := newDesktopEngineCOMFixture(t), newDesktopEngineCOMFixture(t)
	controller.vtable[25] = syscall.NewCallback(func(_ uintptr, out *unsafe.Pointer) uintptr {
		core.refs++
		*out = unsafe.Pointer(core)
		return 0
	})
	core.vtable[34] = syscall.NewCallback(func(_, _, _ uintptr) uintptr { return 0x80004005 })
	e := newDesktopEngine(1, "", nil)
	e.controllerCompleted(0, unsafe.Pointer(controller))
	if e.Err() == nil || e.Ready() || controller.closes != 1 || controller.refs != 1 || core.refs != 1 {
		t.Fatalf("初始化失败未清理：err=%v close=%d refs=%d/%d", e.Err(), controller.closes, controller.refs, core.refs)
	}
	environment := newDesktopEngineCOMFixture(t)
	environment.vtable[3] = syscall.NewCallback(func(_, _, _ uintptr) uintptr { return 0x80004005 })
	e = newDesktopEngine(1, "", nil)
	e.environmentCompleted(0, unsafe.Pointer(environment))
	if e.Err() == nil || environment.refs != 1 {
		t.Fatalf("环境引用未释放：%v refs=%d", e.Err(), environment.refs)
	}
}

func desktopEngineFixtureString(value string) uintptr {
	return syscall.NewCallback(func(_ uintptr, out **uint16) uintptr {
		encoded, _ := syscall.UTF16PtrFromString(value)
		// SHStrDupW 使用 COM 任务分配器，生产函数可正常 CoTaskMemFree。
		result, _, _ := systemDLL("shlwapi.dll").NewProc("SHStrDupW").Call(uintptr(unsafe.Pointer(encoded)), uintptr(unsafe.Pointer(out)))
		return result
	})
}

func TestDesktopEngineMessageUsesEventSourceAndRejectsNonString(t *testing.T) {
	for _, tc := range []struct {
		source      string
		stringError bool
		want        int
	}{
		{"about:blank", false, 1}, {"https://example.com", false, 0}, {"about:blank", true, 0},
	} {
		args := newDesktopEngineCOMFixture(t)
		args.vtable[3] = desktopEngineFixtureString(tc.source)
		args.vtable[5] = desktopEngineFixtureString("trusted-request")
		if tc.stringError {
			args.vtable[5] = syscall.NewCallback(func(_ uintptr, out *unsafe.Pointer) uintptr { *out = nil; return 0x80070057 })
		}
		calls := 0
		e := newDesktopEngine(1, "", func(source, raw string) {
			calls++
			if source != "about:blank" || raw != "trusted-request" {
				t.Errorf("消息内容变化：%q %q", source, raw)
			}
		})
		e.messageReceived(0, unsafe.Pointer(args))
		if calls != tc.want {
			t.Errorf("source=%q stringError=%v calls=%d", tc.source, tc.stringError, calls)
		}
		e.Close()
		e.messageReceived(0, unsafe.Pointer(args))
		if calls != tc.want {
			t.Error("关闭后仍转发消息")
		}
	}
}

func TestDesktopEngineDeniesEveryPermission(t *testing.T) {
	args := newDesktopEngineCOMFixture(t)
	state := uintptr(99)
	args.vtable[7] = syscall.NewCallback(func(_, value uintptr) uintptr { state = value; return 0 })
	e := newDesktopEngine(1, "", nil)
	if result := e.permissionRequested(0, unsafe.Pointer(args)); result != 0 || state != 2 {
		t.Fatalf("未拒绝权限：HRESULT=%x state=%d", result, state)
	}
	e.Close()
}
