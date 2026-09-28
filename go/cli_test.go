package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestChineseReportKeepsEveryDeviceAndUnknownCharge(t *testing.T) {
	rows := []deviceResult{
		{Device: device{ID: "one", PID: 0x00bf, Info: modelInfo{Name: "DeathAdder V4 Pro", Kind: "鼠标", Connection: "无线接收器", Battery: true}}, Reading: reading{Percent: 100, ChargeKnown: true}},
		{Device: device{ID: "two", PID: 0x00bf, Info: modelInfo{Name: "DeathAdder V4 Pro", Kind: "鼠标", Connection: "无线接收器", Battery: true}}, Reading: reading{Percent: 39, ChargeError: errUnsupported}},
		{Device: device{ID: "legacy", PID: 0x0003, Info: modelInfo{Name: "Legacy Mouse", Kind: "鼠标", Connection: "USB"}}, Err: errUnsupported},
		{Device: device{ID: "sleep", PID: 0x00b7, Info: modelInfo{Name: "DeathAdder V3 Pro", Kind: "鼠标", Battery: true}}, Err: errAsleep},
	}
	var out bytes.Buffer
	printReport(&out, rows, time.Date(2026, 9, 28, 22, 30, 0, 0, time.UTC))
	s := out.String()
	for _, want := range []string{"鼠标工具", "[01]", "[02]", "[03]", "[04]", "100%", "39%", "充电状态未知", "未适配", "休眠", "one", "two"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "battery") || strings.Contains(s, "discharging") {
		t.Fatalf("English status leaked: %s", s)
	}
}

func TestCLIArguments(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		wantWatch int
		wantErr   bool
	}{{nil, 0, false}, {[]string{"-once"}, 0, false}, {[]string{"-watch", "10"}, 10, false}, {[]string{"-watch", "0"}, 0, true}, {[]string{"-watch", "-1"}, 0, true}, {[]string{"-watch", "abc"}, 0, true}, {[]string{"-once", "-watch", "10"}, 0, true}, {[]string{"stray"}, 0, true}} {
		opts, err := parseOptions(tc.args)
		if (err != nil) != tc.wantErr {
			t.Errorf("%v: err=%v", tc.args, err)
		}
		if err == nil && opts.Watch != tc.wantWatch {
			t.Errorf("%v watch=%d", tc.args, opts.Watch)
		}
	}
}

func TestUSBIdentityDoesNotMergeSamePID(t *testing.T) {
	for _, s := range []string{`USB\VID_1532&PID_00BF\SERIAL1`, `USB\VID_1532&PID_00BF\SERIAL2`} {
		p, ok := physicalUSBPID(s)
		if !ok || p != 0x00bf {
			t.Errorf("missing USB physical device %s", s)
		}
	}
	for _, s := range []string{`USB\VID_1532&PID_00BF&MI_01\ABC`, `HID\VID_1532&PID_00BF&MI_00\ABC`, `USB\VID_1234&PID_00BF\ABC`} {
		if _, ok := physicalUSBPID(s); ok {
			t.Errorf("not a Razer USB root: %s", s)
		}
	}
}

func TestWatchRecoversEnumerationError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	enumerate := func() ([]device, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("测试：设备暂时移除")
		}
		if calls == 3 {
			cancel()
		}
		return nil, nil
	}
	var out, errOut bytes.Buffer
	code := runQuery(ctx, options{Watch: 1}, &out, &errOut, enumerate)
	if code != 0 || calls != 3 || !strings.Contains(errOut.String(), "设备暂时移除") || !strings.Contains(out.String(), "未发现支持的鼠标") {
		t.Fatalf("code=%d calls=%d out=%s err=%s", code, calls, out.String(), errOut.String())
	}
}

func TestReportExitCodes(t *testing.T) {
	for _, tc := range []struct {
		rows []deviceResult
		want int
	}{
		{nil, 1},
		{[]deviceResult{{Device: device{Info: modelInfo{Battery: false}}, Err: errUnsupported}}, 0},
		{[]deviceResult{{Device: device{Info: modelInfo{Battery: true}}, Err: errAsleep}}, 2},
		{[]deviceResult{{Device: device{Info: modelInfo{Battery: true}}, Err: errAsleep}, {Device: device{Info: modelInfo{Battery: true}}, Reading: reading{Percent: 0, ChargeKnown: true}}}, 0},
	} {
		if got := reportExitCode(tc.rows); got != tc.want {
			t.Errorf("exit=%d want %d", got, tc.want)
		}
	}
}

func TestChineseModelNameIsNotDoublePrefixed(t *testing.T) {
	if got := displayName("雷蛇设备（型号待适配）"); got != "雷蛇设备（型号待适配）" {
		t.Fatalf("品牌重复：%s", got)
	}
	if got := displayName("Razer DeathAdder V4 Pro (Wireless)"); got != "雷蛇 DeathAdder V4 Pro （无线）" {
		t.Fatalf("连接模式未中文化：%s", got)
	}
}

func TestMouseArgumentsAcceptActionsAndRejectConflicts(t *testing.T) {
	valid := [][]string{{"-interactive"}, {"-details"}, {"-set-dpi", "1600"}, {"-set-dpi=1600,1200", "-device", `USB\VID_1532&PID_00BF\ONE`}, {"-set-rate", "1000"}}
	for _, args := range valid {
		if _, err := parseOptions(args); err != nil {
			t.Errorf("合法鼠标命令 %v 被拒绝：%v", args, err)
		}
	}
	invalid := [][]string{{"-set-dpi", "0"}, {"-set-dpi", "1600,"}, {"-set-dpi", "1600,1200,800"}, {"-set-dpi", "1600", "-once"}, {"-set-dpi", "1600", "-watch", "1"}, {"-set-dpi", "1600", "-interactive"}, {"-set-rate", "1000", "-h"}, {"-set-rate", "1000", "-supported"}, {"-set-rate", "1000", "-set-dpi", "1600"}, {"-set-rate", "1000", "-set-rate", "500"}, {"-device", ""}}
	for _, args := range invalid {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("不安全或歧义命令 %v 被接受", args)
		}
	}
}

func TestSingleMouseReportHasNoCountersOrProtocolNoise(t *testing.T) {
	var out bytes.Buffer
	printReport(&out, []deviceResult{{Device: device{PID: 0x00bf, Info: models[0x00bf]}, Reading: reading{Percent: 68, ChargeKnown: true}}}, time.Now())
	s := out.String()
	for _, unwanted := range []string{"发现 1 台", "电量读取", "未读到", "[01]", "VID", "PID"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("单鼠标简报仍有 %q：\n%s", unwanted, s)
		}
	}
	if !strings.Contains(s, "68%") {
		t.Fatalf("应保留实际电量：%s", s)
	}
}

func TestReportDisplaysActualMouseSettingsAndUnknownValues(t *testing.T) {
	rows := []deviceResult{{Device: device{PID: 0x00bf, Info: models[0x00bf]}, Reading: reading{Percent: 56, ChargeKnown: true}, DPI: dpiValue{1600, 1200}, Rate: 4000}}
	var out bytes.Buffer
	printReport(&out, rows, time.Now())
	for _, want := range []string{"56%", "X 1600 / Y 1200", "4000 Hz"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("缺少实际状态 %q：%s", want, out.String())
		}
	}
	rows[0].DPI = dpiValue{}
	rows[0].DPIErr = errors.New("鼠标暂时断开")
	rows[0].Rate = 0
	rows[0].RateErr = errors.New("回报率接口拒绝响应")
	out.Reset()
	printReport(&out, rows, time.Now())
	if !strings.Contains(out.String(), "鼠标暂时断开") || !strings.Contains(out.String(), "回报率接口拒绝响应") || strings.Contains(out.String(), " 0 Hz") {
		t.Fatalf("未知读数不得变成零：%s", out.String())
	}
}

func TestSupportedDirectoryOnlyIncludesMice(t *testing.T) {
	var out bytes.Buffer
	printSupported(&out)
	if !strings.Contains(out.String(), "DeathAdder V4 Pro") || !strings.Contains(out.String(), "HyperPolling") {
		t.Fatal("目录缺少鼠标或对应接收器")
	}
	if strings.Contains(out.String(), "BlackWidow") || strings.Contains(out.String(), "BlackShark") {
		t.Fatal("鼠标目录混入键盘或耳机")
	}
}

func TestDPIParsingLeavesModelLimitsToBackend(t *testing.T) {
	for _, input := range []string{"1", "99", "50000", "65535", "50,100"} {
		if _, err := parseDPI(input); err != nil {
			t.Errorf("正整数 %q 应由型号能力决定是否可用：%v", input, err)
		}
	}
	for _, input := range []string{"0", "-1", "65536", "1600,0", "1600,65536"} {
		if _, err := parseDPI(input); err == nil {
			t.Errorf("无法编码的 DPI %q 应拒绝", input)
		}
	}
}

func TestCrossBrandHelpAndDirectoryAreExplicitAboutDetection(t *testing.T) {
	var help, directory bytes.Buffer
	printHelp(&help)
	printSupported(&directory)
	for _, want := range []string{"罗技", "HID++", "LIGHTSPEED", "Unifying", "Bolt"} {
		if !strings.Contains(directory.String(), want) {
			t.Errorf("目录缺少支持边界 %q", want)
		}
	}
	if strings.Contains(help.String(), "物理 USB ID") || !strings.Contains(help.String(), "设备 ID") {
		t.Fatal("设备选择说明必须容纳蓝牙与接收器槽位")
	}
}

func TestCrossBrandReportUsesDeviceCapabilitiesAndUnknownBattery(t *testing.T) {
	caps := mouseFeatures{Battery: true, DPIRanges: []dpiRange{{200, 12000, 50}}, PollRates: []int{125, 500, 1000}}
	logi := device{VID: 0x046d, PID: 0x00bf, ID: "receiver/slot/2", Info: modelInfo{Name: "Logitech G Example", Connection: "LIGHTSPEED"}, Caps: &caps}
	row := deviceResult{Device: logi, Reading: reading{PercentUnknown: true, Level: "电量充足", VoltageMV: 3900, ChargeText: "使用电池"}, DPI: dpiValue{800, 800}, Rate: 1000}
	var out bytes.Buffer
	printReport(&out, []deviceResult{row}, time.Now())
	printDetails(&out, []deviceResult{row})
	for _, want := range []string{"罗技 G Example", "电量充足", "3900 mV", "使用电池", "VID 046D / PID 00BF", "200–12000（步长 50）", "统一设置双轴"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("缺少鼠标实际能力 %q：%s", want, out.String())
		}
	}
	for _, bad := range []string{"雷蛇", "0%", "45000", "电量偏低"} {
		if strings.Contains(out.String(), bad) {
			t.Errorf("品牌/电量/能力串用 %q：%s", bad, out.String())
		}
	}
	if got := reportExitCode([]deviceResult{row}); got != 0 {
		t.Fatalf("罗技有效读数应成功，exit=%d", got)
	}
	row.Err = errors.New("电量查询失败")
	row.DPIErr = errors.New("DPI 查询失败")
	row.RateErr = errors.New("回报率查询失败")
	if got := reportExitCode([]deviceResult{row}); got != 2 {
		t.Fatalf("罗技所有能力失败应返回2，exit=%d", got)
	}
}

func TestLogitechSamePIDWithoutCapabilitiesDoesNotInheritRazer(t *testing.T) {
	d := device{VID: 0x046d, PID: 0x00bf, Info: modelInfo{Name: "Unknown Mouse"}}
	var out bytes.Buffer
	printDetails(&out, []deviceResult{{Device: d}})
	if strings.Contains(out.String(), "45000") || strings.Contains(out.String(), "8000 Hz") || strings.Contains(out.String(), "雷蛇") {
		t.Fatalf("不能只凭 PID 使用雷蛇能力：%s", out.String())
	}
}

func TestUnknownBatteryKeepsLevelVoltageAndChargeText(t *testing.T) {
	for _, tc := range []struct {
		r    reading
		want string
	}{
		{reading{PercentUnknown: true, Level: "较低"}, "较低"},
		{reading{PercentUnknown: true, VoltageMV: 3750}, "3750 mV"},
		{reading{PercentUnknown: true}, "未知"},
	} {
		if got := batteryLabel(tc.r); got != tc.want {
			t.Errorf("实际电量信息被修改：%q want%q", got, tc.want)
		}
		if strings.Contains(chargeLabel(modelInfo{}, tc.r), "偏低") {
			t.Fatal("未知百分比不能被当成0%")
		}
	}
	if got := chargeLabel(modelInfo{NoCharge: true}, reading{ChargeText: "充电完成"}); got != "充电完成" {
		t.Fatalf("应采用实际充电文本：%s", got)
	}
}

func TestReadOnlyDetailsWithoutSpecificReasonStillExplainRestriction(t *testing.T) {
	caps := mouseFeatures{DPIReadOnly: true, RateReadOnly: true}
	var out bytes.Buffer
	printDetails(&out, []deviceResult{{Device: device{VID: 0x046d, Info: modelInfo{Name: "Test Mouse"}, Caps: &caps}}})
	if strings.Contains(out.String(), "未适配") || !strings.Contains(out.String(), "仅支持读取") {
		t.Fatalf("已支持读取时不能误称未适配：%s", out.String())
	}
}

func TestBatteryDiscoveryTimeoutIsVisibleInDefaultReport(t *testing.T) {
	s := newLogiSimulator(false)
	s.failFeature = 0x1004
	backend, err := probeLogitech(s, 0xff)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	d := device{VID: 0x046d, Info: modelInfo{Name: backend.Name}, Backend: backend}
	row := readMouse(d)
	var out bytes.Buffer
	printReport(&out, []deviceResult{row}, time.Now())
	if !strings.Contains(out.String(), "电量功能 1004 查询失败") || !strings.Contains(out.String(), "simulated timeout") || strings.Contains(out.String(), "未适配电量查询") {
		t.Fatalf("传输故障不能被展示成不支持：%s", out.String())
	}
	if got := reportExitCode([]deviceResult{row}); got != 0 {
		t.Fatalf("电量发现失败但 DPI/回报率有效，应保留部分成功退出码：%d", got)
	}
}

func TestAllLogitechDiscoveryFailuresAreNotSuccessfulIdentification(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
		want int
	}{{"三种能力均超时", true, 2}, {"三种能力均未提供", false, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLogiSimulator(false)
			delete(s.features, 0x1004)
			delete(s.features, 0x2201)
			delete(s.features, 0x8060)
			transport := logiTransportFunc(func(q []byte, match func([]byte) bool, timeout time.Duration) ([]byte, error) {
				if tc.fail && q[2] == 0 && q[3]&0xf0 == 0 {
					id := uint16(q[4])<<8 | uint16(q[5])
					if id == 0x1004 || id == 0x2202 || id == 0x8061 {
						return nil, errors.New("能力发现超时")
					}
				}
				return s.Exchange(q, match, timeout)
			})
			backend, err := probeLogitech(transport, 0xff)
			if err != nil {
				t.Fatal(err)
			}
			d := device{VID: 0x046d, Info: modelInfo{Name: backend.Name}, Backend: backend}
			var out, errOut bytes.Buffer
			code := runQuery(context.Background(), options{}, &out, &errOut, func() ([]device, error) { return []device{d}, nil })
			if code != tc.want {
				t.Fatalf("发现异常与真正缺失应区分：exit=%d want=%d out=%s err=%s", code, tc.want, out.String(), errOut.String())
			}
		})
	}
}
