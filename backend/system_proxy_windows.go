//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	regSZ               = 1
	regDWORD            = 4
	rrfRTRegSZ          = 0x00000002
	rrfRTRegDWORD       = 0x00000010
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	procRegGetValueW    = advapi32.NewProc("RegGetValueW")
	procRegSetKeyValueW = advapi32.NewProc("RegSetKeyValueW")
)

func systemProxyStatus() (systemProxyResponse, error) {
	enabled, err := readRegistryDWORD("ProxyEnable")
	if err == syscall.ERROR_FILE_NOT_FOUND {
		// Windows may omit this value until a proxy is configured. That is the
		// same as the system proxy being disabled, not an API failure.
		enabled = 0
	} else if err != nil {
		return systemProxyResponse{}, fmt.Errorf("read ProxyEnable: %w", err)
	}
	server, err := readRegistryString("ProxyServer")
	if err != nil && err != syscall.ERROR_FILE_NOT_FOUND {
		return systemProxyResponse{}, fmt.Errorf("read ProxyServer: %w", err)
	}
	return systemProxyResponse{Supported: true, Enabled: enabled != 0, Server: server}, nil
}

func setSystemProxy(enabled bool) error {
	if enabled {
		if err := writeRegistryString("ProxyServer", "127.0.0.1:2081"); err != nil {
			return fmt.Errorf("set ProxyServer: %w", err)
		}
		if err := writeRegistryString("ProxyOverride", "<local>;localhost;127.0.0.1"); err != nil {
			return fmt.Errorf("set ProxyOverride: %w", err)
		}
	}
	value := uint32(0)
	if enabled {
		value = 1
	}
	if err := writeRegistryDWORD("ProxyEnable", value); err != nil {
		return fmt.Errorf("set ProxyEnable: %w", err)
	}
	return nil
}

func readRegistryDWORD(name string) (uint32, error) {
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var value uint32
	size := uint32(unsafe.Sizeof(value))
	result, _, _ := procRegGetValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(internetSettingsKey))), uintptr(unsafe.Pointer(valueName)), rrfRTRegDWORD,
		0, uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&size)),
	)
	if result != 0 {
		return 0, syscall.Errno(result)
	}
	return value, nil
}

func readRegistryString(name string) (string, error) {
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var size uint32
	result, _, _ := procRegGetValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(internetSettingsKey))), uintptr(unsafe.Pointer(valueName)), rrfRTRegSZ,
		0, 0, uintptr(unsafe.Pointer(&size)),
	)
	if result != 0 {
		return "", syscall.Errno(result)
	}
	buffer := make([]uint16, (size+1)/2)
	result, _, _ = procRegGetValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(internetSettingsKey))), uintptr(unsafe.Pointer(valueName)), rrfRTRegSZ,
		0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)),
	)
	if result != 0 {
		return "", syscall.Errno(result)
	}
	return syscall.UTF16ToString(buffer), nil
}

func writeRegistryString(name, value string) error {
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	result, _, _ := procRegSetKeyValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(internetSettingsKey))), uintptr(unsafe.Pointer(valueName)),
		regSZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2),
	)
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}

func writeRegistryDWORD(name string, value uint32) error {
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	result, _, _ := procRegSetKeyValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(internetSettingsKey))), uintptr(unsafe.Pointer(valueName)),
		regDWORD, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value),
	)
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}
