package main

import (
	"strings"
	"testing"
)

func TestCatalogBatteryProtocol(t *testing.T) {
	// 这些值来自固定的 OpenRazer 源码，覆盖旧设备、新设备和键盘接收器。
	cases := []struct {
		pid        uint16
		tid        byte
		waitMillis int
		noCharge   bool
	}{
		{0x001F, 0xFF, 35, false}, // Naga Epic
		{0x007D, 0x3F, 60, false}, // DeathAdder V2 Pro 无线
		{0x00BF, 0x1F, 35, false}, // DeathAdder V4 Pro 无线
		{0x0062, 0x1F, 405, true}, // Atheris 接收器
		{0x0094, 0x1F, 405, true}, // Orochi V2 接收器
		{0x00B8, 0x1F, 60, true},  // Viper V3 HyperSpeed
		{0x025A, 0x3F, 35, false}, // BlackWidow V3 Pro 有线
		{0x0271, 0x9F, 35, false}, // BlackWidow V3 Mini 无线
		{0x02BA, 0x9F, 35, false}, // BlackWidow V4 Mini 无线
		{0x02D5, 0x9F, 35, false}, // BlackWidow V4 TKL 无线
	}
	for _, tc := range cases {
		m, ok := models[tc.pid]
		if !ok || !m.Battery {
			t.Errorf("PID %04X 缺少经过核对的电量能力", tc.pid)
			continue
		}
		if m.TID != tc.tid || !m.Scale255 || m.WaitMillis != tc.waitMillis || m.NoCharge != tc.noCharge {
			t.Errorf("PID %04X 电量参数 = %+v；期望 TID=%02X、255刻度、等待%dms、NoCharge=%t", tc.pid, m, tc.tid, tc.waitMillis, tc.noCharge)
		}
	}
}

func TestCatalogIdentificationDoesNotEnableBattery(t *testing.T) {
	cases := []struct {
		pid  uint16
		name string
		kind string
	}{
		{0x0084, "DeathAdder V2", "鼠标"},
		{0x00CB, "Basilisk V3 35K", "鼠标"}, // TID switch 中出现，但没有 charge_level 绑定
		{0x0203, "BlackWidow Chroma", "键盘"},
		{0x0504, "Kraken 7.1 Chroma", "耳机"},
		{0x0F08, "Base Station Chroma", "配件"},
		{0x0F12, "Raptor 27", "其他"},
	}
	for _, tc := range cases {
		m, ok := models[tc.pid]
		if !ok || !strings.Contains(m.Name, tc.name) || m.Kind != tc.kind {
			t.Errorf("PID %04X 的型号识别错误：%+v", tc.pid, m)
		}
		if m.Battery || m.TID != 0 || m.Scale255 || m.NoCharge || m.WaitMillis != 0 {
			t.Errorf("仅识别的 PID %04X 不应启用电量命令：%+v", tc.pid, m)
		}
	}
}

func TestCatalogConnectionModes(t *testing.T) {
	for pid, want := range map[uint16]string{
		0x0048: "有线", // Orochi 的常量没有 WIRED 后缀，device_type 有明确标注。
		0x009C: "无线接收器",
		0x00BF: "无线接收器",
		0x0504: "USB",
	} {
		if got := models[pid].Connection; got != want {
			t.Errorf("PID %04X 连接方式 = %q，期望 %q", pid, got, want)
		}
	}
}

func TestCatalogMetadataIsComplete(t *testing.T) {
	if len(models) < 250 {
		t.Fatalf("设备目录不完整：仅 %d 个 PID", len(models))
	}
	for pid, m := range models {
		if strings.TrimSpace(m.Name) == "" {
			t.Errorf("PID %04X 型号名为空", pid)
		}
		switch m.Kind {
		case "鼠标", "键盘", "耳机", "配件", "其他":
		default:
			t.Errorf("PID %04X 类别无效：%q", pid, m.Kind)
		}
		switch m.Connection {
		case "USB", "有线", "无线接收器":
		default:
			t.Errorf("PID %04X 连接方式无效：%q", pid, m.Connection)
		}
		if m.Battery && (m.TID == 0 || !m.Scale255 || m.WaitMillis < 35) {
			t.Errorf("PID %04X 电量参数缺失：%+v", pid, m)
		}
	}
	if _, ok := models[0xFFFF]; ok {
		t.Fatal("未知 PID 不应伪装成已知型号")
	}
}
