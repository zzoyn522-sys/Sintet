package workshop

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AnalyzeLocalCompatibility adds actionable warnings based on local file
// metadata and the selected Minecraft profile. Unreadable third-party
// manifests are skipped; one malformed mod must not hide the rest of the list.
func AnalyzeLocalCompatibility(dir, kind, game, loader string, items []LocalItem) []LocalItem {
	result := append([]LocalItem(nil), items...)
	if len(result) == 0 {
		return result
	}
	for i := range result {
		result[i].Warnings = nil
	}
	switch kind {
	case "mods":
		analyzeModCompatibility(dir, game, loader, result)
	case "shaders":
		analyzeShaderCompatibility(dir, loader, result)
	case "resources":
		analyzeResourceCompatibility(dir, game, result)
	}
	return result
}

func addLocalWarning(item *LocalItem, warning LocalWarning) {
	for _, current := range item.Warnings {
		if current == warning {
			return
		}
	}
	item.Warnings = append(item.Warnings, warning)
}

func workshopLoader(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fabric":
		return "fabric"
	case "quilt":
		return "quilt"
	case "forge":
		return "forge"
	case "neoforge":
		return "neoforge"
	default:
		return ""
	}
}

func analyzeModCompatibility(dir, game, targetLoader string, items []LocalItem) {
	targetLoader = workshopLoader(targetLoader)
	modsDir := filepath.Join(dir, "mods")
	installed := map[string][]string{}
	localNames := map[string]string{}
	type parsedMod struct {
		index int
		meta  rangeMod
	}
	var parsed []parsedMod
	requirements := map[string][]modRequirement{}
	owners := map[string][]int{}

	for i := range items {
		item := &items[i]
		if item.ModID != "" {
			localNames[strings.ToLower(item.ModID)] = item.Name
		}
		file := filepath.Join(modsDir, item.Filename)
		loader := workshopLoader(item.Loader)
		if loader == "" {
			continue
		}
		if targetLoader != "" && loader != targetLoader && !(loader == "fabric" && targetLoader == "quilt") {
			addLocalWarning(item, LocalWarning{Code: "mod-loader", Detail: item.Loader, Extra: targetLoader})
		}
		meta, err := rangeMetadata(file, loader)
		if err != nil {
			continue
		}
		if len(meta.game) > 0 && !matchesAll(game, meta.game) {
			addLocalWarning(item, LocalWarning{Code: "mod-game", Detail: game})
		}
		for id, versions := range meta.ids {
			installed[strings.ToLower(id)] = append(installed[strings.ToLower(id)], versions...)
		}
		parsed = append(parsed, parsedMod{index: i, meta: meta})
	}

	for _, current := range parsed {
		if !items[current.index].Enabled {
			continue
		}
		for id, rules := range current.meta.deps {
			key := strings.ToLower(id)
			requirements[key] = append(requirements[key], rules...)
			owners[key] = append(owners[key], current.index)
		}
	}
	for id, rules := range requirements {
		versions := installed[id]
		if satisfiesVersions(versions, rules) {
			continue
		}
		code := "dependency-missing"
		if len(versions) > 0 {
			code = "dependency-version"
		}
		name := localNames[id]
		if name == "" {
			name = dependencyDisplayName(id)
		}
		for _, owner := range owners[id] {
			addLocalWarning(&items[owner], LocalWarning{Code: code, Detail: name})
		}
	}
}

func dependencyDisplayName(id string) string {
	switch strings.ToLower(id) {
	case "fabric-api":
		return "Fabric API"
	case "cloth-config":
		return "Cloth Config"
	case "architectury":
		return "Architectury API"
	case "forgeconfigapiport":
		return "Forge Config API Port"
	case "moonlight":
		return "Moonlight Lib"
	case "geckolib":
		return "GeckoLib"
	default:
		return id
	}
}

func analyzeShaderCompatibility(dir, loader string, items []LocalItem) {
	loader = workshopLoader(loader)
	modsDir := filepath.Join(dir, "mods")
	hasShaderLoader := false
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, file := range files {
			if !file.Type().IsRegular() || strings.HasSuffix(strings.ToLower(file.Name()), disabledSuffix) {
				continue
			}
			path := filepath.Join(modsDir, file.Name())
			for _, id := range ModIDs(path) {
				switch {
				case (loader == "fabric" || loader == "quilt") && strings.EqualFold(id, "iris"),
					(loader == "forge" || loader == "neoforge") && strings.EqualFold(id, "oculus"):
					hasShaderLoader = true
				}
			}
			if strings.Contains(strings.ToLower(file.Name()), "optifine") {
				hasShaderLoader = true
			}
		}
	}
	for i := range items {
		file := filepath.Join(dir, "shaderpacks", items[i].Filename)
		archive, err := zip.OpenReader(file)
		if err != nil {
			addLocalWarning(&items[i], LocalWarning{Code: "invalid-archive"})
			continue
		}
		valid := isShaderArchive(&archive.Reader)
		_ = archive.Close()
		if !valid {
			addLocalWarning(&items[i], LocalWarning{Code: "invalid-shader"})
			continue
		}
		if !hasShaderLoader {
			addLocalWarning(&items[i], LocalWarning{Code: "shader-loader"})
		}
	}
}

func analyzeResourceCompatibility(dir, game string, items []LocalItem) {
	targetFormat := resourceFormatForGame(game)
	if targetFormat == 0 {
		return
	}
	for i := range items {
		archive, err := zip.OpenReader(filepath.Join(dir, "resourcepacks", items[i].Filename))
		if err != nil {
			addLocalWarning(&items[i], LocalWarning{Code: "invalid-archive"})
			continue
		}
		meta, err := readPackMetadata(&archive.Reader)
		_ = archive.Close()
		if err != nil {
			addLocalWarning(&items[i], LocalWarning{Code: "invalid-resource"})
			continue
		}
		format := packVersion(meta.Format)
		min, max := packVersion(meta.Minimum), packVersion(meta.Maximum)
		if min != "" && max != "" {
			lo, loOK := packFormatNumber(min)
			hi, hiOK := packFormatNumber(max)
			if loOK && hiOK && (targetFormat < lo || targetFormat > hi) {
				addLocalWarning(&items[i], LocalWarning{Code: "resource-version", Detail: fmt.Sprint(lo, "–", hi), Extra: game + "|" + strconv.Itoa(targetFormat)})
				continue
			}
		}
		if supportedFormatRangeExcludes(meta.Supported, targetFormat) {
			addLocalWarning(&items[i], LocalWarning{Code: "resource-version", Detail: format, Extra: game + "|" + strconv.Itoa(targetFormat)})
			continue
		}
		if n, ok := packFormatNumber(format); ok && n > targetFormat {
			addLocalWarning(&items[i], LocalWarning{Code: "resource-version", Detail: format, Extra: game + "|" + strconv.Itoa(targetFormat)})
		}
	}
}

func supportedFormatRangeExcludes(raw json.RawMessage, target int) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var bounds struct {
		Min json.RawMessage `json:"min_inclusive"`
		Max json.RawMessage `json:"max_inclusive"`
	}
	if json.Unmarshal(raw, &bounds) == nil {
		min, max := packVersion(bounds.Min), packVersion(bounds.Max)
		lo, loOK := packFormatNumber(min)
		hi, hiOK := packFormatNumber(max)
		return loOK && hiOK && (target < lo || target > hi)
	}
	var interval []json.RawMessage
	if json.Unmarshal(raw, &interval) != nil || len(interval) != 2 {
		return false
	}
	lo, loOK := packFormatNumber(packVersion(interval[0]))
	hi, hiOK := packFormatNumber(packVersion(interval[1]))
	return loOK && hiOK && (target < lo || target > hi)
}

func packFormatNumber(value string) (int, bool) {
	if n, err := strconv.Atoi(value); err == nil {
		return n, true
	}
	major, minor, ok := strings.Cut(value, ".")
	if !ok || minor != "0" {
		return 0, false
	}
	n, err := strconv.Atoi(major)
	return n, err == nil
}

func resourceFormatForGame(game string) int {
	var major, minor, patch int
	if _, err := fmt.Sscanf(game, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return 0
	}
	if major != 1 {
		return 0
	}
	switch {
	case minor == 21 && patch >= 9:
		return 69
	case minor == 21 && patch >= 6:
		return 63
	case minor == 21 && patch >= 5:
		return 55
	case minor == 21 && patch >= 4:
		return 46
	case minor == 21 && patch >= 2:
		return 42
	case minor == 21:
		return 34
	case minor == 20 && patch >= 5:
		return 32
	case minor == 20 && patch >= 3:
		return 22
	case minor == 20 && patch == 2:
		return 18
	case minor == 20:
		return 15
	case minor == 19 && patch >= 4:
		return 13
	case minor == 19:
		return 12
	case minor == 18:
		return 8
	case minor == 17:
		return 7
	case minor == 16 && patch >= 2:
		return 6
	case minor == 16:
		return 5
	case minor == 15:
		return 5
	default:
		return 0
	}
}
