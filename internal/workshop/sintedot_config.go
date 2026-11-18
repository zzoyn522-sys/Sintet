package workshop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

func stageBrokenConfigs(ctx context.Context, root, stage string) ([]sintedotTxFile, []string, []string, error) {
	var files []sintedotTxFile
	var changes, issues []string
	folder := filepath.Join(root, "config")
	if err := localDirectory(folder); os.IsNotExist(err) {
		return nil, nil, nil, nil
	} else if err != nil {
		return nil, nil, nil, err
	}
	err := filepath.WalkDir(folder, func(path string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if !item.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" && ext != ".toml" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		const maxConfig = 4 << 20
		data, err := io.ReadAll(io.LimitReader(file, maxConfig+1))
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if len(data) > maxConfig {
			issues = append(issues, rel+": файл слишком большой для быстрой проверки")
			return nil
		}
		if ext == ".json" && utf8.Valid(data) && json.Valid(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})) {
			return nil
		}
		if ext == ".toml" {
			var document map[string]any
			if toml.Unmarshal(data, &document) == nil {
				return nil
			}
		}
		broken := rel + ".broken"
		if _, err = os.Lstat(filepath.Join(root, broken)); err == nil {
			issues = append(issues, rel+": синтаксис повреждён, имя .broken уже занято")
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		dest := filepath.Join(stage, broken)
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(dest, data, 0600); err != nil {
			return err
		}
		files = append(files, sintedotTxFile{Path: rel, Original: true, Expected: fmt.Sprintf("%x", sha256.Sum256(data))}, sintedotTxFile{Path: broken, Replacement: true})
		changes = append(changes, rel+" → "+broken+" (ошибка синтаксиса)")
		return nil
	})
	return files, changes, issues, err
}
