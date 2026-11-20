package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type mrpackIndex struct {
	Format       int               `json:"formatVersion"`
	Game         string            `json:"game"`
	VersionID    string            `json:"versionId"`
	Name         string            `json:"name"`
	Dependencies map[string]string `json:"dependencies"`
	Files        []mrpackFile      `json:"files"`
}

type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	Size      int64             `json:"fileSize"`
}

func newPackStage(cacheRoot string) (string, error) {
	if !filepath.IsAbs(cacheRoot) {
		return "", errors.New("неверная папка кэша")
	}
	if err := os.MkdirAll(cacheRoot, 0700); err != nil {
		return "", err
	}
	return os.MkdirTemp(cacheRoot, "pack-")
}

func publicPackDownload(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Port() == "" && allowedPackDownload(raw)
}

func (m *Manager) PrepareLocalPack(ctx context.Context, file io.Reader, name, cacheRoot string) (PackPlan, error) {
	result := PackPlan{
		Created: time.Now(), Warnings: []string{}, LocalArchive: true,
		Project: Project{Provider: "modrinth", Kind: "modpacks", Name: name},
	}
	folder, err := newPackStage(cacheRoot)
	if err != nil {
		return result, err
	}
	keep := false
	defer func() {
		if !keep {
			removePackStage(folder)
		}
	}()
	result.folder = folder
	archive := filepath.Join(folder, "source.zip")
	out, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	n, err := io.Copy(out, io.LimitReader(&packContextReader{ctx, file}, (8<<30)+1))
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		return result, errors.Join(err, closeErr)
	}
	if n > 8<<30 {
		return result, errors.New("архив сборки превышает лимит установки")
	}
	if err := m.parsePack(ctx, archive, &result); err != nil {
		return result, err
	}
	result, err = m.rememberPackPlan(result)
	keep = err == nil
	return result, err
}

var exportPackFolders = []string{
	"mods", "resourcepacks", "shaderpacks", "config", "defaultconfigs",
	"kubejs", "scripts", "datapacks", "global_packs", "openloader", "patchouli_books", "paxi",
}

func packExportIndex(name, game, loader, version string) (mrpackIndex, error) {
	dependencies := map[string]string{"minecraft": game}
	if loader != "vanilla" {
		key := map[string]string{"fabric": "fabric-loader", "forge": "forge", "neoforge": "neoforge"}[loader]
		if key == "" {
			return mrpackIndex{}, errors.New("этот загрузчик не поддерживается для .mrpack")
		}
		if loader == "forge" {
			version = strings.TrimPrefix(version, game+"-")
		}
		dependencies[key] = version
	}
	if _, _, err := packLoader(game, dependencies); err != nil {
		return mrpackIndex{}, err
	}
	return mrpackIndex{
		Format: 1, Game: "minecraft", Name: name,
		VersionID:    "sintet-" + time.Now().UTC().Format("20060102-150405.000000000"),
		Dependencies: dependencies, Files: []mrpackFile{},
	}, nil
}

func exportPackHashes(ctx context.Context, root *os.Root, relative string, size int64, output io.Writer) (map[string]string, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return nil, errors.New("файл изменился во время экспорта")
	}
	h1, h512 := sha1.New(), sha512.New()
	n, err := io.Copy(io.MultiWriter(output, h1, h512), io.LimitReader(&packContextReader{ctx, file}, size+1))
	if err != nil {
		return nil, err
	}
	if n != size {
		return nil, errors.New("файл изменился во время экспорта")
	}
	return map[string]string{"sha1": hex.EncodeToString(h1.Sum(nil)), "sha512": hex.EncodeToString(h512.Sum(nil))}, nil
}

func exportPackFiles(ctx context.Context, root *os.Root) ([]mrpackFile, error) {
	var files []mrpackFile
	var total int64
	for _, folder := range exportPackFolders {
		err := fs.WalkDir(root.FS(), folder, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				if name == folder && errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				if !safePackPath(name) || strings.HasSuffix(strings.ToLower(name), ".disabled") {
					return fs.SkipDir
				}
				return nil
			}
			lower := strings.ToLower(name)
			if name == folder || !entry.Type().IsRegular() || !safePackPath(name) || strings.HasSuffix(lower, ".disabled") || strings.HasSuffix(lower, ".broken") {
				return nil
			}
			if folder == "mods" && !strings.HasSuffix(lower, ".jar") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size := info.Size()
			total += size
			if size > 1<<30 || total > 8<<30 || len(files) == 10000 {
				return errors.New("сборка превышает лимиты экспорта")
			}
			hashes, err := exportPackHashes(ctx, root, name, size, io.Discard)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			files = append(files, mrpackFile{Path: name, Size: size, Hashes: hashes})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(files, func(a, b mrpackFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

func (c *Client) mrpackDownloadLinks(ctx context.Context, files []mrpackFile) map[string]string {
	hashes := []string{}
	seen := map[string]bool{}
	for _, file := range files {
		folder, _, _ := strings.Cut(file.Path, "/")
		if folder != "mods" && (folder != "resourcepacks" && folder != "shaderpacks" || path.Ext(strings.ToLower(file.Path)) != ".zip") {
			continue
		}
		hash := file.Hashes["sha512"]
		if !seen[hash] {
			hashes = append(hashes, hash)
			seen[hash] = true
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	links := map[string]string{}
	for start := 0; start < len(hashes) && ctx.Err() == nil; start += 64 {
		batch := hashes[start:min(start+64, len(hashes))]
		body, _ := json.Marshal(map[string]any{"hashes": batch, "algorithm": "sha512"})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Modrinth, "/")+"/version_files", bytes.NewReader(body))
		if err != nil {
			break
		}
		req.Header.Set("User-Agent", "Sintet/0.9 (sintet.xyz)")
		req.Header.Set("Content-Type", "application/json")
		response, err := c.HTTP.Do(req)
		if err != nil {
			break
		}
		var rows map[string]mrVersion
		if response.StatusCode == http.StatusOK {
			_ = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&rows)
		}
		response.Body.Close()
		if rows == nil {
			break
		}
		for _, hash := range batch {
			for _, file := range rows[hash].Files {
				if strings.EqualFold(file.Hashes["sha512"], hash) && publicModSource("modrinth", file.URL) != "" {
					links[hash] = file.URL
					break
				}
			}
		}
	}
	return links
}

func (m *Manager) ExportPack(ctx context.Context, folder, destination, name, game, loader, version string) error {
	index, err := packExportIndex(name, game, loader, version)
	if err != nil {
		return err
	}
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("папка сборки недоступна")
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return err
	}
	defer root.Close()
	files, err := exportPackFiles(ctx, root)
	if err != nil {
		return err
	}
	links := m.Client.mrpackDownloadLinks(ctx, files)
	var overrides []mrpackFile
	for _, file := range files {
		if download := links[file.Hashes["sha512"]]; download != "" {
			file.Downloads = []string{download}
			if strings.HasPrefix(file.Path, "resourcepacks/") || strings.HasPrefix(file.Path, "shaderpacks/") {
				file.Env = map[string]string{"client": "required", "server": "unsupported"}
			}
			index.Files = append(index.Files, file)
		} else {
			overrides = append(overrides, file)
		}
	}
	manifest, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if len(manifest) > 4<<20 {
		return errors.New("манифест сборки превышает лимит экспорта")
	}
	out, err := os.CreateTemp(filepath.Dir(destination), ".sintet-pack-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	archive := zip.NewWriter(out)
	indexFile, err := archive.Create("modrinth.index.json")
	if err == nil {
		_, err = indexFile.Write(manifest)
	}
	if err != nil {
		return err
	}
	for _, file := range overrides {
		entry, err := archive.Create("overrides/" + file.Path)
		if err != nil {
			return err
		}
		hashes, err := exportPackHashes(ctx, root, file.Path, file.Size, entry)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Path, err)
		}
		if hashes["sha512"] != file.Hashes["sha512"] {
			return fmt.Errorf("%s: файл изменился во время экспорта", file.Path)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), destination)
}
