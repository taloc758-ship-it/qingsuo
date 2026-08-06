//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// configureBackgroundCommand prevents child tools from creating a console
// window when QingSuo itself is running as a desktop background service.
func configureBackgroundCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
