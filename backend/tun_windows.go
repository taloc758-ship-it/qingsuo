//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func tunModeSupported() bool {
	return true
}

func processElevated() bool {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()

	var elevation uint32
	var returned uint32
	err = syscall.GetTokenInformation(token, syscall.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &returned)
	return err == nil && elevation != 0
}
