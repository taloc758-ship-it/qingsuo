//go:build !windows

package main

import "os/exec"

func configureBackgroundCommand(_ *exec.Cmd) {}
