//go:build !windows

package main

import "errors"

func startPrelaunchUpdater() error {
	return errors.New("Sintet update bootstrap is only available on Windows")
}
func runTargetedBootstrap(string) error { return errors.New("Windows only") }
func showTargetedBootstrapError(error)  {}
