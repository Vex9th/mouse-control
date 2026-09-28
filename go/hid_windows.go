//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type guid struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

var hidGUID = guid{0x4D1E55B2, 0xF16F, 0x11CF, [8]byte{0x88, 0xCB, 0, 0x11, 0x11, 0, 0, 0x30}}

var (
	setupapi        = systemDLL("setupapi.dll")
	hidDLL          = systemDLL("hid.dll")
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	cfgmgr          = systemDLL("cfgmgr32.dll")
	getClassDevs    = setupapi.NewProc("SetupDiGetClassDevsW")
	enumInfo        = setupapi.NewProc("SetupDiEnumDeviceInfo")
	enumInterfaces  = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	interfaceDetail = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	destroyInfo     = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	getProperty     = setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	getParent       = cfgmgr.NewProc("CM_Get_Parent")
	getIDSize       = cfgmgr.NewProc("CM_Get_Device_ID_Size")
	getID           = cfgmgr.NewProc("CM_Get_Device_IDW")
	getPreparsed    = hidDLL.NewProc("HidD_GetPreparsedData")
	freePreparsed   = hidDLL.NewProc("HidD_FreePreparsedData")
	getCaps         = hidDLL.NewProc("HidP_GetCaps")
	getProduct      = hidDLL.NewProc("HidD_GetProductString")
	setFeature      = hidDLL.NewProc("HidD_SetFeature")
	getFeature      = hidDLL.NewProc("HidD_GetFeature")
)

type deviceInfoData struct {
	Size     uint32
	Class    guid
	DevInst  uint32
	Reserved uintptr
}
type interfaceData struct {
	Size     uint32
	Class    guid
	Flags    uint32
	Reserved uintptr
}

// HidP_GetCaps 会写入完整的 64 字节结构，不能只声明用到的前几个字段。
type hidCaps struct {
	Usage, UsagePage, InputLength, OutputLength, FeatureLength uint16
	Reserved                                                   [17]uint16
	Counts                                                     [10]uint16
}

type windowsChannel struct{ handle syscall.Handle }

func (c *windowsChannel) Close() error {
	if c.handle == 0 {
		return nil
	}
	err := syscall.CloseHandle(c.handle)
	c.handle = 0
	return err
}
func (c *windowsChannel) Exchange(b [91]byte, wait time.Duration) ([91]byte, error) {
	var reply [91]byte
	r, _, e := setFeature.Call(uintptr(c.handle), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if r == 0 {
		return reply, windowsError("发送设备指令", e)
	}
	time.Sleep(wait)
	r, _, e = getFeature.Call(uintptr(c.handle), uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)))
	if r == 0 {
		return reply, windowsError("读取设备响应", e)
	}
	return reply, nil
}

func windowsError(action string, e error) error {
	if errno, ok := e.(syscall.Errno); ok {
		return fmt.Errorf("%s失败（Windows 错误 %d）", action, uint32(errno))
	}
	return fmt.Errorf("%s失败：%w", action, e)
}

var errNodeGone = errors.New("设备已移除")

// 不从 exe 所在目录搜索系统 DLL，避免下载目录中的同名文件被加载。
func systemDLL(name string) *syscall.LazyDLL {
	var path [32768]uint16
	n, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)))
	if n == 0 || n >= uintptr(len(path)) {
		panic(windowsError("获取 Windows 系统目录", err))
	}
	return syscall.NewLazyDLL(filepath.Join(syscall.UTF16ToString(path[:n]), name))
}

func nodeID(node uint32) (string, error) {
	var length uint32
	r, _, _ := getIDSize.Call(uintptr(unsafe.Pointer(&length)), uintptr(node), 0)
	if r == 0x0d {
		return "", errNodeGone
	}
	if r != 0 || length > 32767 {
		return "", fmt.Errorf("读取设备标识长度失败（配置管理器状态 %d）", r)
	}
	b := make([]uint16, length+1)
	r, _, _ = getID.Call(uintptr(node), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
	if r == 0x0d {
		return "", errNodeGone
	}
	if r != 0 {
		return "", fmt.Errorf("读取设备标识失败（配置管理器状态 %d）", r)
	}
	return syscall.UTF16ToString(b), nil
}

func physicalParent(node uint32) (string, error) {
	for depth := 0; depth < 32; depth++ {
		id, err := nodeID(node)
		if err != nil {
			return "", err
		}
		if _, ok := physicalUSBPID(id); ok {
			return strings.ToLower(id), nil
		}
		var parent uint32
		r, _, _ := getParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(node), 0)
		if r != 0 {
			return "", fmt.Errorf("无法定位物理 USB 设备（配置管理器状态 %d）", r)
		}
		node = parent
	}
	return "", fmt.Errorf("设备父节点层级异常")
}

func propertyString(info uintptr, d *deviceInfoData, property uint32) string {
	var b [1024]uint16
	r, _, _ := getProperty.Call(info, uintptr(unsafe.Pointer(d)), uintptr(property), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Sizeof(b)), 0)
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(b[:])
}

func discoverRazerDevices() ([]device, error) {
	usb, _ := syscall.UTF16PtrFromString("USB")
	info, _, e := getClassDevs.Call(0, uintptr(unsafe.Pointer(usb)), 0, 2|4)
	if info == uintptr(syscall.InvalidHandle) {
		return nil, windowsError("枚举 USB 设备", e)
	}
	defer destroyInfo.Call(info)
	devices := make(map[string]*device)
	for i := 0; ; i++ {
		d := deviceInfoData{}
		d.Size = uint32(unsafe.Sizeof(d))
		r, _, e := enumInfo.Call(info, uintptr(i), uintptr(unsafe.Pointer(&d)))
		if r == 0 {
			if e != syscall.Errno(259) {
				return nil, windowsError("读取 USB 设备列表", e)
			}
			break
		}
		id, err := nodeID(d.DevInst)
		if errors.Is(err, errNodeGone) {
			continue
		}
		if err != nil {
			return nil, err
		}
		pid, ok := physicalUSBPID(id)
		if !ok || !isMousePID(pid) {
			continue
		}
		key := strings.ToLower(id)
		m, known := models[pid]
		if !known {
			name := propertyString(info, &d, 12)
			if name == "" {
				name = propertyString(info, &d, 0)
			}
			if name == "" || name == "USB Composite Device" || name == "USB 复合设备" {
				name = "雷蛇设备（型号待适配）"
			}
			m = modelInfo{Name: name, Kind: "其他", Connection: "USB"}
		}
		devices[key] = &device{ID: key, PID: pid, Info: m}
	}
	if len(devices) == 0 {
		return nil, nil
	}
	if err := attachChannels(devices); err != nil {
		for _, d := range devices {
			for _, c := range d.Channels {
				c.Close()
			}
		}
		return nil, err
	}
	result := make([]device, 0, len(devices))
	for _, d := range devices {
		result = append(result, *d)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Info.Name != result[j].Info.Name {
			return result[i].Info.Name < result[j].Info.Name
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func discoverDevices() ([]device, error) {
	razer, err := discoverRazerDevices()
	if err != nil {
		return nil, err
	}
	logitech, err := discoverLogitechDevices()
	if err != nil {
		closeDevices(razer)
		return nil, err
	}
	devices := append(razer, logitech...)
	sort.Slice(devices, func(i, j int) bool {
		if deviceVID(devices[i]) != deviceVID(devices[j]) {
			return deviceVID(devices[i]) < deviceVID(devices[j])
		}
		if devices[i].Info.Name != devices[j].Info.Name {
			return devices[i].Info.Name < devices[j].Info.Name
		}
		return devices[i].ID < devices[j].ID
	})
	return devices, nil
}

func attachChannels(devices map[string]*device) error {
	info, _, e := getClassDevs.Call(uintptr(unsafe.Pointer(&hidGUID)), 0, 0, 2|0x10)
	if info == uintptr(syscall.InvalidHandle) {
		return windowsError("枚举 HID 通道", e)
	}
	defer destroyInfo.Call(info)
	for i := 0; ; i++ {
		iface := interfaceData{}
		iface.Size = uint32(unsafe.Sizeof(iface))
		r, _, e := enumInterfaces.Call(info, 0, uintptr(unsafe.Pointer(&hidGUID)), uintptr(i), uintptr(unsafe.Pointer(&iface)))
		if r == 0 {
			if e != syscall.Errno(259) {
				return windowsError("读取 HID 通道列表", e)
			}
			break
		}
		var need uint32
		interfaceDetail.Call(info, uintptr(unsafe.Pointer(&iface)), 0, 0, uintptr(unsafe.Pointer(&need)), 0)
		if need < 6 || need > 65536 {
			continue
		}
		b := make([]byte, need)
		binary.LittleEndian.PutUint32(b[:4], 8)
		d := deviceInfoData{}
		d.Size = uint32(unsafe.Sizeof(d))
		r, _, _ = interfaceDetail.Call(info, uintptr(unsafe.Pointer(&iface)), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, uintptr(unsafe.Pointer(&d)))
		if r == 0 {
			continue
		}
		// UTF-16 按实际缓冲区边界解码，不伪造无限长度数组。
		chars := make([]uint16, (len(b)-4)/2)
		for j := range chars {
			chars[j] = binary.LittleEndian.Uint16(b[4+j*2:])
		}
		path := syscall.UTF16ToString(chars)
		if !strings.Contains(strings.ToLower(path), "vid_1532") {
			continue
		}
		id, err := physicalParent(d.DevInst)
		if err != nil {
			continue
		}
		dev, ok := devices[id]
		if !ok {
			continue
		}
		p, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			continue
		}
		access := uint32(0)
		if hasMouseCommands(dev.PID) {
			access = syscall.GENERIC_WRITE
		}
		h, err := syscall.CreateFile(p, access, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			if hasMouseCommands(dev.PID) {
				dev.AccessError = windowsError("打开 HID 通道", err)
			}
			continue
		}
		if _, known := models[dev.PID]; !known {
			var product [128]uint16
			if r, _, _ := getProduct.Call(uintptr(h), uintptr(unsafe.Pointer(&product[0])), uintptr(unsafe.Sizeof(product))); r != 0 {
				if name := syscall.UTF16ToString(product[:]); name != "" {
					dev.Info.Name = name
				}
			}
		}
		if !hasMouseCommands(dev.PID) {
			syscall.CloseHandle(h)
			continue
		}
		var prep uintptr
		caps := hidCaps{}
		valid := false
		if r, _, _ := getPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&prep))); r != 0 && prep != 0 {
			status, _, _ := getCaps.Call(prep, uintptr(unsafe.Pointer(&caps)))
			valid = status == 0x00110000 && caps.FeatureLength == 91
			freePreparsed.Call(prep)
		}
		if valid {
			dev.Channels = append(dev.Channels, &windowsChannel{handle: h})
		} else {
			syscall.CloseHandle(h)
		}
	}
	return nil
}

func prepareConsole() (func(), bool) {
	getCP := kernel32.NewProc("GetConsoleOutputCP")
	setCP := kernel32.NewProc("SetConsoleOutputCP")
	var mode uint32
	console := syscall.GetConsoleMode(syscall.Handle(os.Stdout.Fd()), &mode) == nil
	var old uintptr
	if console {
		old, _, _ = getCP.Call()
		setCP.Call(65001)
	}
	var pids [2]uint32
	n, _, _ := kernel32.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	// 管道或已有终端运行时绝不等待输入。
	own := console && n == 1 && syscall.GetConsoleMode(syscall.Handle(os.Stdin.Fd()), &mode) == nil
	return func() {
		if old != 0 {
			setCP.Call(old)
		}
	}, own
}

func consoleInteractive() bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(os.Stdin.Fd()), &mode) == nil && syscall.GetConsoleMode(syscall.Handle(os.Stdout.Fd()), &mode) == nil
}
