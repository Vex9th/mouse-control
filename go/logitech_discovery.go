package main

import "fmt"

type logitechHIDCandidate struct {
	path string
	spec hidppCollectionSpec
}
type logitechHIDGroup struct {
	id, name, connection string
	pid                  uint16
	mouse                bool
	candidates           []logitechHIDCandidate
	accessError          error
}

func logitechIdentification(g *logitechHIDGroup, id, name, reason string) device {
	if name == "" {
		name = fmt.Sprintf("Logitech 鼠标（PID %04X）", g.pid)
	}
	caps := mouseFeatures{BatteryReason: reason, DPIReason: reason, RateReason: reason, Notes: []string{reason}}
	kind := "鼠标"
	if _, ok := logitechReceiver(g.pid); ok && id == g.id {
		kind = "接收器"
	}
	return device{ID: id, VID: 0x046d, PID: g.pid, Info: modelInfo{Name: name, Kind: kind, Connection: g.connection}, Caps: &caps}
}

func probeLogitechGroup(g *logitechHIDGroup, receiver receiverInfo, isReceiver bool, t hidppTransport) []device {
	var devices []device
	addresses := []byte{0xff}
	if g.pid == 0xc088 && !isReceiver {
		addresses = []byte{1}
	}
	if isReceiver {
		addresses = nil
		for slot := byte(1); slot <= receiver.Slots; slot++ {
			addresses = append(addresses, slot)
		}
	}
	before := len(devices)
	var lastErr error
	for _, address := range addresses {
		var pairing logitechPairing
		if isReceiver {
			var pairErr error
			pairing, pairErr = readLogitechPairing(t, address, receiver.Family)
			if pairErr == nil && !pairing.Paired {
				continue
			}
			if pairErr != nil {
				lastErr = pairErr
			}
		}
		m, probeErr := probeLogitech(t, address)
		id := g.id
		if isReceiver {
			id += fmt.Sprintf("#slot=%d", address)
		}
		if probeErr != nil {
			lastErr = probeErr
			if pairing.Paired && (pairing.Kind == "鼠标" || pairing.Kind == "轨迹球") {
				if pairing.Identity != "" {
					id += "#id=" + pairing.Identity
				}
				devices = append(devices, logitechIdentification(g, id, pairing.Name, "鼠标当前不应答："+probeErr.Error()))
			}
			continue
		}
		if isReceiver {
			m.SetReceiverIdentity(pairing.Name, pairing.Kind, pairing.Identity)
		} else {
			m.SetDirectConnection()
		}
		if !m.IsMouse && !isReceiver {
			m.SetReceiverIdentity(g.name, "鼠标", "")
		}
		if !m.IsMouse {
			continue
		}
		if m.Identity != "" {
			id += "#id=" + m.Identity
		}
		name := m.Name
		if name == "" {
			name = g.name
		}
		devices = append(devices, device{ID: id, VID: 0x046d, PID: g.pid, Info: modelInfo{Name: name, Kind: m.Kind, Connection: g.connection}, Backend: m})
	}
	// 后端共享同一物理HID连接，所有读数完成后由幂等Close统一回收。
	retained := false
	for _, d := range devices[before:] {
		if d.Backend != nil {
			retained = true
			break
		}
	}
	if !retained {
		t.Close()
	}
	if len(devices) == before {
		reason := "未发现在线且可识别的鼠标"
		if lastErr != nil {
			reason += "：" + lastErr.Error()
		}
		devices = append(devices, logitechIdentification(g, g.id, g.name, reason))
	}
	return devices
}
