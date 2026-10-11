//go:build !windows && !linux

package process

import "syscall"

func groupAlive(pgid int) bool { return syscall.Kill(-pgid, 0) != syscall.ESRCH }
