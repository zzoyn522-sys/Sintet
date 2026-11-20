package launcher

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type installerTestTransport func(*http.Request) (*http.Response, error)

func (f installerTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestForgeInstallerUsesMavenVersion(t *testing.T) {
	for _, tt := range []struct {
		name, base, loader, version, wantURL string
	}{
		{"short Forge version", "1.20.1", "forge", "47.4.23", "https://maven.minecraftforge.net/net/minecraftforge/forge/1.20.1-47.4.23/forge-1.20.1-47.4.23-installer.jar.sha1"},
		{"full Forge version", "1.20.1", "forge", "1.20.1-47.4.23", "https://maven.minecraftforge.net/net/minecraftforge/forge/1.20.1-47.4.23/forge-1.20.1-47.4.23-installer.jar.sha1"},
		{"NeoForge version", "1.21.1", "neoforge", "21.1.215", "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.215/neoforge-21.1.215-installer.jar.sha1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := New(t.TempDir())
			var requested string
			service.HTTP.Transport = installerTestTransport(func(req *http.Request) (*http.Response, error) {
				requested = req.URL.String()
				return nil, fmt.Errorf("stop after checksum request")
			})
			if _, err := service.installForge(context.Background(), tt.base, tt.loader, tt.version, ""); err == nil {
				t.Fatal("expected checksum request to fail in the test")
			}
			if requested != tt.wantURL {
				t.Fatalf("checksum URL = %q, want %q", requested, tt.wantURL)
			}
		})
	}
}

func TestLauncherDownloadRedirectKeepsHTTPS(t *testing.T) {
	for _, target := range []string{"https://cdn.example.test/installer.jar", "http://cdn.example.test/installer.jar"} {
		t.Run(target, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: installerTestTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("fixture"))}
				if requests == 1 {
					response.StatusCode = http.StatusFound
					response.Header = http.Header{"Location": {target}}
				}
				return response, nil
			})}
			data, err := fetch(context.Background(), client, "https://example.test/installer.jar")
			if strings.HasPrefix(target, "https:") {
				if err != nil || string(data) != "fixture" || requests != 2 {
					t.Fatalf("HTTPS redirect failed: requests=%d error=%v", requests, err)
				}
			} else if err == nil || requests != 1 {
				t.Fatalf("HTTP redirect was followed: requests=%d error=%v", requests, err)
			}
		})
	}
}

func TestInstallPlanKeepsNativeOnlyLibrariesOutOfClasspath(t *testing.T) {
	var profile Profile
	err := json.Unmarshal([]byte(`{"libraries":[
		{"name":"net.java.jinput:jinput-platform:2.0.5","natives":{"windows":"natives-windows"},
		 "downloads":{"classifiers":{"natives-windows":{"path":"net/java/jinput/jinput-platform/2.0.5/jinput-platform-2.0.5-natives-windows.jar","url":"https://example.test/windows.jar"}}}},
		{"name":"example:both:1","natives":{"windows":"natives-windows"},
		 "downloads":{"artifact":{"path":"example/both/1/both-1.jar","url":"https://example.test/both.jar"},
		 "classifiers":{"natives-windows":{"path":"example/both/1/both-1-natives-windows.jar","url":"https://example.test/both-windows.jar"}}}},
		{"name":"example:loader:1","url":"https://example.test/maven/"}
	]}`), &profile)
	if err != nil {
		t.Fatal(err)
	}
	service := New(t.TempDir())
	receipt, err := service.plan(context.Background(), profile, "rd-132211")
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Files) != 5 || len(receipt.NativeJars) != 2 || len(receipt.Classpath) != 3 {
		t.Fatalf("unexpected installation plan: files=%v natives=%v classpath=%v", receipt.Files, receipt.NativeJars, receipt.Classpath)
	}
	if !strings.HasSuffix(receipt.NativeJars[0], "/jinput-platform-2.0.5-natives-windows.jar") {
		t.Fatalf("Windows JInput is missing: %v", receipt.NativeJars)
	}
	if filepath.Base(receipt.Classpath[0]) != "both-1.jar" || receipt.Files[4].URL != "https://example.test/maven/example/loader/1/loader-1.jar" {
		t.Fatalf("ordinary artifacts were lost: %v, %v", receipt.Classpath, receipt.Files)
	}
}

func TestFailedLaunchLogCanBeReadAfterRestart(t *testing.T) {
	root := t.TempDir()
	service := New(root)
	ctx, cancel := context.WithCancel(context.Background())
	service.launch(ctx, cancel, Receipt{
		ID:    "build-test",
		Files: []File{{Path: "missing.jar"}},
	}, "Player", LaunchOptions{})

	state := service.State()
	if state.Error == "" || state.Log == "" || state.Busy || state.Running {
		t.Fatalf("unexpected failure state: %+v", state)
	}
	current := service.Logs()
	if !strings.Contains(current, "missing.jar") {
		t.Fatalf("launch failure missing from the journal: %q", current)
	}
	if reopened := New(root).Logs(); reopened != current {
		t.Fatalf("journal lost after restart: %q", reopened)
	}
}

func TestExtractionRejectsOversizedEntryBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "oversized.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	if _, err := w.CreateRaw(&zip.FileHeader{Name: "huge.dll", Method: zip.Store, UncompressedSize64: 1<<30 + 1}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "natives")
	if err := extractZip(archive, dest, nil); err == nil || !strings.Contains(err.Error(), "размер распакованного") {
		t.Fatalf("expected size limit, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "huge.dll")); !os.IsNotExist(err) {
		t.Fatalf("oversized entry was written: %v", err)
	}
}

func TestDownloaderIntegrityAndRecovery(t *testing.T) {
	data := []byte("verified game file")
	hash := sha1.Sum(data)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write(data) }))
	defer server.Close()
	s := New(t.TempDir())
	f := File{Path: "assets/test.bin", URL: server.URL, SHA1: hex.EncodeToString(hash[:]), Size: int64(len(data))}
	if e := s.download(context.Background(), f, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.download(context.Background(), f, nil); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("cache not reused")
	}
	os.WriteFile(filepath.Join(s.Root, "assets/test.bin"), []byte(strings.Repeat("x", len(data))), 0644)
	if e := s.download(context.Background(), f, nil); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 2 {
		t.Fatal("corrupt file not replaced")
	}
	f.SHA1 = strings.Repeat("0", 40)
	f.Path = "bad.bin"
	if s.download(context.Background(), f, nil) == nil {
		t.Fatal("bad hash accepted")
	}
	if _, e := os.Stat(filepath.Join(s.Root, f.Path)); !os.IsNotExist(e) {
		t.Fatal("unverified file published")
	}
}
func TestPathTraversal(t *testing.T) {
	for _, p := range []string{"../escape", "C:/escape", "/absolute", "a/../../outside"} {
		if _, e := join(t.TempDir(), p); e == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.zip")
	out, _ := os.Create(archive)
	z := zip.NewWriter(out)
	f, _ := z.Create("../escape")
	f.Write([]byte("bad"))
	z.Close()
	out.Close()
	if extractZip(archive, filepath.Join(dir, "dest"), nil) == nil {
		t.Fatal("zip slip accepted")
	}
}
func TestRulesArgumentsAndUUID(t *testing.T) {
	var rules []Rule
	json.Unmarshal([]byte(`[{"action":"allow","os":{"name":"windows"}},{"action":"disallow","os":{"arch":"x86"}}]`), &rules)
	if !allowed(rules, nil) {
		t.Fatal("windows x64 excluded")
	}
	json.Unmarshal([]byte(`[{"action":"allow","features":{"is_demo_user":true}}]`), &rules)
	if allowed(rules, nil) {
		t.Fatal("demo enabled")
	}
	if offlineUUID("Notch") != "b50ad385829d3141a2167e7d7539ba7f" {
		t.Fatal(offlineUUID("Notch"))
	}
	args, e := resolveArguments([]json.RawMessage{json.RawMessage(`"${game_directory}"`), json.RawMessage(`{"rules":[{"action":"allow","features":{"is_demo_user":true}}],"value":"--demo"}`)}, map[string]string{"game_directory": "C:/Game With Spaces"})
	if e != nil || len(args) != 1 || args[0] != "C:/Game With Spaces" {
		t.Fatalf("%v %v", args, e)
	}
	_, e = resolveArguments(rawStrings("${missing}"), nil)
	if e == nil {
		t.Fatal("unknown placeholder accepted")
	}
}
func TestCancellationAndIncompleteReceipt(t *testing.T) {
	s := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.downloadAll(ctx, []File{{Path: "x", URL: "https://invalid.invalid"}}) == nil {
		t.Fatal("cancel ignored")
	}
	saveJSON(s.receiptPath("test"), Receipt{Java: "missing", Files: []File{{Path: "missing"}}})
	if s.Installed("test") {
		t.Fatal("incomplete install reported ready")
	}
}
func TestMergeLoaderLibraries(t *testing.T) {
	base := Profile{ID: "base", Libraries: []Library{{Name: "org.test:lib:1"}, {Name: "org.test:native:1:natives-windows"}}}
	child := Profile{ID: "loader", MainClass: "loader.Main", Libraries: []Library{{Name: "org.test:lib:2"}}}
	p := mergeProfiles(base, child)
	if len(p.Libraries) != 2 || p.Libraries[0].Name != "org.test:lib:2" || p.MainClass != "loader.Main" {
		t.Fatal(p)
	}
}
