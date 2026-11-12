package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

type launcherImportSource struct {
	ID            string `json:"id"`
	Launcher      string `json:"launcher"`
	Name          string `json:"name"`
	Game          string `json:"game"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
	Folder        string `json:"folder"`
	icon          string
	used          time.Time
}

var importGameID = regexp.MustCompile("^(?:[0-9]+(?:\\.[0-9]+){1,2}(?:-(?:snapshot|pre|rc)-?[0-9]+)?|[0-9]{2}w[0-9]{2}[a-z])$")
var importLoaderID = regexp.MustCompile("^[0-9][0-9A-Za-z._+\\-]{0,63}$")

func normalizeImportLoader(kind, game, version string) (string, string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "vanilla" {
		return kind, ""
	}
	if kind != "fabric" && kind != "forge" && kind != "neoforge" {
		return "", ""
	}
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, kind+"-")
	version = strings.TrimPrefix(version, game+"-")
	version = strings.TrimSuffix(version, "-"+game)
	if !importLoaderID.MatchString(version) {
		return "", ""
	}
	return kind, version
}

func importVersion(id, base string) (string, string, string) {
	for _, prefix := range []string{"fabric-loader-", "fabric-", "neoforge-", "forge-"} {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		kind := strings.SplitN(prefix, "-", 2)[0]
		version := strings.TrimPrefix(id, prefix)
		if base == "" {
			loader, possibleBase, ok := strings.Cut(version, "-")
			if ok && importGameID.MatchString(possibleBase) {
				version, base = loader, possibleBase
			} else if ok && importGameID.MatchString(loader) && kind == "forge" {
				version, base = possibleBase, loader
			}
		}
		kind, version = normalizeImportLoader(kind, base, version)
		return base, kind, version
	}
	for _, kind := range []string{"forge", "neoforge", "fabric"} {
		game, version, ok := strings.Cut(id, "-"+kind+"-")
		if ok && importGameID.MatchString(game) {
			kind, version = normalizeImportLoader(kind, game, version)
			return game, kind, version
		}
	}
	if importGameID.MatchString(id) && base == "" {
		return id, "vanilla", ""
	}
	return "", "", ""
}

func importName(name, fallback string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if utf8.RuneCountInString(name) < 2 {
		return fallback
	}
	return string([]rune(name)[:min(40, utf8.RuneCountInString(name))])
}

func readImportJSON(path string, value any) bool {
	if !regularImportFile(path) {
		return false
	}
	data, err := readLimitedFile(path, 4<<20)
	if err != nil {
		return false
	}
	if len(data) >= 2 && (bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff})) {
		var order binary.ByteOrder = binary.LittleEndian
		if data[0] == 0xfe {
			order = binary.BigEndian
		}
		encoded := data[2:]
		if len(encoded)%2 != 0 {
			return false
		}
		units := make([]uint16, len(encoded)/2)
		for i := range units {
			units[i] = order.Uint16(encoded[2*i:])
		}
		data = []byte(string(utf16.Decode(units)))
	}
	return json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), value) == nil
}

func importChildren(path string) []os.DirEntry {
	if !safeImportDir(path) {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer dir.Close()
	entries, _ := dir.ReadDir(512)
	return entries
}

func importConfig(path string) map[string]string {
	if !regularImportFile(path) {
		return nil
	}
	data, err := readLimitedFile(path, 256<<10)
	if err != nil {
		return nil
	}
	config := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			config[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return config
}

func detectLauncherImports(roaming, home, documents, selected, target string) []launcherImportSource {
	var candidates []launcherImportSource
	seen := map[string]bool{}
	add := func(source launcherImportSource) {
		if len(candidates) >= 250 || !importGameID.MatchString(source.Game) || source.Loader == "" || !safeImportDir(source.Folder) ||
			pathWithin(target, source.Folder) {
			return
		}
		folder := strings.ToLower(filepath.Clean(source.Folder))
		if seen[folder] {
			return
		}
		seen[folder] = true
		source.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(folder)))
		source.Name = importName(source.Name, "Minecraft "+source.Game)
		candidates = append(candidates, source)
	}
	scan := func(root, kind string) {
		if !safeImportDir(root) {
			return
		}
		parse := func(dir string) {
			switch kind {
			case "prism", "multimc":
				if candidate, ok := readPrismImport(dir, kind); ok {
					add(candidate)
				}
			case "curseforge":
				if candidate, ok := readCurseImport(dir); ok {
					add(candidate)
				}
			case "modrinth":
				for _, candidate := range readModrinthImports(dir) {
					add(candidate)
				}
				if candidate, ok := readModrinthProfile(dir); ok {
					add(candidate)
				}
			}
		}
		parse(root)
		if kind != "modrinth" {
			instances := root
			if !strings.EqualFold(filepath.Base(root), "instances") {
				instances = filepath.Join(root, "instances")
			}
			for _, entry := range importChildren(instances) {
				if entry.IsDir() {
					parse(filepath.Join(instances, entry.Name()))
				}
			}
		} else {
			profiles := root
			if !strings.EqualFold(filepath.Base(root), "profiles") {
				profiles = filepath.Join(root, "profiles")
			}
			for _, entry := range importChildren(profiles) {
				if entry.IsDir() {
					if candidate, ok := readModrinthProfile(filepath.Join(profiles, entry.Name())); ok {
						add(candidate)
					}
				}
			}
		}
	}
	scanPrism := func(root string) {
		scan(root, "prism")
		if config := importConfig(filepath.Join(root, "prismlauncher.cfg")); config["InstanceDir"] != "" {
			path := config["InstanceDir"]
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			if safeImportDir(path) {
				for _, entry := range importChildren(path) {
					if entry.IsDir() {
						if candidate, ok := readPrismImport(filepath.Join(path, entry.Name()), "prism"); ok {
							add(candidate)
						}
					}
				}
			}
		}
	}
	if selected != "" {
		if regularImportFile(filepath.Join(selected, "multimc.cfg")) ||
			regularImportFile(filepath.Join(filepath.Dir(selected), "multimc.cfg")) ||
			strings.EqualFold(filepath.Base(filepath.Dir(selected)), "instances") &&
				regularImportFile(filepath.Join(filepath.Dir(filepath.Dir(selected)), "multimc.cfg")) {
			scan(selected, "multimc")
		} else {
			scanPrism(selected)
		}
		scan(selected, "curseforge")
		scan(selected, "modrinth")
		if strings.EqualFold(filepath.Base(selected), "profiles") || strings.EqualFold(filepath.Base(filepath.Dir(selected)), "profiles") {
			directory := filepath.Dir(selected)
			if strings.EqualFold(filepath.Base(directory), "profiles") {
				directory = filepath.Dir(directory)
			}
			for _, candidate := range readModrinthImports(directory) {
				if pathWithin(selected, candidate.Folder) {
					add(candidate)
				}
			}
		}
		addMinecraftImports(selected, minecraftImportBrand(selected, roaming), add, true)
	} else {
		scanPrism(filepath.Join(roaming, "PrismLauncher"))
		scanPrism(filepath.Join(home, "scoop", "persist", "prismlauncher"))
		for _, root := range []string{filepath.Join(roaming, "MultiMC"), filepath.Join(home, "MultiMC"), filepath.Join(home, "Desktop", "MultiMC"), filepath.Join(home, "Downloads", "MultiMC")} {
			scan(root, "multimc")
		}
		for _, root := range []string{os.Getenv("THESEUS_CONFIG_DIR"), filepath.Join(roaming, "ModrinthApp"), filepath.Join(roaming, "com.modrinth.theseus")} {
			scan(root, "modrinth")
		}
		for _, root := range []string{home, documents} {
			scan(filepath.Join(root, "curseforge", "minecraft"), "curseforge")
		}
		minecraft := filepath.Join(roaming, ".minecraft")
		addMinecraftImports(minecraft, minecraftImportBrand(minecraft, roaming), add, false)
		legacy := filepath.Join(roaming, ".tlauncher", "legacy", "Minecraft")
		addMinecraftImports(legacy, "legacy", add, false)
		addMinecraftImports(filepath.Join(legacy, "game"), "legacy", add, false)
		for _, client := range []struct{ kind, path string }{
			{"legacy", filepath.Join(roaming, ".tlauncher", "legacy.properties")},
			{"tlauncher", filepath.Join(roaming, ".tlauncher", "tlauncher-2.0.properties")},
		} {
			config := importConfig(client.path)
			if folder := config["minecraft.gamedir"]; folder != "" {
				folder = strings.ReplaceAll(folder, "\\:", ":")
				folder = strings.ReplaceAll(folder, "\\\\", "\\")
				addMinecraftImports(folder, client.kind, add, false)
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].used.After(candidates[j].used) })
	return candidates
}

func readPrismImport(dir, kind string) (launcherImportSource, bool) {
	var pack struct {
		Components []struct{ UID, Version string }
	}
	if !safeImportDir(dir) || !readImportJSON(filepath.Join(dir, "mmc-pack.json"), &pack) {
		return launcherImportSource{}, false
	}
	cfg := importConfig(filepath.Join(dir, "instance.cfg"))
	result := launcherImportSource{Launcher: kind, Name: cfg["name"], Loader: "vanilla"}
	if cfg == nil {
		return result, false
	}
	loaderVersion := ""
	for _, component := range pack.Components {
		switch component.UID {
		case "net.minecraft":
			result.Game = component.Version
		case "net.fabricmc.fabric-loader":
			result.Loader, loaderVersion = "fabric", component.Version
		case "net.minecraftforge":
			result.Loader, loaderVersion = "forge", component.Version
		case "net.neoforged":
			result.Loader, loaderVersion = "neoforge", component.Version
		case "org.quiltmc.quilt-loader", "com.mumfrey.liteloader":
			return result, false
		}
	}
	result.Loader, result.LoaderVersion = normalizeImportLoader(result.Loader, result.Game, loaderVersion)
	for _, folder := range []string{".minecraft", "minecraft"} {
		if path := filepath.Join(dir, folder); safeImportDir(path) {
			result.Folder = path
			break
		}
	}
	if cfg["iconKey"] != "" && filepath.Base(cfg["iconKey"]) == cfg["iconKey"] {
		result.icon = filepath.Join(filepath.Dir(filepath.Dir(dir)), "icons", cfg["iconKey"]+".png")
	}
	if result.Name == "" {
		result.Name = filepath.Base(dir)
	}
	return result, result.Folder != ""
}

func readCurseImport(dir string) (launcherImportSource, bool) {
	var data struct {
		Name, GameVersion, ProfileImagePath string
		BaseModLoader                       *struct{ Name string }
	}
	if !safeImportDir(dir) || !readImportJSON(filepath.Join(dir, "minecraftinstance.json"), &data) {
		return launcherImportSource{}, false
	}
	result := launcherImportSource{Launcher: "curseforge", Name: data.Name, Game: data.GameVersion, Loader: "vanilla", Folder: dir}
	if data.BaseModLoader != nil {
		kind, version, ok := strings.Cut(data.BaseModLoader.Name, "-")
		if !ok {
			return result, false
		}
		result.Loader, result.LoaderVersion = normalizeImportLoader(kind, result.Game, version)
	}
	if data.ProfileImagePath != "" {
		path := data.ProfileImagePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if pathWithin(dir, path) {
			result.icon = path
		}
	}
	return result, true
}

func readModrinthProfile(dir string) (launcherImportSource, bool) {
	var data struct {
		Name, Loader string
		Game         string          `json:"game_version"`
		Version      json.RawMessage `json:"loader_version"`
		Metadata     *struct {
			Name, Loader string
			Game         string          `json:"game_version"`
			Version      json.RawMessage `json:"loader_version"`
		}
	}
	if !safeImportDir(dir) || !readImportJSON(filepath.Join(dir, "profile.json"), &data) {
		return launcherImportSource{}, false
	}
	if data.Metadata != nil {
		data.Name, data.Loader, data.Game, data.Version = data.Metadata.Name, data.Metadata.Loader, data.Metadata.Game, data.Metadata.Version
	}
	var version string
	if json.Unmarshal(data.Version, &version) != nil {
		var loader struct{ ID string }
		if json.Unmarshal(data.Version, &loader) == nil {
			version = loader.ID
		}
	}
	kind, version := normalizeImportLoader(data.Loader, data.Game, version)
	return launcherImportSource{Launcher: "modrinth", Name: data.Name, Game: data.Game, Loader: kind, LoaderVersion: version, Folder: dir, icon: filepath.Join(dir, "icon.png")}, true
}

func minecraftImportBrand(dir, roaming string) string {
	if regularImportFile(filepath.Join(dir, "TLauncherProfiles.json")) || regularImportFile(filepath.Join(dir, "tlauncher-2.0.properties")) {
		return "tlauncher"
	}
	legacy := regularImportFile(filepath.Join(roaming, ".tlauncher", "legacy.properties"))
	tlauncher := regularImportFile(filepath.Join(roaming, ".tlauncher", "tlauncher-2.0.properties"))
	if !strings.EqualFold(dir, filepath.Join(roaming, ".minecraft")) || legacy == tlauncher {
		return "minecraft"
	}
	if legacy {
		return "legacy"
	}
	return "tlauncher"
}

func readMinecraftImportVersion(path, id string) (game, loader, version string, used time.Time) {
	var metadata struct {
		InheritsFrom string
		Jar          string
		Libraries    []struct{ Name string }
	}
	if !readImportJSON(path, &metadata) {
		return
	}
	game, loader, version = importVersion(id, metadata.InheritsFrom)
	if loader == "" {
		base := metadata.InheritsFrom
		if !importGameID.MatchString(base) {
			base = metadata.Jar
		}
		if importGameID.MatchString(base) {
			for _, library := range metadata.Libraries {
				pieces := strings.Split(library.Name, ":")
				if len(pieces) < 3 {
					continue
				}
				switch pieces[0] + ":" + pieces[1] {
				case "net.minecraftforge:forge":
					loader = "forge"
				case "net.neoforged:neoforge":
					loader = "neoforge"
				case "net.fabricmc:fabric-loader":
					loader = "fabric"
				default:
					continue
				}
				game = base
				loader, version = normalizeImportLoader(loader, base, pieces[2])
				break
			}
		}
	}
	if info, err := os.Stat(path); err == nil {
		used = info.ModTime()
	}
	return
}

func addMinecraftImports(dir, kind string, add func(launcherImportSource), manual bool) {
	if !safeImportDir(dir) {
		return
	}
	type version struct {
		game, kind, loader string
		used               time.Time
	}
	versions := map[string]version{}
	if manual {
		id := filepath.Base(dir)
		game, loader, loaderVersion, used := readMinecraftImportVersion(filepath.Join(dir, id+".json"), id)
		add(launcherImportSource{Launcher: kind, Name: id, Game: game, Loader: loader, LoaderVersion: loaderVersion, Folder: dir, used: used})
	}
	versionsDir := filepath.Join(dir, "versions")
	for _, entry := range importChildren(versionsDir) {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		folder := filepath.Join(versionsDir, id)
		game, loader, loaderVersion, used := readMinecraftImportVersion(filepath.Join(folder, id+".json"), id)
		if loader == "" || !importGameID.MatchString(game) {
			continue
		}
		versions[id] = version{game, loader, loaderVersion, used}
		if hasImportContent(folder) {
			add(launcherImportSource{Launcher: kind, Name: id, Game: game, Loader: loader, LoaderVersion: loaderVersion, Folder: folder, used: used})
		}
	}
	byVersion := func(id string) version {
		if v, ok := versions[id]; ok {
			return v
		}
		game, loader, loaderVersion := importVersion(id, "")
		return version{game, loader, loaderVersion, time.Time{}}
	}
	var profiles struct {
		Profiles map[string]struct {
			Name, GameDir, LastVersionID string
			LastUsed                     time.Time
		}
	}
	readImportJSON(filepath.Join(dir, "launcher_profiles.json"), &profiles)
	readImportJSON(filepath.Join(dir, "launcher_profiles_microsoft_store.json"), &profiles)
	profileCandidates := []launcherImportSource{}
	for _, profile := range profiles.Profiles {
		v := byVersion(profile.LastVersionID)
		path := profile.GameDir
		if path == "" {
			path = dir
		}
		if v.kind != "" && (hasImportContent(path) || manual || !strings.EqualFold(path, dir)) {
			profileCandidates = append(profileCandidates, launcherImportSource{Launcher: kind, Name: profile.Name, Game: v.game, Loader: v.kind, LoaderVersion: v.loader, Folder: path, used: profile.LastUsed})
		}
	}
	sort.SliceStable(profileCandidates, func(i, j int) bool { return profileCandidates[i].used.After(profileCandidates[j].used) })
	for _, profile := range profileCandidates {
		add(profile)
	}
	sorted := make([]version, 0, len(versions))
	for _, v := range versions {
		sorted = append(sorted, v)
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].used.After(sorted[j].used) })
	if len(sorted) > 0 && (hasImportContent(dir) || manual) {
		v := sorted[0]
		add(launcherImportSource{Launcher: kind, Game: v.game, Loader: v.kind, LoaderVersion: v.loader, Folder: dir, used: v.used})
	}
	for _, entry := range importChildren(filepath.Join(dir, "home")) {
		path := filepath.Join(dir, "home", entry.Name())
		if !entry.IsDir() || !hasImportContent(path) {
			continue
		}
		label := strings.ToLower(entry.Name())
		for _, v := range sorted {
			major, rest, dotted := strings.Cut(v.game, ".")
			family := v.game
			if dotted {
				minor, _, _ := strings.Cut(rest, ".")
				family = major + "." + minor
			}
			if label == v.game || label == family && v.kind == "vanilla" || label == v.kind+"-"+family || label == v.kind+"-"+v.game {
				add(launcherImportSource{Launcher: kind, Name: entry.Name(), Game: v.game, Loader: v.kind, LoaderVersion: v.loader, Folder: path, used: v.used})
				break
			}
		}
	}
}

func hasImportContent(dir string) bool {
	if !safeImportDir(dir) {
		return false
	}
	for _, name := range []string{"mods", "saves", "resourcepacks", "shaderpacks", "config"} {
		if len(importChildren(filepath.Join(dir, name))) != 0 {
			return true
		}
	}
	return regularImportFile(filepath.Join(dir, "servers.dat"))
}
