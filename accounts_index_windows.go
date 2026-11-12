package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sintet/internal/accountfiles"
)

var errInvalidProfileIndex = errors.New("повреждён список аккаунтов")

func (r *profileRoster) indexPath() string  { return filepath.Join(r.root, "accounts.json") }
func (r *profileRoster) backupPath() string { return filepath.Join(r.root, "accounts.json.bak") }

func readProfileIndex(path string) (profileIndex, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return profileIndex{}, nil, err
	}
	var index profileIndex
	if err = json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &index); err != nil || index.Active == "" || index.Profiles == nil {
		return profileIndex{}, data, errInvalidProfileIndex
	}
	return index, data, nil
}

func newProfileRoster(root, base string) *profileRoster {
	r := &profileRoster{root: root, base: base, active: defaultProfile}
	index, primary, err := readProfileIndex(r.indexPath())
	if err == nil {
		r.restoreIndex(index)
		// A shutdown between the two writes can leave a valid but older backup.
		if _, backup, backupErr := readProfileIndex(r.backupPath()); backupErr != nil || !bytes.Equal(primary, backup) {
			_ = accountfiles.Write(r.backupPath(), primary)
		}
		return r
	}
	missing := errors.Is(err, os.ErrNotExist)
	if !missing && !errors.Is(err, errInvalidProfileIndex) {
		r.loadError = fmt.Errorf("не удалось прочитать список аккаунтов: %w", err)
		return r
	}

	if backup, _, backupErr := readProfileIndex(r.backupPath()); backupErr == nil {
		r.restoreIndex(backup)
	} else {
		r.recoverSavedSessions()
		if len(r.slots) == 0 {
			if !missing {
				r.loadError = errInvalidProfileIndex
			}
			return r
		}
	}

	if !missing {
		evidence := filepath.Join(root, fmt.Sprintf("accounts.corrupt-%d.json", time.Now().UnixNano()))
		if err = accountfiles.Write(evidence, primary); err != nil {
			r.loadError = fmt.Errorf("не удалось сохранить копию повреждённого списка: %w", err)
			return r
		}
	}
	if err = r.save(r.active, r.slots); err != nil {
		r.loadError = fmt.Errorf("аккаунты найдены, но не удалось сохранить восстановленный список: %w", err)
	}
	return r
}

func (r *profileRoster) restoreIndex(index profileIndex) {
	for _, record := range index.Profiles {
		if r.find(record.Key) != nil {
			continue
		}
		if slot := r.restore(record); slot != nil {
			r.slots = append(r.slots, slot)
		}
	}
	r.hideDefault = index.HideDefault && len(r.slots) > 0
	if r.find(index.Active) != nil {
		r.active = index.Active
	} else if r.hideDefault {
		r.active = r.slots[len(r.slots)-1].Key
	}
}

func (r *profileRoster) recoverSavedSessions() {
	type recovered struct {
		slot     *profileSlot
		modified time.Time
	}
	var found []recovered
	add := func(slot *profileSlot, path string) {
		if slot == nil {
			return
		}
		if info, err := os.Stat(path); err == nil {
			found = append(found, recovered{slot, info.ModTime()})
		}
	}
	add(r.restore(profileRecord{Key: "legacy-sintet", Provider: "sintet"}), filepath.Join(r.root, "identity.dpapi"))
	add(r.restore(profileRecord{Key: "legacy-ely", Provider: "ely"}), filepath.Join(r.root, "ely.dpapi"))
	dir := filepath.Join(r.root, "accounts")
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		key, ok := strings.CutSuffix(entry.Name(), ".dpapi")
		if !ok || !entry.Type().IsRegular() || !storedProfileKey.MatchString(key) {
			continue
		}
		slot := r.restore(profileRecord{Key: key, Provider: "sintet"})
		if slot == nil {
			slot = r.restore(profileRecord{Key: key, Provider: "ely"})
		}
		if slot == nil {
			slot = r.restore(profileRecord{Key: key, Provider: "microsoft"})
		}
		add(slot, filepath.Join(dir, entry.Name()))
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].modified.After(found[j].modified) })
	seen := make(map[string]bool)
	for _, entry := range found {
		slot := entry.slot
		account := slot.state().Account
		if account == nil {
			continue
		}
		id := slot.Provider + ":" + strings.ToLower(account.ID)
		if seen[id] {
			continue
		}
		seen[id] = true
		r.slots = append(r.slots, slot)
	}
	if len(r.slots) > 0 {
		r.active = r.slots[0].Key
	}
}
