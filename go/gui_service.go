package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	errGUIBusy   = errors.New("正在读取或设置设备，请等待当前操作完成")
	errGUIClosed = errors.New("窗口正在关闭，已停止接收设备请求")
)

type guiSnapshot struct {
	Version   string      `json:"version"`
	ScannedAt string      `json:"scannedAt"`
	Devices   []guiDevice `json:"devices"`
}
type guiDevice struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Vendor       string          `json:"vendor"`
	Connection   string          `json:"connection"`
	VID          uint16          `json:"vid"`
	PID          uint16          `json:"pid"`
	Status       string          `json:"status"`
	Battery      guiBattery      `json:"battery"`
	DPI          guiDPI          `json:"dpi"`
	Rate         guiRate         `json:"rate"`
	Capabilities guiCapabilities `json:"capabilities"`
	Notes        []string        `json:"notes"`
}
type guiBattery struct {
	Percent    *int   `json:"percent"`
	Level      string `json:"level"`
	VoltageMV  *int   `json:"voltageMV"`
	Charging   *bool  `json:"charging"`
	ChargeText string `json:"chargeText"`
	Error      string `json:"error"`
}
type guiDPI struct {
	X     *int   `json:"x"`
	Y     *int   `json:"y"`
	Error string `json:"error"`
}
type guiRate struct {
	Hz    *int   `json:"hz"`
	Error string `json:"error"`
}
type guiDPIRange struct {
	Min  int `json:"min"`
	Max  int `json:"max"`
	Step int `json:"step"`
}
type guiCapabilities struct {
	Battery       bool          `json:"battery"`
	DPIRanges     []guiDPIRange `json:"dpiRanges"`
	SeparateAxes  bool          `json:"separateAxes"`
	PollRates     []int         `json:"pollRates"`
	DPIReadOnly   bool          `json:"dpiReadOnly"`
	RateReadOnly  bool          `json:"rateReadOnly"`
	BatteryReason string        `json:"batteryReason"`
	DPIReason     string        `json:"dpiReason"`
	RateReason    string        `json:"rateReason"`
}
type guiMutation struct {
	Message string    `json:"message"`
	Changed bool      `json:"changed"`
	Device  guiDevice `json:"device"`
}

// 服务只保留枚举入口，不持有设备句柄；每个请求负责关闭自己的完整枚举结果。
type guiService struct {
	enumerate   func() ([]device, error)
	mu          sync.Mutex
	closed      atomic.Bool
	diagnostics atomic.Pointer[guiDiagnosticLog]
}

func newGUIService(enumerate func() ([]device, error)) *guiService {
	s := &guiService{enumerate: enumerate}
	s.SetDiagnostics(newGUIDiagnosticLog(""))
	return s
}

func (s *guiService) begin() error {
	if s.closed.Load() {
		return errGUIClosed
	}
	if !s.mu.TryLock() {
		if s.closed.Load() {
			return errGUIClosed
		}
		return errGUIBusy
	}
	if s.closed.Load() {
		s.mu.Unlock()
		return errGUIClosed
	}
	if s.enumerate == nil {
		s.mu.Unlock()
		return errors.New("设备枚举服务不可用")
	}
	return nil
}

// 先停止接收请求，再等待已接受操作完成；句柄关闭发生在操作释放mu之前。
func (s *guiService) Close() { s.closed.Store(true); s.mu.Lock(); s.mu.Unlock() }

func (s *guiService) Scan() (snapshot guiSnapshot, err error) {
	var devices []device
	locked := false
	defer func() { s.finishDiagnostics(locked, "扫描设备", "", devices, snapshot.Devices, err) }()
	if err := s.begin(); err != nil {
		return guiSnapshot{}, err
	}
	locked = true
	devices, err = s.enumerate()
	if err != nil {
		return guiSnapshot{}, fmt.Errorf("设备枚举失败：%w", err)
	}
	snapshot = guiSnapshot{Version: version, Devices: make([]guiDevice, 0, len(devices))}
	for _, d := range devices {
		snapshot.Devices = append(snapshot.Devices, guiReadDevice(d))
	}
	snapshot.ScannedAt = time.Now().Format(time.RFC3339)
	return snapshot, nil
}

func guiSelectDevice(devices []device, id string) (device, error) {
	if strings.TrimSpace(id) == "" {
		return device{}, errors.New("必须指定完整设备 ID，未执行设置")
	}
	var selected device
	matches := 0
	for _, d := range devices {
		if d.ID == id {
			selected = d
			matches++
		}
	}
	if matches == 0 {
		return device{}, errors.New("所选设备已断开或身份已变化，请刷新后重新选择；未执行设置")
	}
	if matches > 1 {
		return device{}, errors.New("设备 ID 不唯一，请重新扫描；未执行设置")
	}
	return selected, nil
}

func (s *guiService) SetDPI(id string, x, y int) (result guiMutation, err error) {
	var devices []device
	locked := false
	defer func() { s.finishDiagnostics(locked, "设置 DPI", id, devices, []guiDevice{result.Device}, err) }()
	if err := s.begin(); err != nil {
		return guiMutation{}, err
	}
	locked = true
	if strings.TrimSpace(id) == "" {
		return guiMutation{}, errors.New("必须指定完整设备 ID，未执行设置")
	}
	devices, err = s.enumerate()
	if err != nil {
		return guiMutation{}, fmt.Errorf("设备枚举失败：%w", err)
	}
	d, err := guiSelectDevice(devices, id)
	if err != nil {
		return guiMutation{}, err
	}
	actual, changed, err := setMouseDPI(d, dpiValue{X: x, Y: y})
	if err != nil {
		return guiMutation{}, fmt.Errorf("DPI 设置失败：%w", err)
	}
	message := fmt.Sprintf("DPI 读回确认：X %d / Y %d", actual.X, actual.Y)
	if !changed {
		message = fmt.Sprintf("当前 DPI 已为 X %d / Y %d，无需写入", actual.X, actual.Y)
	}
	return guiMutation{Message: message, Changed: changed, Device: guiReadDevice(d)}, nil
}

func (s *guiService) SetRate(id string, rate int) (result guiMutation, err error) {
	var devices []device
	locked := false
	defer func() { s.finishDiagnostics(locked, "设置回报率", id, devices, []guiDevice{result.Device}, err) }()
	if err := s.begin(); err != nil {
		return guiMutation{}, err
	}
	locked = true
	if strings.TrimSpace(id) == "" {
		return guiMutation{}, errors.New("必须指定完整设备 ID，未执行设置")
	}
	devices, err = s.enumerate()
	if err != nil {
		return guiMutation{}, fmt.Errorf("设备枚举失败：%w", err)
	}
	d, err := guiSelectDevice(devices, id)
	if err != nil {
		return guiMutation{}, err
	}
	actual, changed, err := setMouseRate(d, rate)
	if err != nil {
		return guiMutation{}, fmt.Errorf("回报率设置失败：%w", err)
	}
	message := fmt.Sprintf("回报率读回确认：%d Hz", actual)
	if !changed {
		message = fmt.Sprintf("当前回报率已为 %d Hz，无需写入", actual)
	}
	return guiMutation{Message: message, Changed: changed, Device: guiReadDevice(d)}, nil
}

func guiError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func guiInt(value int) *int { return &value }

func guiReadDevice(d device) guiDevice {
	r := readMouse(d)
	f := deviceFeatures(d)
	cap := guiCapabilities{Battery: f.Battery, DPIRanges: make([]guiDPIRange, 0, len(f.DPIRanges)), SeparateAxes: f.SeparateAxes, PollRates: append([]int{}, f.PollRates...), DPIReadOnly: f.DPIReadOnly, RateReadOnly: f.RateReadOnly, BatteryReason: f.BatteryReason, DPIReason: f.DPIReason, RateReason: f.RateReason}
	for _, v := range f.DPIRanges {
		cap.DPIRanges = append(cap.DPIRanges, guiDPIRange{v.Min, v.Max, v.Step})
	}
	result := guiDevice{ID: d.ID, Name: deviceName(d), Vendor: "razer", Connection: d.Info.Connection, VID: deviceVID(d), PID: d.PID, Status: "identified", Capabilities: cap, Notes: append([]string{}, f.Notes...)}
	if result.VID == 0x046d {
		result.Vendor = "logitech"
	} else if result.VID == mchoseVID {
		result.Vendor = "mchose"
	}
	result.Battery.Error = guiError(r.Err)
	if r.Err == nil {
		result.Battery.Level = r.Reading.Level
		result.Battery.ChargeText = r.Reading.ChargeText
		if !r.Reading.PercentUnknown {
			if r.Reading.Percent >= 0 && r.Reading.Percent <= 100 {
				result.Battery.Percent = guiInt(r.Reading.Percent)
			} else {
				result.Battery.Error = "设备返回的电量百分比超出 0..100"
			}
		}
		if r.Reading.VoltageMV > 0 {
			result.Battery.VoltageMV = guiInt(r.Reading.VoltageMV)
		}
		if r.Reading.ChargeError != nil {
			result.Battery.Error = guiError(r.Reading.ChargeError)
		} else if r.Reading.ChargeKnown {
			charging := r.Reading.Charging
			result.Battery.Charging = &charging
		}
	}
	result.DPI.Error = guiError(r.DPIErr)
	if r.DPIErr == nil && r.DPI.X > 0 && r.DPI.Y > 0 {
		result.DPI.X = guiInt(r.DPI.X)
		result.DPI.Y = guiInt(r.DPI.Y)
	}
	result.Rate.Error = guiError(r.RateErr)
	if r.RateErr == nil && r.Rate > 0 {
		result.Rate.Hz = guiInt(r.Rate)
	}
	if d.AccessError != nil {
		result.Notes = append(result.Notes, d.AccessError.Error())
	}
	hasCapability := f.Battery || len(f.DPIRanges) > 0 || len(f.PollRates) > 0
	if hasCapability || f.DiscoveryFailed || d.AccessError != nil {
		result.Status = "unavailable"
	}
	if (f.Battery && r.Err == nil) || (result.DPI.X != nil) || (result.Rate.Hz != nil) {
		result.Status = "online"
	}
	return result
}
