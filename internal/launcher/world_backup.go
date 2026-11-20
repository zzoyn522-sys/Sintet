package launcher

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

const worldBackupKeep = 5

var worldBackupName = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z\.zip$`)

func (s *Service) backupWorlds(ctx context.Context, instance string, interval time.Duration, log io.Writer) {
	if interval <= 0 {
		return
	}
	relative, err := filepath.Rel(s.Root, instance)
	if err != nil || !filepath.IsLocal(relative) {
		fmt.Fprintln(log, "Резервная копия миров: папка сборки недоступна.")
		return
	}
	key := sha256.Sum256([]byte(relative))
	label := filepath.Base(instance)
	if validateID(label) != nil {
		label = "instance"
	}
	backupRoot := filepath.Join(s.Root, "World Backups")
	if err = os.Mkdir(backupRoot, 0700); err == nil || errors.Is(err, fs.ErrExist) {
		err = verifyWorldBackupDirectory(backupRoot)
	}
	if err != nil {
		fmt.Fprintf(log, "\nРезервная копия миров: %v\n", err)
		return
	}
	destination := filepath.Join(backupRoot, fmt.Sprintf("%s-%x", label, key[:6]))
	if err = backupWorldSnapshot(ctx, instance, destination, interval, time.Now()); err != nil {
		fmt.Fprintf(log, "\nРезервная копия миров: %v\n", err)
	}
}

func verifyWorldBackupDirectory(name string) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("копии можно сохранять только в обычную папку")
	}
	return nil
}

func backupWorldSnapshot(ctx context.Context, instance, destination string, interval time.Duration, now time.Time) error {
	root, err := os.OpenRoot(instance)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat("saves")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("saves должна быть обычной папкой")
	}
	worlds, err := fs.ReadDir(root.FS(), "saves")
	if err != nil {
		return err
	}
	worlds = slices.DeleteFunc(worlds, func(world fs.DirEntry) bool {
		if !world.IsDir() {
			return true
		}
		for _, file := range []string{"level.dat", "level.dat_old"} {
			info, err := root.Lstat(path.Join("saves", world.Name(), file))
			if err == nil && info.Mode().IsRegular() {
				return false
			}
		}
		return true
	})
	if len(worlds) == 0 {
		return nil
	}
	if err := os.Mkdir(destination, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := verifyWorldBackupDirectory(destination); err != nil {
		return err
	}
	copies, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	copies = slices.DeleteFunc(copies, func(row fs.DirEntry) bool {
		return !row.Type().IsRegular() || !worldBackupName.MatchString(row.Name())
	})
	if last := len(copies) - 1; last >= 0 {
		stamp, err := time.Parse("20060102T150405.000000000Z.zip", copies[last].Name())
		if err == nil && !stamp.After(now) && now.Sub(stamp) < interval {
			return nil
		}
	}

	tmp, err := os.CreateTemp(destination, ".worlds-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	writer := zip.NewWriter(tmp)
	for _, world := range worlds {
		name := path.Join("saves", world.Name())
		err = fs.WalkDir(root.FS(), name, func(file string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if item.IsDir() || file == path.Join(name, "session.lock") {
				return nil
			}
			if !item.Type().IsRegular() {
				return fmt.Errorf("файл мира %s не является обычным файлом", file)
			}
			source, err := root.Open(file)
			if err != nil {
				return err
			}
			defer source.Close()
			before, err := source.Stat()
			if err != nil || !before.Mode().IsRegular() {
				return fmt.Errorf("файл мира %s изменился при открытии", file)
			}
			archive, err := writer.Create(file)
			if err != nil {
				return err
			}
			written, err := io.Copy(archive, worldBackupReader{ctx, source})
			if err != nil {
				return err
			}
			after, err := source.Stat()
			if err != nil || written != before.Size() || after.Size() != before.Size() || after.ModTime() != before.ModTime() {
				return fmt.Errorf("файл мира %s изменился во время копирования", file)
			}
			return nil
		})
		if err != nil {
			break
		}
	}
	err = errors.Join(err, writer.Close(), tmp.Sync(), tmp.Close(), ctx.Err())
	if err != nil {
		return err
	}
	final := filepath.Join(destination, now.UTC().Format("20060102T150405.000000000Z.zip"))
	if err := os.Rename(tmp.Name(), final); err != nil {
		return err
	}
	for _, old := range copies[:max(0, len(copies)+1-worldBackupKeep)] {
		if err := os.Remove(filepath.Join(destination, old.Name())); err != nil {
			return err
		}
	}
	return nil
}

type worldBackupReader struct {
	ctx context.Context
	io.Reader
}

func (r worldBackupReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
