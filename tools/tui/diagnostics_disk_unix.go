//go:build linux || darwin || freebsd || openbsd || dragonfly

package main

import "golang.org/x/sys/unix"

func diagnosticDiskAvailable(path string) (uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return uint64(stat.Bavail) * uint64(stat.Bsize), err
}
