package main

import (
	"context"
	"net/http"
	"time"
)

const offlineMessage = "Без доступа к интернету"

// InternetAvailable reports the last completed connectivity check.
func (a *App) InternetAvailable() bool {
	return !a.internetOffline.Load()
}

func (a *App) watchConnectivity(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		online := checkInternet(ctx)
		if ctx.Err() != nil {
			return
		}
		a.internetOffline.Store(!online)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func checkInternet(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoints := []string{
		"https://sintet.xyz/",
		"https://piston-meta.mojang.com/mc/game/version_manifest_v2.json",
	}
	results := make(chan bool, len(endpoints))
	for _, address := range endpoints {
		go func(address string) {
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, address, nil)
			if err != nil {
				results <- false
				return
			}
			response, err := client.Do(req)
			if err == nil {
				response.Body.Close()
			}
			results <- err == nil
		}(address)
	}
	for range endpoints {
		if <-results {
			return true
		}
	}
	return false
}
