package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	guiDiagnosticHeader     = "MouseControl 诊断日志 / v1\n"
	guiDiagnosticSeparator  = "\n=== 错误记录 ===\n"
	guiDiagnosticLocation   = `%LOCALAPPDATA%\MouseControl\logs\error.log`
	guiDiagnosticMaxBytes   = 128 * 1024
	guiDiagnosticMaxEntries = 20
	guiDiagnosticBodyBytes  = 4096
)

type guiDiagnosticReport struct {
	Text      string `json:"text"`
	Location  string `json:"location"`
	SaveError string `json:"saveError"`
}
type guiDiagnosticEntry struct {
	First, Last time.Time
	Count       uint64
	Body        string
}
type guiDiagnosticLog struct {
	mu         sync.Mutex
	path       string
	entries    []guiDiagnosticEntry
	saveError  string
	continuous bool
}

// 路径仅由本地宿主提供；空路径用于内存模式，不访问用户目录。
func newGUIDiagnosticLog(path string) *guiDiagnosticLog {
	l := &guiDiagnosticLog{path: path}
	if path == "" {
		return l
	}
	f, e := os.Open(path)
	if errors.Is(e, os.ErrNotExist) {
		return l
	}
	if e != nil {
		l.saveError = guiDiagnosticStorageError("读取历史日志", e)
		return l
	}
	data, e := io.ReadAll(io.LimitReader(f, guiDiagnosticMaxBytes+1))
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		l.saveError = guiDiagnosticStorageError("读取历史日志", e)
		return l
	}
	if len(data) > guiDiagnosticMaxBytes {
		l.saveError = "历史日志超过 128 KiB，未载入"
		return l
	}
	entries, e := parseGUIDiagnosticHistory(string(data))
	if e != nil {
		l.saveError = e.Error()
		return l
	}
	l.entries = entries
	l.continuous = len(entries) > 0
	return l
}
func parseGUIDiagnosticHistory(text string) ([]guiDiagnosticEntry, error) {
	invalid := errors.New("历史日志格式不受支持或已损坏，未载入")
	if !utf8.ValidString(text) || !strings.HasPrefix(text, guiDiagnosticHeader) {
		return nil, invalid
	}
	parts := strings.Split(text, guiDiagnosticSeparator)
	var entries []guiDiagnosticEntry
	for _, part := range parts[1:] {
		lines := strings.SplitN(strings.TrimSuffix(part, "\n"), "\n", 4)
		if len(lines) != 4 {
			return nil, invalid
		}
		if !strings.HasPrefix(lines[0], "首次时间：") || !strings.HasPrefix(lines[1], "最后时间：") || !strings.HasPrefix(lines[2], "重复次数：") {
			return nil, invalid
		}
		first, e := time.Parse(time.RFC3339Nano, strings.TrimPrefix(lines[0], "首次时间："))
		if e != nil {
			return nil, invalid
		}
		last, e := time.Parse(time.RFC3339Nano, strings.TrimPrefix(lines[1], "最后时间："))
		if e != nil || last.Before(first) {
			return nil, invalid
		}
		count, e := strconv.ParseUint(strings.TrimPrefix(lines[2], "重复次数："), 10, 64)
		if e != nil || count == 0 || len(lines[3]) > guiDiagnosticBodyBytes {
			return nil, invalid
		}
		// 历史文件也重做通用脱敏，不原样回传外部改写的内容。
		bodyLines := strings.Split(lines[3], "\n")
		for i, line := range bodyLines {
			bodyLines[i] = sanitizeGUIDiagnostic(line, nil)
		}
		entries = append(entries, guiDiagnosticEntry{first, last, count, truncateGUIDiagnostic(strings.Join(bodyLines, "\n"), guiDiagnosticBodyBytes)})
	}
	if len(entries) > guiDiagnosticMaxEntries {
		entries = entries[len(entries)-guiDiagnosticMaxEntries:]
	}
	return entries, nil
}
func guiDiagnosticStorageError(action string, e error) string {
	var pathError *os.PathError
	if errors.As(e, &pathError) {
		e = pathError.Err
	}
	var linkError *os.LinkError
	if errors.As(e, &linkError) {
		e = linkError.Err
	}
	return action + "失败：" + truncateGUIDiagnostic(sanitizeGUIDiagnostic(e.Error(), nil), 1024)
}

var guiDiagnosticRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:\\\\[?.]\\)?(?:hid|usb|bthledevice|bthenum)[#\\][^\s"'<>]+`),
	regexp.MustCompile(`(?i)[a-z]:[\\/]+(?:users|documents and settings)[\\/]+[^\\/"'<>:\r\n]+`),
	regexp.MustCompile(`/(?:Users|home)/[^/"'<>:\r\n]+`),
	regexp.MustCompile(`/root(?:/[^\s"'<>:]*)?`),
	regexp.MustCompile(`(?i)["']?(?:authorization|access[_-]?token|refresh[_-]?token|token|secret|password|api[_-]?key|cookie|session[_-]?id|serial(?:number)?|序列号|序列编号|identity)["']?\s*[:=：]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|(?:bearer\s+)?[^\s,;]+)`),
	regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
}

func sanitizeGUIDiagnostic(text string, ids []string) string {
	// 先处理所有本次枚举身份，避免错误串提及其他设备时泄露；长 ID 优先。
	ids = append([]string(nil), ids...)
	sort.Slice(ids, func(i, j int) bool { return len(ids[i]) > len(ids[j]) })
	for _, id := range ids {
		if id == "" {
			continue
		}
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(id))
		text = re.ReplaceAllString(text, "[设备身份已隐藏]")
	}
	for _, re := range guiDiagnosticRedactors {
		text = re.ReplaceAllString(text, "[敏感信息已隐藏]")
	}
	// 单字段不能插入新的记录分隔符，也不保留控制字符。
	text = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), " ")
}
func truncateGUIDiagnostic(text string, limit int) string {
	const marker = "…[内容过长，已截断]"
	if len(text) <= limit {
		return text
	}
	n := limit - len(marker)
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n] + marker
}
func (l *guiDiagnosticLog) record(operation string, d *guiDevice, message string, ids []string) {
	if l == nil {
		return
	}
	var body strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&body, "%s：%s\n", label, sanitizeGUIDiagnostic(value, ids))
		}
	}
	line("版本", version)
	line("运行系统", runtime.GOOS+"/"+runtime.GOARCH)
	line("操作", operation)
	if d != nil {
		line("设备", fmt.Sprintf("%04X:%04X", d.VID, d.PID))
		line("型号", d.Name)
		line("连接", d.Connection)
		line("读取状态", d.Status)
	}
	line("错误", message)
	if d != nil {
		line("电量错误", d.Battery.Error)
		line("DPI 错误", d.DPI.Error)
		line("回报率错误", d.Rate.Error)
		line("电量能力说明", d.Capabilities.BatteryReason)
		line("DPI 能力说明", d.Capabilities.DPIReason)
		line("回报率能力说明", d.Capabilities.RateReason)
		for _, note := range d.Notes {
			line("设备说明", note)
		}
	}
	text := truncateGUIDiagnostic(strings.TrimSuffix(body.String(), "\n"), guiDiagnosticBodyBytes)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if n := len(l.entries); l.continuous && n > 0 && l.entries[n-1].Body == text {
		last := &l.entries[n-1]
		last.Last = now
		if last.Count < ^uint64(0) {
			last.Count++
		}
	} else {
		l.entries = append(l.entries, guiDiagnosticEntry{now, now, 1, text})
		if len(l.entries) > guiDiagnosticMaxEntries {
			l.entries = append([]guiDiagnosticEntry(nil), l.entries[len(l.entries)-guiDiagnosticMaxEntries:]...)
		}
	}
	l.continuous = true
	if l.path != "" {
		if e := l.saveLocked(); e != nil {
			l.saveError = guiDiagnosticStorageError("保存日志", e)
		} else {
			l.saveError = ""
		}
	}
}
func (l *guiDiagnosticLog) success() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.continuous = false
	l.mu.Unlock()
}
func (l *guiDiagnosticLog) renderLocked() string {
	var out strings.Builder
	out.WriteString(guiDiagnosticHeader)
	fmt.Fprintf(&out, "当前版本：%s\n系统：%s/%s\n导出时间：%s\n提交 GitHub Issues 时，请复制以下错误日志。设备身份、常见用户目录与令牌字段已脱敏。\n", version, runtime.GOOS, runtime.GOARCH, time.Now().Format(time.RFC3339))
	if len(l.entries) == 0 {
		out.WriteString("暂无错误记录。\n")
	}
	for _, entry := range l.entries {
		out.WriteString(guiDiagnosticSeparator)
		fmt.Fprintf(&out, "首次时间：%s\n最后时间：%s\n重复次数：%d\n%s\n", entry.First.Format(time.RFC3339Nano), entry.Last.Format(time.RFC3339Nano), entry.Count, entry.Body)
	}
	return out.String()
}
func (l *guiDiagnosticLog) saveLocked() error {
	text := l.renderLocked()
	if len(text) > guiDiagnosticMaxBytes {
		return errors.New("日志超过 128 KiB，未保存")
	}
	dir := filepath.Dir(l.path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	file, e := os.CreateTemp(dir, ".error-log-*")
	if e != nil {
		return e
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, e = file.WriteString(text); e == nil {
		e = file.Sync()
	}
	closeErr := file.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(temp, l.path)
}
func (l *guiDiagnosticLog) report() guiDiagnosticReport {
	if l == nil {
		l = newGUIDiagnosticLog("")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	r := guiDiagnosticReport{Text: l.renderLocked(), SaveError: l.saveError}
	if l.path != "" {
		r.Location = guiDiagnosticLocation
	}
	if r.SaveError != "" {
		r.Text += "\n日志文件问题：" + r.SaveError + "\n当前内存中的错误仍可复制。\n"
	}
	return r
}
func (s *guiService) SetDiagnostics(log *guiDiagnosticLog) { s.diagnostics.Store(log) }

// Diagnostics 只访问已缓存日志，不枚举设备，也不调用任何后端读取方法。
func (s *guiService) Diagnostics() guiDiagnosticReport { return s.diagnostics.Load().report() }
func diagnosticGUIFailure(d guiDevice) bool {
	return d.Status == "unavailable" || (d.Capabilities.Battery && d.Battery.Error != "") || (len(d.Capabilities.DPIRanges) > 0 && d.DPI.Error != "") || (len(d.Capabilities.PollRates) > 0 && d.Rate.Error != "")
}
func guiDiagnosticMetadata(d device) guiDevice {
	f := deviceFeatures(d)
	return guiDevice{VID: deviceVID(d), PID: d.PID, Name: d.Info.Name, Connection: d.Info.Connection, Capabilities: guiCapabilities{BatteryReason: f.BatteryReason, DPIReason: f.DPIReason, RateReason: f.RateReason}}
}
func (s *guiService) finishDiagnostics(locked bool, operation, id string, devices []device, views []guiDevice, err error) {
	if locked {
		defer s.mu.Unlock()
	}
	defer closeDevices(devices)
	// 在释放句柄与服务锁前保存，Close 会等待本次操作的日志写入完成。
	s.recordDiagnostics(operation, id, devices, views, err)
}
func (s *guiService) recordDiagnostics(operation, id string, devices []device, views []guiDevice, err error) {
	log := s.diagnostics.Load()
	if log == nil {
		return
	}
	ids := []string{id}
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	if err != nil {
		var selected *guiDevice
		for _, d := range devices {
			if d.ID == id {
				v := guiDiagnosticMetadata(d)
				selected = &v
				break
			}
		}
		log.record(operation, selected, err.Error(), ids)
		return
	}
	count := 0
	for i := range views {
		failed := diagnosticGUIFailure(views[i])
		for _, d := range devices {
			if d.ID == views[i].ID && deviceFeatures(d).DiscoveryFailed {
				failed = true
				break
			}
		}
		if failed {
			log.record(operation, &views[i], "设备读取或能力发现失败", ids)
			count++
		}
	}
	if count == 0 {
		log.success()
	}
}
