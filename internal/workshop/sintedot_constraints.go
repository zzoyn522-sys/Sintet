package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	maven "github.com/masahiro331/go-mvn-version"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type SintedotRequirement struct {
	Mod   string `json:"mod"`
	File  string `json:"file"`
	Range string `json:"range"`
}
type SintedotConflict struct {
	ModID        string                `json:"modId"`
	Installed    []string              `json:"installed"`
	Requirements []SintedotRequirement `json:"requirements"`
}
type modRequirement struct {
	SintedotRequirement
	match versionRange
}
type rangeMod struct {
	ids     map[string][]string
	primary map[string]string
	deps    map[string][]modRequirement
}
type rangeGraph struct {
	mods     map[string]rangeMod
	required map[string][]modRequirement
	files    map[string][]string
}

var errSintedotRanges = errors.New("требования к версиям модов конфликтуют")

func scanRangeGraph(folder, loader string) (rangeGraph, error) {
	g := rangeGraph{map[string]rangeMod{}, map[string][]modRequirement{}, map[string][]string{}}
	files, err := os.ReadDir(folder)
	if err != nil {
		return g, err
	}
	for _, f := range files {
		if !f.Type().IsRegular() || !safeFilename(f.Name()) {
			continue
		}
		mod, err := rangeMetadata(filepath.Join(folder, f.Name()), loader)
		if err != nil {
			return g, fmt.Errorf("%s: %w", f.Name(), err)
		}
		g.mods[f.Name()] = mod
		for id, rules := range mod.deps {
			g.required[id] = append(g.required[id], rules...)
		}
		for id := range mod.primary {
			g.files[id] = append(g.files[id], f.Name())
		}
	}
	return g, nil
}

func matchesAll(version string, rules []modRequirement) bool {
	for _, r := range rules {
		if !r.match(version) {
			return false
		}
	}
	return true
}

func satisfiesVersions(versions []string, rules []modRequirement) bool {
	return slices.ContainsFunc(versions, func(v string) bool { return matchesAll(v, rules) })
}

func (g rangeGraph) conflict(id string) SintedotConflict {
	c := SintedotConflict{ModID: id, Installed: []string{}, Requirements: []SintedotRequirement{}}
	for _, mod := range g.mods {
		for _, v := range mod.ids[id] {
			if !slices.Contains(c.Installed, v) {
				c.Installed = append(c.Installed, v)
			}
		}
	}
	sort.Strings(c.Installed)
	for _, r := range g.required[id] {
		c.Requirements = append(c.Requirements, r.SintedotRequirement)
	}
	return c
}

func (g rangeGraph) satisfied(id string) bool {
	for _, mod := range g.mods {
		if satisfiesVersions(mod.ids[id], g.required[id]) {
			return true
		}
	}
	return false
}

func (g rangeGraph) conflicts() []SintedotConflict {
	var matches []SintedotConflict
	for id, rules := range g.required {
		if g.satisfied(id) {
			continue
		}
		conflict := g.conflict(id)
		if len(conflict.Installed) > 0 || len(rules) > 1 {
			matches = append(matches, conflict)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ModID < matches[j].ModID })
	return matches
}

func newerLocalVersion(loader, a, b string) bool {
	if loader == "fabric" || loader == "quilt" {
		va, ok := parseFabricVersion(a)
		vb, bOK := parseFabricVersion(b)
		return ok && bOK && compareFabric(va, vb) > 0
	}
	va, err := maven.NewVersion(a)
	vb, bErr := maven.NewVersion(b)
	return err == nil && bErr == nil && va.Compare(vb) > 0
}

func (g rangeGraph) duplicatePlan(loader string, registered map[string]string) ([]string, []SintedotConflict) {
	var disabled []string
	var conflicts []SintedotConflict
	ids := make([]string, 0, len(g.files))
	for id := range g.files {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		files := g.files[id]
		if len(files) < 2 {
			continue
		}
		keep := ""
		for _, file := range files {
			mod := g.mods[file]
			if len(mod.primary) != 1 {
				continue
			}
			safe := true
			for provided, versions := range mod.ids {
				if !satisfiesVersions(versions, g.required[provided]) {
					safe = false
				}
			}
			if !safe {
				continue
			}
			if keep == "" || newerLocalVersion(loader, mod.primary[id], g.mods[keep].primary[id]) ||
				mod.primary[id] == g.mods[keep].primary[id] && registered[keep] == "" && registered[file] != "" {
				keep = file
			}
		}
		safe := keep != ""
		for _, file := range files {
			if file == keep || !safe {
				continue
			}
			mod := g.mods[file]
			safe = len(mod.primary) == 1
			for provided := range mod.ids {
				if _, exists := g.mods[keep].ids[provided]; !exists && len(g.required[provided]) > 0 {
					safe = false
				}
			}
		}
		if !safe {
			conflicts = append(conflicts, g.conflict(id))
			continue
		}
		for _, file := range files {
			if file != keep {
				disabled = append(disabled, file)
			}
		}
	}
	return disabled, conflicts
}

func (m *Manager) rangeCatalogueVersion(ctx context.Context, stage string, target Target, record Installed, id string, rules []modRequirement, allowed func(Version) bool) (Version, error) {
	rows, err := m.Client.Versions(ctx, record.Provider, record.Project, target.Game, target.Loader)
	if err != nil {
		return Version{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Published.After(rows[j].Published) })
	dir, err := os.MkdirTemp(stage, ".range-")
	if err != nil {
		return Version{}, err
	}
	defer os.RemoveAll(dir)
	for _, v := range rows {
		if v.Project != record.Project || !v.Compatible(target.Game, target.Loader) || !verifiableModDownload(v) || !allowed(v) {
			continue
		}
		path := filepath.Join(dir, v.Filename)
		if _, err = m.download(ctx, v, path, func(int64, int64) {}); err != nil {
			return Version{}, err
		}
		mod, err := rangeMetadata(path, target.Loader)
		_ = os.Remove(path)
		if err != nil {
			continue
		}
		if satisfiesVersions(mod.ids[id], rules) {
			return v, nil
		}
	}
	return Version{}, nil
}

func rangeMetadata(path, loader string) (rangeMod, error) {
	mod := rangeMod{map[string][]string{}, map[string]string{}, map[string][]modRequirement{}}
	jar, err := zip.OpenReader(path)
	if err != nil {
		return mod, err
	}
	defer jar.Close()
	budget := uint64(32 << 20)
	err = rangeArchive(&jar.Reader, filepath.Base(path), loader, &mod, &budget, 0)
	return mod, err
}

func rangeArchive(jar *zip.Reader, file, loader string, mod *rangeMod, budget *uint64, depth int) error {
	var paths []string
	if loader == "fabric" || loader == "quilt" {
		raw, err := zipBytes(jar, "fabric.mod.json", 256<<10)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var f struct {
			ID, Version, Environment string
			Provides                 []string
			Depends                  map[string]json.RawMessage
			Jars                     []struct{ File string }
		}
		if err = json.Unmarshal(raw, &f); err != nil {
			return err
		}
		if f.Environment == "server" {
			return nil
		}
		if f.ID != "" {
			mod.ids[f.ID] = append(mod.ids[f.ID], f.Version)
			if depth == 0 {
				mod.primary[f.ID] = f.Version
			}
		}
		for _, alias := range f.Provides {
			mod.ids[alias] = append(mod.ids[alias], f.Version)
		}
		for id, spec := range f.Depends {
			if runtimeRequirement(id) {
				continue
			}
			match, err := parseFabricRange(spec)
			if err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			var raw string
			if json.Unmarshal(spec, &raw) != nil {
				raw = string(spec)
			}
			mod.deps[id] = append(mod.deps[id], modRequirement{SintedotRequirement{f.ID, file, raw}, match})
		}
		for _, child := range f.Jars {
			paths = append(paths, child.File)
		}
	} else {
		if err := forgeRangeMetadata(jar, file, loader, mod, depth); err != nil {
			return err
		}
		raw, err := zipBytes(jar, "META-INF/jarjar/metadata.json", 256<<10)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var nested struct{ Jars []struct{ Path string } }
		if err = json.Unmarshal(raw, &nested); err != nil {
			return err
		}
		for _, child := range nested.Jars {
			paths = append(paths, child.Path)
		}
	}
	if depth >= 3 {
		return nil
	}
	for i, path := range paths {
		if i >= 128 || *budget == 0 {
			break
		}
		raw, err := zipBytes(jar, path, *budget)
		if err != nil {
			continue
		}
		*budget -= uint64(len(raw))
		nested, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			continue
		}
		if err = rangeArchive(nested, file, loader, mod, budget, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func forgeRangeMetadata(jar *zip.Reader, file, loader string, mod *rangeMod, depth int) error {
	names := []string{"META-INF/mods.toml"}
	if loader == "neoforge" {
		names = []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}
	}
	for _, manifest := range names {
		raw, err := zipBytes(jar, manifest, 256<<10)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var f struct {
			Mods []struct {
				ID      string `toml:"modId"`
				Version string
			}
			Dependencies map[string][]struct {
				ID         string `toml:"modId"`
				Range      string `toml:"versionRange"`
				Type, Side string
				Mandatory  *bool
			}
		}
		if err = toml.Unmarshal(raw, &f); err != nil {
			return err
		}
		for _, entry := range f.Mods {
			v := entry.Version
			if v == "${file.jarVersion}" {
				if raw, err := zipBytes(jar, "META-INF/MANIFEST.MF", 256<<10); err == nil {
					if match := manifestVersion.FindSubmatch(raw); len(match) > 1 {
						v = strings.TrimSpace(string(match[1]))
					}
				}
			}
			if entry.ID == "" {
				continue
			}
			mod.ids[entry.ID] = append(mod.ids[entry.ID], v)
			if depth == 0 {
				mod.primary[entry.ID] = v
			}
		}
		for owner, deps := range f.Dependencies {
			for _, dep := range deps {
				if runtimeRequirement(dep.ID) || strings.EqualFold(dep.Side, "server") || dep.Type != "" && dep.Type != "required" || dep.Mandatory != nil && !*dep.Mandatory {
					continue
				}
				match, err := parseMavenRange(dep.Range)
				if err != nil {
					return fmt.Errorf("%s: %w", dep.ID, err)
				}
				mod.deps[dep.ID] = append(mod.deps[dep.ID], modRequirement{SintedotRequirement{owner, file, dep.Range}, match})
			}
		}
		return nil
	}
	return nil
}

func runtimeRequirement(id string) bool {
	return id == "" || contains([]string{"java", "minecraft", "fabricloader", "quilt_loader", "forge", "neoforge"}, id)
}

func registeredNames(records map[string]Installed) map[string]string {
	names := map[string]string{}
	for k, r := range records {
		names[r.Filename] = k
	}
	return names
}

// Re-read the staged graph after each catalogue replacement. Dependencies can
// change between two releases of the same project.
func (m *Manager) settleSintedotRanges(ctx context.Context, stage string, target Target, planner maintenancePlanner) ([]string, []SintedotConflict, error) {
	folder := filepath.Join(stage, "mods")
	stageTarget := target
	stageTarget.Directory = stage
	var changes []string
	visited := map[string]bool{}
	exact := map[string]string{}
	for k, v := range planner.chosen {
		provider, _, _ := strings.Cut(k, ":")
		for _, d := range v.Dependencies {
			if d.Kind == "required" && d.Project != "" && d.Version != "" {
				exact[key(provider, d.Project)] = d.Version
			}
		}
	}
	for step := 0; step < 32; step++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		g, err := scanRangeGraph(folder, target.Loader)
		if err != nil {
			return nil, nil, err
		}
		records, err := registry(stage)
		if err != nil {
			return nil, nil, err
		}
		names := registeredNames(records)
		disabled, duplicates := g.duplicatePlan(target.Loader, names)
		conflicts := g.conflicts()
		if len(conflicts) == 0 && len(duplicates) == 0 {
			if len(disabled) == 0 {
				return changes, nil, nil
			}
			for _, file := range disabled {
				if err := quarantineMod(filepath.Join(folder, file)); err != nil {
					return nil, nil, err
				}
				changes = append(changes, "Отключён дубликат по mod_id: "+file)
			}
			continue
		}
		if len(conflicts) == 0 {
			return nil, duplicates, nil
		}
		conflict := conflicts[0]
		id := conflict.ModID
		file, k := "", ""
		for candidate, mod := range g.mods {
			if _, found := mod.ids[id]; !found || names[candidate] == "" || planner.blocked[names[candidate]] {
				continue
			}
			if k != "" {
				return nil, conflicts, nil
			}
			file, k = candidate, names[candidate]
		}
		if k == "" {
			return nil, conflicts, nil
		}
		record := records[k]
		pair := k + ":" + record.Version + ":" + id
		if visited[pair] {
			return nil, conflicts, nil
		}
		visited[pair] = true
		m.maintenanceProgress("Подбор совместимой версии "+id, 95)
		v, err := m.rangeCatalogueVersion(ctx, stage, target, record, id, g.required[id], func(v Version) bool {
			return v.ID != record.Version && (exact[k] == "" || exact[k] == v.ID) &&
				(!planner.stableOnly || v.Release == "" || v.Release == "release")
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			*planner.issues = append(*planner.issues, id+": "+err.Error())
			return nil, conflicts, nil
		}
		if v.ID == "" {
			return nil, conflicts, nil
		}
		for _, other := range g.files[id] {
			if other == file {
				continue
			}
			old := g.mods[other]
			if len(old.primary) != 1 {
				return nil, conflicts, nil
			}
			for alias := range old.ids {
				if alias != id && len(g.required[alias]) > 0 {
					return nil, conflicts, nil
				}
			}
			if err := quarantineMod(filepath.Join(folder, other)); err != nil {
				return nil, nil, err
			}
			changes = append(changes, "Отключён дубликат по mod_id: "+other)
		}
		choice := maintenancePlanner{manager: m, stableOnly: planner.stableOnly, target: target, records: records, active: map[string]bool{}, blocked: planner.blocked,
			chosen: map[string]Version{}, planned: map[string]PlanEntry{}, issues: planner.issues}
		for name := range g.mods {
			if _, err := os.Stat(filepath.Join(folder, name)); err == nil {
				choice.active[strings.ToLower(name)] = true
			}
		}
		for key, selected := range planner.chosen {
			if key != k && exact[key] != "" {
				choice.chosen[key] = selected
			}
		}
		if err = choice.visit(ctx, record.Provider, v); err != nil {
			*planner.issues = append(*planner.issues, err.Error())
			return nil, conflicts, nil
		}
		entries := make([]PlanEntry, 0, len(choice.planned))
		for _, entry := range choice.planned {
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Version.Filename < entries[j].Version.Filename })
		if err = m.install(ctx, Plan{Kind: "mods", Target: stageTarget, Entries: entries, maintenance: true}); err != nil {
			return nil, nil, err
		}
		for _, entry := range entries {
			if err = verifyModArchive(ctx, filepath.Join(folder, entry.Version.Filename)); err != nil {
				return nil, nil, err
			}
			changes = append(changes, entry.Project.Name+" → "+entry.Version.Name)
		}
	}
	g, err := scanRangeGraph(folder, target.Loader)
	if err != nil {
		return nil, nil, err
	}
	if conflicts := g.conflicts(); len(conflicts) > 0 {
		return nil, conflicts, nil
	}
	return nil, nil, errSintedotRanges
}

func restoreDisabledRequirements(ctx context.Context, folder, loader string, records map[string]Installed) ([]string, error) {
	type candidate struct {
		name string
		mod  rangeMod
	}
	files, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	known := map[string]Installed{}
	for _, record := range records {
		known[strings.ToLower(record.Filename)] = record
	}
	var pool []candidate
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(file.Name(), disabledSuffix)
		if name == file.Name() || !file.Type().IsRegular() || !safeFilename(name) {
			continue
		}
		path := filepath.Join(folder, file.Name())
		meta, e := inspectMod(path)
		if e != nil || incompatibleModLoader(meta.loader, loader) {
			continue
		}
		mod, e := rangeMetadata(path, loader)
		if e != nil || len(mod.primary) != 1 {
			continue
		}
		if record := known[strings.ToLower(name)]; record.Filename != "" {
			hash, e := fileHash(path)
			if e != nil || hash != record.Hash {
				continue
			}
		}
		pool = append(pool, candidate{name, mod})
	}
	var enabled []string
	for len(pool) > 0 {
		graph, err := scanRangeGraph(folder, loader)
		if err != nil {
			return nil, err
		}
		fits := map[string][]int{}
		for i, c := range pool {
			canEnable := true
			for id, versions := range c.mod.ids {
				if len(graph.files[id]) > 0 || !satisfiesVersions(versions, graph.required[id]) {
					canEnable = false
				}
			}
			if !canEnable {
				continue
			}
			for id, versions := range c.mod.ids {
				if len(graph.required[id]) > 0 && !graph.satisfied(id) && satisfiesVersions(versions, graph.required[id]) {
					fits[id] = append(fits[id], i)
				}
			}
		}
		index := -1
		for _, indices := range fits {
			if len(indices) == 1 && (index < 0 || indices[0] < index) {
				index = indices[0]
			}
		}
		if index < 0 {
			break
		}
		c := pool[index]
		pool = append(pool[:index], pool[index+1:]...)
		dest := filepath.Join(folder, c.name)
		if _, err := os.Lstat(dest); !os.IsNotExist(err) {
			continue
		}
		if err := verifyModArchive(ctx, dest+disabledSuffix); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if err := os.Rename(dest+disabledSuffix, dest); err != nil {
			return nil, err
		}
		enabled = append(enabled, c.name)
	}
	return enabled, nil
}

func sintedotReplacement(root, loader, downloaded string, records map[string]Installed, reserved map[string]bool) (Installed, string, error) {
	incoming, err := rangeMetadata(downloaded, loader)
	if err != nil {
		return Installed{}, "", err
	}
	if len(incoming.primary) != 1 {
		return Installed{}, "", nil
	}
	files, err := os.ReadDir(filepath.Join(root, "mods"))
	if err != nil {
		return Installed{}, "", err
	}
	var result Installed
	var recordKey string
	for _, f := range files {
		if !f.Type().IsRegular() || !safeFilename(f.Name()) || reserved[strings.ToLower(f.Name())] {
			continue
		}
		path := filepath.Join(root, "mods", f.Name())
		old, err := rangeMetadata(path, loader)
		if err != nil || len(old.primary) != 1 {
			continue
		}
		sameID := false
		for id := range old.primary {
			_, sameID = incoming.primary[id]
		}
		if !sameID {
			continue
		}
		// A replacement must continue to expose every ID the old JAR exposed.
		safe := true
		for id := range old.ids {
			if len(incoming.ids[id]) == 0 {
				safe = false
				break
			}
		}
		if !safe || result.Filename != "" {
			return Installed{}, "", nil
		}
		digest, err := fileHash(path)
		if err != nil {
			return Installed{}, "", err
		}
		result = Installed{Filename: f.Name(), Hash: digest}
		for k, r := range records {
			if !strings.EqualFold(r.Filename, f.Name()) {
				continue
			}
			if r.Hash != digest {
				return Installed{}, "", nil
			}
			recordKey = k
			break
		}
	}
	return result, recordKey, nil
}
