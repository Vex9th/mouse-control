package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type logitechPairing struct {
	Name, Kind, Identity string
	Paired               bool
}

func readLogitechPairingRegister(t hidppTransport, selector, part byte) ([]byte, error) {
	q := []byte{0x10, 0xFF, 0x83, 0xB5, selector, part, 0}
	match := func(r []byte) bool {
		return hidppReplyMatches(q, r) && (r[2] == 0x8F || r[2] == 0xFF || r[4] == selector)
	}
	r, err := t.Exchange(q, match, 800*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !match(r) {
		return nil, fmt.Errorf("%w：配对表响应不符", errFrame)
	}
	if r[2] == 0x8F || r[2] == 0xFF {
		return nil, &hidppProtocolError{Legacy: r[2] == 0x8F, Code: r[5]}
	}
	return r[4:], nil
}
func logitechLegacyKind(kind byte) string {
	switch kind & 15 {
	case 1:
		return "键盘"
	case 2:
		return "鼠标"
	case 8:
		return "轨迹球"
	case 9:
		return "触控板"
	default:
		return "未知"
	}
}

// 返回配对记录，不代表设备在线。失败保留错误供调用方决定是否继续只读 ping。
func readLogitechPairing(t hidppTransport, slot byte, family string) (logitechPairing, error) {
	var result logitechPairing
	if slot < 1 || slot > 7 || (family == "bolt" && slot > 6) {
		return result, errors.New("无效接收器槽位")
	}
	if family != "bolt" && family != "unifying" && family != "nano" && family != "lightspeed" {
		return result, errors.New("该接收器族没有已适配的配对读取协议")
	}
	selector := byte(0x20) + slot - 1
	if family == "bolt" {
		selector = 0x50 + slot
	}
	p, err := readLogitechPairingRegister(t, selector, 0)
	if err != nil {
		return result, fmt.Errorf("槽位 %d 配对信息：%w", slot, err)
	}
	if err = requireHIDPPData(p, 8); err != nil {
		return result, err
	}
	wpid := binary.BigEndian.Uint16(p[3:5])
	result.Kind = logitechLegacyKind(p[7])
	var serial []byte
	if family == "bolt" {
		wpid = binary.LittleEndian.Uint16(p[2:4])
		result.Kind = logitechLegacyKind(p[1])
		serial = append([]byte(nil), p[4:8]...)
	}
	if wpid == 0 || wpid == 0xFFFF {
		return result, errors.New("配对表未返回有效 WPID，槽位状态未知")
	}
	result.Paired = true
	result.Identity = fmt.Sprintf("wpid:%04X", wpid)
	var detailErrors []error
	if family != "bolt" {
		info, e := readLogitechPairingRegister(t, 0x30+slot-1, 0)
		if e != nil {
			detailErrors = append(detailErrors, fmt.Errorf("配对序列号：%w", e))
		} else if e = requireHIDPPData(info, 5); e != nil {
			detailErrors = append(detailErrors, e)
		} else {
			serial = append([]byte(nil), info[1:5]...)
		}
	}
	if len(serial) == 4 {
		nonzero, nonones := false, false
		for _, b := range serial {
			nonzero = nonzero || b != 0
			nonones = nonones || b != 0xFF
		}
		if nonzero && nonones {
			result.Identity += "/serial:" + hex.EncodeToString(serial)
		}
	}
	selector = 0x40 + slot - 1
	part := byte(0)
	if family == "bolt" {
		selector, part = 0x60+slot, 1
	}
	name, e := readLogitechPairingRegister(t, selector, part)
	if e != nil {
		detailErrors = append(detailErrors, fmt.Errorf("配对名称：%w", e))
	} else {
		start, lengthIndex := 2, 1
		if family == "bolt" {
			start, lengthIndex = 3, 2
		}
		if e = requireHIDPPData(name, start); e != nil {
			detailErrors = append(detailErrors, e)
		} else {
			n := int(name[lengthIndex])
			if n > len(name)-start {
				n = len(name) - start
				detailErrors = append(detailErrors, errors.New("配对名称只收到首片段"))
			}
			result.Name = cleanLogitechText(string(name[start : start+n]))
		}
	}
	return result, errors.Join(detailErrors...)
}
