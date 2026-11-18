package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"

	"sintet/internal/packsync"
	p2p "sintet/internal/social"
	"sintet/internal/workshop"
)

type sharedTransfer struct {
	ID     string                    `json:"id"`
	Build  string                    `json:"build_id"`
	Hashes []string                  `json:"hashes"`
	Offer  webrtc.SessionDescription `json:"offer"`
}

func (m *sharedBuildManager) pollTransfers() {
	go m.pollHost()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if !m.app.internetOffline.Load() {
				m.receiveTransfers()
			}
		}
	}
}

func (m *sharedBuildManager) pollHost() {
	ticker := time.NewTicker(7 * time.Second)
	defer ticker.Stop()
	m.refreshHost()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.refreshHost()
		}
	}
}

func (m *sharedBuildManager) refreshHost() {
	session, err := m.session()
	m.hostMu.Lock()
	defer m.hostMu.Unlock()
	if m.hostEnded || m.app.internetOffline.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
	defer cancel()
	fields := map[string]any{"session": m.hostID}
	if m.host.account != "" && m.host.account != session.account {
		_ = m.host.request(ctx, "host-offline", "", fields, nil)
		m.host = sharedBuildSession{}
	}
	if err != nil {
		return
	}
	if _, err := os.Stat(m.buildRoot(session)); err != nil {
		return
	}
	m.host = session
	_ = session.request(ctx, "host-online", "", fields, nil)
}

func (m *sharedBuildManager) closeHost() {
	m.hostMu.Lock()
	defer m.hostMu.Unlock()
	m.hostEnded = true
	if m.host.account == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = m.host.request(ctx, "host-offline", "", map[string]any{"session": m.hostID}, nil)
}

func (m *sharedBuildManager) receiveTransfers() {
	session, err := m.session()
	if err != nil {
		return
	}
	// Accounts with no local build data have nothing to seed.
	cacheRoot := filepath.Join(m.app.profiles.root, "SharedBuilds", session.account)
	if files, err := os.ReadDir(cacheRoot); err != nil || len(files) == 0 {
		return
	}
	var offers []sharedTransfer
	if err := session.request(m.ctx, "incoming", "", nil, &offers); err != nil {
		return
	}
	for _, offer := range offers {
		if !sharedUUID(offer.ID) || !sharedUUID(offer.Build) || len(offer.Hashes) == 0 {
			continue
		}
		if !slices.ContainsFunc(offer.Hashes, func(hash string) bool {
			if !sharedDigest.MatchString(hash) {
				return false
			}
			stat, err := os.Lstat(filepath.Join(m.cache(session, offer.Build), hash))
			return err == nil && stat.Mode().IsRegular() && stat.Size() > 0 && stat.Size() <= packsync.MaxMod
		}) {
			continue
		}
		m.mu.Lock()
		_, running := m.uploads[offer.ID]
		if running || len(m.uploads) >= 2 {
			m.mu.Unlock()
			continue
		}
		m.uploads[offer.ID] = struct{}{}
		m.mu.Unlock()
		go m.answerSharedTransfer(session, offer)
	}
}

func (m *sharedBuildManager) answerSharedTransfer(session sharedBuildSession, offer sharedTransfer) {
	defer func() {
		m.mu.Lock()
		delete(m.uploads, offer.ID)
		m.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	defer cancel()
	var peer *p2p.Peer
	var err error
	if offerUsesRelay(&offer.Offer) {
		peer, err = session.relayTransferPeer(ctx, offer.ID, -1)
	} else {
		peer, err = p2p.NewPeer(-1, true)
	}
	if err != nil {
		return
	}
	defer peer.Close()
	allowed := make(map[string]struct{}, len(offer.Hashes))
	for _, hash := range offer.Hashes {
		allowed[hash] = struct{}{}
	}
	peer.SetPackHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/file/"
		if strings.HasPrefix(r.URL.Path, "/icon/") {
			prefix = "/icon/"
		}
		hash, matches := strings.CutPrefix(r.URL.Path, prefix)
		if _, ok := allowed[hash]; !matches || !ok || !sharedDigest.MatchString(hash) {
			http.NotFound(w, r)
			return
		}
		select {
		case m.streams <- struct{}{}:
			defer func() { <-m.streams }()
		default:
			http.Error(w, "Transfer busy", http.StatusTooManyRequests)
			return
		}
		active, err := m.session()
		if err != nil || active.account != session.account {
			http.Error(w, "Access expired", http.StatusForbidden)
			return
		}
		file := filepath.Join(m.cache(session, offer.Build), hash)
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > packsync.MaxMod {
			http.NotFound(w, r)
			return
		}
		var check struct{ Allowed bool }
		if err := session.request(r.Context(), "transfer-check", "", map[string]any{"transfer": offer.ID, "hash": hash}, &check); err != nil || !check.Allowed {
			http.Error(w, "Access expired", http.StatusForbidden)
			return
		}
		if prefix == "/icon/" {
			png, err := workshop.ModIcon(file)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", fmt.Sprint(len(png)))
			_, _ = w.Write(png)
			return
		}
		input, err := os.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer input.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.CopyN(w, input, info.Size())
	}))
	signal, finish := context.WithTimeout(ctx, 25*time.Second)
	defer finish()
	answer, err := peer.Answer(signal, offer.Offer)
	if err != nil {
		return
	}
	if err := session.request(signal, "transfer-answer", "", map[string]any{"transfer": offer.ID, "description": answer}, nil); err != nil {
		return
	}
	connect, finishConnect := context.WithTimeout(ctx, 35*time.Second)
	defer finishConnect()
	if err := peer.Wait(connect); err != nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.Closed():
			return
		case <-ticker.C:
			active, err := m.session()
			if err != nil || active.account != session.account {
				return
			}
			var check struct{ Allowed bool }
			if err := session.request(ctx, "transfer-check", "", map[string]any{"transfer": offer.ID, "hash": offer.Hashes[0]}, &check); err != nil || !check.Allowed {
				return
			}
		}
	}
}

type sharedPeerSource struct {
	hashes []string
	mu     sync.Mutex
	peer   *p2p.Peer
	id     string
	err    error
}

type sharedBuildFetcher struct {
	session sharedBuildSession
	build   string
	seeds   map[string][]string
	sources map[string]*sharedPeerSource
}

type sharedSourceUnavailable struct{}

func (sharedSourceUnavailable) Error() string {
	return "файл недоступен. Открой Sintet у лидера или автора файла и попробуй снова."
}

func (sharedSourceUnavailable) Is(target error) bool { return target == net.ErrClosed }

func newSharedBuildFetcher(s sharedBuildSession, build SharedBuild, mods []sharedBuildMod, cache string) *sharedBuildFetcher {
	f := &sharedBuildFetcher{s, build.ID, make(map[string][]string), make(map[string]*sharedPeerSource)}
	for _, mod := range mods {
		hash := mod.File.SHA256
		if _, ok := f.seeds[hash]; ok || packsync.ValidCached(cache, mod.File) {
			continue
		}
		f.seeds[hash] = nil
		for _, seed := range []string{build.OwnerID, mod.Author} {
			if !sharedUUID(seed) || seed == s.account || len(f.seeds[hash]) > 0 && f.seeds[hash][0] == seed {
				continue
			}
			f.seeds[hash] = append(f.seeds[hash], seed)
			if f.sources[seed] == nil {
				f.sources[seed] = &sharedPeerSource{}
			}
			f.sources[seed].hashes = append(f.sources[seed].hashes, hash)
		}
	}
	return f
}

func (f *sharedBuildFetcher) PackGet(ctx context.Context, route string) (*http.Response, error) {
	prefix := "/file/"
	if strings.HasPrefix(route, "/icon/") {
		prefix = "/icon/"
	}
	hash, matches := strings.CutPrefix(route, prefix)
	if !matches || !sharedDigest.MatchString(hash) {
		return nil, errors.New("неверный файл общей сборки")
	}
	var quotaErr error
	for _, seed := range f.seeds[hash] {
		source := f.sources[seed]
		source.mu.Lock()
		if source.peer == nil && source.err == nil {
			source.peer, source.id, source.err = f.connect(ctx, seed, source.hashes)
		}
		peer := source.peer
		if errors.Is(source.err, errTURNLimit) {
			quotaErr = source.err
		}
		source.mu.Unlock()
		if peer == nil {
			continue
		}
		res, err := peer.PackGet(ctx, route)
		if err == nil {
			switch res.StatusCode {
			case http.StatusOK, http.StatusTooManyRequests:
				return res, nil
			default:
				res.Body.Close()
			}
		}
	}
	if quotaErr != nil {
		return nil, quotaErr
	}
	return nil, sharedSourceUnavailable{}
}

func (s sharedBuildSession) relayTransferPeer(ctx context.Context, id string, port int) (*p2p.Peer, error) {
	var response struct {
		Servers []webrtc.ICEServer `json:"iceServers"`
		Metered bool               `json:"metered"`
	}
	if err := s.request(ctx, "transfer-relay", "", map[string]any{"transfer": id}, &response); err != nil {
		err = normalizeTURNError(err)
		if errors.Is(err, errTURNLimit) && s.social != nil {
			s.social.notifyTURNLimit()
		}
		return nil, err
	}
	if !response.Metered {
		return nil, errors.New("Проверка лимита TURN временно недоступна")
	}
	peer, err := p2p.NewMeteredRelayPeer(port, response.Servers, nil)
	if err == nil && s.social != nil {
		go s.social.watchTURNPeer(s.client, s.account, peer)
	}
	return peer, err
}

func (f *sharedBuildFetcher) connect(ctx context.Context, seed string, hashes []string) (*p2p.Peer, string, error) {
	peer, id, err := f.connectTransfer(ctx, seed, hashes, false)
	if errors.Is(err, errDirectTunnel) && ctx.Err() == nil && f.relayAvailable() {
		return f.connectTransfer(ctx, seed, hashes, true)
	}
	return peer, id, err
}

func (f *sharedBuildFetcher) relayAvailable() bool {
	return f.session.social != nil && f.session.social.snapshot().RelayEnabled
}

func (f *sharedBuildFetcher) connectTransfer(ctx context.Context, seed string, hashes []string, relay bool) (*p2p.Peer, string, error) {
	signal, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	id := uuid.NewString()
	var peer *p2p.Peer
	var err error
	var opened, ready bool
	defer func() {
		if !ready {
			if peer != nil {
				peer.Close()
			}
			if opened {
				f.end(id)
			}
		}
	}()
	input := map[string]any{"seed": seed, "transfer": id, "hashes": hashes}
	if relay {
		input["description"] = webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "sintet-turn-pending"}
		if err = f.session.request(signal, "transfer-open", f.build, input, nil); err != nil {
			return nil, "", err
		}
		opened = true
		peer, err = f.session.relayTransferPeer(signal, id, 0)
	} else {
		peer, err = p2p.NewPeer(0, true)
	}
	if err != nil {
		return nil, "", err
	}
	offer, err := peer.Offer(signal)
	if err != nil {
		return nil, "", err
	}
	if relay {
		if !offerUsesRelay(offer) {
			return nil, "", errors.New("TURN не выдал ICE-кандидата")
		}
		if err = f.session.request(signal, "transfer-relay-offer", "", map[string]any{"transfer": id, "description": offer}, nil); err != nil {
			return nil, "", err
		}
	} else {
		input["description"] = offer
		if err = f.session.request(signal, "transfer-open", f.build, input, nil); err != nil {
			return nil, "", err
		}
		opened = true
	}
	ticker := time.NewTicker(1800 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state struct{ Answer *webrtc.SessionDescription }
		if err := f.session.request(signal, "transfer-read", "", map[string]any{"transfer": id}, &state); err != nil {
			return nil, "", err
		}
		if state.Answer != nil {
			if err := peer.SetAnswer(*state.Answer); err != nil {
				return nil, "", err
			}
			break
		}
		select {
		case <-signal.Done():
			return nil, "", signal.Err()
		case <-ticker.C:
		}
	}
	budget := 32 * time.Second
	if !relay && f.relayAvailable() {
		budget = 8 * time.Second
	}
	connect, cancelConnect := context.WithTimeout(ctx, budget)
	defer cancelConnect()
	if err := peer.Wait(connect); err != nil {
		if !relay {
			err = fmt.Errorf("%w: %w", errDirectTunnel, err)
		}
		return nil, "", err
	}
	ready = true
	return peer, id, nil
}

func (f *sharedBuildFetcher) end(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = f.session.request(ctx, "transfer-close", "", map[string]any{"transfer": id}, nil)
}

func (f *sharedBuildFetcher) close() {
	var pending sync.WaitGroup
	for _, source := range f.sources {
		if source.peer != nil {
			source.peer.Close()
			pending.Add(1)
			go func() {
				defer pending.Done()
				f.end(source.id)
			}()
		}
	}
	pending.Wait()
}
