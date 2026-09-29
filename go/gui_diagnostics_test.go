package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestGUIDiagnosticsAreMemoryOnlyAndDoNotQueryDevices(t *testing.T) {
	calls := 0
	s := newGUIService(func() ([]device, error) { calls++; return nil, nil })
	r := s.Diagnostics()
	if calls != 0 || r.Location != "" || r.SaveError != "" || !strings.Contains(r.Text, "暂无错误记录") {
		t.Fatalf("初始日志 %+v calls=%d", r, calls)
	}
	s.Scan()
	r = s.Diagnostics()
	if calls != 1 || !strings.Contains(r.Text, "暂无错误记录") {
		t.Fatalf("成功扫描被记为错误 %+v", r)
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{`"text":`, `"location":`, `"saveError":`} {
		if !strings.Contains(string(b), k) {
			t.Fatal(string(b))
		}
	}
}
func TestGUIDiagnosticsRecordFailuresAndCapabilitiesWithoutIdentifiers(t *testing.T) {
	id1 := `USB\VID_3837&PID_1014\SECRET-SERIAL-ONE`
	id2 := "private-opaque-identity-two"
	b := guiUsableBackend()
	b.batteryErr = fmt.Errorf("读取 %s 被拒绝；token=TOPSECRET", id2)
	b.dpiErr = errors.New(`open C:\Users\Alice\AppData\private.txt: access denied`)
	b.rateErr = errors.New("打开 /Users/bob/Private/device.log: permission denied")
	b.caps.BatteryReason = "内部错误 " + id1
	b.caps.DPIReason = "DPI 能力获取失败"
	b.caps.RateReason = "回报率能力获取失败"
	s := newGUIService(func() ([]device, error) {
		return []device{guiFixtureDevice(id1, b), {ID: id2, VID: 0x3837, PID: 0x1014, Info: modelInfo{Name: "MCHOSE 共用接收器"}, Caps: &mouseFeatures{}}}, nil
	})
	_, e := s.Scan()
	if e != nil {
		t.Fatal(e)
	}
	r := s.Diagnostics()
	for _, v := range []string{version, runtime.GOOS + "/" + runtime.GOARCH, "扫描设备", "046D:C548", "GUI Mouse", "Bolt", "被拒绝", "access denied", "permission denied", "DPI 能力获取失败", "回报率能力获取失败"} {
		if !strings.Contains(r.Text, v) {
			t.Fatalf("缺少 %q\n%s", v, r.Text)
		}
	}
	for _, v := range []string{id1, id2, "SECRET-SERIAL-ONE", "TOPSECRET", "Alice", "bob", "C:\\Users", "/Users/bob"} {
		if strings.Contains(r.Text, v) {
			t.Fatalf("泄露 %q\n%s", v, r.Text)
		}
	}
}
func TestGUIDiagnosticsSavePlainTextReloadAndMergeConsecutiveErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "error.log")
	log := newGUIDiagnosticLog(path)
	log.record("扫描设备", nil, "同一错误", nil)
	log.record("扫描设备", nil, "同一错误", nil)
	r := log.report()
	if r.SaveError != "" || r.Location != `%LOCALAPPDATA%\MouseControl\logs\error.log` || !strings.Contains(r.Text, "重复次数：2") || !strings.Contains(r.Text, "首次时间：") || !strings.Contains(r.Text, "最后时间：") {
		t.Fatal(r)
	}
	disk, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(disk), "同一错误") || strings.HasPrefix(string(disk), "{") {
		t.Fatal("持久日志不是纯文本")
	}
	reloaded := newGUIDiagnosticLog(path)
	if got := reloaded.report(); got.SaveError != "" || !strings.Contains(got.Text, "重复次数：2") {
		t.Fatal(got)
	}
	reloaded.record("设置 DPI", nil, "其他错误", nil)
	reloaded.record("扫描设备", nil, "同一错误", nil)
	if strings.Count(reloaded.report().Text, "操作：") != 3 {
		t.Fatal("不连续错误被合并")
	}
}
func TestGUIDiagnosticsBoundedUTF8History(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	log := newGUIDiagnosticLog(path)
	for i := 0; i < 35; i++ {
		log.record("扫描设备", nil, fmt.Sprintf("错误%02d %s", i, strings.Repeat("中文诊断", 5000)), nil)
	}
	r := log.report()
	disk, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if len(disk) > 128*1024 || len(r.Text) > 128*1024 || !utf8.Valid(disk) || !utf8.ValidString(r.Text) {
		t.Fatalf("长度或编码不正确 disk=%d report=%d", len(disk), len(r.Text))
	}
	if n := strings.Count(r.Text, "操作："); n > 20 || n == 0 {
		t.Fatalf("记录数 %d", n)
	}
	if strings.Contains(r.Text, "错误00 ") || !strings.Contains(r.Text, "错误34 ") {
		t.Fatal("未保留最新记录")
	}
	if rr := newGUIDiagnosticLog(path).report(); rr.SaveError != "" || !strings.Contains(rr.Text, "错误34 ") {
		t.Fatal(rr.SaveError)
	}
}
func TestGUIDiagnosticsSaveFailureKeepsDeviceCauseAndCopyableMemory(t *testing.T) {
	dir := t.TempDir()
	block := filepath.Join(dir, "private-user-file")
	if e := os.WriteFile(block, []byte("occupied"), 0600); e != nil {
		t.Fatal(e)
	}
	log := newGUIDiagnosticLog(filepath.Join(block, "error.log"))
	cause := errors.New("原始枚举错误")
	s := newGUIService(func() ([]device, error) { return nil, cause })
	s.SetDiagnostics(log)
	if _, e := s.Scan(); !errors.Is(e, cause) {
		t.Fatalf("原始错误被覆盖 %v", e)
	}
	r := s.Diagnostics()
	if r.SaveError == "" || !strings.Contains(r.Text, cause.Error()) || strings.Contains(r.SaveError, dir) || strings.Contains(r.Text, dir) {
		t.Fatalf("写入失败报告 %+v", r)
	}
}
func TestGUIDiagnosticsRejectCorruptOrOversizeHistory(t *testing.T) {
	for _, data := range []string{"unversioned secret", strings.Repeat("x", 128*1024+1)} {
		path := filepath.Join(t.TempDir(), "error.log")
		os.WriteFile(path, []byte(data), 0600)
		r := newGUIDiagnosticLog(path).report()
		if r.SaveError == "" || strings.Contains(r.Text, "unversioned secret") {
			t.Fatalf("接受损坏历史 %+v", r)
		}
	}
}
func TestGUIDiagnosticsRedactCommonPathsTokensAndHIDInterfaces(t *testing.T) {
	log := newGUIDiagnosticLog("")
	secret := `HID\\VID_3837&PID_4035#SERIAL88 C:\Users\Jane Doe\Desktop\file.txt: denied /home/charlie/private/file: denied /root/secret.txt: denied \\?\hid#vid_3837&pid_4035#SERIAL77#{GUID} serial=SERIAL99 authorization=Bearer TOKEN99 password=PASS99`
	log.record("扫描设备", nil, secret, nil)
	r := log.report()
	for _, v := range []string{"SERIAL88", "SERIAL77", "SERIAL99", "Jane Doe", "charlie", "/root/secret", "TOKEN99", "PASS99"} {
		if strings.Contains(r.Text, v) {
			t.Fatalf("泄露 %s\n%s", v, r.Text)
		}
	}
}
func TestGUIDiagnosticsRedactQuotedSecretsAndEscapedUserDirectories(t *testing.T) {
	log := newGUIDiagnosticLog("")
	log.record("扫描设备", nil, `{"token":"TOKENJSON", "serialNumber":"SERIALJSON", "path":"C:\\Users\\AlicePrivate\\Desktop\\file"} 序列号：SERIALCN secret='SECRET WITH SPACES'`, nil)
	for _, secret := range []string{"TOKENJSON", "SERIALJSON", "AlicePrivate", "SERIALCN", "SECRET WITH SPACES"} {
		if strings.Contains(log.report().Text, secret) {
			t.Fatalf("转义内容泄露 %s\n%s", secret, log.report().Text)
		}
	}
}
func TestGUIDiagnosticsSuccessfulMutationIsNotAnErrorAndBreaksDedup(t *testing.T) {
	b := guiUsableBackend()
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
	s.SetDPI("one", 1600, 1600)
	s.SetRate("one", 500)
	if !strings.Contains(s.Diagnostics().Text, "暂无错误记录") {
		t.Fatal(s.Diagnostics().Text)
	}
	b.setErr = errors.New("设备拒绝")
	s.SetDPI("one", 2000, 2000)
	s.SetDPI("one", 2000, 2000)
	if !strings.Contains(s.Diagnostics().Text, "重复次数：2") {
		t.Fatal(s.Diagnostics().Text)
	}
	b.setErr = nil
	s.Scan()
	b.setErr = errors.New("设备拒绝")
	s.SetDPI("one", 2000, 2000)
	if strings.Count(s.Diagnostics().Text, "操作：设置 DPI") != 2 {
		t.Fatal("成功操作未中断连续合并")
	}
}
func TestGUIDiagnosticsCaptureUnavailablePartialAndMutationErrors(t *testing.T) {
	b := guiUsableBackend()
	b.rateErr = errors.New("只回报率失败")
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
	s.Scan()
	if !strings.Contains(s.Diagnostics().Text, "只回报率失败") {
		t.Fatal("部分读取失败未记")
	}
	b.rateErr = nil
	s.SetDPI("gone-private-id", 1600, 1600)
	b.setErr = errors.New("回报率设置被设备拒绝")
	if _, err := s.SetRate("one", 500); !errors.Is(err, b.setErr) {
		t.Fatalf("未保留回报率设置错误：%v", err)
	}
	r := s.Diagnostics()
	for _, v := range []string{"设置 DPI", "设置回报率", "身份已变化"} {
		if !strings.Contains(r.Text, v) {
			t.Fatal(r.Text)
		}
	}
	if strings.Contains(r.Text, "gone-private-id") {
		t.Fatal("泄露请求ID")
	}
	unavailable := mchoseIdentification("opaque", 0x1014, "未发现已适配的 HID 通道", true)
	s = newGUIService(func() ([]device, error) { return []device{unavailable}, nil })
	s.Scan()
	if !strings.Contains(s.Diagnostics().Text, "3837:1014") || !strings.Contains(s.Diagnostics().Text, "HID 通道") {
		t.Fatal(s.Diagnostics().Text)
	}
}
func TestGUIDiagnosticsCapturePartialCapabilityDiscoveryFailure(t *testing.T) {
	b := guiUsableBackend()
	b.caps.Battery = false
	b.caps.DiscoveryFailed = true
	b.caps.BatteryReason = "电量能力发现超时"
	s := newGUIService(func() ([]device, error) { return []device{guiFixtureDevice("one", b)}, nil })
	snapshot, err := s.Scan()
	if err != nil || snapshot.Devices[0].Status != "online" {
		t.Fatalf("其他读数应继续可用：%+v %v", snapshot, err)
	}
	if !strings.Contains(s.Diagnostics().Text, "电量能力发现超时") {
		t.Fatal("部分能力发现错误未记录")
	}
}
func TestGUIDiagnosticsCloseWaitsForAcceptedOperationLog(t *testing.T) {
	log := newGUIDiagnosticLog("")
	entered := make(chan struct{})
	s := newGUIService(func() ([]device, error) {
		close(entered)
		return nil, errors.New("需要完成记录的错误")
	})
	s.SetDiagnostics(log)
	log.mu.Lock()
	scanDone := make(chan struct{})
	go func() { s.Scan(); close(scanDone) }()
	<-entered
	closeDone := make(chan struct{})
	go func() { s.Close(); close(closeDone) }()
	premature := false
	select {
	case <-closeDone:
		premature = true
	case <-time.After(20 * time.Millisecond):
	}
	log.mu.Unlock()
	<-scanDone
	<-closeDone
	if premature {
		t.Fatal("Close 在已接受操作的日志记录完成前返回")
	}
	if !strings.Contains(s.Diagnostics().Text, "需要完成记录的错误") {
		t.Fatal("关闭丢失错误日志")
	}
}
func TestGUIDiagnosticsConcurrentReportAndRecording(t *testing.T) {
	log := newGUIDiagnosticLog("")
	s := newGUIService(nil)
	s.SetDiagnostics(log)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if i%2 == 0 {
					log.record("扫描设备", nil, "并发错误", nil)
				} else {
					s.Diagnostics()
					s.SetDiagnostics(log)
				}
			}
		}(i)
	}
	wg.Wait()
	if !strings.Contains(s.Diagnostics().Text, "并发错误") {
		t.Fatal("日志丢失")
	}
}
func TestGUIDiagnosticsBusyAndClosedDoNotPerformExtraIO(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := newGUIService(func() ([]device, error) { close(entered); <-release; return nil, nil })
	done := make(chan struct{})
	go func() { s.Scan(); close(done) }()
	<-entered
	if _, e := s.SetRate("hidden-id", 500); !errors.Is(e, errGUIBusy) {
		t.Fatal(e)
	}
	if !strings.Contains(s.Diagnostics().Text, "等待当前操作") {
		t.Fatal(s.Diagnostics().Text)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("扫描未完成")
	}
	s.Close()
	s.Scan()
	if !strings.Contains(s.Diagnostics().Text, "窗口正在关闭") {
		t.Fatal(s.Diagnostics().Text)
	}
}
