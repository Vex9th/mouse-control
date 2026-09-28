//go:build !windows

package main

import "errors"

func discoverDevices() ([]device, error) {
	return nil, errors.New("电量查询仅支持 Windows 10 / 11 x64")
}
func prepareConsole() (func(), bool) { return func() {}, false }
func consoleInteractive() bool       { return false }
