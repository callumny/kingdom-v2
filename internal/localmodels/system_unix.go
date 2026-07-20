//go:build !windows

package localmodels

import "syscall"

func detachedSysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
