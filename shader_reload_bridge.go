package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

type shaderReloadRequest struct {
	id, run, filename string
	delivered         bool
	result            chan string
}

type shaderReloadBridge struct {
	mu      sync.Mutex
	run     string
	seen    time.Time
	pending *shaderReloadRequest
}

func (b *shaderReloadBridge) poll(w http.ResponseWriter, run string) {
	if run == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.run, b.seen = run, time.Now()
	if p := b.pending; p != nil && p.run == run && !p.delivered {
		p.delivered = true
		w.Header().Set("X-Sintet-Shader-Reload", p.id+" "+base64.RawURLEncoding.EncodeToString([]byte(p.filename)))
	}
}

func (b *shaderReloadBridge) complete(run, id, result string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.pending; p != nil && p.delivered && p.run == run && p.id == id {
		switch result {
		case "applied", "inactive", "unsupported", "failed":
			select {
			case p.result <- result:
			default:
			}
		}
	}
}

func (b *shaderReloadBridge) reload(ctx context.Context, run, filename string) string {
	b.mu.Lock()
	if b.run != run || time.Since(b.seen) > 3*time.Second {
		b.mu.Unlock()
		return "unavailable"
	}
	if b.pending != nil {
		b.mu.Unlock()
		return "unconfirmed"
	}
	p := &shaderReloadRequest{id: rand.Text(), run: run, filename: filename, result: make(chan string, 1)}
	b.pending = p
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.pending = nil
		b.mu.Unlock()
	}()
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case result := <-p.result:
		return result
	case <-timer.C:
		return "unconfirmed"
	case <-ctx.Done():
		return "unconfirmed"
	}
}
