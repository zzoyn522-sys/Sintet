package social

import (
	"bytes"
	"context"
	"errors"
	"github.com/hashicorp/yamux"
	"net"
	"net/http"
	"time"
)

// Pack requests use a separate encrypted data channel. Nothing listens on a
// public TCP port and old Minecraft streams retain their original wire format.
func (p *Peer) SetPackHandler(handler http.Handler) {
	p.mu.Lock()
	p.packHandler = handler
	p.mu.Unlock()
}

type packListener struct{ session *yamux.Session }

func (l packListener) Accept() (net.Conn, error) { return l.session.AcceptStream() }
func (l packListener) Close() error              { return l.session.Close() }
func (l packListener) Addr() net.Addr            { return l.session.LocalAddr() }
func (p *Peer) servePack(session *yamux.Session) {
	limit := make(chan struct{}, 8)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case limit <- struct{}{}:
			defer func() { <-limit }()
		default:
			http.Error(w, "Too many pack requests", 429)
			return
		}
		p.mu.Lock()
		h := p.packHandler
		p.mu.Unlock()
		if h == nil {
			http.Error(w, "Pack sharing unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet && !(r.Method == http.MethodPost && r.URL.Path == "/lobby") {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if p.relay && r.Method == http.MethodGet && r.URL.Path != "/manifest" {
			http.Error(w, "Pack files unavailable over relay", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096}
	_ = server.Serve(packListener{session})
}
func (p *Peer) Relayed() bool { return p.relay }

func (p *Peer) PackGet(ctx context.Context, path string) (*http.Response, error) {
	if len(path) > 200 || (path != "/manifest" && (len(path) != 70 || path[:6] != "/file/")) {
		return nil, errors.New("invalid pack endpoint")
	}
	return p.packRequest(ctx, http.MethodGet, path, nil)
}

func (p *Peer) LobbyExchange(ctx context.Context, data []byte) (*http.Response, error) {
	if len(data) > 2048 {
		return nil, errors.New("lobby update too large")
	}
	return p.packRequest(ctx, http.MethodPost, "/lobby", data)
}

func (p *Peer) packRequest(ctx context.Context, method, path string, data []byte) (*http.Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closed:
		return nil, net.ErrClosed
	case <-p.packReady:
	}
	p.mu.Lock()
	session := p.packSession
	p.mu.Unlock()
	transport := &http.Transport{DisableKeepAlives: true, Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stream, err := session.OpenStream()
		if err != nil {
			return nil, err
		}
		if deadline, ok := ctx.Deadline(); ok {
			stream.SetDeadline(deadline)
		}
		return stream, nil
	}, ResponseHeaderTimeout: 15 * time.Second}
	request, err := http.NewRequestWithContext(ctx, method, "http://sintet-pack"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client.Do(request)
}
