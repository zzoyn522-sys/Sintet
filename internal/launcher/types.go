package launcher

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type File struct {
	ID     string `json:"id,omitempty"`
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA1   string `json:"sha1"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size"`
}
type Version struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	SHA1        string `json:"sha1"`
	ReleaseTime string `json:"releaseTime"`
	Installed   bool   `json:"installed"`
}
type Manifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []Version `json:"versions"`
}
type Rule struct {
	Action string `json:"action"`
	OS     struct {
		Name    string `json:"name"`
		Arch    string `json:"arch"`
		Version string `json:"version"`
	} `json:"os"`
	Features map[string]bool `json:"features"`
}
type Library struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	Downloads struct {
		Artifact    File            `json:"artifact"`
		Classifiers map[string]File `json:"classifiers"`
	} `json:"downloads"`
	Rules   []Rule            `json:"rules"`
	Natives map[string]string `json:"natives"`
	Extract struct {
		Exclude []string `json:"exclude"`
	} `json:"extract"`
}
type Profile struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	InheritsFrom       string `json:"inheritsFrom"`
	MainClass          string `json:"mainClass"`
	MinecraftArguments string `json:"minecraftArguments"`
	Assets             string `json:"assets"`
	AssetIndex         File   `json:"assetIndex"`
	Downloads          struct {
		Client         File `json:"client"`
		ClientMappings File `json:"client_mappings"`
	} `json:"downloads"`
	Libraries []Library `json:"libraries"`
	Arguments struct {
		Game []json.RawMessage `json:"game"`
		JVM  []json.RawMessage `json:"jvm"`
	} `json:"arguments"`
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Logging struct {
		Client struct {
			Argument string `json:"argument"`
			File     File   `json:"file"`
		} `json:"client"`
	} `json:"logging"`
}
type AssetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
	Virtual        bool `json:"virtual"`
	MapToResources bool `json:"map_to_resources"`
}
type Receipt struct {
	ID            string   `json:"id"`
	Base          string   `json:"base"`
	Loader        string   `json:"loader"`
	LoaderVersion string   `json:"loaderVersion"`
	Java          string   `json:"java"`
	Files         []File   `json:"files"`
	Profile       Profile  `json:"profile"`
	NativeJars    []string `json:"nativeJars"`
	Classpath     []string `json:"classpath"`
	AssetIndexID  string   `json:"assetIndexID"`
	GameDir       string   `json:"gameDir"`
}
type Catalog struct {
	Versions []Version `json:"versions"`
	Latest   string    `json:"latest"`
	Cached   bool      `json:"cached"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_.+\-]+$`)

func validateID(id string) error {
	if !safeID.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("неверный идентификатор версии")
	}
	return nil
}
func join(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) || strings.Contains(relative, ":") {
		return "", fmt.Errorf("недопустимый путь %q", relative)
	}
	p := filepath.Join(root, filepath.FromSlash(relative))
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("путь вне каталога")
	}
	return p, nil
}
func saveJSON(path string, v any) error {
	data, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return save(path, data)
}
func save(path string, data []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, data, 0644); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func newHTTP() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 80, MaxIdleConnsPerHost: 24, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 35 * time.Second}}
}
func request(ctx context.Context, c *http.Client, url string) (*http.Response, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1:") {
		return nil, fmt.Errorf("небезопасный URL")
	}
	r, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("User-Agent", "Sintet/0.7 (Minecraft launcher)")
	resp, e := c.Do(r)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return resp, nil
}
func fetch(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	resp, e := request(ctx, c, url)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
func jsonURL(ctx context.Context, c *http.Client, url string, v any) error {
	data, e := fetch(ctx, c, url)
	if e != nil {
		return e
	}
	return json.Unmarshal(data, v)
}
func validFile(path string, f File, full bool) bool {
	st, e := os.Stat(path)
	if e != nil || st.IsDir() || st.Size() == 0 || (f.Size > 0 && st.Size() != f.Size) {
		return false
	}
	if !full || (f.SHA1 == "" && f.SHA256 == "") {
		return true
	}
	in, e := os.Open(path)
	if e != nil {
		return false
	}
	defer in.Close()
	if f.SHA256 != "" {
		h := sha256.New()
		if _, e = io.Copy(h, in); e != nil {
			return false
		}
		return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), f.SHA256)
	}
	h := sha1.New()
	if _, e = io.Copy(h, in); e != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), f.SHA1)
}
func offlineUUID(name string) string {
	b := md5.Sum([]byte("OfflinePlayer:" + name))
	b[6] = (b[6] & 0x0f) | 0x30
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

// OfflineProfileID returns the UUID used to launch a local Minecraft profile.
func OfflineProfileID(name string) string {
	id := offlineUUID(name)
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}
func allowed(rules []Rule, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	result := false
	for _, r := range rules {
		match := true
		if r.OS.Name != "" && r.OS.Name != "windows" {
			match = false
		}
		if r.OS.Arch != "" {
			arch := "x86_64"
			if runtime.GOARCH == "386" {
				arch = "x86"
			}
			if r.OS.Arch != arch && !(r.OS.Arch == "amd64" && runtime.GOARCH == "amd64") {
				match = false
			}
		}
		if r.OS.Version != "" {
			ok, _ := regexp.MatchString(r.OS.Version, "10.0")
			match = match && ok
		}
		for k, v := range r.Features {
			if features[k] != v {
				match = false
			}
		}
		if match {
			result = r.Action == "allow"
		}
	}
	return result
}
func mavenPath(name string) (string, error) {
	parts := strings.Split(name, "@")
	ext := "jar"
	if len(parts) > 1 {
		ext = parts[1]
	}
	p := strings.Split(parts[0], ":")
	if len(p) < 3 {
		return "", fmt.Errorf("неверная Maven координата %s", name)
	}
	suffix := ""
	if len(p) > 3 {
		suffix = "-" + p[3]
	}
	return strings.ReplaceAll(p[0], ".", "/") + "/" + p[1] + "/" + p[2] + "/" + p[1] + "-" + p[2] + suffix + "." + ext, nil
}
