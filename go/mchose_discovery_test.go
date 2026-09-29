package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestMchoseCollectionRequiresExactDescriptor(t *testing.T) {
	for _, page := range []uint16{0xff01, 0xff0b} {
		if !mchoseCollectionCompatible(0x3837, 0x4035, page, 64, 64, true, true) {
			t.Fatal("拒绝已知接口")
		}
	}
	for _, v := range []struct {
		vid, pid, page uint16
		in, out        int
		ridIn, ridOut  bool
	}{
		{0x3837, 0x4035, 1, 64, 64, true, true}, {0x3837, 0x4035, 0xff01, 65, 64, true, true}, {0x3837, 0x4035, 0xff01, 64, 63, true, true}, {0x3837, 0x4035, 0xff01, 64, 64, false, true}, {0x3837, 0x4035, 0xff01, 64, 64, true, false}, {0x3837, 0x9999, 0xff01, 64, 64, true, true}, {0x5253, 0x4035, 0xff01, 64, 64, true, true},
	} {
		if mchoseCollectionCompatible(v.vid, v.pid, v.page, v.in, v.out, v.ridIn, v.ridOut) {
			t.Fatalf("接受不符描述符 %+v", v)
		}
	}
}
func TestMchoseDiscoveryFailureClosesAndDoesNotInventReceiverModel(t *testing.T) {
	s := newMchoseSimulator()
	s.failure = errors.New("设备不在线")
	d := probeMchoseDevice("USB/A", 0x1014, s)
	if d.Backend != nil || s.closed != 1 || d.Info.Kind != "接收器" || strings.Contains(d.Info.Name, "A5") || !deviceFeatures(d).DiscoveryFailed || d.AccessError == nil {
		t.Fatalf("虚假识别 %+v closed=%d", d, s.closed)
	}
	r := readMouse(d)
	if reportExitCode([]deviceResult{r}) == 0 {
		t.Fatal("探测失败显示成功")
	}
}
func TestMchoseGUIAndCLIKeepVendorAndReadOnlyReasons(t *testing.T) {
	var sim *mchoseSimulator
	svc := newGUIService(func() ([]device, error) {
		sim = newMchoseSimulator()
		return []device{probeMchoseDevice("USB/A", 0x1014, sim)}, nil
	})
	snap, e := svc.Scan()
	if e != nil || len(snap.Devices) != 1 {
		t.Fatalf("Scan %v %+v", e, snap)
	}
	d := snap.Devices[0]
	if d.Vendor != "mchose" || d.VID != 0x3837 || d.Status != "online" || !d.Capabilities.DPIReadOnly || d.Capabilities.DPIReason == "" {
		t.Fatalf("DTO %+v", d)
	}
	if sim.closed != 1 {
		t.Fatalf("GUI 未关闭句柄 %d", sim.closed)
	}
	dev := mchoseIdentification("USB/A", 0x1014, "未发现在线鼠标", true)
	var out bytes.Buffer
	printDetails(&out, []deviceResult{readMouse(dev)})
	if !strings.Contains(out.String(), "迈从") || strings.Contains(out.String(), "厂商：雷蛇") {
		t.Fatal(out.String())
	}
	if got := displayName("MCHOSE A5 V3 Pro"); got != "迈从 A5 V3 Pro" {
		t.Fatal(got)
	}
}
func TestMchoseTransportRejectsAnythingButKnownReadFrames(t *testing.T) {
	q, _ := mchoseReadRequest(0x0900, nil)
	if !mchoseWireRequest(q) {
		t.Fatal("拒绝合法只读请求")
	}
	for _, index := range []int{0, 1, 2, 3, 4, 6, 7, 8, 63} {
		b := append([]byte(nil), q...)
		b[index] ^= 1
		if mchoseWireRequest(b) {
			t.Fatalf("允许修改后的请求 offset=%d", index)
		}
	}
	var out bytes.Buffer
	printSupported(&out)
	if !strings.Contains(out.String(), "迈从鼠标 · 实验性只读") || !strings.Contains(out.String(), "4035") {
		t.Fatal("目录遗漏迈从")
	}
}

func TestMchoseDescriptorFailureRemainsARealFailure(t *testing.T) {
	cause := errors.New("HidP_GetCaps NTSTATUS 0xC0110001")
	g := &mchoseHIDGroup{id: "USB/A", pid: 0x4035, descriptorErr: cause}
	d := mchoseGroupIdentification(g)
	if !deviceFeatures(d).DiscoveryFailed || !errors.Is(d.AccessError, cause) || !strings.Contains(deviceFeatures(d).BatteryReason, "0xC0110001") {
		t.Fatalf("描述符错误被吞掉 %+v", d)
	}
	g.descriptorErr = nil
	d = mchoseGroupIdentification(g)
	if deviceFeatures(d).DiscoveryFailed || d.AccessError != nil {
		t.Fatal("正常未匹配接口被当作系统失败")
	}
}
