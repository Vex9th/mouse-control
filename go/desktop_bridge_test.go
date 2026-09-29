package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDesktopNavigationAllowsOnlyOneExactEmbeddedDocument(t *testing.T) {
	const html = "<html><body>鼠标工具</body></html>"
	dataURI := "data:text/html;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	for _, initial := range []string{"about:blank", dataURI} {
		gate := newDesktopNavigationGate(html)
		for _, blocked := range []string{"https://example.com", "file:///C:/test.html", "data:text/html,<script>alert(1)</script>", dataURI + "A", "about:blank#other"} {
			if gate.Allow(blocked) {
				t.Fatalf("意外放行 %q", blocked)
			}
		}
		if !gate.Allow(initial) {
			t.Fatal("未放行本次内嵌文档")
		}
		if gate.Allow(initial) || gate.Allow("about:blank") {
			t.Fatal("页面加载后仍允许重新导航")
		}
	}
}

func TestDesktopBridgeOnlyAcceptsBoundedTrustedOperations(t *testing.T) {
	token := strings.Repeat("a", 64)
	valid := `{"id":"request-1","token":"` + token + `","action":"setDPI","deviceId":"usb:mouse","x":1000,"y":1000}`
	r, err := decodeDesktopRequest(valid, token)
	if err != nil || r.X != 1000 || r.ID != "request-1" {
		t.Fatalf("合法请求被拒绝：%+v %v", r, err)
	}
	for _, bad := range []string{
		strings.Replace(valid, token, strings.Repeat("b", 64), 1),
		strings.Replace(valid, "setDPI", "exec", 1),
		strings.Replace(valid, `"x":1000`, `"x":1.5`, 1),
		strings.Replace(valid, `"x":1000`, `"command":"whoami","x":1000`, 1),
		strings.Replace(valid, "usb:mouse", "", 1),
		valid + `{}`, strings.Repeat(" ", 20000) + valid,
		strings.Replace(valid, "request-1", "<script>", 1),
	} {
		if _, err := decodeDesktopRequest(bad, token); err == nil {
			t.Fatalf("接受了非法请求：%.120s", bad)
		}
	}
}

func TestDesktopDocumentEmbedsBridgeAndRestrictsContent(t *testing.T) {
	html, err := desktopDocument("<!doctype html><html><head></head><body><div id=app></div></body></html>", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"Content-Security-Policy", "connect-src 'none'", "font-src data:;", "frame-src 'none'", "mouseNativeSubmit", "postMessage"} {
		if !strings.Contains(html, part) {
			t.Errorf("缺少%s", part)
		}
	}
	if _, err := desktopDocument(strings.Repeat("x", 2*1024*1024), strings.Repeat("a", 64)); err == nil {
		t.Fatal("接受了超出NavigateToString上限的页面")
	}
}

func TestDesktopDiagnosticActionsRejectDeviceParameters(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, action := range []string{"diagnostics", "copyDiagnostics", "openIssue"} {
		raw := `{"id":"support-1","token":"` + token + `","action":"` + action + `"}`
		if _, err := decodeDesktopRequest(raw, token); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		for _, extra := range []string{`,"deviceId":"usb:mouse"`, `,"x":1`, `,"rate":125`, `,"url":"https://example.com"`, `,"text":"arbitrary"`} {
			if _, err := decodeDesktopRequest(strings.TrimSuffix(raw, "}")+extra+"}", token); err == nil {
				t.Fatalf("%s 接受了外部参数 %s", action, extra)
			}
		}
	}
}
