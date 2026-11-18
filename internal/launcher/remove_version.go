package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) DeleteVersion(id string) error {
	if validateID(id) != nil || strings.HasPrefix(strings.ToLower(id), "build-") ||
		strings.EqualFold(id, "family") || !filepath.IsLocal(id) || strings.TrimRight(id, ". ") != id {
		return errors.New("неверная версия Minecraft")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy || s.state.Running {
		return errors.New("закрой игру и дождись завершения текущей операции")
	}

	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	receiptDir := filepath.Join(".sintet", "installed")
	if ok, err := safeRemovalTarget(root, receiptDir, true); err != nil || !ok {
		return errors.New("не удалось открыть данные установленных версий")
	}
	receiptPath := filepath.Join(receiptDir, id+".json")
	if ok, err := safeRemovalTarget(root, receiptPath, false); err != nil || !ok {
		return errors.New("версия не установлена")
	}
	receipt, err := s.readReceipt(id)
	if err != nil {
		return err
	}
	if receipt.Base == "" || receipt.Loader == "" || ProfileID(receipt.Base, receipt.Loader, receipt.LoaderVersion) != id {
		return errors.New("неверная запись установки Minecraft")
	}

	paths := []string{filepath.Join("instances", id), filepath.Join("natives", id)}
	references, err := s.otherVersionFiles(root, receiptDir, id)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	if references != nil {
		for _, file := range receipt.Files {
			path := filepath.Clean(filepath.FromSlash(file.Path))
			key := strings.ToLower(path)
			if !knownVersionArtifact(receipt.Base, path) || references[key] || seen[key] {
				continue
			}
			seen[key] = true
			paths = append(paths, path)
		}
	}
	paths = append(paths, filepath.Join(".sintet", "profiles", id+".json"), receiptPath)
	return removeVersionPaths(root, paths)
}

func (s *Service) otherVersionFiles(root *os.Root, receiptDir, id string) (map[string]bool, error) {
	entries, err := fs.ReadDir(root.FS(), filepath.ToSlash(receiptDir))
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]bool)
	mark := func(path string) {
		if filepath.IsAbs(path) {
			if relative, err := filepath.Rel(s.Root, path); err == nil {
				path = relative
			}
		}
		referenced[strings.ToLower(filepath.Clean(filepath.FromSlash(path)))] = true
	}
	for _, entry := range entries {
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") || strings.EqualFold(entry.Name(), id+".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, nil
		}
		other, err := s.readReceipt(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if err != nil {
			// An unreadable receipt may still refer to shared downloads.
			return nil, nil
		}
		for _, file := range other.Files {
			mark(file.Path)
		}
		for _, file := range other.Classpath {
			mark(file)
		}
		for _, file := range other.NativeJars {
			mark(file)
		}
		if other.AssetIndexID != "" {
			mark(filepath.Join("assets", "indexes", other.AssetIndexID+".json"))
		}
	}
	return referenced, nil
}

func knownVersionArtifact(base, path string) bool {
	if !filepath.IsLocal(path) {
		return false
	}
	name := filepath.ToSlash(path)
	for _, prefix := range []string{"libraries/", "assets/objects/", "assets/indexes/", "assets/log_configs/", "versions/" + base + "/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func safeRemovalTarget(root *os.Root, path string, directory bool) (bool, error) {
	if path == "." || !filepath.IsLocal(path) {
		return false, fmt.Errorf("небезопасный путь версии: %s", path)
	}
	current := "."
	parts := strings.Split(path, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if directory || index < len(parts)-1 {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false, fmt.Errorf("небезопасная папка версии: %s", current)
			}
		} else if !info.Mode().IsRegular() {
			return false, fmt.Errorf("небезопасный файл версии: %s", current)
		}
	}
	return true, nil
}

func removeVersionPaths(root *os.Root, paths []string) error {
	staging, err := os.MkdirTemp(root.Name(), ".sintet-remove-")
	if err != nil {
		return err
	}
	stage := filepath.Base(staging)
	moved := make([]string, 0, len(paths))
	for index, path := range paths {
		present, err := safeRemovalTarget(root, path, index < 2)
		if err == nil && !present {
			continue
		}
		target := filepath.Join(staging, path)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(target), 0700)
		}
		if err == nil {
			err = os.Rename(filepath.Join(root.Name(), path), target)
		}
		if err != nil {
			var restore error
			for i := len(moved) - 1; i >= 0; i-- {
				path := moved[i]
				present, check := safeRemovalTarget(root, filepath.Dir(path), true)
				switch {
				case check != nil:
					restore = errors.Join(restore, check)
				case !present:
					restore = errors.Join(restore, fmt.Errorf("папка восстановления недоступна: %s", filepath.Dir(path)))
				default:
					restore = errors.Join(restore, os.Rename(filepath.Join(staging, path), filepath.Join(root.Name(), path)))
				}
			}
			if restore != nil {
				return fmt.Errorf("не удалось завершить удаление: %w. Папка восстановления: %s", errors.Join(err, restore), staging)
			}
			_ = cleanVersionStaging(root, stage)
			return err
		}
		moved = append(moved, path)
	}
	if err := cleanVersionStaging(root, stage); err != nil {
		return fmt.Errorf("версия удалена, но часть файлов осталась в папке %s: %w", staging, err)
	}
	return nil
}

func cleanVersionStaging(root *os.Root, stage string) error {
	present, err := safeRemovalTarget(root, stage, true)
	if err != nil || !present {
		return err
	}
	return os.RemoveAll(filepath.Join(root.Name(), stage))
}
