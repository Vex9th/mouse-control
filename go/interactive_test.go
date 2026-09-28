package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type cliTrackingChannel struct {
	closed    bool
	exchanges int
}

func (c *cliTrackingChannel) Exchange([91]byte, time.Duration) ([91]byte, error) {
	c.exchanges++
	return [91]byte{}, errors.New("测试通道不可读")
}
func (c *cliTrackingChannel) Close() error { c.closed = true; return nil }

func TestSettingsNeverGuessAmongSameModelMice(t *testing.T) {
	one, two := &cliTrackingChannel{}, &cliTrackingChannel{}
	devices := []device{{ID: "one", PID: 0x00bf, Info: models[0x00bf], Channels: []featureChannel{one}}, {ID: "two", PID: 0x00bf, Info: models[0x00bf], Channels: []featureChannel{two}}}
	var out, errOut bytes.Buffer
	opts, _ := parseOptions([]string{"-set-dpi", "1600"})
	code := runSettings(opts, &out, &errOut, func() ([]device, error) { return devices, nil })
	if code != 3 || one.exchanges != 0 || two.exchanges != 0 || !one.closed || !two.closed || !strings.Contains(errOut.String(), "多只鼠标") {
		t.Fatalf("多设备设置应拒绝且关闭通道：code=%d err=%s", code, errOut.String())
	}
	if _, err := selectWriteDevice(devices, "on"); err == nil {
		t.Fatal("不得使用 USB ID 前缀匹配")
	}
	if got, err := selectWriteDevice(devices, "TWO"); err != nil || got.ID != "two" {
		t.Fatalf("完整 ID 应忽略大小写：%v %v", got, err)
	}
}

func TestInteractiveEOFAndInvalidInputTerminate(t *testing.T) {
	for _, input := range []string{"", "\ninvalid\n", "0\n", "2\n"} {
		var out, errOut bytes.Buffer
		code := runInteractive(context.Background(), strings.NewReader(input), &out, &errOut, func() ([]device, error) { return nil, nil })
		if code != 0 {
			t.Errorf("输入 %q 应正常退出：code=%d err=%s", input, code, errOut.String())
		}
	}
}

func TestInteractiveDisconnectedSelectionNeverTargetsReplacement(t *testing.T) {
	oldChannel, newChannel := &cliTrackingChannel{}, &cliTrackingChannel{}
	old := device{ID: "old", PID: 0x00bf, Info: models[0x00bf], Channels: []featureChannel{oldChannel}}
	replacement := device{ID: "replacement", PID: 0x00bf, Info: models[0x00bf], Channels: []featureChannel{newChannel}}
	calls := 0
	enumerate := func() ([]device, error) {
		calls++
		if calls == 1 {
			return []device{old}, nil
		}
		return []device{replacement}, nil
	}
	var out, errOut bytes.Buffer
	code := runInteractive(context.Background(), strings.NewReader("3\n1600\n0\n"), &out, &errOut, enumerate)
	if code != 0 || !strings.Contains(errOut.String(), "未找到指定鼠标") || !oldChannel.closed || !newChannel.closed {
		t.Fatalf("必须保留原选中 ID 并拒绝替换鼠标：code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	// 初次读取旧鼠标与设置后的状态刷新允许读；替换设备不得收到私有写命令。
}

func TestEnumerationErrorClosesReturnedChannels(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		c := &cliTrackingChannel{}
		enumerate := func() ([]device, error) { return []device{{Channels: []featureChannel{c}}}, errors.New("枚举中断") }
		var out, errOut bytes.Buffer
		if interactive {
			runInteractive(context.Background(), strings.NewReader("0\n"), &out, &errOut, enumerate)
		} else {
			runSettings(options{SetRate: 1000}, &out, &errOut, enumerate)
		}
		if !c.closed {
			t.Fatalf("枚举失败也必须关闭已返回通道，interactive=%v", interactive)
		}
	}
}

func TestDirectSettingReportsVerifiedValueAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		ignoreWrite bool
		wantCode    int
		wantText    string
	}{
		{"设置双轴 DPI", []string{"-set-dpi", "1600,1200"}, false, 0, "X 1600 / Y 1200"},
		{"相同 DPI 跳过", []string{"-set-dpi", "800"}, false, 0, "无需写入"},
		{"设置回报率", []string{"-set-rate", "4000"}, false, 0, "4000 Hz"},
		{"读回不一致", []string{"-set-dpi", "1600"}, true, 2, "核对不一致"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sim := &mouseSimulator{dpi: dpiValue{800, 800}, rate: 1000, ignoreWrite: tc.ignoreWrite}
			d := simulatedMouse(0x00bf, sim)
			d.ID = "one"
			opts, err := parseOptions(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := runSettings(opts, &out, &errOut, func() ([]device, error) { return []device{d}, nil })
			if code != tc.wantCode || !strings.Contains(out.String()+errOut.String(), tc.wantText) {
				t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
			}
			if tc.wantCode != 0 && strings.Contains(out.String(), "读回确认") {
				t.Fatal("失败操作被输出为成功")
			}
		})
	}
}

type cliReadOnlyBackend struct {
	closed bool
	writes int
}

func (b *cliReadOnlyBackend) Features() mouseFeatures {
	if b.closed {
		panic("不能访问已关闭后端的能力")
	}
	return mouseFeatures{Battery: true, DPIRanges: []dpiRange{{400, 400, 1}, {800, 800, 1}}, DPIReadOnly: true, RateReadOnly: true, DPIReason: "当前模式锁定 DPI", RateReason: "此接口只提供读取"}
}
func (b *cliReadOnlyBackend) ReadBattery() (reading, error) {
	return reading{PercentUnknown: true, Level: "电量充足"}, nil
}
func (b *cliReadOnlyBackend) ReadDPI() (dpiValue, error) { return dpiValue{800, 800}, nil }
func (b *cliReadOnlyBackend) ReadRate() (int, error)     { return 1000, nil }
func (b *cliReadOnlyBackend) SetDPI(dpiValue) (dpiValue, bool, error) {
	b.writes++
	return dpiValue{}, false, errors.New("不应写入")
}
func (b *cliReadOnlyBackend) SetRate(int) (int, bool, error) {
	b.writes++
	return 0, false, errors.New("不应写入")
}
func (b *cliReadOnlyBackend) Close() error { b.closed = true; return nil }

func TestInteractiveReadOnlyCapabilitiesSurviveClosedBackend(t *testing.T) {
	b := &cliReadOnlyBackend{}
	d := device{VID: 0x046d, PID: 0x00bf, ID: "bluetooth/mouse", Info: modelInfo{Name: "Logi Test Mouse"}, Backend: b}
	var out, errOut bytes.Buffer
	code := runInteractive(context.Background(), strings.NewReader("5\n3\n4\n0\n"), &out, &errOut, func() ([]device, error) { return []device{d}, nil })
	if code != 0 || !b.closed || b.writes != 0 {
		t.Fatalf("菜单必须保留能力但关闭后端且禁写：code=%d closed=%t writes=%d", code, b.closed, b.writes)
	}
	for _, want := range []string{"罗技 Test Mouse", "400 / 800", "只读"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("详情未使用保留的能力 %q：%s", want, out.String())
		}
	}
	for _, want := range []string{"当前模式锁定 DPI", "此接口只提供读取"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("未解释只读限制 %q：%s", want, errOut.String())
		}
	}
}

func TestQuerySnapshotsCapabilitiesBeforeBackendClose(t *testing.T) {
	b := &cliReadOnlyBackend{}
	d := device{VID: 0x046d, PID: 0x00bf, Info: modelInfo{Name: "Test Mouse"}, Backend: b}
	var out, errOut bytes.Buffer
	code := runQuery(context.Background(), options{Details: true}, &out, &errOut, func() ([]device, error) { return []device{d}, nil })
	if code != 0 || !b.closed || !strings.Contains(out.String(), "当前模式锁定 DPI") || !strings.Contains(out.String(), "1000 Hz") {
		t.Fatalf("查询后能力不可用：code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}
