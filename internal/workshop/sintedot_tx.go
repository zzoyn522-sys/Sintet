package workshop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"sintet/internal/accountfiles"
)

type sintedotTxFile struct {
	Path        string `json:"path"`
	Original    bool   `json:"original"`
	Replacement bool   `json:"replacement"`
	Expected    string `json:"-"`
}

type sintedotTx struct {
	Status   string           `json:"status"`
	Backup   string           `json:"backup"`
	Files    []sintedotTxFile `json:"files"`
	Previous *sintedotTx      `json:"previous,omitempty"`
}

func txPath(root string) string {
	return filepath.Join(root, ".sintet", "sintedot_tx.json")
}

func checkedTxPath(root, rel string) (string, error) {
	if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel || strings.Contains(rel, ":") {
		return "", errors.New("неверный путь в журнале Sintedot")
	}
	path := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if (!info.IsDir() && !info.Mode().IsRegular()) || (i < len(parts)-1 && !info.IsDir()) {
			return "", fmt.Errorf("небезопасный путь Sintedot: %s", rel)
		}
	}
	return path, nil
}

func validateSintedotTx(tx *sintedotTx) bool {
	if tx == nil || filepath.Base(tx.Backup) != tx.Backup || !strings.HasPrefix(tx.Backup, ".sintedot-backup-") || strings.Contains(tx.Backup, ":") {
		return false
	}
	seen := map[string]bool{}
	for _, f := range tx.Files {
		rel := f.Path
		if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel || strings.Contains(rel, ":") || seen[strings.ToLower(rel)] {
			return false
		}
		if rel != "mods" && rel != ".sintet-mods.json" {
			ext := strings.ToLower(filepath.Ext(strings.TrimSuffix(rel, ".broken")))
			if !strings.HasPrefix(rel, "config"+string(filepath.Separator)) || (ext != ".json" && ext != ".toml") {
				return false
			}
		}
		seen[strings.ToLower(rel)] = true
	}
	return len(seen) > 0
}

func loadSintedotTx(root string) (*sintedotTx, error) {
	path, err := checkedTxPath(root, filepath.Join(".sintet", "sintedot_tx.json"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tx sintedotTx
	if json.Unmarshal(data, &tx) != nil || !validateSintedotTx(&tx) ||
		(tx.Previous != nil && (!validateSintedotTx(tx.Previous) || tx.Previous.Previous != nil || tx.Previous.Status != "committed")) ||
		!contains([]string{"pending", "committed", "rolling_back", "rolled_back"}, tx.Status) {
		return nil, errors.New("журнал Sintedot повреждён")
	}
	return &tx, nil
}

func writeSintedotTx(root string, tx *sintedotTx) error {
	if _, err := checkedTxPath(root, filepath.Join(".sintet", "sintedot_tx.json")); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	return accountfiles.Write(txPath(root), data)
}

// A backup entry exists exactly when the original was moved. Recovery can therefore
// resume after interruption without moving a restored original a second time.
func restoreSintedotTx(root string, tx *sintedotTx) error {
	backup, err := checkedTxPath(root, tx.Backup)
	if err != nil {
		return err
	}
	if err = localDirectory(backup); err != nil {
		return err
	}
	discard, err := os.MkdirTemp(backup, ".discard-")
	if err != nil {
		return err
	}
	if tx.Status != "rolling_back" {
		defer os.RemoveAll(discard)
	}

	for i := len(tx.Files) - 1; i >= 0; i-- {
		f := tx.Files[i]
		live, err := checkedTxPath(root, f.Path)
		if err != nil {
			return err
		}
		saved, err := checkedTxPath(backup, f.Path)
		if err != nil {
			return err
		}
		_, savedErr := os.Lstat(saved)
		if f.Original && os.IsNotExist(savedErr) {
			if _, err = os.Lstat(live); err != nil {
				return fmt.Errorf("нет исходного файла %s: %w", f.Path, err)
			}
			continue
		}
		if f.Original && savedErr != nil {
			return savedErr
		}
		if err = moveTxFile(live, filepath.Join(discard, fmt.Sprint(i))); err != nil && !os.IsNotExist(err) {
			return err
		}
		if f.Original {
			if err = moveTxFile(saved, live); err != nil {
				return err
			}
		}
	}
	if tx.Status == "pending" && tx.Previous != nil {
		return writeSintedotTx(root, tx.Previous)
	}
	tx.Status = "rolled_back"
	tx.Previous = nil
	return writeSintedotTx(root, tx)
}

func RecoverSintedot(root string) error {
	tx, err := loadSintedotTx(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if tx.Status == "pending" || tx.Status == "rolling_back" {
		return restoreSintedotTx(root, tx)
	}
	return nil
}

func CanRollbackSintedot(root string) bool {
	tx, err := loadSintedotTx(root)
	return err == nil && (tx.Status == "committed" || tx.Status == "rolling_back")
}

func RollbackSintedot(root string) error {
	tx, err := loadSintedotTx(root)
	if os.IsNotExist(err) || (err == nil && tx.Status != "committed" && tx.Status != "rolling_back") {
		return errors.New("нет завершённой операции Sintedot для отката")
	}
	if err != nil {
		return err
	}
	if tx.Status == "committed" {
		for _, f := range tx.Files {
			if !f.Original {
				continue
			}
			saved, err := checkedTxPath(root, filepath.Join(tx.Backup, f.Path))
			if err != nil {
				return err
			}
			if _, err = os.Lstat(saved); err != nil {
				return fmt.Errorf("в резервной копии отсутствует %s: %w", f.Path, err)
			}
		}
		tx.Status = "rolling_back"
		if err = writeSintedotTx(root, tx); err != nil {
			return err
		}
	}
	return restoreSintedotTx(root, tx)
}

func applySintedotTx(root, stage string, files []sintedotTxFile) (string, error) {
	if err := RecoverSintedot(root); err != nil {
		return "", err
	}
	previous, err := loadSintedotTx(root)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	tx := &sintedotTx{Status: "pending", Files: files}
	if previous != nil && previous.Status == "committed" {
		previous.Previous = nil
		tx.Previous = previous
	}
	backup, err := os.MkdirTemp(root, ".sintedot-backup-")
	if err != nil {
		return "", err
	}
	tx.Backup = filepath.Base(backup)
	if !validateSintedotTx(tx) {
		return "", errors.New("недопустимый план Sintedot")
	}
	for i, f := range tx.Files {
		live, err := checkedTxPath(root, f.Path)
		if err != nil {
			return "", err
		}
		_, err = os.Lstat(live)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && !f.Original && strings.HasSuffix(f.Path, ".broken") {
			return "", fmt.Errorf("карантин уже занят: %s", f.Path)
		}
		if strings.HasPrefix(f.Path, "config"+string(filepath.Separator)) && f.Original && os.IsNotExist(err) {
			return "", fmt.Errorf("конфиг исчез во время проверки: %s", f.Path)
		}
		if f.Expected != "" {
			digest, err := fileHash(live)
			if err != nil || digest != f.Expected {
				return "", fmt.Errorf("конфиг изменился во время проверки: %s", f.Path)
			}
		}
		tx.Files[i].Original = err == nil
		if err = os.MkdirAll(filepath.Dir(filepath.Join(backup, f.Path)), 0700); err != nil {
			return "", err
		}
	}
	if err = writeSintedotTx(root, tx); err != nil {
		return "", err
	}
	err = moveSintedotFiles(root, stage, backup, tx)
	if err == nil {
		committed := *tx
		committed.Status, committed.Previous = "committed", nil
		err = writeSintedotTx(root, &committed)
	}
	if err != nil {
		return "", errors.Join(err, restoreSintedotTx(root, tx))
	}
	return backup, nil
}

func moveSintedotFiles(root, stage, backup string, tx *sintedotTx) error {
	for _, f := range tx.Files {
		live := filepath.Join(root, f.Path)
		if f.Original {
			if err := moveTxFile(live, filepath.Join(backup, f.Path)); err != nil {
				return err
			}
		}
		if f.Replacement {
			if err := moveTxFile(filepath.Join(stage, f.Path), live); err != nil {
				return err
			}
		}
	}
	return nil
}

func moveTxFile(source, dest string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	if err = windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: dest, Err: err}
	}
	return nil
}

func RecoverSintedotInstances(root string) error {
	if err := localDirectory(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	var recovery []error
	for _, parent := range []string{filepath.Join(root, "instances"), filepath.Join(root, "instances", "family")} {
		if err := localDirectory(parent); os.IsNotExist(err) {
			continue
		} else if err != nil {
			recovery = append(recovery, err)
			continue
		}
		dirs, err := os.ReadDir(parent)
		if err != nil {
			recovery = append(recovery, err)
			continue
		}
		for _, item := range dirs {
			if !item.IsDir() {
				continue
			}
			if err = RecoverSintedot(filepath.Join(parent, item.Name())); err != nil {
				recovery = append(recovery, fmt.Errorf("%s: %w", item.Name(), err))
			}
		}
	}
	return errors.Join(recovery...)
}
