//go:build !windows

package launcher

import "errors"

func availableDiskSpace(string) (uint64, error) { return 0, errors.New("disk probe not supported") }
func platformDiskFull(error) bool               { return false }
