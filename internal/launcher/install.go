package launcher

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func extractZip(archive, dest string, filter func(string) bool) error {
	r, e := zip.OpenReader(archive)
	if e != nil {
		return e
	}
	defer r.Close()
	const maxEntries = 100000
	const maxExpanded = 1 << 30
	if len(r.File) > maxEntries {
		return fmt.Errorf("слишком много файлов в ZIP-архиве")
	}
	if e := os.MkdirAll(dest, 0755); e != nil {
		return e
	}
	var diskRequired uint64
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() || (filter != nil && !filter(entry.Name)) {
			continue
		}
		if entry.UncompressedSize64 > maxExpanded-diskRequired {
			return fmt.Errorf("превышен допустимый размер распакованного ZIP-архива")
		}
		diskRequired += entry.UncompressedSize64
	}
	if err := (&Service{}).checkDiskSpace(dest, int64(diskRequired)); err != nil {
		return err
	}
	root, e := os.OpenRoot(dest)
	if e != nil {
		return e
	}
	defer root.Close()
	var expanded uint64
	for _, entry := range r.File {
		if filter != nil && !filter(entry.Name) {
			continue
		}
		path := filepath.Clean(filepath.FromSlash(entry.Name))
		if !filepath.IsLocal(path) || path == "." || strings.Contains(entry.Name, ":") {
			return fmt.Errorf("недопустимый путь в ZIP-архиве: %q", entry.Name)
		}
		if !entry.Mode().IsRegular() && !entry.Mode().IsDir() {
			return fmt.Errorf("недопустимый тип файла в ZIP-архиве: %s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if e = mkdirArchivePath(root, path); e != nil {
				return e
			}
			continue
		}
		if entry.UncompressedSize64 > maxExpanded-expanded {
			return fmt.Errorf("превышен допустимый размер распакованного ZIP-архива")
		}
		expanded += entry.UncompressedSize64
		if e = mkdirArchivePath(root, filepath.Dir(path)); e != nil {
			return e
		}
		e = func() error {
			in, e := entry.Open()
			if e != nil {
				return e
			}
			defer in.Close()
			out, e := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
			if e != nil {
				return e
			}
			n, e := io.Copy(out, io.LimitReader(in, int64(entry.UncompressedSize64)+1))
			ce := out.Close()
			if e != nil {
				return e
			}
			if uint64(n) != entry.UncompressedSize64 {
				return fmt.Errorf("неверный размер файла в ZIP-архиве: %s", entry.Name)
			}
			return ce
		}()
		if e != nil {
			return e
		}
	}
	return nil
}

func mkdirArchivePath(root *os.Root, path string) error {
	current := "."
	for _, part := range strings.Split(path, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if err := root.Mkdir(current, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return nil
}
func (s *Service) ensureJava(ctx context.Context, major int) (string, error) {
	if major == 0 {
		major = 8
	}
	dir := filepath.Join(s.Root, "runtime", "java-"+strconv.Itoa(major))
	find := func(dir string) string {
		var found string
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
			if e == nil && !d.IsDir() && d.Name() == "java.exe" && filepath.Base(filepath.Dir(p)) == "bin" {
				found = p
				return filepath.SkipAll
			}
			return nil
		})
		return found
	}
	if java := find(dir); java != "" {
		return java, nil
	}
	s.stage(fmt.Sprintf("Загрузка Java %d", major))
	var releases []struct {
		Binary struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
				Size     int64  `json:"size"`
			} `json:"package"`
		} `json:"binary"`
	}
	url := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?architecture=x64&image_type=jre&os=windows&vendor=eclipse", major)
	if e := jsonURL(ctx, s.HTTP, url, &releases); e != nil {
		return "", e
	}
	if len(releases) == 0 {
		fallback := strings.Replace(url, "image_type=jre", "image_type=jdk", 1)
		if e := jsonURL(ctx, s.HTTP, fallback, &releases); e != nil {
			return "", e
		}
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("не найден Windows x64 runtime Java %d", major)
	}
	pkg := releases[0].Binary.Package
	file := File{Path: fmt.Sprintf("runtime/java-%d.zip", major), URL: pkg.Link, SHA256: pkg.Checksum, Size: pkg.Size}
	s.setProgress(0, pkg.Size, 0, 1)
	if e := s.download(ctx, file, func(n int64) { s.setProgress(n, pkg.Size, 1, 1) }); e != nil {
		return "", e
	}
	s.stage("Распаковка Java")
	if e := ctx.Err(); e != nil {
		return "", e
	}
	tmp, e := os.MkdirTemp(filepath.Join(s.Root, "runtime"), "unpack-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(tmp)
	archive, _ := join(s.Root, file.Path)
	if e = extractZip(archive, tmp, nil); e != nil {
		return "", e
	}
	java := find(tmp)
	if java == "" {
		return "", fmt.Errorf("java.exe отсутствует в архиве")
	}
	cmd := exec.CommandContext(ctx, java, "-version")
	hideProcess(cmd)
	if data, e := cmd.CombinedOutput(); e != nil {
		return "", fmt.Errorf("Java не запускается: %s: %w", data, e)
	}
	if e = os.Rename(tmp, dir); e != nil {
		return "", e
	}
	return find(dir), nil
}
func mergeProfiles(base, child Profile) Profile {
	out := base
	out.ID = child.ID
	out.InheritsFrom = base.ID
	if child.MainClass != "" {
		out.MainClass = child.MainClass
	}
	if child.Type != "" {
		out.Type = child.Type
	}
	if child.MinecraftArguments != "" {
		out.MinecraftArguments = child.MinecraftArguments
	}
	out.Arguments.JVM = append(append([]json.RawMessage{}, base.Arguments.JVM...), child.Arguments.JVM...)
	out.Arguments.Game = append(append([]json.RawMessage{}, base.Arguments.Game...), child.Arguments.Game...)
	libs := append(append([]Library{}, base.Libraries...), child.Libraries...)
	out.Libraries = nil
	positions := map[string]int{}
	for _, lib := range libs {
		parts := strings.Split(lib.Name, ":")
		key := lib.Name
		if len(parts) >= 3 {
			key = parts[0] + ":" + parts[1]
			if len(parts) > 3 {
				key += ":" + parts[3]
			}
		}
		if index, ok := positions[key]; ok {
			out.Libraries[index] = lib
		} else {
			positions[key] = len(out.Libraries)
			out.Libraries = append(out.Libraries, lib)
		}
	}
	return out
}
func (s *Service) install(ctx context.Context, id, base, kind, loaderVersion, javaPath string) error {
	p, e := s.baseProfile(ctx, base)
	if e != nil {
		return e
	}
	if p.MainClass == "" || p.Downloads.Client.URL == "" {
		return fmt.Errorf("у версии %s нет поддерживаемого клиента", base)
	}
	s.stage("Подготовка библиотек и ресурсов")
	receipt, e := s.plan(ctx, p, base)
	if e != nil {
		return e
	}
	if e = s.downloadAll(ctx, receipt.Files); e != nil {
		return e
	}
	java := javaPath
	if javaPath != "" {
		e = checkJavaForProfile(ctx, javaPath, p.JavaVersion.MajorVersion)
	} else {
		java, e = s.ensureJava(ctx, p.JavaVersion.MajorVersion)
	}
	if e != nil {
		return e
	}
	receipt.Java = java
	if kind != "vanilla" {
		s.stage("Установка " + kind + " " + loaderVersion)
		var child Profile
		if kind == "fabric" {
			url := "https://meta.fabricmc.net/v2/versions/loader/" + base + "/" + loaderVersion + "/profile/json"
			if e = jsonURL(ctx, s.HTTP, url, &child); e != nil {
				return e
			}
			if e = saveJSON(filepath.Join(s.Root, "versions", child.ID, child.ID+".json"), child); e != nil {
				return e
			}
		} else {
			child, e = s.installForge(ctx, kind, loaderVersion, java)
			if e != nil {
				return e
			}
			if child.InheritsFrom != base {
				return fmt.Errorf("загрузчик предназначен для %s вместо %s", child.InheritsFrom, base)
			}
		}
		p = mergeProfiles(p, child)
		extra, e := s.plan(ctx, p, base)
		if e != nil {
			return e
		}
		receipt = extra
		receipt.Java = java
		s.stage("Загрузка библиотек " + kind)
		if e = s.downloadAll(ctx, receipt.Files); e != nil {
			return e
		}
	}
	receipt.ID = id
	receipt.Base = base
	receipt.Loader = kind
	receipt.LoaderVersion = loaderVersion
	receipt.GameDir = filepath.Join(s.Root, "instances", id)
	if e = os.MkdirAll(receipt.GameDir, 0755); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Join(receipt.GameDir, "mods"), 0755); e != nil {
		return e
	}
	s.stage("Подготовка нативных библиотек и ресурсов")
	if e = s.prepare(ctx, &receipt); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	receipt.Profile.ID = id
	receipt.Profile.InheritsFrom = base
	if e = saveJSON(filepath.Join(s.Root, ".sintet", "profiles", id+".json"), receipt.Profile); e != nil {
		return e
	}
	return saveJSON(s.receiptPath(id), receipt)
}
func (s *Service) plan(ctx context.Context, p Profile, base string) (Receipt, error) {
	r := Receipt{Profile: p, Base: base}
	seen := map[string]bool{}
	add := func(f File) {
		if !seen[f.Path] {
			seen[f.Path] = true
			r.Files = append(r.Files, f)
		}
	}
	client := p.Downloads.Client
	client.Path = "versions/" + base + "/" + base + ".jar"
	add(client)
	for _, lib := range p.Libraries {
		if !allowed(lib.Rules, nil) {
			continue
		}
		artifact := lib.Downloads.Artifact
		if artifact.Path == "" {
			path, e := mavenPath(lib.Name)
			if e != nil {
				return r, e
			}
			artifact.Path = path
			artifact.SHA1 = lib.SHA1
			artifact.Size = lib.Size
			url := lib.URL
			if url == "" {
				url = "https://libraries.minecraft.net/"
			}
			artifact.URL = strings.TrimRight(url, "/") + "/" + path
		}
		artifact.Path = "libraries/" + artifact.Path
		add(artifact)
		abs, e := join(s.Root, artifact.Path)
		if e != nil {
			return r, e
		}
		r.Classpath = append(r.Classpath, abs)
		if classifier := lib.Natives["windows"]; classifier != "" {
			classifier = strings.ReplaceAll(classifier, "${arch}", "64")
			native, ok := lib.Downloads.Classifiers[classifier]
			if !ok {
				return r, fmt.Errorf("нет native %s", classifier)
			}
			native.Path = "libraries/" + native.Path
			add(native)
			r.NativeJars = append(r.NativeJars, native.Path)
		}
	}
	clientPath, _ := join(s.Root, client.Path)
	r.Classpath = append(r.Classpath, clientPath)
	if p.AssetIndex.URL != "" {
		index := p.AssetIndex
		id := index.ID
		if id == "" {
			id = p.Assets
		}
		if validateID(id) != nil {
			return r, fmt.Errorf("неверный asset index")
		}
		r.AssetIndexID = id
		index.Path = "assets/indexes/" + id + ".json"
		if e := s.download(ctx, index, nil); e != nil {
			return r, e
		}
		add(index)
		var assets AssetIndex
		indexPath, _ := join(s.Root, index.Path)
		if e := readJSON(indexPath, &assets); e != nil {
			return r, e
		}
		for _, asset := range assets.Objects {
			if len(asset.Hash) != 40 {
				return r, fmt.Errorf("неверный asset hash")
			}
			add(File{Path: "assets/objects/" + asset.Hash[:2] + "/" + asset.Hash, URL: "https://resources.download.minecraft.net/" + asset.Hash[:2] + "/" + asset.Hash, SHA1: asset.Hash, Size: asset.Size})
		}
	}
	if log := p.Logging.Client.File; log.URL != "" {
		log.Path = "assets/log_configs/" + log.ID
		add(log)
	}
	return r, nil
}
func (s *Service) prepare(ctx context.Context, r *Receipt) error {
	nativeDir := filepath.Join(s.Root, "natives", r.ID)
	if r.ID == "" {
		nativeDir = filepath.Join(s.Root, "natives", r.Base)
	}
	if e := os.MkdirAll(nativeDir, 0755); e != nil {
		return e
	}
	for _, jar := range r.NativeJars {
		if e := ctx.Err(); e != nil {
			return e
		}
		path, e := join(s.Root, jar)
		if e != nil {
			return e
		}
		if e = extractZip(path, nativeDir, func(name string) bool { return !strings.HasPrefix(name, "META-INF/") }); e != nil {
			return e
		}
	}
	if r.AssetIndexID != "" {
		var index AssetIndex
		if e := readJSON(filepath.Join(s.Root, "assets", "indexes", r.AssetIndexID+".json"), &index); e != nil {
			return e
		}
		if index.Virtual || index.MapToResources {
			for name, asset := range index.Objects {
				if e := ctx.Err(); e != nil {
					return e
				}
				src := filepath.Join(s.Root, "assets", "objects", asset.Hash[:2], asset.Hash)
				targets := []string{}
				if index.Virtual {
					targets = append(targets, filepath.Join(s.Root, "assets", "virtual", r.AssetIndexID))
				}
				if index.MapToResources {
					targets = append(targets, filepath.Join(r.GameDir, "resources"))
				}
				for _, root := range targets {
					dest, e := join(root, name)
					if e != nil {
						return e
					}
					if validFile(dest, File{SHA1: asset.Hash, Size: asset.Size}, false) {
						continue
					}
					if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
						return e
					}
					if e = os.Link(src, dest); e != nil {
						data, e := os.ReadFile(src)
						if e != nil {
							return e
						}
						if e = save(dest, data); e != nil {
							return e
						}
					}
				}
			}
		}
	}
	return nil
}
func (s *Service) installForge(ctx context.Context, kind, version, java string) (Profile, error) {
	group := "https://maven.minecraftforge.net/net/minecraftforge/forge/"
	artifact := "forge"
	if kind == "neoforge" {
		group = "https://maven.neoforged.net/releases/net/neoforged/neoforge/"
		artifact = "neoforge"
	}
	url := group + version + "/" + artifact + "-" + version + "-installer.jar"
	sha, e := fetch(ctx, s.HTTP, url+".sha1")
	if e != nil {
		return Profile{}, e
	}
	hash := strings.Fields(string(sha))
	if len(hash) == 0 || len(hash[0]) != 40 {
		return Profile{}, fmt.Errorf("неверная SHA1 установщика")
	}
	f := File{Path: ".sintet/installers/" + kind + "-" + version + ".jar", URL: url, SHA1: hash[0]}
	if e = s.download(ctx, f, nil); e != nil {
		return Profile{}, e
	}
	jar, _ := join(s.Root, f.Path)
	archive, e := zip.OpenReader(jar)
	if e != nil {
		return Profile{}, e
	}
	var child Profile
	for _, entry := range archive.File {
		if entry.Name == "version.json" {
			in, err := entry.Open()
			if err != nil {
				archive.Close()
				return child, err
			}
			err = json.NewDecoder(in).Decode(&child)
			in.Close()
			if err != nil {
				archive.Close()
				return child, err
			}
			break
		}
	}
	archive.Close()
	if child.ID == "" {
		return child, fmt.Errorf("старый формат установщика %s пока не поддерживается", kind)
	}
	if e = validateID(child.ID); e != nil {
		return child, e
	}
	profiles := filepath.Join(s.Root, "launcher_profiles.json")
	if _, err := os.Stat(profiles); os.IsNotExist(err) {
		if e = saveJSON(profiles, map[string]any{"profiles": map[string]any{}, "version": 3}); e != nil {
			return child, e
		}
	}
	logPath := filepath.Join(s.Root, ".sintet", "logs", kind+"-install.log")
	if e = os.MkdirAll(filepath.Dir(logPath), 0755); e != nil {
		return child, e
	}
	log, e := os.Create(logPath)
	if e != nil {
		return child, e
	}
	defer log.Close()
	s.mu.Lock()
	s.state.Log = logPath
	s.mu.Unlock()
	cmd := exec.CommandContext(ctx, java, "-jar", jar, "--installClient", s.Root)
	cmd.Dir = s.Root
	cmd.Stdout = log
	cmd.Stderr = log
	hideProcess(cmd)
	if e = cmd.Run(); e != nil {
		return child, fmt.Errorf("установщик %s завершился с ошибкой: %w; журнал: %s", kind, e, logPath)
	}
	if e = readJSON(filepath.Join(s.Root, "versions", child.ID, child.ID+".json"), &child); e != nil {
		return child, e
	}
	return child, nil
}
