package launcher

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func RebasePath(path, oldRoot, newRoot string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	rel, e := filepath.Rel(oldRoot, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return path
	}
	return filepath.Join(newRoot, rel)
}

// Rebase only stored local paths; download URLs and relative artifact paths stay intact.
func RebaseReceipts(root, oldRoot string) error {
	paths, e := filepath.Glob(filepath.Join(root, ".sintet", "installed", "*.json"))
	if e != nil {
		return e
	}
	for _, path := range paths {
		var receipt Receipt
		if e = readJSON(path, &receipt); e != nil {
			return e
		}
		receipt.Java = RebasePath(receipt.Java, oldRoot, root)
		receipt.GameDir = RebasePath(receipt.GameDir, oldRoot, root)
		for i, p := range receipt.Classpath {
			receipt.Classpath[i] = RebasePath(p, oldRoot, root)
		}
		if e = saveJSON(path, receipt); e != nil {
			return e
		}
	}
	return nil
}
func nestedPath(parent, child string) bool {
	rel, e := filepath.Rel(parent, child)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// Existing nonempty destinations are never overwritten. A cross-volume copy
// verifies every copied file and leaves the original as a fallback.
func MigrateRoot(oldRoot, newRoot string) error {
	oldRoot = filepath.Clean(oldRoot)
	newRoot = filepath.Clean(newRoot)
	if strings.EqualFold(oldRoot, newRoot) {
		return nil
	}
	if !filepath.IsAbs(oldRoot) || !filepath.IsAbs(newRoot) || nestedPath(oldRoot, newRoot) || nestedPath(newRoot, oldRoot) {
		return fmt.Errorf("папки игры не должны быть вложены друг в друга")
	}
	if _, e := os.Stat(oldRoot); os.IsNotExist(e) {
		return os.MkdirAll(newRoot, 0755)
	} else if e != nil {
		return e
	}
	if entries, e := os.ReadDir(newRoot); e == nil {
		if len(entries) != 0 {
			return fmt.Errorf("папка назначения не пуста; выберите пустую папку, чтобы не перезаписать файлы")
		}
		if e = os.Remove(newRoot); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(newRoot), 0755); e != nil {
		return e
	}
	if e := os.Rename(oldRoot, newRoot); e == nil {
		if e = RebaseReceipts(newRoot, oldRoot); e != nil {
			_ = RebaseReceipts(newRoot, newRoot)
			_ = os.Rename(newRoot, oldRoot)
			_ = RebaseReceipts(oldRoot, newRoot)
			return e
		}
		return nil
	}
	if e := copyTree(oldRoot, newRoot); e != nil {
		return fmt.Errorf("перенос не завершён, исходные файлы сохранены: %w", e)
	}
	return RebaseReceipts(newRoot, oldRoot)
}
func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		target, e := join(dest, rel)
		if e != nil {
			return e
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("символическая ссылка не переносится: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer in.Close()
		info, e := in.Stat()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode())
		if e != nil {
			return e
		}
		digest := sha256.New()
		n, e := io.Copy(io.MultiWriter(out, digest), in)
		closeError := out.Close()
		if e != nil {
			return e
		}
		if closeError != nil {
			return closeError
		}
		if n != info.Size() || !validFile(target, File{Size: n, SHA256: fmt.Sprintf("%x", digest.Sum(nil))}, true) {
			return fmt.Errorf("не пройдена проверка копии %s", rel)
		}
		return nil
	})
}
