package workshop

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/google/uuid"
	"sintet/internal/accountfiles"
)

const maxWorldMetadata = 32 << 20
const deletedWorldsFolder = ".sintet-deleted-worlds"

var errWorldMetadata = errors.New("не удалось прочитать настройки мира")

type LocalWorld struct {
	Folder     string `json:"folder"`
	Name       string `json:"name"`
	Editable   bool   `json:"editable"`
	Difficulty int    `json:"difficulty"`
	Commands   int    `json:"commands"`
}

func WorldDirectory(instance, world string) (string, error) {
	if err := ordinaryContentPath(instance, true); err != nil {
		return "", err
	}
	return localWorldPath(filepath.Join(instance, "saves"), world)
}

func LocalWorldDetails(instance string) ([]LocalWorld, error) {
	folders, err := LocalWorlds(instance)
	if err != nil {
		return nil, err
	}
	worlds := make([]LocalWorld, 0, len(folders))
	for _, folder := range folders {
		row := LocalWorld{Folder: folder, Name: folder, Difficulty: -1, Commands: -1}
		data, fields, err := readWorldMetadata(filepath.Join(instance, "saves", folder, "level.dat"))
		if err == nil {
			if name := fields["LevelName"]; name.kind == 8 {
				row.Editable = true
				if label := decodeWorldName(data[name.start+2 : name.end]); label != "" {
					row.Name = label
				}
			}
			if difficulty := fields["Difficulty"]; difficulty.kind == 1 && data[difficulty.start] <= 3 && fields["hardcore"].kind == 1 && data[fields["hardcore"].start] == 0 {
				if locked := fields["DifficultyLocked"]; locked.kind != 1 || data[locked.start] == 0 {
					row.Difficulty = int(data[difficulty.start])
				}
			}
		}
		if commands := fields["allowCommands"]; commands.kind == 1 && data[commands.start] <= 1 {
			row.Commands = int(data[commands.start])
		}
		worlds = append(worlds, row)
	}
	return worlds, nil
}

func EditWorld(instance, folder, name string, difficulty, commands int) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 48 || strings.ContainsFunc(name, unicode.IsControl) || difficulty < -1 || difficulty > 3 {
		return errors.New("введи название мира до 48 символов и выбери допустимую сложность")
	}
	dir, err := WorldDirectory(instance, folder)
	if err != nil {
		return err
	}
	file := filepath.Join(dir, "level.dat")
	data, fields, err := readWorldMetadata(file)
	if err != nil {
		return err
	}
	title := fields["LevelName"]
	if title.kind != 8 {
		return errWorldMetadata
	}
	if commands != -1 {
		value := fields["allowCommands"]
		if commands < 0 || commands > 1 || value.kind != 1 || data[value.start] > 1 {
			return errors.New("настройка команд недоступна для этого мира")
		}
		data[value.start] = byte(commands)
	}
	if difficulty >= 0 {
		value, hardcore, locked := fields["Difficulty"], fields["hardcore"], fields["DifficultyLocked"]
		if value.kind != 1 || hardcore.kind != 1 || data[hardcore.start] != 0 || locked.kind == 1 && data[locked.start] != 0 {
			return errors.New("сложность этого мира заблокирована")
		}
		data[value.start] = byte(difficulty)
	}
	return writeWorldMetadata(file, data[:title.start], encodeWorldName(name), data[title.end:])
}

func writeWorldMetadata(file string, chunks ...[]byte) error {
	var result bytes.Buffer
	out := gzip.NewWriter(&result)
	for _, chunk := range chunks {
		if _, err := out.Write(chunk); err != nil {
			return err
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	return accountfiles.Write(file, result.Bytes())
}

func TrashWorld(instance, folder string) (string, error) {
	dir, err := WorldDirectory(instance, folder)
	if err != nil {
		return "", err
	}
	trash := filepath.Join(instance, deletedWorldsFolder)
	if err := os.Mkdir(trash, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := ordinaryContentPath(trash, true); err != nil {
		return "", err
	}
	id := uuid.NewString()
	slot := filepath.Join(trash, id)
	if err := os.Mkdir(slot, 0700); err != nil {
		return "", err
	}
	if err := os.Rename(dir, filepath.Join(slot, folder)); err != nil {
		_ = os.Remove(slot)
		return "", err
	}
	return id, nil
}

func RestoreWorld(instance, id string) error {
	key, err := uuid.Parse(id)
	if err != nil || key.String() != id {
		return errors.New("копия удалённого мира недоступна")
	}
	saves := filepath.Join(instance, "saves")
	trash := filepath.Join(instance, deletedWorldsFolder)
	slot := filepath.Join(trash, id)
	for _, dir := range []string{instance, saves, trash, slot} {
		if err := ordinaryContentPath(dir, true); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(slot)
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return errors.New("копия удалённого мира недоступна")
	}
	folder := entries[0].Name()
	world, err := localWorldPath(slot, folder)
	if err != nil {
		return err
	}
	destination := filepath.Join(saves, folder)
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return errors.New("мир с такой папкой уже существует")
	}
	if err := os.Rename(world, destination); err != nil {
		return err
	}
	_ = os.Remove(slot)
	return nil
}

type worldTag struct {
	kind       byte
	start, end int
}

func readWorldMetadata(file string) ([]byte, map[string]worldTag, error) {
	if err := ordinaryContentPath(file, false); err != nil {
		return nil, nil, err
	}
	input, err := os.Open(file)
	if err != nil {
		return nil, nil, err
	}
	defer input.Close()
	reader, err := gzip.NewReader(io.LimitReader(input, maxWorldMetadata+1))
	if err != nil {
		return nil, nil, errWorldMetadata
	}
	defer reader.Close()
	reader.Multistream(false)
	data, err := io.ReadAll(io.LimitReader(reader, maxWorldMetadata+1))
	if err != nil || len(data) > maxWorldMetadata {
		return nil, nil, errWorldMetadata
	}
	scan := worldNBT{data: data}
	if scan.number(1) != 10 {
		return nil, nil, errWorldMetadata
	}
	scan.name()
	root := scan.fields(0)
	if scan.invalid || scan.pos != len(data) || root["Data"].kind != 10 {
		return nil, nil, errWorldMetadata
	}
	world := root["Data"]
	scan.pos = world.start
	fields := scan.fields(1)
	if scan.invalid || scan.pos != world.end {
		return nil, nil, errWorldMetadata
	}
	return data, fields, nil
}

// Skip unedited NBT tags by length so their original bytes are preserved.
type worldNBT struct {
	data    []byte
	pos     int
	invalid bool
}

func (n *worldNBT) take(size int) []byte {
	if n.invalid || size < 0 || size > len(n.data)-n.pos {
		n.invalid = true
		return nil
	}
	out := n.data[n.pos : n.pos+size]
	n.pos += size
	return out
}

func (n *worldNBT) number(size int) int {
	b := n.take(size)
	if len(b) != size {
		return 0
	}
	switch size {
	case 1:
		return int(b[0])
	case 2:
		return int(binary.BigEndian.Uint16(b))
	default:
		return int(int32(binary.BigEndian.Uint32(b)))
	}
}

func (n *worldNBT) name() string {
	return string(n.take(n.number(2)))
}

func (n *worldNBT) fields(depth int) map[string]worldTag {
	fields := make(map[string]worldTag)
	for !n.invalid {
		kind := byte(n.number(1))
		if kind == 0 {
			break
		}
		name, start := n.name(), n.pos
		n.skip(kind, depth+1)
		if depth == 0 && name == "Data" || depth == 1 && slices.Contains([]string{"LevelName", "Difficulty", "DifficultyLocked", "hardcore", "allowCommands", "DataPacks"}, name) || depth == 2 && (name == "Enabled" || name == "Disabled") {
			if _, exists := fields[name]; exists {
				n.invalid = true
			}
			fields[name] = worldTag{kind, start, n.pos}
		}
	}
	return fields
}

func (n *worldNBT) skip(kind byte, depth int) {
	if n.invalid || depth > 64 {
		n.invalid = true
		return
	}
	switch kind {
	case 1:
		n.take(1)
	case 2:
		n.take(2)
	case 3, 5:
		n.take(4)
	case 4, 6:
		n.take(8)
	case 7, 11, 12:
		width := 1
		if kind == 11 {
			width = 4
		}
		if kind == 12 {
			width = 8
		}
		n.take(n.number(4) * width)
	case 8:
		n.take(n.number(2))
	case 9:
		item, count := byte(n.number(1)), n.number(4)
		if count < 0 || count > len(n.data)-n.pos || count > 0 && item == 0 {
			n.invalid = true
		}
		for i := 0; i < count && !n.invalid; i++ {
			n.skip(item, depth+1)
		}
	case 10:
		for !n.invalid {
			child := byte(n.number(1))
			if child == 0 {
				break
			}
			n.take(n.number(2))
			n.skip(child, depth+1)
		}
	default:
		n.invalid = true
	}
}

func encodeWorldName(name string) []byte {
	data := make([]byte, 2, len(name)+2)
	for _, unit := range utf16.Encode([]rune(name)) {
		switch {
		case unit > 0 && unit <= 0x7f:
			data = append(data, byte(unit))
		case unit <= 0x7ff:
			data = append(data, 0xc0|byte(unit>>6), 0x80|byte(unit&0x3f))
		default:
			data = append(data, 0xe0|byte(unit>>12), 0x80|byte((unit>>6)&0x3f), 0x80|byte(unit&0x3f))
		}
	}
	binary.BigEndian.PutUint16(data[:2], uint16(len(data)-2))
	return data
}

func decodeWorldName(data []byte) string {
	units := make([]uint16, 0, len(data))
	for len(data) != 0 {
		value := uint16(data[0])
		width := 1
		switch {
		case value < 0x80:
		case value&0xe0 == 0xc0 && len(data) >= 2 && data[1]&0xc0 == 0x80:
			value, width = (value&0x1f)<<6|uint16(data[1]&0x3f), 2
		case value&0xf0 == 0xe0 && len(data) >= 3 && data[1]&0xc0 == 0x80 && data[2]&0xc0 == 0x80:
			value, width = (value&0x0f)<<12|uint16(data[1]&0x3f)<<6|uint16(data[2]&0x3f), 3
		default:
			return strings.ToValidUTF8(string(data), "�")
		}
		units = append(units, value)
		data = data[width:]
	}
	return string(utf16.Decode(units))
}
