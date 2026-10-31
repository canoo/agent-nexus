//go:build !linux && !darwin && !freebsd && !openbsd && !dragonfly && !windows

package main

import "fmt"

func diagnosticDiskAvailable(string) (uint64, error) {
	return 0, fmt.Errorf("disk capacity unavailable on this platform")
}
