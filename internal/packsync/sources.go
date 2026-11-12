package packsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Source struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

// Resolvers are supplied by the launcher, never by a remote manifest. A future
// SintedMods adapter can implement this interface without changing the queues.
type Resolver interface {
	Name() string
	Resolve(context.Context, []File) (map[string]Source, error)
}

func validSource(s Source) bool {
	if len(s.URL) > 2048 {
		return false
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch s.Provider {
	case "modrinth":
		return u.Host == "cdn.modrinth.com"
	case "curseforge":
		return u.Host == "edge.forgecdn.net" || u.Host == "mediafilez.forgecdn.net" || u.Host == "media.forgecdn.net"
	case "sintedmods":
		return u.Host == "sintet.xyz" && strings.HasPrefix(u.Path, "/api/catalog/files/")
	}
	return false
}
func resolverHTTP(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copy := *client
	copy.Timeout = 8 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copy
}
func lookupJSON(ctx context.Context, client *http.Client, base, path, key string, input, out any) error {
	raw, _ := json.Marshal(input)
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Sintet/0.8.9.3 (pack sync; sintet.xyz)")
	if key != "" {
		request.Header.Set("x-api-key", key)
	}
	response, err := resolverHTTP(client).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("catalog lookup status %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(out)
}

type ModrinthResolver struct {
	HTTP    *http.Client
	BaseURL string
}

func (ModrinthResolver) Name() string { return "modrinth" }
func (r ModrinthResolver) Resolve(ctx context.Context, files []File) (map[string]Source, error) {
	result := map[string]Source{}
	hashes := []string{}
	wanted := map[string][]File{}
	for _, file := range files {
		if strings.HasPrefix(file.Path, "mods/") && file.SHA1 != "" {
			if _, ok := wanted[file.SHA1]; !ok {
				hashes = append(hashes, file.SHA1)
			}
			wanted[file.SHA1] = append(wanted[file.SHA1], file)
		}
	}
	base := r.BaseURL
	if base == "" {
		base = "https://api.modrinth.com/v2"
	}
	for len(hashes) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n := min(100, len(hashes))
		var response map[string]struct {
			Files []struct {
				URL    string            `json:"url"`
				Size   int64             `json:"size"`
				Hashes map[string]string `json:"hashes"`
			} `json:"files"`
		}
		err := lookupJSON(ctx, r.HTTP, base, "/version_files", "", map[string]any{"hashes": hashes[:n], "algorithm": "sha1"}, &response)
		hashes = hashes[n:]
		if err != nil {
			return result, err
		}
		for hash, version := range response {
			for _, wanted := range wanted[hash] {
				for _, file := range version.Files {
					source := Source{"modrinth", file.URL}
					if file.Hashes["sha1"] == wanted.SHA1 && file.Size == wanted.Size && validSource(source) {
						result[wanted.SHA256] = source
						break
					}
				}
			}
		}
	}
	return result, nil
}

type CurseForgeResolver struct {
	HTTP         *http.Client
	BaseURL, Key string
}

func (CurseForgeResolver) Name() string { return "curseforge" }
func (r CurseForgeResolver) Resolve(ctx context.Context, files []File) (map[string]Source, error) {
	result := map[string]Source{}
	if r.Key == "" {
		return result, nil
	}
	fingerprints := []uint32{}
	wanted := map[uint32][]File{}
	for _, file := range files {
		if strings.HasPrefix(file.Path, "mods/") && file.CurseFingerprint != 0 && file.SHA1 != "" {
			if _, ok := wanted[file.CurseFingerprint]; !ok {
				fingerprints = append(fingerprints, file.CurseFingerprint)
			}
			wanted[file.CurseFingerprint] = append(wanted[file.CurseFingerprint], file)
		}
	}
	base := r.BaseURL
	if base == "" {
		base = "https://api.curseforge.com/v1"
	}
	for len(fingerprints) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n := min(100, len(fingerprints))
		var response struct {
			Data struct {
				Exact []struct {
					File struct {
						Fingerprint uint32 `json:"fileFingerprint"`
						URL         string `json:"downloadUrl"`
						Size        int64  `json:"fileLength"`
						Hashes      []struct {
							Value string `json:"value"`
							Algo  int    `json:"algo"`
						} `json:"hashes"`
					} `json:"file"`
				} `json:"exactMatches"`
			} `json:"data"`
		}
		err := lookupJSON(ctx, r.HTTP, base, "/fingerprints/432", r.Key, map[string]any{"fingerprints": fingerprints[:n]}, &response)
		fingerprints = fingerprints[n:]
		if err != nil {
			return result, err
		}
		for _, match := range response.Data.Exact {
			file := match.File
			source := Source{"curseforge", file.URL}
			if !validSource(source) {
				continue
			}
			sha1 := ""
			for _, hash := range file.Hashes {
				if hash.Algo == 1 {
					sha1 = strings.ToLower(hash.Value)
				}
			}
			for _, wanted := range wanted[file.Fingerprint] {
				if sha1 == wanted.SHA1 && file.Size == wanted.Size {
					result[wanted.SHA256] = source
				}
			}
		}
	}
	return result, nil
}

func ResolveSources(ctx context.Context, files []File, resolvers []Resolver) map[string]Source {
	result := map[string]Source{}
	for _, resolver := range resolvers {
		for _, file := range files {
			if result[file.SHA256].URL != "" {
				continue
			}
			for _, source := range file.Sources {
				if source.Provider == resolver.Name() && validSource(source) {
					result[file.SHA256] = source
					break
				}
			}
		}
		unresolved := []File{}
		for _, file := range files {
			if strings.HasPrefix(file.Path, "mods/") && result[file.SHA256].URL == "" {
				unresolved = append(unresolved, file)
			}
		}
		if len(unresolved) == 0 {
			break
		}
		lookup, cancel := context.WithTimeout(ctx, 12*time.Second)
		matches, _ := resolver.Resolve(lookup, unresolved)
		cancel()
		for _, file := range unresolved {
			source := matches[file.SHA256]
			if source.Provider == resolver.Name() && validSource(source) {
				result[file.SHA256] = source
			}
		}
	}
	// These hints let guests use the host's verified CurseForge lookup without
	// shipping the host's API key. SHA-256 is still checked after every download.
	for _, file := range files {
		if result[file.SHA256].URL != "" {
			continue
		}
		for _, source := range file.Sources {
			if validSource(source) {
				result[file.SHA256] = source
				break
			}
		}
	}
	return result
}
func AddSourceHints(ctx context.Context, m Manifest, resolvers []Resolver) Manifest {
	m.Files = append([]File{}, m.Files...)
	matches := ResolveSources(ctx, m.Files, resolvers)
	for i := range m.Files {
		if source := matches[m.Files[i].SHA256]; source.URL != "" {
			m.Files[i].Sources = []Source{source}
		}
	}
	return m
}

// Retained for callers that only need the original Modrinth lookup.
func ModrinthURLs(ctx context.Context, m Manifest) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	matches, _ := (ModrinthResolver{}).Resolve(ctx, m.Files)
	result := map[string]string{}
	for hash, source := range matches {
		result[hash] = source.URL
	}
	return result
}
