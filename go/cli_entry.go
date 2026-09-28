//go:build !gui

package main

import (
	"fmt"
	"os"
)

func main() {
	restore, ownConsole := prepareConsole()
	code, enteredMenu := run()
	if ownConsole && !enteredMenu {
		fmt.Print("\n按回车键关闭窗口…")
		var line string
		fmt.Scanln(&line)
	}
	restore()
	os.Exit(code)
}
