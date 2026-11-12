package packsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Fetcher interface {
	PackGet(context.Context, string) (*http.Response, error)
}

type ManifestHTTPError int

func (e ManifestHTTPError) Error() string {
	return fmt.Sprintf("хост вернул HTTP %d для манифеста сборки", e)
}

func ReadManifest(ctx context.Context, peer Fetcher) (Manifest, error) {
	r, err := peer.PackGet(ctx, "/manifest")
	if err != nil {
		return Manifest{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return Manifest{}, ManifestHTTPError(r.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxManifest+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("не удалось прочитать манифест: %w", err)
	}
	if len(data) > MaxManifest {
		return Manifest{}, errors.New("слишком большой манифест")
	}
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&m); err != nil {
		return m, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return m, errors.New("некорректные данные после манифеста")
	}
	return m, Validate(m)
}
func CopyVerified(ctx context.Context, input io.Reader, cache string, f File, onBytes func(int64)) error {
	if !hashPattern.MatchString(f.SHA256) || f.Size < 0 || f.Size > MaxMod {
		return errors.New("неверный файл")
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return err
	}
	out, err := os.CreateTemp(cache, "download-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	limited := io.LimitReader(input, f.Size+1)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, e := limited.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > f.Size {
				return errors.New("файл больше размера из манифеста")
			}
			if _, err = out.Write(buffer[:n]); err != nil {
				return err
			}
			h.Write(buffer[:n])
			if onBytes != nil {
				onBytes(int64(n))
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if total != f.Size {
		return io.ErrUnexpectedEOF
	}
	if hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return errors.New("SHA-256 скачанного файла не совпал: " + f.Path)
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	dest := filepath.Join(cache, f.SHA256)
	if err = os.Rename(out.Name(), dest); err != nil && !ValidCached(cache, f) {
		return err
	}
	return nil
}

// Populate only a newly-created directory. Existing user instances are never
// overwritten and untrusted paths cannot escape into launcher configuration.
func Populate(ctx context.Context, m Manifest, cache, directory string) error {
	if err := Validate(m); err != nil {
		return err
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		return errors.New("папка сборки уже существует; выберите отдельный инстанс")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			RemoveUnpublished(directory, m)
		}
	}()
	for _, f := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !ValidCached(cache, f) {
			return errors.New("повреждён кэш сборки")
		}
		dest := filepath.Join(directory, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		source, err := os.Open(filepath.Join(cache, f.SHA256))
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			source.Close()
			return err
		}
		hash := sha256.New()
		copied, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(source, f.Size+1))
		err = copyErr
		if err == nil && (copied != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256) {
			err = errors.New("кэш изменился во время записи сборки")
		}
		source.Close()
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	complete = true
	return nil
}

// Only for a fresh, unpublished instance owned by the calling operation.
// Remove explicit allowed files and empty directories, never recurse or touch
// unrelated content if another process added something to this directory.
func RemoveUnpublished(directory string, m Manifest) {
	if Validate(m) != nil {
		return
	}
	root, err := filepath.Abs(directory)
	if err != nil || root == filepath.VolumeName(root)+string(os.PathSeparator) {
		return
	}
	dirs := map[string]bool{root: true}
	files := []string{"instance.json", ".sintet-p2p.json"}
	for _, f := range m.Files {
		files = append(files, f.Path)
	}
	for _, name := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if safeSource(root, file) {
			if info, err := os.Lstat(file); err == nil && info.Mode().IsRegular() {
				_ = os.Remove(file)
			}
		}
		parent := filepath.Dir(file)
		for parent != root {
			rel, e := filepath.Rel(root, parent)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				break
			}
			dirs[parent] = true
			parent = filepath.Dir(parent)
		}
	}
	for _, name := range []string{"mods", "config", "saves", "resourcepacks"} {
		dirs[filepath.Join(root, name)] = true
	}
	ordered := []string{}
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, dir := range ordered {
		if dir == root || safeSource(root, dir) {
			_ = os.Remove(dir)
		}
	}
}
