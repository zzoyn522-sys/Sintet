package packsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type packFetchFunc func(context.Context, string) (*http.Response, error)

func (f packFetchFunc) PackGet(ctx context.Context, path string) (*http.Response, error) {
	return f(ctx, path)
}

func TestPeerDownloadRetriesTruncatedFileAndReusesCache(t *testing.T) {
	payload := []byte("complete pack file")
	digest := sha256.Sum256(payload)
	file := File{Path: "mods/fixture.jar", Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:])}
	manifest := Manifest{Format: 2, Name: "Resume", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14", Files: []File{file}}
	manifest.Digest = fingerprint(manifest)
	requests := 0
	peer := packFetchFunc(func(_ context.Context, path string) (*http.Response, error) {
		if path != "/file/"+file.SHA256 {
			t.Errorf("unexpected peer path %s", path)
		}
		requests++
		data := payload
		if requests == 1 {
			data = payload[:4]
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cache := t.TempDir()
	options := DownloadOptions{Resolvers: []Resolver{}}
	var progress int64
	if err := DownloadWithOptions(ctx, manifest, cache, peer, nil, func(_ string, done, total int64) {
		progress = done
		if done > total || total != file.Size {
			t.Errorf("invalid progress %d/%d", done, total)
		}
	}, options); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || progress != file.Size || !ValidCached(cache, file) {
		t.Fatalf("retry did not commit a verified file, requests=%d progress=%d", requests, progress)
	}
	if err := DownloadWithOptions(ctx, manifest, cache, nil, nil, nil, options); err != nil {
		t.Fatal("verified file was downloaded again", err)
	}
}

type poolResolver struct{ sources map[string]Source }

func (poolResolver) Name() string { return "modrinth" }
func (r poolResolver) Resolve(context.Context, []File) (map[string]Source, error) {
	return r.sources, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type poolPeer struct {
	data        map[string][]byte
	active, max atomic.Int32
	started     chan struct{}
	release     <-chan struct{}
}

func (p *poolPeer) PackGet(ctx context.Context, path string) (*http.Response, error) {
	n := p.active.Add(1)
	for {
		old := p.max.Load()
		if n <= old || p.max.CompareAndSwap(old, n) {
			break
		}
	}
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		p.active.Add(-1)
		return nil, ctx.Err()
	case <-p.release:
	}
	p.active.Add(-1)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(p.data[path]))}, nil
}
func TestIndependentCDNAndP2PWorkerLimitsAndFallback(t *testing.T) {
	root := t.TempDir()
	sources := map[string]Source{}
	data := map[string][]byte{}
	var files []File
	for i := 0; i < 13; i++ {
		path := fmt.Sprintf("mods/test-%02d.jar", i)
		payload := []byte(fmt.Sprintf("jar-fixture-%02d", i))
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		files = append(files, File{Path: path, Size: int64(len(payload)), SHA256: hash})
		data["/file/"+hash] = payload
		if i < 9 {
			sources[hash] = Source{"modrinth", "https://cdn.modrinth.com/" + hash}
		}
	}
	manifest := Manifest{Format: 2, Name: "Pools", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14", Files: files}
	manifest.Digest = fingerprint(manifest)
	release := make(chan struct{})
	started := make(chan struct{}, 16)
	peer := &poolPeer{data: data, started: started, release: release}
	var cdnActive, cdnMax atomic.Int32
	cdnStarted := make(chan struct{}, 16)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credential leaked into download")
		}
		n := cdnActive.Add(1)
		for {
			old := cdnMax.Load()
			if n <= old || cdnMax.CompareAndSwap(old, n) {
				break
			}
		}
		cdnStarted <- struct{}{}
		select {
		case <-r.Context().Done():
			cdnActive.Add(-1)
			return nil, r.Context().Err()
		case <-release:
		}
		cdnActive.Add(-1)
		hash := r.URL.Path[1:]
		payload := data["/file/"+hash]
		if hash == files[0].SHA256 {
			payload = []byte("forged CDN file")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload))}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- DownloadWithOptions(ctx, manifest, root, peer, nil, nil, DownloadOptions{HTTP: client, Resolvers: []Resolver{poolResolver{sources}}})
	}()
	for i := 0; i < 6; i++ {
		select {
		case <-cdnStarted:
		case <-ctx.Done():
			t.Fatal("CDN queue stalled", ctx.Err())
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("P2P queue was blocked behind CDN", ctx.Err())
		}
	}
	if cdnMax.Load() != 6 || peer.max.Load() != 2 {
		t.Fatal("worker limits not applied", cdnMax.Load(), peer.max.Load())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if cdnMax.Load() > 6 || peer.max.Load() > 2 {
		t.Fatal("pool exceeded configured limit")
	}
	for _, file := range files {
		if !ValidCached(root, file) {
			t.Fatal("missing or forged result", file.Path)
		}
	}
}

func TestConfigAndDefaultConfigContentsAffectDigestButSourceHintsDoNot(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "config"), 0700)
	os.MkdirAll(filepath.Join(root, "defaultconfigs"), 0700)
	file := filepath.Join(root, "config", "settings.toml")
	os.WriteFile(file, []byte("range=16"), 0600)
	os.WriteFile(filepath.Join(root, "defaultconfigs", "server.toml"), []byte("difficulty=2"), 0600)
	meta := Manifest{Name: "Configs", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}
	first, err := Scan(context.Background(), root, meta, t.TempDir())
	if err != nil || len(first.Manifest.Files) != 2 {
		t.Fatal(first.Manifest, err)
	}
	os.WriteFile(file, []byte("range=32"), 0600)
	second, err := Scan(context.Background(), root, meta, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Manifest.Digest == second.Manifest.Digest {
		t.Fatal("changed mod setting did not trigger resync")
	}
	hints := first.Manifest
	hints.Files = append([]File{}, hints.Files...)
	hints.Files[0].CurseFingerprint = 123
	if fingerprint(hints) != first.Manifest.Digest {
		t.Fatal("catalog metadata changed content identity")
	}
	hints.Files[0].Sources = []Source{{"modrinth", "https://cdn.modrinth.com/test"}}
	if Validate(hints) == nil {
		t.Fatal("accepted CDN source for a private config")
	}
}

func TestHostSnapshotLimitsFileStreamsAcrossAllPeerHandlers(t *testing.T) {
	snapshot, _ := fixture(t)
	snapshot.slots = make(chan struct{}, 2)
	// Both handlers close over the same lobby-wide semaphore, rather than each
	// peer getting its own two-stream quota.
	handlerA, handlerB := snapshot.Handler(), snapshot.Handler()
	file := snapshot.Manifest.Files[0]
	blocker := &blockingWriter{entered: make(chan struct{}, 2), release: make(chan struct{})}
	var wg sync.WaitGroup
	for _, h := range []http.Handler{handlerA, handlerB} {
		wg.Add(1)
		go func(h http.Handler) {
			defer wg.Done()
			r, _ := http.NewRequest("GET", "http://sintet-pack/file/"+file.SHA256, nil)
			h.ServeHTTP(blocker, r)
		}(h)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-blocker.entered:
		case <-time.After(time.Second):
			t.Fatal("file stream not started")
		}
	}
	output := &statusWriter{}
	request, _ := http.NewRequest("GET", "http://sintet-pack/file/"+file.SHA256, nil)
	snapshot.Handler().ServeHTTP(output, request)
	if output.status != 429 {
		t.Fatal("host exceeded shared P2P limit", output.status)
	}
	close(blocker.release)
	wg.Wait()
}

type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *blockingWriter) Header() http.Header { return http.Header{} }
func (w *blockingWriter) WriteHeader(int)     {}
func (w *blockingWriter) Write(p []byte) (int, error) {
	w.entered <- struct{}{}
	<-w.release
	return len(p), nil
}

type statusWriter struct{ status int }

func (w *statusWriter) Header() http.Header         { return http.Header{} }
func (w *statusWriter) WriteHeader(status int)      { w.status = status }
func (w *statusWriter) Write(p []byte) (int, error) { return len(p), nil }
