//go:build windows

package main

import (
	"os"
	"testing"
	"time"
	"unsafe"
)

func TestWindowsABISizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("仅支持Windows x64")
	}
	if unsafe.Sizeof(deviceInfoData{}) != 32 || unsafe.Sizeof(interfaceData{}) != 32 || unsafe.Sizeof(hidCaps{}) != 64 {
		t.Fatal("Win32结构布局不符合x64 ABI")
	}
	if unsafe.Sizeof(hidAttributes{}) != 12 || unsafe.Sizeof(devicePropertyKey{}) != 20 || unsafe.Offsetof(hidCaps{}.Counts) != 44 {
		t.Fatal("Logitech HID枚举结构布局不符合x64 ABI")
	}
}

// 真机测试必须显式启用，不在普通单元测试时访问用户设备。
func TestHardwareBattery(t *testing.T) {
	if os.Getenv("RAZER_HARDWARE_TEST") != "1" {
		t.Skip("设置 RAZER_HARDWARE_TEST=1 运行真机只读测试")
	}
	devices, err := discoverDevices()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDevices(devices)
	if len(devices) == 0 {
		t.Fatal("未发现雷蛇USB设备")
	}
	success := 0
	for _, d := range devices {
		if deviceVID(d) != 0x1532 {
			continue
		}
		t.Logf("PID=%04X name=%s channels=%d", d.PID, d.Info.Name, len(d.Channels))
		if !d.Info.Battery {
			continue
		}
		if len(d.Channels) > 0 {
			b, err := d.Channels[0].Exchange(makeRequest(d.Info.TID, 0x80), time.Duration(d.Info.WaitMillis)*time.Millisecond)
			t.Logf("raw battery frame=%X err=%v", b, err)
		}
		r, err := readDevice(d)
		if err != nil {
			t.Errorf("读取电量失败 PID=%04X: %v", d.PID, err)
			continue
		}
		t.Logf("battery=%d chargeKnown=%t charging=%t chargeError=%v", r.Percent, r.ChargeKnown, r.Charging, r.ChargeError)
		if !d.Info.NoCharge && !r.ChargeKnown {
			t.Errorf("充电命令未验证成功 PID=%04X", d.PID)
		}
		success++
	}
	if success == 0 {
		t.Fatal("没有成功的电量读数")
	}
}

func TestHardwareMouseRead(t *testing.T) {
	if os.Getenv("RAZER_HARDWARE_TEST") != "1" {
		t.Skip("设置 RAZER_HARDWARE_TEST=1 运行真机只读测试")
	}
	devices, err := discoverDevices()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDevices(devices)
	if len(devices) == 0 {
		t.Fatal("未发现鼠标")
	}
	for _, d := range devices {
		if deviceVID(d) != 0x1532 {
			continue
		}
		if cap := mouseCapabilities[d.PID]; cap.PollV2 && len(d.Channels) > 0 {
			b, e := d.Channels[0].Exchange(mouseRequest(cap.PollTID, 0, 0xc0, 0), 60*time.Millisecond)
			t.Logf("raw poll frame=%X err=%v", b, e)
		}
		r := readMouse(d)
		t.Logf("PID=%04X DPI=%+v DPIerr=%v Rate=%d RateErr=%v battery=%+v err=%v", d.PID, r.DPI, r.DPIErr, r.Rate, r.RateErr, r.Reading, r.Err)
		cap := mouseCapabilities[d.PID]
		if cap.MaxDPI > 0 && r.DPIErr != nil {
			t.Error(r.DPIErr)
		}
		if len(cap.PollRates) > 0 && r.RateErr != nil {
			t.Error(r.RateErr)
		}
	}
}

// 仅在明确安排的真机设置测试中启用；测试前记录，退出前恢复并再次核对。
func TestHardwareMouseSettingsRoundTrip(t *testing.T) {
	if os.Getenv("RAZER_SETTINGS_TEST") != "1" {
		t.Skip("未启用短暂改值及恢复的真机设置测试")
	}
	devices, err := discoverDevices()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDevices(devices)
	if len(devices) != 1 || deviceVID(devices[0]) != 0x1532 || devices[0].PID != 0x00bf {
		t.Fatal("本次设置验证仅允许单只V4 Pro无线鼠标")
	}
	d := devices[0]
	dpi, _, err := readMouseDPI(d)
	if err != nil {
		t.Fatal(err)
	}
	rate, _, err := readMouseRate(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BEFORE DPI=%+v RATE=%d", dpi, rate)
	defer func() {
		r, _, e := setMouseRate(d, rate)
		if e != nil {
			t.Errorf("恢复回报率失败：%v", e)
		}
		v, _, e := setMouseDPI(d, dpi)
		if e != nil {
			t.Errorf("恢复DPI失败：%v", e)
		}
		t.Logf("RESTORED DPI=%+v RATE=%d", v, r)
	}()
	target := dpiValue{dpi.X + 100, dpi.Y + 100}
	if target.X > 45000 || target.Y > 45000 {
		target = dpiValue{dpi.X - 100, dpi.Y - 100}
	}
	if target.X < 100 || target.Y < 100 {
		t.Fatal("当前分轴值不能安全构造相邻测试值")
	}
	v, changed, err := setMouseDPI(d, target)
	if err != nil || !changed || v != target {
		t.Fatalf("DPI设置：%+v %t %v", v, changed, err)
	}
	t.Logf("CHANGED DPI=%+v", v)
	other := 1000
	if rate == other {
		other = 500
	}
	r, changed, err := setMouseRate(d, other)
	if err != nil || !changed || r != other {
		t.Fatalf("回报率设置：%d %t %v", r, changed, err)
	}
	t.Logf("CHANGED RATE=%d", r)
}
