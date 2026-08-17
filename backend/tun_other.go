//go:build !windows

package main

func tunModeSupported() bool {
	return false
}

func processElevated() bool {
	return false
}
