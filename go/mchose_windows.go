//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// 此传输仅适用于已验证描述符的单个物理设备 collection。
// 复用现有 OVERLAPPED 的固定缓冲区、取消和句柄回收逻辑。
type mchoseWindows struct {
	mu         sync.Mutex
	handle     syscall.Handle
	closed     bool
	flushInput func(syscall.Handle) error
}

func (t *mchoseWindows) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return syscall.CloseHandle(t.handle)
}
func mchoseWait(p *hidPending, deadline time.Time) error {
	if p.complete {
		return nil
	}
	ms := deadlineMillis(deadline)
	if ms == 0 {
		return errors.New("迈从读取超时，设备可能休眠或离线")
	}
	r, e := syscall.WaitForSingleObject(p.ov.HEvent, ms)
	if e != nil {
		return windowsError("等待迈从响应", e)
	}
	if r == syscall.WAIT_TIMEOUT {
		return errors.New("迈从读取超时，设备可能休眠或离线")
	}
	if r != 0 {
		return fmt.Errorf("等待迈从响应状态异常 %d", r)
	}
	return p.finish(false)
}
func (t *mchoseWindows) Exchange(q []byte, match func([]byte) bool, timeout time.Duration) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errors.New("迈从通道已关闭")
	}
	if !mchoseWireRequest(q) || match == nil || timeout <= 0 || timeout > 3*time.Second {
		return nil, errors.New("迈从传输只允许已适配的只读请求")
	}
	deadline := time.Now().Add(timeout)
	flush := t.flushInput
	if flush == nil {
		flush = flushWindowsHIDInput
	}
	if e := flush(t.handle); e != nil {
		return nil, e
	}
	read, e := startHIDIO(t.handle, make([]byte, 64), false)
	if e != nil {
		return nil, e
	}
	defer func() { read.close() }()
	write, e := startHIDIO(t.handle, append([]byte(nil), q...), true)
	if e != nil {
		return nil, e
	}
	defer write.close()
	if e = mchoseWait(write, deadline); e != nil {
		return nil, e
	}
	if write.n != 64 {
		return nil, errors.New("迈从读取指令发送不完整")
	}
	for {
		if e = mchoseWait(read, deadline); e != nil {
			return nil, e
		}
		if read.n > 64 {
			return nil, fmt.Errorf("%w：迈从输入长度越界", errFrame)
		}
		b := read.buf[:read.n]
		if match(b) {
			return append([]byte(nil), b...), nil
		}
		read.close()
		if deadlineMillis(deadline) == 0 {
			return nil, errors.New("迈从读取超时")
		}
		read, e = startHIDIO(t.handle, make([]byte, 64), false)
		if e != nil {
			return nil, e
		}
	}
}

func mchosePhysicalID(info uintptr, d *deviceInfoData, path string, pid uint16) string {
	node := d.DevInst
	for depth := 0; depth < 32; depth++ {
		id, e := nodeID(node)
		if e != nil {
			break
		}
		lower := strings.ToLower(id)
		parts := strings.Split(lower, `\`)
		if len(parts) == 3 && parts[0] == "usb" && parts[1] == fmt.Sprintf("vid_3837&pid_%04x", pid) {
			return lower
		}
		var parent uint32
		r, _, _ := getParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(node), 0)
		if r != 0 {
			break
		}
		node = parent
	}
	if id := hidContainerID(info, d); id != "" {
		return "hid:3837:" + id
	}
	return strings.ToLower(path)
}
func mchoseHasReportID(prep uintptr, caps hidCaps, output bool) (bool, error) {
	kind := uintptr(0)
	values, buttons := caps.Counts[2], caps.Counts[1]
	if output {
		kind = 1
		values, buttons = caps.Counts[5], caps.Counts[4]
	}
	var errs []error
	for _, item := range []struct {
		count uint16
		proc  *syscall.LazyProc
	}{{values, getHIDValueCaps}, {buttons, getHIDButtonCaps}} {
		if item.count == 0 {
			continue
		}
		if item.count > 256 {
			errs = append(errs, fmt.Errorf("%w：HID 能力记录数量 %d 超出上限", errFrame, item.count))
			continue
		}
		records := make([][72]byte, item.count)
		count := item.count
		r, _, _ := item.proc.Call(kind, uintptr(unsafe.Pointer(&records[0])), uintptr(unsafe.Pointer(&count)), prep)
		if r != 0x00110000 {
			errs = append(errs, fmt.Errorf("%s(type=%d) NTSTATUS 0x%08X", item.proc.Name, kind, uint32(r)))
			continue
		}
		if count > item.count {
			errs = append(errs, fmt.Errorf("%w：HID 能力返回数量越界", errFrame))
			continue
		}
		for _, record := range records[:count] {
			if record[2] == 0x4d {
				return true, nil
			}
		}
	}
	return false, errors.Join(errs...)
}
func inspectMchoseHID(info uintptr, d *deviceInfoData, path string, groups map[string]*mchoseHIDGroup) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return
	}
	h, e := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if e != nil {
		return
	}
	defer syscall.CloseHandle(h)
	a := hidAttributes{}
	a.Size = uint32(unsafe.Sizeof(a))
	r, _, _ := getHIDAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&a)))
	if r == 0 || !mchoseUSBSupported(a.Vendor, a.Product) {
		return
	}
	id := mchosePhysicalID(info, d, path, a.Product)
	g := groups[id]
	if g == nil {
		g = &mchoseHIDGroup{id: id, pid: a.Product}
		groups[id] = g
	}
	var prep uintptr
	if r, _, e := getPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&prep))); r == 0 || prep == 0 {
		g.descriptorErr = errors.Join(g.descriptorErr, fmt.Errorf("HidD_GetPreparsedData：%w", e))
		return
	}
	defer freePreparsed.Call(prep)
	var caps hidCaps
	if r, _, _ := getCaps.Call(prep, uintptr(unsafe.Pointer(&caps))); r != 0x00110000 {
		g.descriptorErr = errors.Join(g.descriptorErr, fmt.Errorf("HidP_GetCaps NTSTATUS 0x%08X", uint32(r)))
		return
	}
	if !mchoseCollectionCompatible(a.Vendor, a.Product, caps.UsagePage, int(caps.InputLength), int(caps.OutputLength), true, true) {
		return
	}
	inputID, inputErr := mchoseHasReportID(prep, caps, false)
	outputID, outputErr := mchoseHasReportID(prep, caps, true)
	if !inputID || !outputID {
		g.descriptorErr = errors.Join(g.descriptorErr, inputErr, outputErr)
		return
	}
	g.candidates = append(g.candidates, mchoseHIDCandidate{path, caps.UsagePage})
}
func enumerateMchoseGroups() ([]*mchoseHIDGroup, error) {
	info, _, e := getClassDevs.Call(uintptr(unsafe.Pointer(&hidGUID)), 0, 0, 2|0x10)
	if info == uintptr(syscall.InvalidHandle) {
		return nil, windowsError("枚举迈从 HID", e)
	}
	defer destroyInfo.Call(info)
	groups := map[string]*mchoseHIDGroup{}
	for i := 0; ; i++ {
		iface := interfaceData{}
		iface.Size = uint32(unsafe.Sizeof(iface))
		r, _, e := enumInterfaces.Call(info, 0, uintptr(unsafe.Pointer(&hidGUID)), uintptr(i), uintptr(unsafe.Pointer(&iface)))
		if r == 0 {
			if e != syscall.Errno(259) {
				return nil, windowsError("读取迈从 HID 列表", e)
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
		inspectMchoseHID(info, &d, syscall.UTF16ToString(chars), groups)
	}
	result := make([]*mchoseHIDGroup, 0, len(groups))
	for _, g := range groups {
		result = append(result, g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, nil
}
func discoverMchoseDevices() ([]device, error) {
	groups, e := enumerateMchoseGroups()
	if e != nil {
		return nil, e
	}
	var result []device
	for _, g := range groups {
		d := mchoseGroupIdentification(g)
		if len(g.candidates) > 4 {
			d = mchoseIdentification(g.id, g.pid, "迈从控制通道数量超过安全上限", true)
		} else {
			sort.Slice(g.candidates, func(i, j int) bool {
				if g.candidates[i].page != g.candidates[j].page {
					return g.candidates[i].page < g.candidates[j].page
				}
				return g.candidates[i].path < g.candidates[j].path
			})
			for _, c := range g.candidates {
				p, err := syscall.UTF16PtrFromString(c.path)
				if err != nil {
					continue
				}
				h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
				if err != nil {
					d = mchoseIdentification(g.id, g.pid, windowsError("打开迈从控制通道", err).Error(), true)
					continue
				}
				d = probeMchoseDevice(g.id, g.pid, &mchoseWindows{handle: h})
				if d.Backend != nil {
					break
				}
			}
		}
		result = append(result, d)
	}
	return result, nil
}
