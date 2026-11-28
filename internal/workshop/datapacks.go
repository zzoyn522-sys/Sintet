package workshop

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var errDataPackSettings = errors.New("не удалось прочитать настройки дата-паков этого мира")

type WorldDataPack struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
}

type WorldContent struct {
	World string   `json:"world"`
	Packs []string `json:"packs"`
}

type WorldDataPackSettings struct {
	Folder   string          `json:"folder"`
	Name     string          `json:"name"`
	Editable bool            `json:"editable"`
	Packs    []WorldDataPack `json:"packs"`
}

func WorldDataPackDirectory(instance, world string) (string, error) {
	dir, err := WorldDirectory(instance, world)
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "datapacks")
	return dir, ordinaryContentPath(dir, true)
}

func installedDataPacks(dir string) ([]string, error) {
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, child := range children {
		file := filepath.Join(dir, child.Name())
		if child.IsDir() {
			if ordinaryContentPath(file, true) == nil && ordinaryContentPath(filepath.Join(file, "pack.mcmeta"), false) == nil {
				result = append(result, child.Name())
			}
		} else if strings.EqualFold(filepath.Ext(file), ".zip") && ordinaryContentPath(file, false) == nil {
			result = append(result, child.Name())
		}
	}
	return result, nil
}

func LocalWorldDataPacks(instance string) ([]WorldContent, error) {
	worlds, err := LocalWorlds(instance)
	if err != nil {
		return nil, err
	}
	result := make([]WorldContent, 0, len(worlds))
	for _, folder := range worlds {
		content := WorldContent{World: folder}
		dir, err := WorldDataPackDirectory(instance, folder)
		if err == nil {
			content.Packs, err = installedDataPacks(dir)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, content)
	}
	return result, nil
}

func LocalWorldDataPackSettings(instance string) ([]WorldDataPackSettings, error) {
	worlds, err := LocalWorldDataPacks(instance)
	if err != nil {
		return nil, err
	}
	result := make([]WorldDataPackSettings, 0, len(worlds))
	for _, world := range worlds {
		if len(world.Packs) == 0 {
			continue
		}
		data, tags, err := readWorldMetadata(filepath.Join(instance, "saves", world.World, "level.dat"))
		content := WorldDataPackSettings{Folder: world.World, Name: world.World}
		var enabled, disabled dataPackList
		if err == nil {
			if title := tags["LevelName"]; title.kind == 8 {
				if name := decodeWorldName(data[title.start+2 : title.end]); name != "" {
					content.Name = name
				}
			}
			enabled, disabled, err = readDataPackLists(data, tags["DataPacks"])
			content.Editable = err == nil
		}
		for _, name := range world.Packs {
			pack := WorldDataPack{Name: name}
			if content.Editable {
				pack.Mode = dataPackMode(dataPackID(name), enabled, disabled)
			}
			content.Packs = append(content.Packs, pack)
		}
		result = append(result, content)
	}
	return result, nil
}

func EditWorldDataPacks(instance, world string, changes []WorldDataPack) error {
	dir, err := WorldDataPackDirectory(instance, world)
	if err != nil {
		return err
	}
	names, err := installedDataPacks(dir)
	if err != nil {
		return err
	}
	installed := make(map[string]bool, len(names))
	for _, name := range names {
		installed[name] = true
	}

	file := filepath.Join(filepath.Dir(dir), "level.dat")
	data, tags, err := readWorldMetadata(file)
	if err != nil {
		return err
	}
	enabled, disabled, err := readDataPackLists(data, tags["DataPacks"])
	if err != nil {
		return err
	}
	for _, change := range changes {
		if !installed[change.Name] {
			return errors.New("дата-пак больше не доступен в этом мире")
		}
		id := dataPackID(change.Name)
		if change.Mode == dataPackMode(id, enabled, disabled) {
			continue
		}
		enabled.remove(id)
		disabled.remove(id)
		switch change.Mode {
		case "auto":
		case "enabled":
			enabled.ids = append(enabled.ids, id)
		case "disabled":
			disabled.ids = append(disabled.ids, id)
		default:
			return errors.New("неизвестный режим дата-пака")
		}
	}
	first, second := enabled, disabled
	if first.start > second.start {
		first, second = second, first
	}
	return writeWorldMetadata(file, data[:first.start], first.encode(), data[first.end:second.start], second.encode(), data[second.end:])
}

func dataPackID(name string) string {
	return string(encodeWorldName("file/" + name)[2:])
}

func dataPackMode(id string, enabled, disabled dataPackList) string {
	if slices.Contains(enabled.ids, id) {
		return "enabled"
	}
	if slices.Contains(disabled.ids, id) {
		return "disabled"
	}
	return "auto"
}

type dataPackList struct {
	worldTag
	ids []string
}

func (l *dataPackList) remove(id string) {
	l.ids = slices.DeleteFunc(l.ids, func(entry string) bool { return entry == id })
}

func (l dataPackList) encode() []byte {
	data := binary.BigEndian.AppendUint32([]byte{8}, uint32(len(l.ids)))
	for _, id := range l.ids {
		data = binary.BigEndian.AppendUint16(data, uint16(len(id)))
		data = append(data, id...)
	}
	return data
}

func readDataPackLists(data []byte, compound worldTag) (enabled, disabled dataPackList, err error) {
	if compound.kind != 10 {
		return enabled, disabled, errDataPackSettings
	}
	scan := worldNBT{data: data, pos: compound.start}
	tags := scan.fields(2)
	if scan.invalid || scan.pos != compound.end {
		return enabled, disabled, errDataPackSettings
	}
	for i, label := range []string{"Enabled", "Disabled"} {
		tag := tags[label]
		if tag.kind != 9 {
			return enabled, disabled, errDataPackSettings
		}
		list := worldNBT{data: data[:tag.end], pos: tag.start}
		kind, count := list.number(1), list.number(4)
		if kind != 8 && !(kind == 0 && count == 0) {
			return enabled, disabled, errDataPackSettings
		}
		row := dataPackList{worldTag: tag}
		for j := 0; j < count && !list.invalid; j++ {
			row.ids = append(row.ids, list.name())
		}
		if list.invalid || list.pos != tag.end {
			return enabled, disabled, errDataPackSettings
		}
		if i == 0 {
			enabled = row
		} else {
			disabled = row
		}
	}
	return enabled, disabled, nil
}
