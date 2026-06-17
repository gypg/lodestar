//go:build windows

package shutdown

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

func setupPlatformSignals(quit chan<- os.Signal) {
	go func() {
		handler := func(ctrlType uint32) uintptr {
			switch ctrlType {
			case windows.CTRL_CLOSE_EVENT, windows.CTRL_LOGOFF_EVENT, windows.CTRL_SHUTDOWN_EVENT:
				ilog.Warnf("Received Windows console event: %d", ctrlType)
				select {
				case quit <- os.Interrupt:
				default:
					ilog.Warnf("Signal channel full, console event ignored")
				}
				// Block the handler so the OS doesn't terminate us before
				// Listen() finishes running the registered shutdown functions.
				// The process will exit via os.Exit(0) in Listen().
				time.Sleep(30 * time.Second)
				return 1
			}
			return 0
		}
		cb := windows.NewCallback(handler)
		ret, _, err := procSetConsoleCtrlHandler.Call(cb, 1) // 1 = TRUE (add handler)
		if ret == 0 {
			ilog.Errorf("Failed to set console ctrl handler: %v", err)
		}
	}()
}
