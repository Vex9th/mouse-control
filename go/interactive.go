package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type inputLine struct {
	text string
	err  error
}

// 菜单等待输入时也响应 Ctrl+C；读取端在退出时不再向菜单发送内容。
func menuInput(ctx context.Context, in io.Reader) <-chan inputLine {
	lines := make(chan inputLine)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			select {
			case lines <- inputLine{text: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- inputLine{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return lines
}

func runInteractive(ctx context.Context, in io.Reader, out, errOut io.Writer, enumerate func() ([]device, error)) (code int) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	lines := menuInput(ctx, in)
	var inputErr error
	readLine := func(prompt string) (string, bool) {
		fmt.Fprint(out, prompt)
		select {
		case <-ctx.Done():
			return "", false
		case line, ok := <-lines:
			if !ok {
				return "", false
			}
			if line.err != nil {
				inputErr = line.err
				return "", false
			}
			return strings.TrimSpace(line.text), true
		}
	}
	defer func() {
		if inputErr != nil {
			fmt.Fprintln(errOut, "输入读取失败：", inputErr)
			code = 3
		}
		fmt.Fprintln(out, "\n已退出。")
	}()
	var rows []deviceResult
	selectedID := ""
	refresh := func() {
		started := time.Now()
		devices, err := enumerate()
		rows = nil
		if err == nil {
			for _, d := range devices {
				if ctx.Err() != nil {
					break
				}
				r := snapshotMouseResult(readMouse(d))
				rows = append(rows, r)
			}
		}
		closeDevices(devices)
		if err != nil {
			fmt.Fprintln(errOut, "设备枚举失败：", err)
			return
		}
		// 已选中的鼠标断开后保留原 ID，绝不自动转向后来插入的鼠标。
		if selectedID == "" && len(rows) == 1 {
			selectedID = rows[0].Device.ID
		}
		printReport(out, rows, started)
	}
	if ctx.Err() != nil {
		return 0
	}
	refresh()
	for ctx.Err() == nil {
		var selected *deviceResult
		for i := range rows {
			if strings.EqualFold(rows[i].Device.ID, selectedID) {
				selected = &rows[i]
				break
			}
		}
		if selected != nil {
			fmt.Fprintf(out, "\n当前鼠标：%s\n", deviceName(selected.Device))
		} else if selectedID != "" {
			fmt.Fprintf(out, "\n原选中鼠标已断开：%s\n请重新选择鼠标后设置。\n", cleanLabel(selectedID))
		} else {
			fmt.Fprintln(out, "\n尚未选择鼠标。")
		}
		fmt.Fprintln(out, "  1  刷新状态    2  选择鼠标    3  设置 DPI\n  4  设置回报率  5  查看详情    0  退出")
		choice, ok := readLine("请选择 > ")
		if !ok {
			return 0
		}
		switch choice {
		case "0", "q", "Q", "exit":
			return 0
		case "1":
			refresh()
		case "2":
			refresh()
			if len(rows) == 0 {
				continue
			}
			for i, r := range rows {
				fmt.Fprintf(out, "  %d  %s\n     %s\n", i+1, deviceName(r.Device), cleanLabel(r.Device.ID))
			}
			text, ok := readLine("输入鼠标编号（回车取消） > ")
			if !ok {
				return 0
			}
			if text == "" {
				continue
			}
			n, err := strconv.Atoi(text)
			if err != nil || n < 1 || n > len(rows) {
				fmt.Fprintln(errOut, "编号无效，请输入列表中的鼠标编号。")
				continue
			}
			selectedID = rows[n-1].Device.ID
		case "3", "4":
			if selected == nil {
				fmt.Fprintln(errOut, "请先选择一只已连接的鼠标。")
				continue
			}
			cap := deviceFeatures(selected.Device)
			opts := options{Device: selectedID}
			if choice == "3" {
				if cap.DPIReadOnly || len(cap.DPIRanges) == 0 {
					fmt.Fprintf(errOut, "DPI 无法设置：%s\n", settingRestriction(cap.DPIReadOnly, cap.DPIReason))
					continue
				}
				fmt.Fprintf(out, "允许范围：%s。\n", formatDPIRanges(cap.DPIRanges))
				if cap.SeparateAxes {
					fmt.Fprintln(out, "输入单值设置双轴，或输入 X,Y 分别设置。")
				} else {
					fmt.Fprintln(out, "此型号统一设置双轴，请输入单个 DPI 值。")
				}
				text, ok := readLine("新 DPI（回车取消） > ")
				if !ok {
					return 0
				}
				if text == "" {
					continue
				}
				dpi, err := parseDPI(text)
				if err != nil {
					fmt.Fprintln(errOut, "输入无效：", err)
					continue
				}
				opts.SetDPI = &dpi
			} else {
				if cap.RateReadOnly || len(cap.PollRates) == 0 {
					fmt.Fprintf(errOut, "回报率无法设置：%s\n", settingRestriction(cap.RateReadOnly, cap.RateReason))
					continue
				}
				fmt.Fprintf(out, "可用档位：%s\n", formatRates(cap.PollRates))
				text, ok := readLine("新回报率（回车取消） > ")
				if !ok {
					return 0
				}
				if text == "" {
					continue
				}
				n, err := strconv.Atoi(text)
				if err != nil || n < 1 || n > 8000 {
					fmt.Fprintln(errOut, "请输入可用档位中的整数回报率。")
					continue
				}
				opts.SetRate = n
			}
			runSettings(opts, out, errOut, enumerate)
			refresh()
		case "5":
			if selected == nil {
				fmt.Fprintln(errOut, "请先选择一只已连接的鼠标。")
				continue
			}
			printDetails(out, []deviceResult{*selected})
		default:
			fmt.Fprintln(errOut, "请输入 0–5 之间的菜单编号。")
		}
	}
	return 0
}
