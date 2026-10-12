//go:build windows

package xray

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func attachChildLifetime(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    true,
	}
}
