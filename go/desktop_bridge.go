package main

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type desktopNavigationGate struct {
	initialNavigation bool
	documentURI       string
}

func newDesktopNavigationGate(document string) desktopNavigationGate {
	return desktopNavigationGate{true, "data:text/html;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(document))}
}

func (g *desktopNavigationGate) Allow(uri string) bool {
	// NavigateToString 的事件可报告精确 data URI，文档 Source 仍为 about:blank。
	if !g.initialNavigation || (uri != "about:blank" && uri != g.documentURI) {
		return false
	}
	g.initialNavigation = false
	g.documentURI = ""
	return true
}

type desktopRequest struct {
	ID       string `json:"id"`
	Token    string `json:"token"`
	Action   string `json:"action"`
	DeviceID string `json:"deviceId,omitempty"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	Rate     int    `json:"rate,omitempty"`
}
type desktopReply struct {
	ID     string `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func decodeDesktopRequest(raw, token string) (desktopRequest, error) {
	var r desktopRequest
	if len(raw) > 16384 {
		return r, errors.New("界面请求过长")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, fmt.Errorf("界面请求格式错误：%w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return r, errors.New("界面请求包含多余数据")
	}
	if len(token) != 64 || subtle.ConstantTimeCompare([]byte(r.Token), []byte(token)) != 1 {
		return r, errors.New("界面来源校验失败")
	}
	if len(r.ID) < 1 || len(r.ID) > 64 {
		return r, errors.New("无效请求编号")
	}
	for _, c := range r.ID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return r, errors.New("无效请求编号")
		}
	}
	if len(r.DeviceID) > 2048 {
		return r, errors.New("设备标识过长")
	}
	switch r.Action {
	case "scan", "diagnostics", "copyDiagnostics", "openIssue":
		if r.DeviceID != "" || r.X != 0 || r.Y != 0 || r.Rate != 0 {
			return r, errors.New("此请求不接受设置参数")
		}
	case "setDPI":
		if r.DeviceID == "" || r.X < 1 || r.X > 65535 || r.Y < 1 || r.Y > 65535 || r.Rate != 0 {
			return r, errors.New("DPI 设置参数无效")
		}
	case "setRate":
		if r.DeviceID == "" || r.Rate < 1 || r.Rate > 8000 || r.X != 0 || r.Y != 0 {
			return r, errors.New("回报率设置参数无效")
		}
	default:
		return r, errors.New("不支持的界面操作")
	}
	return r, nil
}

func desktopDocument(html, token string) (string, error) {
	if len(html) > 1900000 || !strings.Contains(html, "<head>") || len(token) != 64 {
		return "", errors.New("内嵌界面格式或大小无效，请重新构建")
	}
	secret, _ := json.Marshal(token)
	const policy = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; font-src data:; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">`
	bridge := `<script>(()=>{const token=` + string(secret) + `;Object.defineProperty(window,'mouseNativeSubmit',{value:request=>{window.chrome.webview.postMessage(JSON.stringify({...request,token}));return Promise.resolve(true)},writable:false,configurable:false});addEventListener('dragover',e=>e.preventDefault());addEventListener('drop',e=>e.preventDefault());})();</script>`
	return strings.Replace(html, "<head>", "<head>"+policy+bridge, 1), nil
}

func executeDesktopRequest(s *guiService, r desktopRequest) desktopReply {
	var result any
	var err error
	switch r.Action {
	case "diagnostics":
		result = s.Diagnostics()
	case "scan":
		result, err = s.Scan()
	case "setDPI":
		result, err = s.SetDPI(r.DeviceID, r.X, r.Y)
	case "setRate":
		result, err = s.SetRate(r.DeviceID, r.Rate)
	default:
		err = errors.New("不支持的界面操作")
	}
	reply := desktopReply{ID: r.ID, Result: result}
	if err != nil {
		reply.Error = err.Error()
		reply.Result = nil
	}
	return reply
}
