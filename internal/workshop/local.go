package workshop

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type LocalItem struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Filename    string `json:"filename"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Project     string `json:"project"`
	Enabled     bool   `json:"enabled"`
	Size        int64  `json:"size"`
	Icon        string `json:"icon"`
	Loader      string `json:"loader"`
	ModID       string `json:"modID,omitempty"`
	Authors     string `json:"authors,omitempty"`
	PackFormat  string `json:"packFormat,omitempty"`
}

func zipBytes(file *zip.Reader, name string, limit uint64) ([]byte, error) {
	for _, entry := range file.File {
		if entry.Name == name {
			if entry.UncompressedSize64 > limit {
				return nil, errors.New("слишком большой файл метаданных")
			}
			r, err := entry.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
			if uint64(len(data)) > limit {
				return nil, errors.New("слишком большой файл метаданных")
			}
			return data, err
		}
	}
	return nil, os.ErrNotExist
}

var tomlName = regexp.MustCompile(`(?m)^\s*displayName\s*=\s*["']([^"']+)["']`)
var tomlVersion = regexp.MustCompile(`(?m)^\s*version\s*=\s*["']([^"']+)["']`)
var tomlAuthors = regexp.MustCompile(`(?m)^\s*authors\s*=\s*["']([^"']+)["']`)
var tomlDescription = regexp.MustCompile(`(?m)^[ \t]*description[ \t]*=[ \t]*(?:"""([\s\S]*?)"""|'''([\s\S]*?)'''|"((?:\\.|[^"\\])*)"|'([^']*)')`)
var modTable = regexp.MustCompile(`(?m)^[ \t]*\[\[mods\]\][ \t]*(?:#[^\r\n]*)?\r?$`)
var tomlTable = regexp.MustCompile(`(?m)^[ \t]*\[`)
var manifestVersion = regexp.MustCompile(`(?m)^Implementation-Version: ([^\r\n]+)`)
var tomlID = regexp.MustCompile(`(?m)^\s*modId\s*=\s*["']([^"']+)["']`)

func modIDs(path string) []string {
	jar, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer jar.Close()
	if data, err := zipBytes(&jar.Reader, "fabric.mod.json", 256<<10); err == nil {
		var meta struct{ ID string }
		if json.Unmarshal(data, &meta) == nil && meta.ID != "" {
			return []string{meta.ID}
		}
	}
	for _, name := range []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"} {
		if data, err := zipBytes(&jar.Reader, name, 256<<10); err == nil {
			result := []string{}
			for _, match := range tomlID.FindAllSubmatch(data, -1) {
				result = append(result, string(match[1]))
			}
			return result
		}
	}
	return nil
}

type packMetadata struct {
	Description any             `json:"description"`
	Format      json.RawMessage `json:"pack_format"`
	Minimum     json.RawMessage `json:"min_format"`
	Maximum     json.RawMessage `json:"max_format"`
}

func readPackMetadata(archive *zip.Reader) (packMetadata, error) {
	data, err := zipBytes(archive, "pack.mcmeta", 128<<10)
	if err != nil {
		return packMetadata{}, err
	}
	var metadata struct{ Pack *packMetadata }
	if json.Unmarshal(data, &metadata) != nil || metadata.Pack == nil {
		return packMetadata{}, errors.New("неверный pack.mcmeta")
	}
	pack := *metadata.Pack
	if packVersion(pack.Format) == "" && (packVersion(pack.Minimum) == "" || packVersion(pack.Maximum) == "") {
		return packMetadata{}, errors.New("в pack.mcmeta не указан формат")
	}
	return pack, nil
}

func packVersion(raw json.RawMessage) string {
	var number int
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if json.Unmarshal(raw, &number) == nil && number >= 0 {
		return strconv.Itoa(number)
	}
	var version []int
	if json.Unmarshal(raw, &version) == nil {
		if len(version) == 1 && version[0] >= 0 {
			return strconv.Itoa(version[0])
		}
		if len(version) == 2 && version[0] >= 0 && version[1] >= 0 {
			return fmt.Sprintf("%d.%d", version[0], version[1])
		}
	}
	return ""
}

func writePackText(out *strings.Builder, value any, depth int) {
	if depth >= 32 {
		return
	}
	switch value := value.(type) {
	case string:
		out.WriteString(value)
	case []any:
		for _, part := range value {
			writePackText(out, part, depth+1)
		}
	case map[string]any:
		writePackText(out, value["text"], depth+1)
		if value["text"] == nil {
			writePackText(out, value["fallback"], depth+1)
		}
		writePackText(out, value["extra"], depth+1)
	}
}

func localMetadata(path, kind string, item *LocalItem) {
	if kind == "shaders" {
		return
	}
	jar, err := zip.OpenReader(path)
	if err != nil {
		item.Description = "Не удалось прочитать архив"
		return
	}
	defer jar.Close()
	if kind == "resources" {
		pack, err := readPackMetadata(&jar.Reader)
		if err != nil {
			item.Description = "Не удалось прочитать pack.mcmeta"
			return
		}
		var description strings.Builder
		writePackText(&description, pack.Description, 0)
		item.Description = description.String()
		item.PackFormat = packVersion(pack.Format)
		if min, max := packVersion(pack.Minimum), packVersion(pack.Maximum); min != "" && max != "" {
			item.PackFormat = min
			if min != max {
				item.PackFormat += " – " + max
			}
		}
		return
	}
	if data, err := zipBytes(&jar.Reader, "fabric.mod.json", 256<<10); err == nil {
		var meta struct {
			ID, Name, Version, Description string
			Authors                        []json.RawMessage
		}
		if json.Unmarshal(data, &meta) == nil {
			if meta.Name != "" {
				item.Name = meta.Name
			}
			authors := make([]string, 0, len(meta.Authors))
			for _, value := range meta.Authors {
				var name string
				if json.Unmarshal(value, &name) != nil {
					var person struct{ Name string }
					if json.Unmarshal(value, &person) == nil {
						name = person.Name
					}
				}
				if name != "" {
					authors = append(authors, name)
				}
			}
			item.ModID, item.Version, item.Description = meta.ID, meta.Version, meta.Description
			item.Authors, item.Loader = strings.Join(authors, ", "), "Fabric"
			return
		}
	}
	for _, name := range []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"} {
		if data, err := zipBytes(&jar.Reader, name, 256<<10); err == nil {
			if table := modTable.FindIndex(data); table != nil {
				data = data[table[1]:]
				if next := tomlTable.FindIndex(data); next != nil {
					data = data[:next[0]]
				}
			}
			if match := tomlName.FindSubmatch(data); len(match) > 1 {
				item.Name = string(match[1])
			}
			if match := tomlVersion.FindSubmatch(data); len(match) > 1 {
				version := string(match[1])
				if version == "${file.jarVersion}" {
					if manifest, e := zipBytes(&jar.Reader, "META-INF/MANIFEST.MF", 128<<10); e == nil {
						if value := manifestVersion.FindSubmatch(manifest); len(value) > 1 {
							version = string(value[1])
						}
					}
				}
				if !strings.Contains(version, "${") {
					item.Version = version
				}
			}
			if match := tomlID.FindSubmatch(data); len(match) > 1 {
				item.ModID = string(match[1])
			}
			if match := tomlAuthors.FindSubmatch(data); len(match) > 1 {
				item.Authors = string(match[1])
			}
			if match := tomlDescription.FindSubmatch(data); len(match) > 1 {
				for i, value := range match[1:] {
					if len(value) == 0 {
						continue
					}
					text := string(value)
					if i == 2 {
						if unquoted, e := strconv.Unquote("\"" + text + "\""); e == nil {
							text = unquoted
						}
					}
					item.Description = strings.TrimSpace(text)
					break
				}
			}
			item.Loader = "Forge"
			if strings.Contains(name, "neoforge") {
				item.Loader = "NeoForge"
			}
			return
		}
	}
	if data, err := zipBytes(&jar.Reader, "mcmod.info", 256<<10); err == nil {
		type legacyMod struct {
			ModID, Name, Version, Description string
			AuthorList                        []string
		}
		var mods []legacyMod
		if json.Unmarshal(data, &mods) != nil {
			var legacy struct{ ModList []legacyMod }
			if json.Unmarshal(data, &legacy) == nil {
				mods = legacy.ModList
			}
		}
		if len(mods) > 0 {
			mod := mods[0]
			if mod.Name != "" {
				item.Name = mod.Name
			}
			item.ModID, item.Version, item.Description = mod.ModID, mod.Version, mod.Description
			item.Authors, item.Loader = strings.Join(mod.AuthorList, ", "), "Forge"
		}
	}
}

func Scan(dir, kind string) ([]LocalItem, error) {
	folder, err := localFolder(dir, kind)
	if err != nil {
		return nil, err
	}
	if err := localDirectory(folder); err != nil {
		if os.IsNotExist(err) {
			return []LocalItem{}, nil
		}
		return nil, err
	}
	rows, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	records, _, err := localRegistry(dir, kind)
	if err != nil {
		return nil, err
	}
	byFilename := map[string]Installed{}
	for _, record := range records {
		byFilename[strings.ToLower(record.Filename)] = record
	}
	result := []LocalItem{}
	for _, file := range rows {
		if !file.Type().IsRegular() {
			continue
		}
		name := file.Name()
		base := strings.TrimSuffix(name, disabledSuffix)
		if !safeContentFilename(base, kind) {
			continue
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		title := strings.TrimSuffix(base, filepath.Ext(base))
		item := LocalItem{Name: title, Filename: name, Enabled: name == base, Size: info.Size(), Provider: "Локальный файл"}
		localMetadata(filepath.Join(folder, name), kind, &item)
		if record, ok := byFilename[strings.ToLower(base)]; ok {
			item.Provider = record.Provider
			item.Project = record.Project
			item.Icon = record.Icon
			if item.Name == title && record.Name != "" {
				item.Name = record.Name
			}
		}
		result = append(result, item)
	}
	return result, nil
}
func Toggle(dir, filename string, enabled bool) error {
	return ToggleLocal(dir, "mods", filename, enabled)
}
