package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"sintet/internal/identity"
	"sintet/internal/launcher"
	"sintet/internal/packsync"
	"sintet/internal/workshop"
)

func TestSharedBuildDeleteChecksOwnershipAndPreservesFilesOnServerFailure(t *testing.T) {
	const id = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	const profile = "build-sharedtest-1234567890"
	const accountID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	token := strings.Repeat("b", 43)
	build := SharedBuild{ID: id, Name: "Shared Test", OwnerID: accountID, Role: "owner", OriginProfile: profile,
		Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}
	var editor, blocked, deleted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/launcher/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"account": identity.Account{ID: accountID, Username: "PackOwner"}, "token": token, "expires": time.Now().Add(time.Hour)})
		case "/api/shared-builds":
			var request struct{ Action, Build string }
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Build != id {
				http.Error(w, "unexpected request", http.StatusBadRequest)
				return
			}
			switch request.Action {
			case "detail":
				detail := build
				if editor.Load() {
					detail.Role = "editor"
				}
				_ = json.NewEncoder(w).Encode(detail)
			case "delete":
				if blocked.Load() {
					http.Error(w, "deletion refused", http.StatusForbidden)
					return
				}
				deleted.Store(true)
				_, _ = w.Write([]byte("{}"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	client := identity.New(server.URL, filepath.Join(root, "identity.dpapi"))
	address, err := client.Start()
	if err != nil {
		t.Fatal(err)
	}
	authorizeFixture(t, address, token)
	roster := newProfileRoster(root, server.URL)
	slot := roster.find(roster.active)
	if slot == nil || slot.sintet == nil {
		t.Fatal("fixture account was not restored")
	}
	slot.sintet.ServiceBase = server.URL
	gameRoot := filepath.Join(root, "Minecraft")
	app := &App{profiles: roster, settingsPath: filepath.Join(root, "settings.json"),
		service: launcher.New(gameRoot), workshop: workshop.NewManager(workshop.New(""))}
	manager := newSharedBuildManager(app, context.Background())
	app.sharedBuilds = manager
	local := WorkshopBuild{ID: profile, Name: build.Name, Game: build.Game, Loader: build.Loader, LoaderVersion: build.LoaderVersion}
	if err := app.saveWorkshopBuilds(workshopBuildStore{Builds: []WorkshopBuild{local}}); err != nil {
		t.Fatal(err)
	}
	s, err := manager.session()
	if err != nil {
		t.Fatal(err)
	}
	saveSharedSnapshot(filepath.Join(manager.buildRoot(s), "builds.json"), []SharedBuild{build})
	instance := filepath.Join(gameRoot, "instances", profile)
	if err := os.MkdirAll(instance, 0700); err != nil {
		t.Fatal(err)
	}

	editor.Store(true)
	if err := app.DeleteSharedWorkshopBuild(id); err == nil || deleted.Load() {
		t.Fatal("editor was allowed to delete the shared build", err)
	}
	editor.Store(false)
	blocked.Store(true)
	if err := app.DeleteWorkshopBuild(profile); err == nil {
		t.Fatal("server refusal was ignored")
	}
	if _, err := os.Stat(instance); err != nil {
		t.Fatal("local files were removed after server refusal", err)
	}
	blocked.Store(false)
	if err := app.DeleteWorkshopBuild(profile); err != nil || !deleted.Load() {
		t.Fatal("linked deletion did not reach the server", err)
	}
	if _, err := os.Stat(instance); !os.IsNotExist(err) {
		t.Fatal("owner's instance still exists", err)
	}
	if _, exists := app.workshopBuildByID(profile); exists || manager.linkedBuild(s, profile) != "" {
		t.Fatal("deleted build remained in a local index")
	}
}

func sharedTestJar(t *testing.T, filename, modID string) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	jar := zip.NewWriter(file)
	entry, err := jar.Create("fabric.mod.json")
	if err == nil {
		err = json.NewEncoder(entry).Encode(map[string]string{"id": modID, "version": "1.0"})
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := jar.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedBuildRevisionReplacesOnlyManagedMods(t *testing.T) {
	ctx := context.Background()
	dir, cache := t.TempDir(), t.TempDir()
	source := filepath.Join(t.TempDir(), "sample.jar")
	sharedTestJar(t, source, "sample")
	file, key, err := cacheSharedMod(ctx, cache, source)
	if err != nil || key != "mod:sample" || !packsync.ValidCached(cache, file) {
		t.Fatalf("unexpected JAR metadata: %v %s %v", file, key, err)
	}
	for _, folder := range []string{"mods", "saves"} {
		if err := os.Mkdir(filepath.Join(dir, folder), 0700); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(dir, "mods", "old.jar")
	save := filepath.Join(dir, "saves", "world")
	for _, file := range []string{old, save} {
		if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	build := SharedBuild{Name: "Shared Build", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}
	revision, err := sharedManifest(build, []sharedBuildMod{{File: file}})
	if err != nil {
		t.Fatal(err)
	}
	if err = replaceSharedMods(ctx, cache, dir, revision, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(save); err != nil || string(data) != "original" {
		t.Fatal("world changed", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old mod stayed in managed directory", err)
	}
	if err := os.WriteFile(filepath.Join(cache, file.SHA256), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceSharedMods(ctx, cache, dir, revision, false); err == nil {
		t.Fatal("corrupt JAR was accepted")
	}
	if err := validateSharedJar(filepath.Join(dir, filepath.FromSlash(file.Path))); err != nil {
		t.Fatal("previous mod disappeared after failed staging", err)
	}
}

func TestSharedBuildTransfersJarAcrossSeparateWebRTCPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	author := identity.Account{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Username: "JarAuthor"}
	leader := identity.Account{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Username: "PackOwner"}
	build := SharedBuild{ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", OwnerID: leader.ID}
	authorToken, leaderToken := strings.Repeat("a", 43), strings.Repeat("b", 43)
	var mutex sync.Mutex
	var active sharedTransfer
	var answer *webrtc.SessionDescription
	var expected string
	seed, guest := t.TempDir(), t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data struct {
			Code        string                     `json:"code"`
			Action      string                     `json:"action"`
			Build       string                     `json:"build"`
			Seed        string                     `json:"seed"`
			Transfer    string                     `json:"transfer"`
			Hash        string                     `json:"hash"`
			Hashes      []string                   `json:"hashes"`
			Description *webrtc.SessionDescription `json:"description"`
		}
		_ = json.NewDecoder(r.Body).Decode(&data)
		if r.URL.Path == "/api/launcher/token" {
			account := author
			if data.Code == leaderToken {
				account = leader
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"account": account, "token": data.Code, "expires": time.Now().Add(time.Hour)})
			return
		}
		if r.URL.Path != "/api/shared-builds" {
			http.NotFound(w, r)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		isAuthor := r.Header.Get("Authorization") == "Bearer "+authorToken
		isLeader := r.Header.Get("Authorization") == "Bearer "+leaderToken
		var reply any
		switch {
		case data.Action == "transfer-open" && isLeader && data.Build == build.ID && data.Seed == author.ID && data.Description != nil && len(data.Hashes) == 1 && data.Hashes[0] == expected:
			active = sharedTransfer{ID: data.Transfer, Build: data.Build, Hashes: data.Hashes, Offer: *data.Description}
			reply = map[string]any{}
		case data.Action == "incoming" && isAuthor:
			reply = []sharedTransfer{}
			if active.ID != "" && answer == nil {
				reply = []sharedTransfer{active}
			}
		case data.Transfer == active.ID && data.Action == "transfer-answer" && isAuthor:
			answer = data.Description
			reply = map[string]any{}
		case data.Transfer == active.ID && data.Action == "transfer-read" && isLeader:
			reply = map[string]any{"answer": answer}
		case data.Transfer == active.ID && data.Action == "transfer-check" && isAuthor && data.Hash == expected:
			reply = map[string]any{"allowed": true}
		case data.Transfer == active.ID && data.Action == "transfer-close" && isLeader:
			active = sharedTransfer{}
			reply = map[string]any{}
		default:
			http.Error(w, "unexpected transfer operation", 403)
			return
		}
		_ = json.NewEncoder(w).Encode(reply)
	}))
	defer server.Close()
	newAccount := func(root, token string) *profileRoster {
		client := identity.New(server.URL, filepath.Join(root, "identity.dpapi"))
		address, err := client.Start()
		if err != nil {
			t.Fatal(err)
		}
		authorizeFixture(t, address, token)
		roster := newProfileRoster(root, server.URL)
		slot := roster.find(roster.active)
		if slot == nil || slot.sintet == nil {
			t.Fatal("fixture account was not restored")
		}
		slot.sintet.ServiceBase = server.URL
		return roster
	}
	app := &App{profiles: newAccount(seed, authorToken)}
	remote := &App{profiles: newAccount(guest, leaderToken)}
	m := newSharedBuildManager(app, ctx)
	authorSession, err := m.session()
	if err != nil {
		t.Fatal(err)
	}
	leaderSession, err := newSharedBuildManager(remote, ctx).session()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "private.jar")
	sharedTestJar(t, source, "private_mod")
	file, _, err := cacheSharedMod(ctx, m.cache(authorSession, build.ID), source)
	if err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	expected = file.SHA256
	mutex.Unlock()
	go m.pollTransfers()
	cache := filepath.Join(guest, "download")
	fetcher := newSharedBuildFetcher(leaderSession, build, []sharedBuildMod{{File: file, Author: author.ID}}, cache)
	defer fetcher.close()
	res, err := fetcher.PackGet(ctx, "/file/"+file.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("unexpected transfer status: %d", res.StatusCode)
	}
	if err := packsync.CopyVerified(ctx, res.Body, cache, file, nil); err != nil || !packsync.ValidCached(cache, file) {
		t.Fatal("received JAR did not match", err)
	}
}

func TestSharedSintedotAuditsAndAppliesCachedDraft(t *testing.T) {
	const id = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	const account = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	token := strings.Repeat("b", 43)
	build := SharedBuild{ID: id, Name: "Draft", OwnerID: account, Role: "owner", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14", DraftVersion: 9, SintedotAvailable: true}
	var requests atomic.Int32
	var legacy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/launcher/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"account": identity.Account{ID: account, Username: "PackOwner"}, "token": token, "expires": time.Now().Add(time.Hour)})
		case "/api/shared-builds":
			var request struct {
				Action, Build string
				Version       int
				Key           string
				Enabled       bool
				Disable       []string
				Mods          []sharedBuildMod
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Build != id {
				http.Error(w, "unexpected request", 400)
				return
			}
			switch request.Action {
			case "detail":
				detail := build
				detail.SintedotAvailable = !legacy.Load()
				_ = json.NewEncoder(w).Encode(detail)
			case "toggle":
				if request.Key != "legacy:old" || request.Enabled || request.Version != 9 {
					t.Errorf("unexpected legacy patch: %+v", request)
					http.Error(w, "unexpected patch", 400)
					return
				}
				requests.Add(1)
				_, _ = w.Write([]byte("{}"))
			case "sintedot-apply":
				if request.Version != 9 || len(request.Mods) != 0 || len(request.Disable) != 1 || request.Disable[0] != "legacy:old" {
					t.Errorf("unexpected staged patch: %+v", request)
					http.Error(w, "unexpected patch", 400)
					return
				}
				requests.Add(1)
				_, _ = w.Write([]byte("{}"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	client := identity.New(server.URL, filepath.Join(root, "identity.dpapi"))
	address, err := client.Start()
	if err != nil {
		t.Fatal(err)
	}
	authorizeFixture(t, address, token)
	roster := newProfileRoster(root, server.URL)
	slot := roster.find(roster.active)
	if slot == nil || slot.sintet == nil {
		t.Fatal("fixture account missing")
	}
	slot.sintet.ServiceBase = server.URL
	app := &App{profiles: roster, service: launcher.New(filepath.Join(root, "Minecraft")), workshop: workshop.NewManager(workshop.New(""))}
	m := newSharedBuildManager(app, t.Context())
	app.sharedBuilds = m
	s, err := m.session()
	if err != nil {
		t.Fatal(err)
	}

	for i, name := range []string{"old", "new"} {
		path := filepath.Join(root, name+".jar")
		output, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(output)
		f, err := writer.Create("fabric.mod.json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.NewEncoder(f).Encode(map[string]any{"id": "library", "version": string(rune('1' + i))}); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err = output.Close(); err != nil {
			t.Fatal(err)
		}
		file, _, err := cacheSharedMod(t.Context(), m.cache(s, id), path)
		if err != nil {
			t.Fatal(err)
		}
		build.Mods = append(build.Mods, sharedBuildMod{Key: "legacy:" + name, File: file, Enabled: true, Approved: true, Author: account})
	}
	target := "shared:" + id
	if err = app.RunSintedotWithOptions(target, "repair", workshop.SintedotOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 || !app.SintedotState().Preview || len(app.SintedotState().Changes) != 1 {
		t.Fatalf("preview: %+v", app.SintedotState())
	}
	if err = app.RunSintedotWithOptions(target, "repair", workshop.SintedotOptions{}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || app.SintedotState().Preview {
		t.Fatalf("apply: %+v", app.SintedotState())
	}

	legacy.Store(true)
	report := app.RunSintedotReport(target, "repair", workshop.SintedotOptions{})
	if requests.Load() != 2 || report.Preview || report.Stage != "Исправления применены" || report.Error != "" {
		t.Fatalf("legacy repair failed: %+v", report)
	}
}
