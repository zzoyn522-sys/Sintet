//go:build !windows

package launcher

import "os/exec"

func hideProcess(cmd *exec.Cmd) {}
