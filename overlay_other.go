//go:build !windows

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sintet/internal/sldcfg"
)

type overlayBridge struct{}

func (a *App) watchOverlay(ctx context.Context) {}
func (a *App) PreviewOverlay(_ sldcfg.OverlayAppearance, _ string, _ int) error {
	return fmt.Errorf("оверлей пока доступен только в Windows")
}
func (a *App) OverlayPreviewInput(_ [][4]int) {}
func (a *App) serveOverlayPreview(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (a *App) prepareGameOverlay(_ string) (string, io.Closer, error) { return "", nil, nil }
func (a *App) prepareJavaBridge() (string, error)                     { return "", nil }
func (a *App) presenceMedia(bool) (*presenceMusic, []byte)            { return nil, nil }
