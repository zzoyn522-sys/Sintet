package main

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sintet/internal/launcher"
	"sintet/internal/packsync"
	p2p "sintet/internal/social"
)

func TestGuestReceivesRealP2PManifestButCannotDownloadWithoutConsent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source := t.TempDir()
	os.MkdirAll(filepath.Join(source, "mods"), 0700)
	os.MkdirAll(filepath.Join(source, "config"), 0700)
	os.WriteFile(filepath.Join(source, "mods", "test.jar"), []byte("PK\x03\x04fixture"), 0600)
	os.WriteFile(filepath.Join(source, "config", "test.toml"), []byte("value=true"), 0600)
	snapshot, err := packsync.Scan(ctx, source, packsync.Manifest{Name: "Host fixture", Game: "1.20.1", Loader: "forge", LoaderVersion: "1.20.1-47.4.23"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	host, err := p2p.NewPeer(target.Addr().(*net.TCPAddr).Port, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	host.SetPackHandler(snapshot.Handler())
	peer, err := p2p.NewPeer(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	offer, err := peer.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := host.Answer(ctx, *offer)
	if err != nil {
		t.Fatal(err)
	}
	if err = peer.SetAnswer(*answer); err != nil {
		t.Fatal(err)
	}
	port, err := peer.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	app := &App{profiles: &profileRoster{root: root}, settings: launcher.DefaultSettings(filepath.Join(root, "Minecraft")), service: launcher.New(filepath.Join(root, "Minecraft"))}
	manager := newSocialManager(app, ctx)
	manager.epoch = 1
	manager.guest = "fixture-peer"
	manager.peers[manager.guest] = peer
	manager.state.LocalPort = port
	if err = manager.prepareGuestPack(ctx, peer, 1, socialAccount{ID: "fixture-host", Username: "Host"}, false); err != nil {
		t.Fatal(err)
	}
	state := manager.snapshot()
	if state.Pack == nil || state.Pack.Phase != "required" || state.Pack.Missing != 2 || state.Pack.Digest != snapshot.Manifest.Digest || state.Pack.Restart {
		t.Fatal(state.Pack)
	}
	if app.State().Busy || app.State().Running || app.remotePackBusy.Load() {
		t.Fatal("game or download started before consent")
	}
	if _, err = os.Stat(manager.packCache()); !os.IsNotExist(err) {
		t.Fatal("downloaded files before consent", err)
	}
	if err = manager.startPackSync("forged-digest"); err == nil {
		t.Fatal("accepted stale or forged consent")
	}
	if err = manager.connectPreparedPack(ctx); err == nil {
		t.Fatal("joined with incompatible build")
	}
	if err = packsync.Download(ctx, snapshot.Manifest, manager.packCache(), peer, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Manifest.Files {
		if !packsync.ValidCached(manager.packCache(), file) {
			t.Fatal("P2P file verification failed", file.Path)
		}
	}
	manager.disconnect()
	if manager.snapshot().Pack != nil {
		t.Fatal("left previous host's manifest after leaving")
	}
}
