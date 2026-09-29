//go:build gui && windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type desktopIntegrationFontProbe struct {
	DPR            float64 `json:"dpr"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	VisualViewport any     `json:"visualViewport"`
	FontsStatus    string  `json:"fontsStatus"`
	Fonts          []struct {
		Family string `json:"family"`
		Status string `json:"status"`
		Weight string `json:"weight"`
	} `json:"fonts"`
	Samples []struct {
		Kind       string  `json:"kind"`
		Element    string  `json:"element"`
		Text       string  `json:"text"`
		Family     string  `json:"family"`
		Size       float64 `json:"size"`
		Weight     string  `json:"weight"`
		LineHeight string  `json:"lineHeight"`
		Color      string  `json:"color"`
		Background string  `json:"background"`
		FontReady  bool    `json:"fontReady"`
		Rect       any     `json:"rect"`
		Ancestors  []struct {
			Element    string `json:"element"`
			Transform  string `json:"transform"`
			Scale      string `json:"scale"`
			Rotate     string `json:"rotate"`
			Translate  string `json:"translate"`
			Zoom       string `json:"zoom"`
			Filter     string `json:"filter"`
			Opacity    string `json:"opacity"`
			Background string `json:"background"`
		} `json:"ancestors"`
	} `json:"samples"`
}

type desktopIntegrationLayoutProbe struct {
	Width, Height, ScrollWidth, ScrollHeight int
	Regions                                  []struct {
		Element                  string
		Left, Top, Right, Bottom float64
		ClippedBy                []string
	}
	Overflow []struct {
		Element                                              string
		ClientWidth, ClientHeight, ScrollWidth, ScrollHeight int
		OverflowX, OverflowY                                 string
	}
}

func desktopIntegrationAssertLayout(t *testing.T, name string, p desktopIntegrationLayoutProbe) {
	t.Helper()
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s 真实布局探针：%s", name, encoded)
	if p.Width <= 0 || p.Height <= 0 || len(p.Regions) == 0 {
		t.Fatalf("%s 未取得有效视口和可见控件", name)
	}
	if p.ScrollWidth > p.Width+1 || p.ScrollHeight > p.Height+1 {
		t.Errorf("%s 文档发生滚动溢出：viewport=%dx%d scroll=%dx%d", name, p.Width, p.Height, p.ScrollWidth, p.ScrollHeight)
	}
	for _, r := range p.Regions {
		if r.Left < -1 || r.Top < -1 || r.Right > float64(p.Width)+1 || r.Bottom > float64(p.Height)+1 {
			t.Errorf("%s 可见控件/区域越过视口：%+v", name, r)
		}
		if len(r.ClippedBy) != 0 {
			t.Errorf("%s 控件/区域被祖先裁切：%+v", name, r)
		}
	}
	for _, e := range p.Overflow {
		t.Errorf("%s 存在实际滚动或隐藏溢出的容器：%+v", name, e)
	}
}

func desktopIntegrationDPI(t *testing.T) func() {
	t.Helper()
	dpi, err := beginDesktopDPI()
	if err != nil {
		t.Fatal(err)
	}
	context, _, _ := desktopUser32.NewProc("GetThreadDpiAwarenessContext").Call()
	equal, _, _ := desktopUser32.NewProc("AreDpiAwarenessContextsEqual").Call(context, ^uintptr(3))
	awareness, _, _ := desktopUser32.NewProc("GetAwarenessFromDpiAwarenessContext").Call(context)
	var processAwareness int32 = -1
	processHR, _, _ := systemDLL("shcore.dll").NewProc("GetProcessDpiAwareness").Call(0, uintptr(unsafe.Pointer(&processAwareness)))
	t.Logf("共享 DPI 初始化：previous=0x%016x context=0x%016x equalPMv2=%d threadAwareness=%d processHRESULT=0x%08x processAwareness=%d", dpi.previous, context, equal, int32(awareness), uint32(processHR), processAwareness)
	return func() {
		if err := dpi.Close(); err != nil {
			t.Error(err)
		} else {
			t.Log("窗口及 COM 清理后，原 UI 线程 DPI 上下文已恢复")
		}
	}
}

func desktopIntegrationAssertFonts(t *testing.T, p desktopIntegrationFontProbe) {
	t.Helper()
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("最终字体与缩放探针：%s", encoded)
	found := false
	for _, face := range p.Fonts {
		if strings.Trim(face.Family, `"'`) == "Mouse UI Sans" && face.Status == "loaded" {
			found = true
		}
	}
	if !found || p.FontsStatus != "loaded" {
		t.Error("Mouse UI Sans 未作为真实 FontFace 加载；不能用 fonts.check 的回退成功代替")
	}
	if p.DPR <= 0 || p.Width <= 0 || p.Height <= 0 {
		t.Errorf("无效真实视口：DPR=%v width=%d height=%d", p.DPR, p.Width, p.Height)
	}
	if len(p.Samples) == 0 {
		t.Fatal("页面没有字体样本")
	}
	for _, sample := range p.Samples {
		minimum := 12.0
		if sample.Kind == "body" || sample.Kind == "input" {
			minimum = 14
		}
		if sample.Size < minimum {
			t.Errorf("%s 字号 %.2fpx 低于 %.0fpx：%s", sample.Kind, sample.Size, minimum, sample.Element)
		}
		if !strings.Contains(sample.Family, "Mouse UI Sans") || !sample.FontReady {
			t.Errorf("%s 未使用已加载界面字体：family=%q ready=%v", sample.Element, sample.Family, sample.FontReady)
		}
		for _, a := range sample.Ancestors {
			if a.Transform != "none" || (a.Scale != "none" && a.Scale != "") || (a.Rotate != "none" && a.Rotate != "") || (a.Translate != "none" && a.Translate != "") || (a.Zoom != "1" && a.Zoom != "normal" && a.Zoom != "") {
				t.Errorf("文字仍经过 transform/zoom：text=%s ancestor=%+v", sample.Element, a)
			}
		}
	}
}

func TestDesktopIntegrationCacheRemovalWaitsForHandle(t *testing.T) {
	for _, delayedClose := range []bool{true, false} {
		t.Run(fmt.Sprintf("delayedClose=%v", delayedClose), func(t *testing.T) {
			path, err := os.MkdirTemp("", "mouse-control-cache-test-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(path)
			file := filepath.Join(path, "locked")
			if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(windows.StringToUTF16Ptr(file), windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if delayedClose {
				done := make(chan error, 1)
				go func() { time.Sleep(50 * time.Millisecond); done <- windows.CloseHandle(handle) }()
				err = desktopIntegrationRemoveCache(path, time.Second)
				if closeErr := <-done; closeErr != nil {
					t.Fatal(closeErr)
				}
				if err != nil {
					t.Fatalf("短暂占用结束后没有清理缓存：%v", err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("缓存目录仍然存在：%v", err)
				}
				return
			}
			defer windows.CloseHandle(handle)
			err = desktopIntegrationRemoveCache(path, 30*time.Millisecond)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("持续占用被当作清理成功，或丢失目录信息：%v", err)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("持续占用时不应删除文件：%v", err)
			}
		})
	}
}

// 必须在交互式 Windows 桌面显式启用。扫描真实设备，不启用演示数据，
// 不点击设置；意外的非扫描请求在测试入口拒绝，避免测试修改用户鼠标。
func TestDesktopNativeVueReadOnlyIntegration(t *testing.T) {
	if os.Getenv("RAZER_GUI_TEST") != "1" {
		t.Skip("设置 RAZER_GUI_TEST=1 后运行真实 WebView2/Vue/设备只读集成测试")
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("桌面发行目标为 Windows x64")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPI := desktopIntegrationDPI(t)
	defer restoreDPI()
	previous := desktopActive
	ole32 := systemDLL("ole32.dll")
	hr, _, _ := ole32.NewProc("CoInitializeEx").Call(0, 2)
	if err := desktopHRESULT("初始化测试 STA", hr); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ole32.NewProc("CoUninitialize").Call()
		desktopActive = previous
	}()
	if r, _, err := kernel32.NewProc("SetDefaultDllDirectories").Call(0x800); r == 0 {
		t.Fatal(windowsError("设置系统组件搜索范围", err))
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	dataPath, err := os.MkdirTemp("", "mouse-control-webview2-test-*")
	if err != nil {
		t.Fatal(err)
	}
	var browserExit *desktopIntegrationBrowserExit
	w := &desktopWindow{service: newGUIService(discoverDevices), token: hex.EncodeToString(secret[:]), replies: make(chan desktopReply, 1), shutdownDone: make(chan struct{}), initializing: true}
	desktopActive = w
	defer func() {
		w.closing.Store(true)
		w.service.Close() // 等待正在扫描的请求关闭全部设备句柄。
		if w.hwnd != 0 {
			desktopUser32.NewProc("KillTimer").Call(w.hwnd, 1)
			desktopUser32.NewProc("KillTimer").Call(w.hwnd, 2)
		}
		w.boundary.Close()
		if w.view != nil {
			w.view.Close()
		}
		if w.hwnd != 0 {
			desktopUser32.NewProc("DestroyWindow").Call(w.hwnd)
			instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
			desktopUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(desktopText("MouseControlWindow"))), instance)
		}
		// WM_DESTROY 留下的 WM_QUIT 不能污染同一测试线程的下一轮消息泵。
		var message desktopMessage
		for {
			r, _, _ := desktopUser32.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0x12, 0x12, 1)
			if r == 0 {
				break
			}
		}
		// Close 只发起进程关闭。官方 BrowserProcessExited 在进程组和 UDF
		// 释放后才触发；等待期间继续泵 STA 消息，不能直接阻塞回调线程。
		released := true
		if browserExit != nil {
			if err := browserExit.Wait(15 * time.Second); err != nil {
				released = false
				t.Errorf("WebView2 释放失败，保留本次缓存 %q：%v", dataPath, err)
			} else {
				t.Logf("WebView2 进程组已退出并释放缓存：pid=%d", browserExit.pid)
			}
			if err := browserExit.Close(); err != nil {
				t.Error(err)
			}
		} else if w.view != nil && w.view.started {
			released = false
			t.Errorf("未建立浏览器退出确认，保留本次缓存：%s", dataPath)
		}
		if released {
			if err := desktopIntegrationRemoveCache(dataPath, 5*time.Second); err != nil {
				t.Error(err)
			} else {
				t.Log("本次独占 WebView2 缓存已删除")
			}
		}
		t.Log("窗口和设备服务已关闭")
	}()
	if err := w.createWindow(); err != nil {
		t.Fatal(err)
	}
	context, _, _ := desktopUser32.NewProc("GetWindowDpiAwarenessContext").Call(w.hwnd)
	equal, _, _ := desktopUser32.NewProc("AreDpiAwarenessContextsEqual").Call(context, ^uintptr(3))
	windowDPI, _, _ := desktopUser32.NewProc("GetDpiForWindow").Call(w.hwnd)
	if equal == 0 || windowDPI == 0 {
		t.Fatalf("实际测试窗口 DPI 无效：PMv2=%v DPI=%d", equal != 0, windowDPI)
	}
	t.Logf("实际窗口 DPI：PMv2=true DPI=%d scale=%.3f", windowDPI, float64(windowDPI)/96)
	// 独立唤醒消息泵，确保页面失联时仍能检查限时；生产任务通知仍使用编号 1。
	if r, _, err := desktopUser32.NewProc("SetTimer").Call(w.hwnd, 2, 100, 0); r == 0 {
		t.Fatal(windowsError("启动测试限时通知", err))
	}
	type scanProbe struct {
		Size  string `json:"size"`
		Reply struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		} `json:"reply"`
		Text           string                        `json:"text"`
		Title          string                        `json:"title"`
		Busy           string                        `json:"busy"`
		State          string                        `json:"state"`
		SelectedID     string                        `json:"selectedID"`
		Alerts         []string                      `json:"alerts"`
		FullErrorEntry bool                          `json:"fullErrorEntry"`
		EmptyText      string                        `json:"emptyText"`
		Fonts          desktopIntegrationFontProbe   `json:"fonts"`
		Layout         desktopIntegrationLayoutProbe `json:"layout"`
	}
	var probe, minimumProbe *scanProbe
	var callbackErr error
	scanIDs := make(map[string]bool)
	readyMessages := 0
	w.view = newDesktopEngine(w.hwnd, dataPath, func(source, raw string) {
		if strings.HasPrefix(raw, "probe") {
			if source != "about:blank" || !w.boundary.Trusted() || len(raw) > 1<<20 {
				callbackErr = fmt.Errorf("测试探针来源或大小无效：source=%q length=%d", source, len(raw))
				return
			}
			switch {
			case strings.HasPrefix(raw, "probe-ready:"):
				readyMessages++
				t.Logf("DOMContentLoaded: %s", strings.TrimPrefix(raw, "probe-ready:"))
			case strings.HasPrefix(raw, "probe-error:"):
				t.Logf("页面脚本错误: %s", strings.TrimPrefix(raw, "probe-error:"))
			case strings.HasPrefix(raw, "probe:"):
				var value scanProbe
				if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, "probe:")), &value); err != nil {
					callbackErr = fmt.Errorf("解析渲染探针：%w", err)
				} else if value.Size == "minimum" {
					minimumProbe = &value
				} else if value.Size == "default" {
					probe = &value
				} else {
					callbackErr = fmt.Errorf("未知布局探针尺寸：%q", value.Size)
				}
			default:
				callbackErr = fmt.Errorf("未知测试探针")
			}
			return
		}
		request, err := decodeDesktopRequest(raw, w.token)
		if err != nil {
			callbackErr = err
		} else if request.Action != "scan" {
			callbackErr = fmt.Errorf("只读测试拒绝意外操作：%s", request.Action)
			return
		} else {
			scanIDs[request.ID] = true
			t.Logf("收到真实 native scan 请求：id=%s source=%s", request.ID, source)
		}
		w.receive(source, raw)
	})
	if err := w.view.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := desktopIntegrationPump(30*time.Second, func() (bool, error) { return w.view.Ready(), w.view.Err() }); err != nil {
		t.Fatalf("WebView2 初始化：%v", err)
	}
	browserExit, err = newDesktopIntegrationBrowserExit(w.view)
	if err != nil {
		t.Fatal(err)
	}
	w.initializing = false
	const script = `<script>(()=>{
const send=(kind,value)=>window.chrome.webview.postMessage(kind+JSON.stringify(value));
const label=e=>e.tagName.toLowerCase()+(e.id?'#'+e.id:'')+(typeof e.className==='string'&&e.className?'.'+e.className.trim().replace(/\s+/g,'.'):'');
const settled=async()=>{await new Promise(requestAnimationFrame);await document.fonts.ready;await new Promise(r=>setTimeout(r,550));await new Promise(requestAnimationFrame);await document.fonts.ready;};
const visible=e=>{
 const r=e.getBoundingClientRect();if(r.width<=0||r.height<=0)return false;
 for(let a=e;a;a=a.parentElement){const s=getComputedStyle(a),b=a.getBoundingClientRect();if(s.display==='none'||s.visibility==='hidden'||s.visibility==='collapse'||Number(s.opacity)===0)return false;
  // 仅排除明确的 1px 辅助技术隐藏节点；普通 overflow 裁切仍必须参与边界检查。
  if(b.width<=1&&b.height<=1&&((s.clip!=='auto'&&s.clip!=='none')||s.clipPath!=='none'))return false;
 }
 return true;
};
const fonts=()=>{
 const samples=[];
 const add=(e,kind)=>{if(!e||!visible(e))return;const s=getComputedStyle(e),ancestors=[];for(let a=e;a;a=a.parentElement){const c=getComputedStyle(a);ancestors.push({element:label(a),transform:c.transform,scale:c.scale,rotate:c.rotate,translate:c.translate,zoom:c.zoom,filter:c.filter,opacity:c.opacity,background:c.backgroundColor});}samples.push({kind,element:label(e),text:(e.textContent||'').trim().slice(0,80),family:s.fontFamily,size:parseFloat(s.fontSize),weight:s.fontWeight,lineHeight:s.lineHeight,color:s.color,background:s.backgroundColor,fontReady:document.fonts.check(s.fontWeight+' '+s.fontSize+' '+s.fontFamily),rect:e.getBoundingClientRect().toJSON(),ancestors});};
 add(document.body,'body');
 document.querySelectorAll('input:not([type=checkbox]):not([type=radio]),select,textarea').forEach(e=>add(e,'input'));
 document.body.querySelectorAll('*').forEach(e=>{if(e instanceof HTMLElement&&!['SCRIPT','STYLE','TEMPLATE','OPTION'].includes(e.tagName)&&visible(e)&&Array.from(e.childNodes).some(n=>n.nodeType===Node.TEXT_NODE&&n.textContent.trim()))add(e,'text');});
 return{dpr:devicePixelRatio,width:innerWidth,height:innerHeight,visualViewport:visualViewport?{width:visualViewport.width,height:visualViewport.height,scale:visualViewport.scale}:null,fontsStatus:document.fonts.status,fonts:Array.from(document.fonts,f=>({family:f.family,status:f.status,weight:f.weight})),samples};
};
const layout=()=>{
 const regions=[],overflow=[],clips=v=>/^(hidden|clip|auto|scroll)$/.test(v);
 document.querySelectorAll('main,aside,header,footer,section,form,fieldset,input:not([type=hidden]):not([type=checkbox]):not([type=radio]),button,select,textarea,label,p,h1,h2,h3,[role=button],[role=alert],[role=status]').forEach(e=>{
  if(!visible(e))return;const r=e.getBoundingClientRect(),clippedBy=[];
  for(let a=e.parentElement;a;a=a.parentElement){const c=getComputedStyle(a),b=a.getBoundingClientRect();if((clips(c.overflowX)&&(r.left<b.left-1||r.right>b.right+1))||(clips(c.overflowY)&&(r.top<b.top-1||r.bottom>b.bottom+1)))clippedBy.push(label(a));}
  regions.push({element:label(e),left:r.left,top:r.top,right:r.right,bottom:r.bottom,clippedBy});
 });
 document.querySelectorAll('*').forEach(e=>{
  if(!(e instanceof HTMLElement)||!visible(e))return;const s=getComputedStyle(e);
  // 设备名允许单行省略；表单和主要区域不能靠裁切来掩盖布局溢出。
  if(s.textOverflow==='ellipsis'&&s.whiteSpace==='nowrap'&&!e.querySelector('button,input,select,textarea'))return;
  if((clips(s.overflowX)&&e.scrollWidth>e.clientWidth+1)||(clips(s.overflowY)&&e.scrollHeight>e.clientHeight+1))overflow.push({element:label(e),clientWidth:e.clientWidth,clientHeight:e.clientHeight,scrollWidth:e.scrollWidth,scrollHeight:e.scrollHeight,overflowX:s.overflowX,overflowY:s.overflowY});
 });
 return{width:innerWidth,height:innerHeight,scrollWidth:Math.max(document.documentElement.scrollWidth,document.body.scrollWidth),scrollHeight:Math.max(document.documentElement.scrollHeight,document.body.scrollHeight),regions,overflow};
};
let scanReply;
const capture=async size=>{
 await settled();const main=document.querySelector('main'),entry=document.querySelector('button[aria-label="查看完整错误信息"]');
 send('probe:',{size,reply:scanReply,text:document.body.innerText,title:document.title,busy:main?.getAttribute('aria-busy')??'',state:main?.dataset.state??'',selectedID:main?.dataset.selectedId??'',alerts:Array.from(document.querySelectorAll('[role=alert]')).filter(visible).map(e=>e.innerText.trim()).filter(Boolean),fullErrorEntry:!!entry&&visible(entry)&&!entry.disabled,emptyText:Array.from(main?.querySelectorAll('[role=status]:not(footer)')??[]).filter(visible).map(e=>e.innerText.trim()).join('\n'),fonts:fonts(),layout:layout()});
};
addEventListener('DOMContentLoaded',async()=>{await settled();send('probe-ready:',{text:document.body.innerText,locationType:location.protocol,locationLength:location.href.length,readyState:document.readyState,nativeSubmit:typeof window.mouseNativeSubmit,fonts:fonts()});},{once:true});
addEventListener('error',e=>send('probe-error:',{message:e.message,fileType:e.filename.slice(0,e.filename.indexOf(':')+1),fileLength:e.filename.length,line:e.lineno}));
addEventListener('unhandledrejection',e=>send('probe-error:',{rejection:String(e.reason)}));
addEventListener('mouse:response',e=>{scanReply=e.detail;void capture('default');},{once:true});
addEventListener('mouse:test-minimum-layout',()=>{void capture('minimum');},{once:true});
})();</script>`
	document, err := desktopDocument(strings.Replace(desktopHTML, "<head>", "<head>"+script, 1), w.token)
	if err != nil {
		t.Fatal(err)
	}
	w.boundary, err = newDesktopBoundary(w.view.Controller(), document)
	if err != nil {
		t.Fatal(err)
	}
	originalNavigation := w.boundary.navigation.invoke
	w.boundary.navigation.invoke = func(sender uintptr, args unsafe.Pointer) uintptr {
		uri, err := desktopCOMString(args, 3)
		result := originalNavigation(sender, args)
		kind := "其他"
		if strings.HasPrefix(uri, "data:") {
			kind = "data"
		} else if strings.HasPrefix(uri, "about:") {
			kind = "about"
		}
		// NavigateToString 的内部 data URI 包含完整文档及口令，绝不写入日志。
		t.Logf("导航：type=%s length=%d err=%v HRESULT=0x%08X initial=%v", kind, len(uri), err, uint32(result), w.boundary.initialNavigation)
		return result
	}
	var settings unsafe.Pointer
	if err := desktopHRESULT("获取页面设置", desktopCOM(w.boundary.core, 3, uintptr(unsafe.Pointer(&settings)))); err != nil || settings == nil {
		t.Fatalf("页面设置不可用：%v", err)
	}
	for _, slot := range []uintptr{8, 10, 12, 14, 16, 18} {
		if err := desktopHRESULT("设置页面权限", desktopCOM(settings, slot, 0)); err != nil {
			desktopCOM(settings, 2)
			t.Fatal(err)
		}
	}
	desktopCOM(settings, 2)
	if err := w.view.NavigateHTML(document); err != nil {
		t.Fatal(err)
	}
	w.view.Resize()
	desktopUser32.NewProc("ShowWindow").Call(w.hwnd, 5)
	if err := desktopHRESULT("显示 WebView2", desktopCOM(w.view.Controller(), 4, 1)); err != nil {
		t.Fatal(err)
	}
	var visible int32
	if err := desktopHRESULT("读取 WebView2 可见性", desktopCOM(w.view.Controller(), 3, uintptr(unsafe.Pointer(&visible)))); err != nil || visible != 1 {
		t.Fatalf("WebView2 未显示：visible=%d err=%v", visible, err)
	}
	t.Logf("WebView2 已显示：visible=%d", visible)
	desktopUser32.NewProc("UpdateWindow").Call(w.hwnd)
	if err := desktopIntegrationPump(45*time.Second, func() (bool, error) { return probe != nil && readyMessages == 1, callbackErr }); err != nil {
		desktopIntegrationDiagnostics(t, w)
		t.Fatalf("等待真实扫描及 Vue 渲染：%v（ready=%d scan=%d）", err, readyMessages, len(scanIDs))
	}
	var zoom float64
	var bounds, client [4]int32
	if err := desktopHRESULT("读取真实 ZoomFactor", desktopCOM(w.view.Controller(), 7, uintptr(unsafe.Pointer(&zoom)))); err != nil {
		t.Fatal(err)
	}
	if err := desktopHRESULT("读取真实 WebView Bounds", desktopCOM(w.view.Controller(), 5, uintptr(unsafe.Pointer(&bounds)))); err != nil {
		t.Fatal(err)
	}
	if result, _, err := desktopUser32.NewProc("GetClientRect").Call(w.hwnd, uintptr(unsafe.Pointer(&client))); result == 0 {
		t.Fatal(windowsError("读取真实客户区", err))
	}
	t.Logf("实际宿主缩放：DPI=%d ZoomFactor=%.4f client=%v bounds=%v", windowDPI, zoom, client, bounds)
	if bounds != client {
		t.Errorf("WebView Bounds 与客户区不一致：%v != %v", bounds, client)
	}
	desktopIntegrationAssertFonts(t, probe.Fonts)
	desktopIntegrationAssertLayout(t, "默认客户区", probe.Layout)
	if err := desktopSetClientSize(w.hwnd, desktopMinimumWidth, desktopMinimumHeight); err != nil {
		t.Fatal(err)
	}
	w.view.Resize()
	if err := w.view.Eval("window.dispatchEvent(new Event('mouse:test-minimum-layout'))"); err != nil {
		t.Fatal(err)
	}
	if err := desktopIntegrationPump(10*time.Second, func() (bool, error) { return minimumProbe != nil, callbackErr }); err != nil {
		t.Fatalf("等待最小客户区真实页面探针：%v", err)
	}
	if minimumProbe.Reply.ID != probe.Reply.ID || minimumProbe.Busy != "false" || minimumProbe.State != probe.State || minimumProbe.SelectedID != probe.SelectedID {
		t.Fatal("最小客户区探针未保留真实扫描完成状态")
	}
	desktopIntegrationAssertFonts(t, minimumProbe.Fonts)
	desktopIntegrationAssertLayout(t, "最小客户区", minimumProbe.Layout)
	t.Logf("Vue 最终正文：\n%s", probe.Text)
	if readyMessages != 1 || len(scanIDs) != 1 || !scanIDs[probe.Reply.ID] {
		t.Fatalf("页面加载与真实扫描回包未匹配：ready=%d scan=%d reply=%q", readyMessages, len(scanIDs), probe.Reply.ID)
	}
	if !strings.Contains(probe.Title, "鼠标工具") || probe.Busy != "false" || strings.Contains(probe.Text, "界面演示") {
		t.Fatalf("Vue 未进入真实模式的扫描完成状态：title=%q aria-busy=%q", probe.Title, probe.Busy)
	}
	if probe.Reply.Error != "" {
		t.Logf("真实 scan 返回错误：%s", probe.Reply.Error)
		for _, p := range []*scanProbe{probe, minimumProbe} {
			if len(p.Alerts) == 0 || !p.FullErrorEntry {
				t.Errorf("%s 未提供可见错误摘要和完整错误入口", p.Size)
			}
		}
		return
	}
	var snapshot guiSnapshot
	if err := json.Unmarshal(probe.Reply.Result, &snapshot); err != nil {
		t.Fatalf("真实 scan DTO 解析失败：%v", err)
	}
	if snapshot.Version != version || snapshot.ScannedAt == "" || snapshot.Devices == nil || len(probe.Alerts) != 0 || len(minimumProbe.Alerts) != 0 {
		t.Fatal("扫描 DTO 或前端接受状态无效")
	}
	t.Logf("真实 scan DTO：version=%s scannedAt=%s devices=%d", snapshot.Version, snapshot.ScannedAt, len(snapshot.Devices))
	if len(snapshot.Devices) == 0 {
		for _, p := range []*scanProbe{probe, minimumProbe} {
			if p.State != "empty" || p.EmptyText == "" {
				t.Errorf("%s 真实空设备结果未提供完成后的空状态说明", p.Size)
			}
		}
		return
	}
	if probe.State != "ready" {
		t.Fatalf("扫描发现真实设备但页面未就绪：state=%q", probe.State)
	}
	brandPrefix := regexp.MustCompile(`(?i)^(Razer |Logitech |Logi |MCHOSE |雷蛇\s*|罗技\s*|迈从\s*)`)
	selectedFound := false
	for _, d := range snapshot.Devices {
		if d.ID == probe.SelectedID {
			selectedFound = true
			name := brandPrefix.ReplaceAllString(d.Name, "")
			if name == "" || !strings.Contains(probe.Text, name) || !strings.Contains(minimumProbe.Text, name) {
				t.Errorf("实际选中设备名称未出现在两个尺寸的 Vue：%q", d.Name)
			}
		}
		brief, _ := json.Marshal(struct {
			Name, Status string
			Battery      guiBattery
			DPI          guiDPI
			Rate         guiRate
		}{d.Name, d.Status, d.Battery, d.DPI, d.Rate})
		t.Logf("设备 DTO 摘要：%s", brief)
	}
	if !selectedFound {
		t.Fatalf("Vue 选中设备不属于真实扫描 DTO：%q", probe.SelectedID)
	}
}

// 仅测试持有 Environment5，便于 engine.Close 释放宿主引用之后接收退出事件。
// ABI 来自 WebView2 SDK 1.0.2739.15；事件语义：
// https://learn.microsoft.com/microsoft-edge/webview2/concepts/process-related-events
type desktopIntegrationBrowserExit struct {
	environment unsafe.Pointer
	handler     *desktopEvent
	token       int64
	pid         uint32
	exited      bool
	err         error
}

func newDesktopIntegrationBrowserExit(e *desktopEngine) (*desktopIntegrationBrowserExit, error) {
	b := &desktopIntegrationBrowserExit{}
	if err := desktopHRESULT("读取测试浏览器 PID", desktopCOM(e.core, 37, uintptr(unsafe.Pointer(&b.pid)))); err != nil {
		return nil, err
	}
	if b.pid == 0 {
		return nil, fmt.Errorf("测试浏览器 PID 无效")
	}
	environmentIID := guid{0x319e423d, 0xe0d7, 0x4b8d, [8]byte{0x92, 0x54, 0xae, 0x94, 0x75, 0xde, 0x9b, 0x17}}
	if err := desktopHRESULT("获取测试浏览器退出接口", desktopCOM(e.environment, 0, uintptr(unsafe.Pointer(&environmentIID)), uintptr(unsafe.Pointer(&b.environment)))); err != nil {
		return nil, err
	}
	if b.environment == nil {
		return nil, fmt.Errorf("浏览器退出接口为空")
	}
	handlerIID := guid{0xfa504257, 0xa216, 0x4911, [8]byte{0xa8, 0x60, 0xfe, 0x88, 0x25, 0x71, 0x28, 0x61}}
	b.handler = newDesktopEvent(handlerIID, func(args unsafe.Pointer) uintptr {
		if args == nil {
			b.err = fmt.Errorf("浏览器退出事件参数为空")
			return 0
		}
		var pid uint32
		if err := desktopHRESULT("读取退出事件 PID", desktopCOM(args, 4, uintptr(unsafe.Pointer(&pid)))); err != nil {
			b.err = err
		} else if pid == b.pid {
			b.exited = true
		}
		return 0
	})
	if err := desktopHRESULT("监听测试浏览器退出", desktopCOM(b.environment, 12, uintptr(unsafe.Pointer(b.handler)), uintptr(unsafe.Pointer(&b.token)))); err != nil {
		b.handler.Release()
		desktopCOM(b.environment, 2)
		return nil, err
	}
	return b, nil
}

func (b *desktopIntegrationBrowserExit) Wait(limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for !b.exited {
		if b.err != nil {
			return b.err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 BrowserProcessExited 超过 %s（pid=%d）", limit, b.pid)
		}
		var message desktopMessage
		r, _, _ := desktopUser32.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
		if r == 0 {
			time.Sleep(10 * time.Millisecond)
		} else if message.Message != 0x12 { // 清理阶段忽略 WM_QUIT，继续接收 COM 退出通知。
			desktopUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
			desktopUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
		}
	}
	return b.err
}

func (b *desktopIntegrationBrowserExit) Close() error {
	err := desktopHRESULT("移除测试浏览器退出监听", desktopCOM(b.environment, 13, uintptr(b.token)))
	desktopCOM(b.environment, 2)
	b.handler.Release()
	return err
}

// 仅接收本测试 MkdirTemp 创建的路径。进程退出后文件系统仍可能短暂报告
// 共享冲突或目录非空；有界重试实际删除和不存在校验，始终保留最终失败原因。
func desktopIntegrationRemoveCache(path string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		err := os.RemoveAll(path)
		if err == nil {
			_, err = os.Stat(path)
			if os.IsNotExist(err) {
				return nil
			}
			if err == nil {
				err = fmt.Errorf("删除后目录仍然存在")
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("清理本次测试缓存 %q 超过 %s：%w", path, limit, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func desktopIntegrationPump(limit time.Duration, done func() (bool, error)) error {
	deadline := time.Now().Add(limit)
	for {
		finished, err := done()
		if err != nil || finished {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("超过 %s", limit)
		}
		running, err := desktopNextMessage()
		if err != nil {
			return err
		}
		if !running {
			return fmt.Errorf("窗口提前退出")
		}
	}
}

// 超时后直接调用 ExecuteScript 获取诊断，不依赖可能失效的页面消息桥。
func desktopIntegrationDiagnostics(t *testing.T, w *desktopWindow) {
	t.Helper()
	t.Logf("宿主诊断：ready=%v err=%v initialNavigation=%v trusted=%v busy=%v", w.view.Ready(), w.view.Err(), w.boundary.initialNavigation, w.boundary.Trusted(), w.busy.Load())
	var visible int32
	var bounds [4]int32
	visibilityErr := desktopHRESULT("读取可见性", desktopCOM(w.view.Controller(), 3, uintptr(unsafe.Pointer(&visible))))
	boundsErr := desktopHRESULT("读取尺寸", desktopCOM(w.view.Controller(), 5, uintptr(unsafe.Pointer(&bounds))))
	t.Logf("控制器：visible=%d err=%v bounds=%v err=%v", visible, visibilityErr, bounds, boundsErr)
	iid := guid{0x49511172, 0xcc67, 0x4bca, [8]byte{0x99, 0x23, 0x13, 0x71, 0x12, 0xf4, 0xc4, 0xcc}}
	completed := false
	var result string
	var resultErr error
	handler := newDesktopCallback(iid, func(hr uintptr, data unsafe.Pointer) uintptr {
		completed = true
		resultErr = desktopHRESULT("读取诊断 DOM", hr)
		result = windows.UTF16PtrToString((*uint16)(data)) // COM 借用字符串，只在回调内复制。
		return 0
	})
	defer handler.Release()
	const script = `({html:document.body&&document.body.innerHTML.slice(0,20000),text:document.body&&document.body.innerText,locationType:location.protocol,locationLength:location.href.length,readyState:document.readyState,nativeSubmit:typeof window.mouseNativeSubmit,webview:!!(window.chrome&&window.chrome.webview),appChildren:document.querySelector('#app')?.childElementCount})`
	err := desktopHRESULT("请求诊断 DOM", desktopCOM(w.boundary.core, 29, uintptr(unsafe.Pointer(desktopText(script))), uintptr(unsafe.Pointer(handler))))
	if err == nil {
		err = desktopIntegrationPump(5*time.Second, func() (bool, error) { return completed, nil })
	}
	// DOM 中可能包含桥接会话口令；诊断日志只保留脱敏后的源码。
	t.Logf("ExecuteScript 诊断：request=%v callback=%v result=%s", err, resultErr, strings.ReplaceAll(result, w.token, "[会话口令已隐藏]"))
}
