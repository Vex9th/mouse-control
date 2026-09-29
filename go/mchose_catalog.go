package main

const mchoseVID uint16 = 0x3837

// 仅收录官方新协议目录 bR 明确列出的型号；旧 Feature Report 家族不能混用。
type mchoseModel struct {
	Name       string
	MaxDPI     int
	Receiver1K bool
}

var mchoseModels = map[uint16]mchoseModel{
	0x4035: {"MCHOSE A5 V3 Pro", 26000, false},
	0x4026: {"MCHOSE A5 V3 Ultra+", 42000, false},
	0x4034: {"MCHOSE A5 V3 Ultra+(3955)", 50000, false},
	0x4027: {"MCHOSE K7 V2 Pro+", 42000, false},
	0x4028: {"MCHOSE K7 V2 Ultra+", 50000, false},
	0x4030: {"MCHOSE A7 V3", 26000, false},
	0x4031: {"MCHOSE A7 V3 Pro", 42000, false},
	0x4032: {"MCHOSE A7 V3 Pro+", 42000, false},
	0x4033: {"MCHOSE A7 V3 Ultra+", 50000, false},
	0x4037: {"MCHOSE K5 Pro", 26000, false},
	0x4038: {"MCHOSE K5 Ultra", 50000, false},
	0x4036: {"MCHOSE R7 Ultra", 50000, false},
	0x402a: {"MCHOSE V7", 26000, true},
	0x4029: {"MCHOSE G3 V3", 12000, true},
}

func mchoseReceiver(pid uint16) bool { return pid == 0x1014 || pid == 0x1018 || pid == 0x1016 }
func mchoseUSBSupported(vid, pid uint16) bool {
	_, ok := mchoseModels[pid]
	return vid == mchoseVID && (ok || mchoseReceiver(pid))
}
func mchoseIdentityMatches(usbPID, reportedPID uint16) bool {
	m, ok := mchoseModels[reportedPID]
	if !ok {
		return false
	}
	if !mchoseReceiver(usbPID) {
		return usbPID == reportedPID
	}
	return (usbPID == 0x1016) == m.Receiver1K
}
func mchoseModelRates(m mchoseModel) []int {
	if m.Receiver1K {
		return []int{125, 500, 1000}
	}
	return []int{125, 500, 1000, 2000, 4000, 8000}
}
