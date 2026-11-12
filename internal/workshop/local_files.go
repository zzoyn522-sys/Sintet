package workshop

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"sintet/internal/accountfiles"
)

const disabledSuffix = ".disabled"

func localFolder(dir, kind string) (string, error) {
	switch kind {
	case "mods":
		return filepath.Join(dir, "mods"), nil
	case "resources":
		return filepath.Join(dir, "resourcepacks"), nil
	case "shaders":
		return filepath.Join(dir, "shaderpacks"), nil
	default:
		return "", errors.New("неизвестный тип файлов")
	}
}

func localDirectory(folder string) error {
	info, err := os.Lstat(folder)
	if err == nil && !info.IsDir() {
		return errors.New("папка файлов сборки должна быть обычной папкой")
	}
	return err
}

func localFilePath(dir, kind, filename string) (string, error) {
	folder, err := localFolder(dir, kind)
	if err != nil {
		return "", err
	}
	if !safeContentFilename(strings.TrimSuffix(filename, disabledSuffix), kind) {
		return "", errors.New("неверное имя файла")
	}
	if err := localDirectory(folder); err != nil {
		return "", err
	}
	path := filepath.Join(folder, filename)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("нужен обычный файл")
	}
	return path, nil
}

func moveLocalFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func localNameAvailable(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("файл «%s» уже есть в сборке", filepath.Base(path))
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AddLocalFiles validates the complete selection before publishing any files.
func AddLocalFiles(dir, kind string, paths []string) (int, error) {
	folder, err := localFolder(dir, kind)
	if err != nil {
		return 0, err
	}
	if len(paths) == 0 || len(paths) > 64 {
		return 0, errors.New("выбери от 1 до 64 файлов")
	}
	if err := os.MkdirAll(folder, 0755); err != nil {
		return 0, err
	}
	if err := localDirectory(folder); err != nil {
		return 0, err
	}
	limit := int64(100 << 20)
	format := "JAR"
	if isZipPack(kind) {
		limit, format = 1<<30, "ZIP"
	}
	type pendingFile struct{ temp, dest string }
	files := make([]pendingFile, 0, len(paths))
	defer func() {
		for _, file := range files {
			_ = os.Remove(file.temp)
		}
	}()
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		if !safeContentFilename(name, kind) {
			return 0, fmt.Errorf("«%s»: выбери файл %s", name, format)
		}
		if key := strings.ToLower(name); seen[key] {
			return 0, fmt.Errorf("«%s» выбран дважды", name)
		} else {
			seen[key] = true
		}
		dest := filepath.Join(folder, name)
		if err := localNameAvailable(dest); err != nil {
			return 0, err
		}
		if err := localNameAvailable(dest + disabledSuffix); err != nil {
			return 0, err
		}
		temp, err := stageLocalFile(path, folder, kind, format, limit)
		if err != nil {
			return 0, fmt.Errorf("«%s»: %w", name, err)
		}
		files = append(files, pendingFile{temp, dest})
	}
	for i, file := range files {
		err := localNameAvailable(file.dest + disabledSuffix)
		if err == nil {
			err = moveLocalFile(file.temp, file.dest)
		}
		if err != nil {
			for _, added := range files[:i] {
				if rollback := os.Remove(added.dest); rollback != nil {
					err = errors.Join(err, rollback)
				}
			}
			return 0, err
		}
	}
	return len(files), nil
}

func stageLocalFile(source, folder, kind, format string, limit int64) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 4 || info.Size() > limit {
		return "", fmt.Errorf("нужен файл %s размером до %d МБ", format, limit>>20)
	}
	temp, err := os.CreateTemp(folder, ".sintet-import-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(temp.Name())
		}
	}()
	n, err := io.Copy(temp, io.LimitReader(input, limit+1))
	if err == nil && (n != info.Size() || n > limit) {
		err = errors.New("размер файла изменился при копировании")
	}
	if err == nil {
		err = temp.Sync()
	}
	err = errors.Join(err, temp.Close())
	if err != nil {
		return "", err
	}
	archive, err := zip.OpenReader(temp.Name())
	if err != nil {
		return "", fmt.Errorf("повреждённый архив %s", format)
	}
	defer archive.Close()
	if kind == "resources" {
		_, err = readPackMetadata(&archive.Reader)
		if err != nil {
			return "", errors.New("в ZIP нет корректного pack.mcmeta")
		}
	}
	if kind == "shaders" && !isShaderArchive(&archive.Reader) {
		return "", errors.New("в ZIP нет папки shaders с файлами шейдеров")
	}
	return temp.Name(), nil
}

func ToggleLocal(dir, kind, filename string, enabled bool) error {
	from, err := localFilePath(dir, kind, filename)
	if err != nil {
		return err
	}
	dest := strings.TrimSuffix(from, disabledSuffix)
	if !enabled {
		dest += disabledSuffix
	}
	if from == dest {
		return nil
	}
	if err := localNameAvailable(dest); err != nil {
		return err
	}
	return moveLocalFile(from, dest)
}

func RemoveLocal(dir, kind, filename string) error {
	path, err := localFilePath(dir, kind, filename)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(filename, disabledSuffix)
	other := strings.TrimSuffix(path, disabledSuffix)
	if other == path {
		other += disabledSuffix
	}
	if _, err := os.Lstat(other); err == nil {
		return os.Remove(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	records, registryName, err := localRegistry(dir, kind)
	if err != nil {
		return err
	}
	removed := false
	for key, record := range records {
		if strings.EqualFold(record.Filename, base) {
			delete(records, key)
			removed = true
		}
	}
	if !removed {
		return os.Remove(path)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".sintet-remove-*")
	if err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Rename(path, temp.Name()); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := accountfiles.Write(filepath.Join(dir, registryName), data); err != nil {
		return errors.Join(err, moveLocalFile(temp.Name(), path))
	}
	return os.Remove(temp.Name())
}

func localRegistry(dir, kind string) (map[string]Installed, string, error) {
	if isZipPack(kind) {
		name := zipPackRegistry(kind)
		records, err := readZipPackRegistry(dir, name)
		return records, name, err
	}
	records, err := registry(dir)
	return records, ".sintet-mods.json", err
}
