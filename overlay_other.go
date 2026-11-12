//go:build !windows

package main

import (
	"context"
	"fmt"
	"io"
)

type overlayBridge struct{}

func (a *App) watchOverlay(ctx context.Context) {}
func (a *App) PreviewOverlay() error {
	return fmt.Errorf("оверлей пока доступен только в Windows")
}

func (a *App) prepareGameOverlay(_ string) (string, io.Closer, error) { return "", nil, nil }
func (a *App) prepareJavaBridge() (string, error)                     { return "", nil }
func (a *App) presenceMedia(bool) (*presenceMusic, []byte)            { return nil, nil }
