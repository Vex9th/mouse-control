package main

import "testing"

func TestDesktopClientPixelsUsesDPI(t *testing.T) {
	for _, tc := range []struct {
		dpi    uint32
		width  int32
		height int32
	}{{96, 900, 540}, {120, 1125, 675}, {144, 1350, 810}, {192, 1800, 1080}} {
		width, height, err := desktopClientPixels(900, 540, tc.dpi)
		if err != nil || width != tc.width || height != tc.height {
			t.Errorf("DPI=%d got=%dx%d err=%v want=%dx%d", tc.dpi, width, height, err, tc.width, tc.height)
		}
	}
	if _, _, err := desktopClientPixels(900, 540, 0); err == nil {
		t.Fatal("未知 DPI 不能默认为96")
	}
}

func TestDesktopFitRectStaysInsideWorkArea(t *testing.T) {
	for _, tc := range []struct {
		name            string
		r, work, expect [4]int32
	}{
		{"unchanged", [4]int32{50, 50, 950, 590}, [4]int32{0, 0, 1920, 1040}, [4]int32{50, 50, 950, 590}},
		{"small-work-area", [4]int32{0, 0, 1800, 1080}, [4]int32{0, 30, 1280, 720}, [4]int32{0, 30, 1280, 720}},
		{"left-monitor", [4]int32{-2200, -50, -1300, 490}, [4]int32{-1920, 0, 0, 1040}, [4]int32{-1920, 0, -1020, 540}},
		{"right-bottom", [4]int32{1700, 900, 2600, 1440}, [4]int32{0, 0, 1920, 1040}, [4]int32{1020, 500, 1920, 1040}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := desktopFitRect(tc.r, tc.work)
			if err != nil || got != tc.expect {
				t.Fatalf("got=%v err=%v want=%v", got, err, tc.expect)
			}
		})
	}
	if _, err := desktopFitRect([4]int32{0, 0, 100, 100}, [4]int32{}); err == nil {
		t.Fatal("空工作区不能被接受")
	}
}
