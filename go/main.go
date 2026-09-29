// 鼠标协议及 CLI 入口逻辑，桌面版复用相同后端。
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

const version = "1.5.1"

func run() (int, bool) {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "参数错误：", err)
		fmt.Fprintln(os.Stderr, "使用 -h 查看中文帮助。")
		return 3, false
	}
	if opts.Help {
		printHelp(os.Stdout)
		return 0, false
	}
	if opts.Supported {
		printSupported(os.Stdout)
		return 0, false
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if opts.Interactive || (len(os.Args) == 1 && consoleInteractive()) {
		return runInteractive(ctx, os.Stdin, os.Stdout, os.Stderr, discoverDevices), true
	}
	if opts.SetDPI != nil || opts.SetRate != 0 {
		return runSettings(opts, os.Stdout, os.Stderr, discoverDevices), false
	}
	return runQuery(ctx, opts, os.Stdout, os.Stderr, discoverDevices), false
}

func runQuery(ctx context.Context, opts options, out, errOut io.Writer, enumerate func() ([]device, error)) int {
	for {
		started := time.Now()
		devices, err := enumerate()
		if err != nil {
			closeDevices(devices)
			fmt.Fprintln(errOut, "设备枚举失败：", err)
			if opts.Watch == 0 {
				return 3
			}
		} else {
			rows := make([]deviceResult, 0, len(devices))
			for _, d := range devices {
				if ctx.Err() != nil {
					break
				}
				if opts.Device != "" && !strings.EqualFold(opts.Device, d.ID) {
					continue
				}
				rows = append(rows, snapshotMouseResult(readMouse(d)))
			}
			closeDevices(devices)
			if ctx.Err() != nil {
				fmt.Fprintln(out, "\n已停止查询。")
				return 0
			}
			printReport(out, rows, started)
			if opts.Device != "" && len(rows) == 0 {
				fmt.Fprintf(errOut, "未找到指定鼠标：%s\n", cleanLabel(opts.Device))
			}
			if opts.Details {
				printDetails(out, rows)
			}
			code := reportExitCode(rows)
			if opts.Watch == 0 {
				return code
			}
		}
		fmt.Fprintf(out, "\n每 %d 秒重新识别并查询 · 按 Ctrl+C 停止\n", opts.Watch)
		timer := time.NewTimer(time.Duration(opts.Watch) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			fmt.Fprintln(out, "\n已停止查询。")
			return 0
		case <-timer.C:
		}
	}
}

func selectWriteDevice(devices []device, id string) (device, error) {
	if id != "" {
		for _, d := range devices {
			if strings.EqualFold(d.ID, id) {
				return d, nil
			}
		}
		return device{}, fmt.Errorf("未找到指定鼠标：%s；请重新查询或选择，未执行设置", cleanLabel(id))
	}
	if len(devices) == 0 {
		return device{}, fmt.Errorf("未发现支持的鼠标，未执行设置")
	}
	if len(devices) > 1 {
		return device{}, fmt.Errorf("连接了多只鼠标，请使用 -device 指定完整的设备 ID；可用 -details 查看，未执行设置")
	}
	return devices[0], nil
}

func runSettings(opts options, out, errOut io.Writer, enumerate func() ([]device, error)) int {
	devices, err := enumerate()
	defer closeDevices(devices)
	if err != nil {
		fmt.Fprintln(errOut, "设备枚举失败：", err)
		return 3
	}
	d, err := selectWriteDevice(devices, opts.Device)
	if err != nil {
		fmt.Fprintln(errOut, "无法设置：", err)
		if len(devices) == 0 {
			return 1
		}
		return 3
	}
	fmt.Fprintf(out, "\n  %s\n", deviceName(d))
	if opts.Details {
		printDetails(out, []deviceResult{{Device: d}})
	}
	if opts.SetDPI != nil {
		actual, changed, err := setMouseDPI(d, *opts.SetDPI)
		if err != nil {
			fmt.Fprintln(errOut, "DPI 设置失败：", err)
			return 2
		}
		if changed {
			fmt.Fprintf(out, "  DPI 读回确认：%s\n", formatDPI(actual))
		} else {
			fmt.Fprintf(out, "  当前 DPI 已为 %s，无需写入。\n", formatDPI(actual))
		}
		return 0
	}
	if opts.SetRate != 0 {
		actual, changed, err := setMouseRate(d, opts.SetRate)
		if err != nil {
			fmt.Fprintln(errOut, "回报率设置失败：", err)
			return 2
		}
		if changed {
			fmt.Fprintf(out, "  回报率读回确认：%d Hz\n", actual)
		} else {
			fmt.Fprintf(out, "  当前回报率已为 %d Hz，无需写入。\n", actual)
		}
		return 0
	}
	fmt.Fprintln(errOut, "未指定设置操作。")
	return 3
}

// 关闭设备之前保存可展示的能力，避免后续界面访问已关闭的后端。
func snapshotMouseResult(r deviceResult) deviceResult {
	cap := deviceFeatures(r.Device)
	r.Device.Caps = &cap
	r.Device.Channels = nil
	r.Device.Backend = nil
	return r
}
