package main

import "fmt"

const (
	desktopDefaultWidth  = 900
	desktopDefaultHeight = 440
	desktopMinimumWidth  = 760
	desktopMinimumHeight = 440
)

func desktopClientPixels(width, height int32, dpi uint32) (int32, int32, error) {
	if width <= 0 || height <= 0 || dpi == 0 {
		return 0, 0, fmt.Errorf("无效客户区尺寸或 DPI：%dx%d DPI=%d", width, height, dpi)
	}
	w, h := (int64(width)*int64(dpi)+48)/96, (int64(height)*int64(dpi)+48)/96
	if w <= 0 || h <= 0 || w > 1<<31-1 || h > 1<<31-1 {
		return 0, 0, fmt.Errorf("客户区物理尺寸超出范围：%dx%d", w, h)
	}
	return int32(w), int32(h), nil
}

func desktopFitRect(r, work [4]int32) ([4]int32, error) {
	w, h := int64(r[2])-int64(r[0]), int64(r[3])-int64(r[1])
	workW, workH := int64(work[2])-int64(work[0]), int64(work[3])-int64(work[1])
	if w <= 0 || h <= 0 || workW <= 0 || workH <= 0 || workW > 1<<31-1 || workH > 1<<31-1 {
		return [4]int32{}, fmt.Errorf("无效窗口或桌面工作区：window=%v work=%v", r, work)
	}
	width, height := int32(min(w, workW)), int32(min(h, workH))
	x := max(work[0], min(r[0], work[2]-width))
	y := max(work[1], min(r[1], work[3]-height))
	return [4]int32{x, y, x + width, y + height}, nil
}
