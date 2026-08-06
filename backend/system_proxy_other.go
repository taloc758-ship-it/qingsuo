//go:build !windows

package main

import "errors"

func systemProxyStatus() (systemProxyResponse, error) {
	return systemProxyResponse{Supported: false}, nil
}

func setSystemProxy(bool) error {
	return errors.New("system proxy control is currently supported only on Windows")
}
