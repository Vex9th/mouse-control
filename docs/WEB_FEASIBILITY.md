# 网页版说明 / Browser version

当前发布的是 **Windows 桌面程序**：Vue 界面内嵌在 Go 程序中，通过系统 WebView2 显示；Go 负责 Windows HID 通信。它没有部署网页，也没有在浏览器中验证真实鼠标。

## 可复用的部分

Vue 页面、设备展示、输入校验和前端状态管理可以复用。浏览器版需要实现另一个 `MouseAPI`，通过 WebHID 连接用户电脑上的设备；Go 的 Windows HID 后端不能直接放入网页运行。

## 浏览器要求与设备限制

- WebHID 需要安全上下文；正式网站使用 HTTPS。
- 首次连接需要用户操作触发设备选择与授权，网页不能静默扫描所有鼠标。
- 浏览器是否提供 `navigator.hid`、是否暴露所需集合与报告，需要在实际浏览器和设备上检查。
- 桌面版支持某个型号，不代表浏览器能访问其所需 HID 接口。普通鼠标输入集合与厂商控制报告也不能混为一谈。
- 型号、连接方式、固件、浏览器版本和授权状态均需要单独验证。

依据：[WebHID 规范](https://wicg.github.io/webhid/)、[Chromium WebHID 说明](https://developer.chrome.com/docs/capabilities/hid)。

合理的实现顺序是：用户授权 → 获取描述符与报告能力 → 只读查询 → 逐型号验证 → 受控设置及独立读回。无法访问所需报告时，继续使用桌面版，不要求用户关闭浏览器的设备保护。

## English

The current release is a Windows desktop application. The Vue UI can be reused, but a browser version needs a separate WebHID transport, explicit user permission and model-by-model testing. The Go Windows HID backend cannot execute directly in a web page.

WebHID requires a secure context and a user-triggered permission flow. Browser support and access to the required HID collections/reports must be checked on the actual device. Desktop compatibility does not imply browser compatibility. No browser-to-mouse hardware path has been verified by this project yet.
