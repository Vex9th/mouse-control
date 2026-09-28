//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

var (
	getHIDAttributes  = hidDLL.NewProc("HidD_GetAttributes")
	getHIDValueCaps   = hidDLL.NewProc("HidP_GetValueCaps")
	getHIDButtonCaps  = hidDLL.NewProc("HidP_GetButtonCaps")
	getDeviceProperty = setupapi.NewProc("SetupDiGetDevicePropertyW")
)

type hidAttributes struct {
	Size                     uint32
	Vendor, Product, Version uint16
}
type devicePropertyKey struct {
	Format guid
	ID     uint32
}

var containerIDKey = devicePropertyKey{Format: guid{0x8c7ed206, 0x3f8a, 0x4827, [8]byte{0xb3, 0xab, 0xae, 0x9e, 0x1f, 0xae, 0xfc, 0x6c}}, ID: 2}

func hidContainerID(info uintptr, d *deviceInfoData) string {
	var value guid
	var kind, required uint32
	r, _, _ := getDeviceProperty.Call(info, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&containerIDKey)), uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value), uintptr(unsafe.Pointer(&required)), 0)
	if r == 0 || kind != 0x0d || required != 16 || value == (guid{}) {
		return ""
	}
	return fmt.Sprintf("%08x-%04x-%04x-%x", value.Data1, value.Data2, value.Data3, value.Data4)
}

func logitechPhysicalID(info uintptr, d *deviceInfoData, path string, pid uint16) (string, string, uint16) {
	node := d.DevInst
	btID := ""
	for depth := 0; depth < 32; depth++ {
		id, err := nodeID(node)
		if err != nil {
			break
		}
		lower := strings.ToLower(id)
		if strings.HasPrefix(lower, `bthenum\`) || strings.HasPrefix(lower, `bthledevice\`) || strings.HasPrefix(lower, `bthle\`) {
			btID = lower
		}
		parts := strings.Split(lower, `\`)
		if btID == "" && len(parts) == 3 && strings.HasPrefix(parts[1], "vid_046d&pid_") && parts[0] == "usb" && !strings.Contains(parts[1], "&mi_") {
			var rootPID uint16
			if _, e := fmt.Sscanf(parts[1], "vid_046d&pid_%04x", &rootPID); e == nil {
				return lower, "USB", rootPID
			}
		}
		var parent uint32
		status, _, _ := getParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(node), 0)
		if status != 0 {
			break
		}
		node = parent
	}
	if btID != "" {
		if id := hidContainerID(info, d); id != "" {
			return "bluetooth:046d:" + id, "蓝牙", pid
		}
		return btID, "蓝牙", pid
	}
	if id := hidContainerID(info, d); id != "" {
		return "hid:046d:" + id, "USB / HID", pid
	}
	// 不能只按PID归并未知拓扑；宁可保留独立接口身份，也不合并两只同型号鼠标。
	return strings.ToLower(path), "HID", pid
}

// HIDP_VALUE_CAPS和HIDP_BUTTON_CAPS均为72字节，ReportID位于偏移2。
// 从描述符能力读取报告ID，不能只凭20字节长度向未知vendor接口发送命令。
func hidReportIDs(prep uintptr, caps hidCaps, output bool) map[byte]bool {
	typeID := uintptr(0)
	valueCount, buttonCount := caps.Counts[2], caps.Counts[1]
	if output {
		typeID = 1
		valueCount, buttonCount = caps.Counts[5], caps.Counts[4]
	}
	ids := map[byte]bool{}
	for _, item := range []struct {
		count uint16
		proc  *syscall.LazyProc
	}{{valueCount, getHIDValueCaps}, {buttonCount, getHIDButtonCaps}} {
		if item.count == 0 || item.count > 256 {
			continue
		}
		b := make([][72]byte, item.count)
		count := item.count
		r, _, _ := item.proc.Call(typeID, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&count)), prep)
		if r != 0x00110000 || count > item.count {
			continue
		}
		for _, record := range b[:count] {
			if hidppReportSize(record[2]) > 0 {
				ids[record[2]] = true
			}
		}
	}
	return ids
}

func inspectLogitechHID(info uintptr, d *deviceInfoData, path string, groups map[string]*logitechHIDGroup) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	h, err := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return
	}
	defer syscall.CloseHandle(h)
	a := hidAttributes{}
	a.Size = uint32(unsafe.Sizeof(a))
	r, _, _ := getHIDAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&a)))
	if r == 0 || a.Vendor != 0x046d {
		return
	}
	id, connection, pid := logitechPhysicalID(info, d, path, a.Product)
	group := groups[id]
	if group == nil {
		group = &logitechHIDGroup{id: id, pid: pid, connection: connection}
		groups[id] = group
	}
	var product [256]uint16
	if r, _, _ := getProduct.Call(uintptr(h), uintptr(unsafe.Pointer(&product[0])), uintptr(unsafe.Sizeof(product))); r != 0 {
		if name := syscall.UTF16ToString(product[:]); name != "" {
			group.name = name
		}
	}
	var prep uintptr
	if r, _, _ := getPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&prep))); r == 0 || prep == 0 {
		return
	}
	defer freePreparsed.Call(prep)
	var caps hidCaps
	if r, _, _ := getCaps.Call(prep, uintptr(unsafe.Pointer(&caps))); r != 0x00110000 {
		return
	}
	if caps.UsagePage == 1 && caps.Usage == 2 {
		group.mouse = true
	}
	if !hidppUsage(caps.UsagePage, caps.Usage, connection == "蓝牙") || caps.InputLength < 7 || caps.InputLength > 4096 || caps.OutputLength < 7 || caps.OutputLength > 4096 {
		return
	}
	inputs, outputs := hidReportIDs(prep, caps, false), hidReportIDs(prep, caps, true)
	if len(inputs) == 0 || len(outputs) == 0 {
		return
	}
	group.candidates = append(group.candidates, logitechHIDCandidate{path: path, spec: hidppCollectionSpec{InputLength: int(caps.InputLength), OutputLength: int(caps.OutputLength), OutputIDs: outputs}})
}

func enumerateLogitechGroups() ([]*logitechHIDGroup, error) {
	info, _, e := getClassDevs.Call(uintptr(unsafe.Pointer(&hidGUID)), 0, 0, 2|0x10)
	if info == uintptr(syscall.InvalidHandle) {
		return nil, windowsError("枚举罗技 HID 设备", e)
	}
	defer destroyInfo.Call(info)
	groups := map[string]*logitechHIDGroup{}
	for i := 0; ; i++ {
		iface := interfaceData{}
		iface.Size = uint32(unsafe.Sizeof(iface))
		r, _, e := enumInterfaces.Call(info, 0, uintptr(unsafe.Pointer(&hidGUID)), uintptr(i), uintptr(unsafe.Pointer(&iface)))
		if r == 0 {
			if e != syscall.Errno(259) {
				return nil, windowsError("读取罗技 HID 列表", e)
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
		if r, _, _ := interfaceDetail.Call(info, uintptr(unsafe.Pointer(&iface)), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, uintptr(unsafe.Pointer(&d))); r == 0 {
			continue
		}
		chars := make([]uint16, (len(b)-4)/2)
		for j := range chars {
			chars[j] = binary.LittleEndian.Uint16(b[4+j*2:])
		}
		inspectLogitechHID(info, &d, syscall.UTF16ToString(chars), groups)
	}
	result := make([]*logitechHIDGroup, 0, len(groups))
	for _, g := range groups {
		result = append(result, g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, nil
}

func openLogitechHID(group *logitechHIDGroup) (*windowsHIDPP, error) {
	t := &windowsHIDPP{bluetooth: group.connection == "蓝牙"}
	for _, candidate := range group.candidates {
		if len(t.channels) >= 8 {
			t.Close()
			return nil, errors.New("罗技 HID 通道数量超出安全上限")
		}
		p, e := syscall.UTF16PtrFromString(candidate.path)
		if e != nil {
			continue
		}
		h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
		if e != nil {
			group.accessError = windowsError("打开罗技 HID++ 控制通道", e)
			continue
		}
		t.channels = append(t.channels, hidppWindowsChannel{handle: h, spec: candidate.spec})
	}
	if len(t.channels) == 0 {
		if group.accessError != nil {
			return nil, group.accessError
		}
		return nil, errors.New("当前连接未开放 HID++ 控制通道；仅识别设备")
	}
	return t, nil
}

func discoverLogitechDevices() ([]device, error) {
	groups, err := enumerateLogitechGroups()
	if err != nil {
		return nil, err
	}
	var devices []device
	for _, g := range groups {
		receiver, isReceiver := logitechReceiver(g.pid)
		if !isReceiver && !g.mouse {
			continue
		}
		if isReceiver {
			g.name = receiver.Name
			g.connection = receiver.Family + " 接收器"
		}
		if isReceiver && receiver.Slots == 0 {
			devices = append(devices, logitechIdentification(g, g.id, g.name, "此旧式接收器仅识别，尚无可用的查询协议"))
			continue
		}
		t, e := openLogitechHID(g)
		if e != nil {
			devices = append(devices, logitechIdentification(g, g.id, g.name, e.Error()))
			continue
		}
		devices = append(devices, probeLogitechGroup(g, receiver, isReceiver, t)...)
	}
	return devices, nil
}
