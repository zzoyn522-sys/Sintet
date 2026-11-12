package packsync

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sintet/internal/identity"
)

func TestCurseForgeRequiresExactSHA1AndDoesNotNeedGuestKeyForHostHint(t *testing.T) {
	payload := []byte("fixture jar")
	a, b := sha256.Sum256(payload), sha1.Sum(payload)
	file := File{Path: "mods/fixture.jar", Size: int64(len(payload)), SHA256: hex.EncodeToString(a[:]), SHA1: hex.EncodeToString(b[:]), CurseFingerprint: 13579}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fingerprints/432" || r.Header.Get("x-api-key") != "test-key" {
			t.Error("incorrect fingerprint API request")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"exactMatches": []any{map[string]any{"file": map[string]any{"fileFingerprint": 13579, "downloadUrl": "https://edge.forgecdn.net/files/1/2/fixture.jar", "fileLength": file.Size, "hashes": []any{map[string]any{"algo": 1, "value": file.SHA1}}}}}}})
	}))
	defer server.Close()
	resolver := CurseForgeResolver{Key: "test-key", BaseURL: server.URL}
	matches, err := resolver.Resolve(context.Background(), []File{file})
	if err != nil || matches[file.SHA256].Provider != "curseforge" {
		t.Fatal(matches, err)
	}
	wrong := file
	wrong.SHA1 = strings.Repeat("0", 40)
	matches, err = resolver.Resolve(context.Background(), []File{wrong})
	if err != nil || len(matches) != 0 {
		t.Fatal("accepted fingerprint collision without matching SHA1", matches, err)
	}
	file.Sources = []Source{{"curseforge", "https://edge.forgecdn.net/files/1/2/fixture.jar"}}
	matched := ResolveSources(context.Background(), []File{file}, []Resolver{CurseForgeResolver{}})
	if matched[file.SHA256].Provider != "curseforge" {
		t.Fatal("guest could not use verified host hint without an API key")
	}
	for _, source := range []Source{{"curseforge", "https://evil.example/test.jar"}, {"curseforge", "https://edge.forgecdn.net:444/test.jar"}, {"modrinth", "http://127.0.0.1/test.jar"}, {"sintedmods", "https://sintet.xyz/api/account/settings"}} {
		if validSource(source) {
			t.Fatal("untrusted source accepted", source)
		}
	}
}

func TestCurseFingerprintIgnoresWhitespaceAndChunkBoundaries(t *testing.T) {
	clean := []byte("abcdef123456789")
	spaced := []byte(" a\tb\nc\rd e f1 2\t3\n4\r5 6 7 8 9 ")
	counter := &curseByteCounter{}
	counter.Write(spaced)
	if counter.n != uint32(len(clean)) {
		t.Fatal(counter.n)
	}
	full := curseHasher{value: 1 ^ uint32(len(clean))}
	full.Write(clean)
	chunked := curseHasher{value: 1 ^ counter.n}
	for _, b := range spaced {
		chunked.Write([]byte{b})
	}
	if chunked.sum() != full.sum() {
		t.Fatal("fingerprint depends on whitespace or read boundaries")
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "mods"), 0700)
	os.WriteFile(filepath.Join(root, "mods", "fixture.jar"), spaced, 0600)
	snapshot, err := Scan(context.Background(), root, Manifest{Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}, t.TempDir())
	if err != nil || snapshot.Manifest.Files[0].CurseFingerprint != full.sum() {
		t.Fatal(snapshot.Manifest, err)
	}
	local, err := Scan(context.Background(), root, Manifest{Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}, "")
	if err != nil || local.Manifest.Digest != snapshot.Manifest.Digest {
		t.Fatal("lookup fingerprints made identical local content incompatible", err)
	}
}

func TestModrinthIsPreferredOverHostCurseForgeHint(t *testing.T) {
	file := File{Path: "mods/shared.jar", SHA256: strings.Repeat("a", 64), Sources: []Source{{"curseforge", "https://edge.forgecdn.net/files/test.jar"}}}
	expected := Source{"modrinth", "https://cdn.modrinth.com/data/test.jar"}
	result := ResolveSources(context.Background(), []File{file}, []Resolver{poolResolver{sources: map[string]Source{file.SHA256: expected}}, CurseForgeResolver{}})
	if result[file.SHA256] != expected {
		t.Fatal("did not prefer Modrinth", result)
	}
}

func TestLiveCurseForgeFingerprintAndCDNLookup(t *testing.T) {
	if os.Getenv("SINTET_CURSE_SYNC_LIVE") != "1" {
		t.Skip("live catalog lookup is opt-in")
	}
	key, err := identity.ReadServiceSecret(filepath.Join(os.Getenv("USERPROFILE"), "Documents", "Sintet", "curseforge.dpapi"))
	if err != nil || key == "" {
		t.Fatal("configured CurseForge key unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://api.curseforge.com/v1/mods/238222/files?gameVersion=1.20.1&pageSize=1", nil)
	request.Header.Set("x-api-key", key)
	client := resolverHTTP(nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Data []struct {
			URL         string `json:"downloadUrl"`
			Size        int64  `json:"fileLength"`
			Fingerprint uint32 `json:"fileFingerprint"`
			Hashes      []struct {
				Value string `json:"value"`
				Algo  int    `json:"algo"`
			} `json:"hashes"`
		} `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&listing)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || len(listing.Data) == 0 {
		t.Fatal("CurseForge metadata unavailable", response.StatusCode, err)
	}
	listed := listing.Data[0]
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 4 || !validSource(Source{"curseforge", next.URL.String()}) {
			return fmt.Errorf("untrusted CDN redirect")
		}
		return nil
	}
	if !validSource(Source{"curseforge", listed.URL}) {
		t.Fatal("CurseForge did not provide a usable CDN URL")
	}
	request, _ = http.NewRequestWithContext(ctx, "GET", listed.URL, nil)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxMod+1))
	response.Body.Close()
	if err != nil || int64(len(payload)) != listed.Size {
		t.Fatal("CDN file length mismatch", err)
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "mods"), 0700)
	os.WriteFile(filepath.Join(root, "mods", "live.jar"), payload, 0600)
	snapshot, err := Scan(ctx, root, Manifest{Game: "1.20.1", Loader: "forge", LoaderVersion: "1.20.1-47.4.23"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := snapshot.Manifest.Files[0]
	if file.CurseFingerprint != listed.Fingerprint {
		t.Fatal("fingerprint differs from official CurseForge record", file.CurseFingerprint, listed.Fingerprint)
	}
	matches, err := (CurseForgeResolver{Key: key}).Resolve(ctx, []File{file})
	if err != nil || matches[file.SHA256].URL == "" {
		t.Fatal("actual fingerprint lookup failed", err)
	}
	request, _ = http.NewRequestWithContext(ctx, "GET", matches[file.SHA256].URL, nil)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	err = CopyVerified(ctx, response.Body, cache, file, nil)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(cache, file.SHA256))
	if err != nil || !bytes.Equal(saved, payload) {
		t.Fatal("verified CDN bytes differ")
	}
	t.Log("Official fingerprint matched; CurseForge CDN download passed SHA-256 verification")
}
