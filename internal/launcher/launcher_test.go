package launcher

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

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
