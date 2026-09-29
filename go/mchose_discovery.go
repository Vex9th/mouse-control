package main

import "fmt"

func mchoseCollectionCompatible(vid, pid, page uint16, inputLength, outputLength int, inputID, outputID bool) bool {
	return mchoseUSBSupported(vid, pid) && (page == 0xff01 || page == 0xff0b) && inputLength == 64 && outputLength == 64 && inputID && outputID
}
func mchoseIdentification(id string, pid uint16, reason string, failed bool) device {
	name, kind, connection := "MCHOSE 共用无线接收器", "接收器", "无线接收器"
	if m, ok := mchoseModels[pid]; ok {
		name, kind, connection = m.Name, "鼠标", "USB"
	}
	caps := mouseFeatures{DiscoveryFailed: failed, BatteryReason: reason, DPIReadOnly: true, RateReadOnly: true, DPIReason: reason, RateReason: reason, Notes: []string{"迈从新协议实验性只读支持；尚无真机验证", reason}}
	return device{ID: id, VID: mchoseVID, PID: pid, Info: modelInfo{Name: name, Kind: kind, Connection: connection}, Caps: &caps}
}
func probeMchoseDevice(id string, pid uint16, t mchoseTransport) device {
	m, e := probeMchose(t, pid)
	if e != nil {
		if t != nil {
			if ce := t.Close(); ce != nil {
				e = fmt.Errorf("%w；关闭迈从通道：%v", e, ce)
			}
		}
		d := mchoseIdentification(id, pid, "未取得有效鼠标响应："+e.Error(), true)
		d.AccessError = e
		return d
	}
	connection := "USB"
	if mchoseReceiver(pid) {
		connection = "无线接收器"
	}
	return device{ID: id, VID: mchoseVID, PID: pid, Info: modelInfo{Name: m.Name, Kind: "鼠标", Connection: connection, Battery: true}, Backend: m}
}

type mchoseHIDCandidate struct {
	path string
	page uint16
}
type mchoseHIDGroup struct {
	id            string
	pid           uint16
	candidates    []mchoseHIDCandidate
	descriptorErr error
}

func mchoseGroupIdentification(g *mchoseHIDGroup) device {
	if g.descriptorErr == nil {
		return mchoseIdentification(g.id, g.pid, "未发现已适配的 HID 0x4D/64 字节通道；仅识别设备", false)
	}
	d := mchoseIdentification(g.id, g.pid, "读取迈从控制描述符失败："+g.descriptorErr.Error(), true)
	d.AccessError = g.descriptorErr
	return d
}
