package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"sintet/internal/accountfiles"
)

type SintedotOptions struct {
	DryRun       bool `json:"dryRun"`
	CheckConfigs bool `json:"checkConfigs"`
	StableOnly   bool `json:"stableOnly"`
}

type SintedotHooks struct {
	Prepare func(context.Context, func(string, float64)) error
	Commit  func(context.Context, string) error
}

type SintedotState struct {
	RunID   uint64 `json:"runId"`
	Preview bool   `json:"preview,omitempty"`
	InstallState
	Target    string             `json:"target"`
	Changes   []string           `json:"changes"`
	Issues    []string           `json:"issues"`
	Journal   []string           `json:"journal,omitempty"`
	Conflicts []SintedotConflict `json:"conflicts,omitempty"`
	Backup    string             `json:"backup,omitempty"`
}

func (m *Manager) SintedotState() SintedotState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.sintedot
	if m.state.Maintenance {
		state.InstallState = m.state
	}
	return state
}

func (m *Manager) CancelSintedot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Maintenance && m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) RunSintedot(ctx context.Context, target Target, mode string, journal []string) error {
	return m.RunSintedotWithOptions(ctx, target, mode, journal, SintedotOptions{CheckConfigs: true}, SintedotHooks{})
}

func (m *Manager) RunSintedotWithOptions(ctx context.Context, target Target, mode string, journal []string, options SintedotOptions, hooks SintedotHooks) (err error) {
	if mode != "update" && mode != "repair" {
		return errors.New("неизвестное действие Sintedot")
	}
	if !contains([]string{"fabric", "quilt", "forge", "neoforge", "vanilla"}, target.Loader) {
		return errors.New("неподдерживаемый загрузчик сборки")
	}
	m.mu.Lock()
	if m.state.Busy {
		m.mu.Unlock()
		return errors.New("мастерская уже выполняет другую операцию")
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.state = InstallState{Maintenance: true, Busy: true, Stage: "Проверка файлов сборки"}
	m.sintedot = SintedotState{RunID: m.sintedot.RunID + 1, Target: target.ID, Journal: journal, Preview: options.DryRun}
	m.mu.Unlock()

	var changes, issues []string
	var conflicts []SintedotConflict
	var backup string
	defer func() {
		cancel()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.cancel = nil
		m.state.Busy = false
		m.sintedot.Issues = issues
		m.sintedot.Conflicts = conflicts
		switch {
		case errors.Is(err, context.Canceled):
			m.state.Stage = "Отменено"
		case err != nil:
			m.state.Error = err.Error()
			m.state.Stage = "Ошибка Sintedot"
		default:
			m.state.Percent = 100
			m.sintedot.Changes = changes
			m.sintedot.Backup = backup
			switch {
			case len(conflicts) > 0 && len(changes) > 0 && !options.DryRun:
				m.state.Stage = "Часть проблем исправлена"
			case len(conflicts) > 0:
				m.state.Stage = "Нужен выбор версий"
			case options.DryRun:
				m.state.Stage = "Проверка завершена"
			case len(changes) == 0 && len(issues) > 0:
				m.state.Stage = "Нужна ручная проверка"
			case len(changes) == 0:
				m.state.Stage = "Изменения не требуются"
			default:
				m.state.Stage = "Исправления применены"
				if mode == "update" {
					m.state.Stage = "Обновление применено"
				}
			}
		}
		m.sintedot.InstallState = m.state
	}()

	if hooks.Prepare != nil {
		if err = hooks.Prepare(ctx, m.maintenanceProgress); err != nil {
			return err
		}
	}
	if err = RecoverSintedot(target.Directory); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(target.Directory, ".sintedot-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	records, err := registry(target.Directory)
	if err != nil {
		return err
	}
	if records == nil {
		records = map[string]Installed{}
	}
	files, original, err := snapshotMods(ctx, filepath.Join(target.Directory, "mods"), filepath.Join(stage, "mods"))
	if err != nil {
		return err
	}
	var repairs, recognized []string
	byFile := map[string]string{}
	for k, record := range records {
		if !safeFilename(record.Filename) || !safeID.MatchString(record.Project) || !safeID.MatchString(record.Version) ||
			!contains([]string{"modrinth", "curseforge", "sintedmods"}, record.Provider) || key(record.Provider, record.Project) != k {
			return errors.New("в списке модов повреждена запись")
		}
		name := strings.ToLower(record.Filename)
		if _, exists := byFile[name]; exists {
			return fmt.Errorf("файл %s указан для нескольких проектов", record.Filename)
		}
		byFile[name] = k
	}

	sort.SliceStable(files, func(i, j int) bool {
		return byFile[strings.ToLower(files[i])] != "" && byFile[strings.ToLower(files[j])] == ""
	})
	active := map[string]bool{}
	blocked := map[string]bool{}
	wrongLoader := map[string]string{}
	knownHashes := map[string]string{}
	for i, name := range files {
		m.maintenanceProgress("Проверка "+name, 1+13*float64(i)/float64(len(files)))
		if err := ctx.Err(); err != nil {
			return err
		}
		if !safeFilename(name) {
			continue
		}
		path := filepath.Join(stage, "mods", name)
		recordKey := byFile[strings.ToLower(name)]
		data, readError := inspectMod(path)
		digest, err := fileHash(path)
		if err != nil {
			return err
		}
		if readError == nil && mode == "repair" {
			readError = verifyModArchive(ctx, path)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if readError != nil {
			var tooLarge *archiveInspectionLimit
			if errors.As(readError, &tooLarge) {
				issues = append(issues, name+": пропущена полная проверка целостности большого архива")
				readError = nil
			}
		}
		if readError != nil {
			if mode == "update" {
				return fmt.Errorf("%s: повреждённый JAR. Сначала запусти ремонт", name)
			}
			if recordKey != "" {
				if err = os.Remove(path); err != nil {
					return err
				}
				repairs = append(repairs, "Повреждённый архив сохранён в резервной копии: "+name)
			} else {
				if err = quarantineMod(path); err != nil {
					return err
				}
				repairs = append(repairs, "Отключён повреждённый архив "+name)
			}
			continue
		}
		if recordKey == "" && (mode == "update" || len(data.depends) > 0 || m.Client.Key != "") {
			record, found := m.identifyModrinth(ctx, path, name, digest)
			if !found {
				record, found = m.identifyCurseForge(ctx, path, name, digest)
			}
			if found && records[key(record.Provider, record.Project)].Filename == "" {
				recordKey = key(record.Provider, record.Project)
				records[recordKey] = record
				byFile[strings.ToLower(name)] = recordKey
				recognized = append(recognized, "Источник "+name+" – "+record.Provider)
			}
		}
		if recordKey != "" && records[recordKey].Hash != digest {
			blocked[recordKey] = true
			issues = append(issues, name+": файл отличается от версии из каталога")
		}
		if duplicate, ok := knownHashes[digest]; ok {
			if mode == "repair" && recordKey == "" {
				if err = quarantineMod(path); err != nil {
					return err
				}
				repairs = append(repairs, "Отключён дубликат "+name+" (копия "+duplicate+")")
				continue
			}
			issues = append(issues, name+": копия "+duplicate)
		} else {
			knownHashes[digest] = name
		}
		if recordKey == "" && mode == "update" {
			issues = append(issues, name+": источник обновления не определён")
		}
		if incompatibleModLoader(data.loader, target.Loader) {
			if target.Loader == "fabric" || target.Loader == "quilt" {
				if recordKey != "" && !blocked[recordKey] {
					wrongLoader[recordKey] = name
				} else {
					if err = quarantineMod(path); err != nil {
						return err
					}
					repairs = append(repairs, fmt.Sprintf("Отключён %s: мод для %s, сборка использует %s", name, data.loader, target.Loader))
					continue
				}
			} else {
				issues = append(issues, fmt.Sprintf("%s: файл для %s, сборка использует %s", name, data.loader, target.Loader))
			}
		}
		active[strings.ToLower(name)] = true
	}

	originalRanges, err := scanRangeGraph(filepath.Join(stage, "mods"), target.Loader)
	if err != nil {
		return err
	}
	registered := registeredNames(records)
	disabled, _ := originalRanges.duplicatePlan(target.Loader, registered)
	for _, name := range disabled {
		if err = quarantineMod(filepath.Join(stage, "mods", name)); err != nil {
			return err
		}
		delete(active, strings.ToLower(name))
		repairs = append(repairs, "Отключён дубликат по mod_id: "+name)
	}

	if mode == "repair" && target.SharedID == "" {
		restored, e := restoreDisabledRequirements(ctx, filepath.Join(stage, "mods"), target.Loader, records)
		if e != nil {
			return e
		}
		for _, name := range restored {
			active[strings.ToLower(name)] = true
			repairs = append(repairs, "Включена обязательная зависимость "+name)
		}
	}
	m.maintenanceProgress("Поиск версий и зависимостей", 15)
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	planner := maintenancePlanner{manager: m, target: target, records: records, active: active, blocked: blocked, stableOnly: options.StableOnly,
		chosen: map[string]Version{}, planned: map[string]PlanEntry{}, issues: &issues}
	for i, k := range keys {
		if err = ctx.Err(); err != nil {
			return err
		}
		if blocked[k] {
			continue
		}
		record := records[k]
		name := strings.ToLower(record.Filename)
		if !active[name] {
			if mode == "update" {
				continue
			}
			// An explicitly disabled mod is left disabled unless another mod needs it.
			if _, e := os.Stat(filepath.Join(stage, "mods", record.Filename+disabledSuffix)); e == nil {
				continue
			}
		}
		m.maintenanceProgress("Проверка "+modLabel(record), 15+35*float64(i+1)/float64(len(keys)))
		var version Version
		if mode == "update" || wrongLoader[k] != "" {
			version, err = planner.latest(ctx, record.Provider, record.Project)
		} else {
			version, err = m.Client.Version(ctx, record.Provider, record.Project, record.Version)
		}
		if err != nil || version.ID == "" || version.Project != record.Project || !version.Compatible(target.Game, target.Loader) {
			message := modLabel(record) + ": не удалось подобрать совместимый файл"
			if err != nil {
				message += " (" + err.Error() + ")"
			}
			issues = append(issues, message)
			if wrong := wrongLoader[k]; wrong != "" {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err = quarantineMod(filepath.Join(stage, "mods", wrong)); err != nil {
					return err
				}
				delete(active, strings.ToLower(wrong))
				repairs = append(repairs, "Отключён мод для другого загрузчика: "+wrong)
			}
			continue
		}
		if wrongLoader[k] != "" && version.ID == record.Version {
			if err = quarantineMod(filepath.Join(stage, "mods", wrongLoader[k])); err != nil {
				return err
			}
			delete(active, name)
			repairs = append(repairs, "Отключена несовместимая копия "+wrongLoader[k])
			issues = append(issues, modLabel(record)+": каталог не предложил другую версию для этого загрузчика")
			continue
		}
		planner.chosen[k] = version
	}
	if err = planner.resolve(ctx, keys); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}

	entries := make([]PlanEntry, 0, len(planner.planned))
	for _, entry := range planner.planned {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Version.Filename < entries[j].Version.Filename })
	stagedRecords, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err = accountfiles.Write(filepath.Join(stage, ".sintet-mods.json"), stagedRecords); err != nil {
		return err
	}
	stagedTarget := target
	stagedTarget.Directory = stage
	if len(entries) > 0 {
		m.maintenanceProgress("Загрузка файлов", 50)
		if err = m.install(ctx, Plan{Kind: "mods", Target: stagedTarget, Entries: entries, maintenance: true}); err != nil {
			return err
		}
		m.maintenanceProgress("Проверка загруженных файлов", 95)
		for _, entry := range entries {
			name := entry.Version.Filename
			path := filepath.Join(stage, "mods", name)
			metadata, err := inspectMod(path)
			if err != nil {
				return fmt.Errorf("%s: не пройдена проверка метаданных: %w", name, err)
			}
			if incompatibleModLoader(metadata.loader, target.Loader) {
				return fmt.Errorf("%s: метаданные файла указывают %s, сборка использует %s", name, metadata.loader, target.Loader)
			}
			if err = verifyModArchive(ctx, path); err != nil {
				return fmt.Errorf("%s: не пройдена проверка архива: %w", name, err)
			}
		}
	}
	rangeChanges, conflicts, err := m.settleSintedotRanges(ctx, stage, target, planner)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		for i := range conflicts {
			conflicts[i].Installed = originalRanges.conflict(conflicts[i].ModID).Installed
		}
		if mode != "repair" || !options.CheckConfigs {
			return nil
		}
		configFiles, fixes, configIssues, e := stageBrokenConfigs(ctx, target.Directory, stage)
		issues = append(issues, configIssues...)
		if e != nil {
			return e
		}
		if len(configFiles) == 0 {
			return nil
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if !options.DryRun {
			m.maintenanceProgress("Сохранение исправленных конфигов", 97)
			backup, e = applySintedotTx(target.Directory, stage, configFiles)
			if e != nil {
				return e
			}
		}
		changes = fixes
		return nil
	}
	finalRecords, err := registry(stage)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if finalRecords[key(entry.Project.Provider, entry.Version.Project)].Version == entry.Version.ID {
			changes = append(changes, fmt.Sprintf("%s → %s", entry.Project.Name, entry.Version.Name))
		}
	}
	changes = append(changes, rangeChanges...)
	changes = append(changes, recognized...)
	modsChanged := len(entries) > 0 || len(rangeChanges) > 0 || len(repairs) > 0
	var configFiles []sintedotTxFile
	if mode == "repair" && options.CheckConfigs {
		var configChanges, configIssues []string
		configFiles, configChanges, configIssues, err = stageBrokenConfigs(ctx, target.Directory, stage)
		if err != nil {
			return err
		}
		repairs = append(repairs, configChanges...)
		issues = append(issues, configIssues...)
	}
	issues = append(issues, maintenanceMissingIDs(stage)...)
	if len(changes) == 0 && len(repairs) == 0 {
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if options.DryRun {
		changes = append(repairs, changes...)
		return nil
	}
	if hooks.Commit != nil {
		m.maintenanceProgress("Сохранение в общую сборку", 97)
		if err = hooks.Commit(ctx, stage); err != nil {
			return err
		}
		changes = append(repairs, changes...)
		return nil
	}
	m.maintenanceProgress("Резервная копия и применение", 97)
	if err = confirmMaintenanceSource(ctx, filepath.Join(target.Directory, "mods"), original); err != nil {
		return err
	}
	txFiles := configFiles
	if modsChanged || len(recognized) > 0 {
		txFiles = append([]sintedotTxFile{{Path: ".sintet-mods.json", Replacement: true}}, txFiles...)
	}
	if modsChanged {
		txFiles = append([]sintedotTxFile{{Path: "mods", Replacement: true}}, txFiles...)
	}
	backup, err = applySintedotTx(target.Directory, stage, txFiles)
	if err != nil {
		return err
	}
	changes = append(repairs, changes...)
	return nil
}

func (m *Manager) maintenanceProgress(stage string, percent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Stage = stage
	m.state.Percent = max(m.state.Percent, percent)
}

func modLabel(mod Installed) string {
	if mod.Name != "" {
		return mod.Name
	}
	return mod.Filename
}

func snapshotMods(ctx context.Context, source, dest string) ([]string, map[string]os.FileInfo, error) {
	if err := os.Mkdir(dest, 0755); err != nil {
		return nil, nil, err
	}
	if err := localDirectory(source); os.IsNotExist(err) {
		return nil, nil, nil
	} else if err != nil {
		return nil, nil, err
	}
	var names []string
	original := map[string]os.FileInfo{}
	err := filepath.WalkDir(source, func(path string, item os.DirEntry, err error) error {
		if err != nil || path == source {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		original[rel] = info
		destPath := filepath.Join(dest, rel)
		if item.IsDir() {
			return os.Mkdir(destPath, 0755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("нельзя безопасно обработать %s", rel)
		}
		if err = copyMaintenanceFile(ctx, path, destPath); err != nil {
			return fmt.Errorf("не удалось создать снимок %s: %w", rel, err)
		}
		if filepath.Dir(rel) == "." {
			names = append(names, item.Name())
		}
		return nil
	})
	return names, original, err
}

func copyMaintenanceFile(ctx context.Context, source, dest string) error {
	from, err := os.Open(source)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(to, contextArchiveReader{ctx, from})
	return errors.Join(err, to.Close())
}

func confirmMaintenanceSource(ctx context.Context, source string, original map[string]os.FileInfo) error {
	changed := errors.New("папка модов изменилась во время работы Sintedot. Запусти проверку заново")
	if err := localDirectory(source); os.IsNotExist(err) && original == nil {
		return ctx.Err()
	} else if err != nil || original == nil {
		return changed
	}
	seen := 0
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil || path == source {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		old := original[rel]
		if err != nil || old == nil || info.Mode() != old.Mode() || info.Size() != old.Size() || !info.ModTime().Equal(old.ModTime()) {
			return changed
		}
		seen++
		return nil
	})
	if err != nil {
		return err
	}
	if seen != len(original) {
		return changed
	}
	return ctx.Err()
}

func quarantineMod(path string) error {
	dest := path + disabledSuffix
	if _, err := os.Lstat(dest); os.IsNotExist(err) {
		return os.Rename(path, dest)
	} else if err != nil {
		return err
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for n := 1; n <= 1000; n++ {
		dest = fmt.Sprintf("%s-sintedot-%d%s%s", base, n, ext, disabledSuffix)
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			return os.Rename(path, dest)
		} else if err != nil {
			return err
		}
	}
	return fmt.Errorf("%s: не удалось выбрать имя для отключённой копии", filepath.Base(path))
}

type scannedMod struct {
	loader                 string
	ids, depends, embedded []string
}

type archiveInspectionLimit struct{}

func (*archiveInspectionLimit) Error() string {
	return "архив слишком велик для полной проверки"
}

type contextArchiveReader struct {
	ctx context.Context
	io.Reader
}

func (r contextArchiveReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func verifyModArchive(ctx context.Context, path string) error {
	jar, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer jar.Close()
	remaining := uint64(1 << 30)
	for _, entry := range jar.File {
		if entry.UncompressedSize64 > remaining {
			return &archiveInspectionLimit{}
		}
		remaining -= entry.UncompressedSize64
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, contextArchiveReader{ctx, reader})
		if err = errors.Join(err, reader.Close()); err != nil {
			return fmt.Errorf("%s: %w", entry.Name, err)
		}
	}
	return nil
}

var forgeDependencyTable = regexp.MustCompile(`(?m)^[ \t]*\[\[dependencies\.[^\]\r\n]+\]\][ \t]*(?:#[^\r\n]*)?\r?$`)
var requiredForgeDependency = regexp.MustCompile(`(?m)^[ \t]*(?:mandatory[ \t]*=[ \t]*true|type[ \t]*=[ \t]*["']required["'])[ \t]*(?:#[^\r\n]*)?\r?$`)
var serverForgeDependency = regexp.MustCompile(`(?mi)^[ \t]*side[ \t]*=[ \t]*["']server["'][ \t]*(?:#[^\r\n]*)?\r?$`)

func forgeDependencies(data []byte) []string {
	var ids []string
	for _, table := range forgeDependencyTable.FindAllIndex(data, -1) {
		fields := data[table[1]:]
		if next := tomlTable.FindIndex(fields); next != nil {
			fields = fields[:next[0]]
		}
		if !requiredForgeDependency.Match(fields) || serverForgeDependency.Match(fields) {
			continue
		}
		if match := tomlID.FindSubmatch(fields); len(match) > 1 && !contains([]string{"minecraft", "forge", "neoforge", "java"}, string(match[1])) {
			ids = append(ids, string(match[1]))
		}
	}
	return ids
}

func inspectMod(path string) (scannedMod, error) {
	jar, err := zip.OpenReader(path)
	if err != nil {
		return scannedMod{}, err
	}
	defer jar.Close()
	result := scannedMod{}
	if raw, e := zipBytes(&jar.Reader, "fabric.mod.json", 256<<10); e == nil {
		metadata, err := parseMaintenanceFabric(raw)
		if err != nil {
			return result, err
		}
		result.loader = "fabric"
		result.ids = append([]string{metadata.ID}, metadata.Provides...)
		for id := range metadata.Depends {
			if !contains([]string{"java", "minecraft", "fabricloader", "quilt_loader"}, id) {
				result.depends = append(result.depends, id)
			}
		}
		budget := uint64(32 << 20)
		result.embedded = fabricEmbeddedIDs(&jar.Reader, metadata, &budget, 0)
	} else if !os.IsNotExist(e) {
		return result, e
	}
	fabric := result.loader == "fabric"
	for _, entry := range jar.File {
		switch entry.Name {
		case "quilt.mod.json":
			if result.loader == "" {
				result.loader = "quilt"
			}
		case "META-INF/mods.toml", "META-INF/neoforge.mods.toml":
			if result.loader == "" {
				result.loader = "forge"
				if entry.Name == "META-INF/neoforge.mods.toml" {
					result.loader = "neoforge"
				}
				result.ids = modIDs(path)
			}
			if fabric {
				result.loader = "" // This archive declares both Fabric and Forge metadata.
			} else {
				raw, err := zipBytes(&jar.Reader, entry.Name, 256<<10)
				if err != nil {
					return result, err
				}
				result.depends = append(result.depends, forgeDependencies(raw)...)
			}
		}
	}
	return result, nil
}

type maintenanceFabric struct {
	ID       string
	Provides []string
	Depends  map[string]json.RawMessage
	Jars     []struct{ File string }
}

func parseMaintenanceFabric(data []byte) (maintenanceFabric, error) {
	var metadata maintenanceFabric
	if json.Unmarshal(data, &metadata) != nil || metadata.ID == "" {
		return metadata, errors.New("неверный fabric.mod.json")
	}
	return metadata, nil
}

func fabricEmbeddedIDs(jar *zip.Reader, metadata maintenanceFabric, budget *uint64, depth int) []string {
	if depth >= 3 || *budget == 0 {
		return nil
	}
	var ids []string
	for i, child := range metadata.Jars {
		if i >= 128 || *budget == 0 {
			break
		}
		data, err := zipBytes(jar, child.File, *budget)
		if err != nil {
			continue
		}
		*budget -= uint64(len(data))
		nested, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			continue
		}
		raw, err := zipBytes(nested, "fabric.mod.json", 256<<10)
		if err != nil {
			continue
		}
		mod, err := parseMaintenanceFabric(raw)
		if err != nil {
			continue
		}
		ids = append(ids, mod.ID)
		ids = append(ids, mod.Provides...)
		ids = append(ids, fabricEmbeddedIDs(nested, mod, budget, depth+1)...)
	}
	return ids
}

func incompatibleModLoader(actual, target string) bool {
	return actual == "fabric" && (target == "forge" || target == "neoforge") ||
		actual == "quilt" && target != "quilt" ||
		(actual == "forge" || actual == "neoforge") && (target == "fabric" || target == "quilt")
}

func missingModIDs(requiredBy map[string][]string, ids map[string]string) []string {
	var result []string
	for id, mods := range requiredBy {
		if _, exists := ids[id]; !exists {
			result = append(result, strings.Join(mods, ", ")+": проверь зависимость "+id+" (может входить в другой JAR)")
		}
	}
	sort.Strings(result)
	return result
}

func maintenanceMissingIDs(stage string) []string {
	files, _ := os.ReadDir(filepath.Join(stage, "mods"))
	ids := map[string]string{}
	required := map[string][]string{}
	for _, f := range files {
		if f.IsDir() || !safeFilename(f.Name()) {
			continue
		}
		meta, err := inspectMod(filepath.Join(stage, "mods", f.Name()))
		if err != nil {
			continue
		}
		for _, id := range append(meta.ids, meta.embedded...) {
			ids[id] = f.Name()
		}
		for _, id := range meta.depends {
			required[id] = append(required[id], f.Name())
		}
	}
	return missingModIDs(required, ids)
}

func (m *Manager) identifyModrinth(ctx context.Context, path, name, digest string) (Installed, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Installed{}, false
	}
	h := sha512.New()
	_, err = io.Copy(h, f)
	_ = f.Close()
	if err != nil {
		return Installed{}, false
	}
	check := hex.EncodeToString(h.Sum(nil))
	var row mrVersion
	if m.Client.get(ctx, "modrinth", "/version_file/"+check+"?algorithm=sha512", &row) != nil {
		return Installed{}, false
	}
	for _, file := range row.Files {
		if strings.EqualFold(file.Hashes["sha512"], check) && safeID.MatchString(row.ProjectID) && safeID.MatchString(row.ID) {
			return Installed{Provider: "modrinth", Project: row.ProjectID, Version: row.ID, Filename: name, Name: row.Name, Hash: digest}, true
		}
	}
	return Installed{}, false
}

type maintenancePlanner struct {
	stableOnly      bool
	manager         *Manager
	target          Target
	records         map[string]Installed
	active, blocked map[string]bool
	chosen          map[string]Version
	planned         map[string]PlanEntry
	walked          map[string]bool
	issues          *[]string
}

func (p *maintenancePlanner) latest(ctx context.Context, provider, id string) (Version, error) {
	rows, err := p.manager.Client.Versions(ctx, provider, id, p.target.Game, p.target.Loader)
	if err != nil {
		return Version{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Published.After(rows[j].Published) })
	record := p.records[key(provider, id)]
	for _, row := range rows {
		if p.stableOnly && row.Release != "" && row.Release != "release" {
			continue
		}
		if row.Project != id || !safeID.MatchString(row.ID) || !row.Compatible(p.target.Game, p.target.Loader) {
			continue
		}
		if row.ID == record.Version && p.active[strings.ToLower(record.Filename)] || verifiableModDownload(row) {
			return row, nil
		}
	}
	return Version{}, fmt.Errorf("нет совместимого релиза для %s", id)
}

func verifiableModDownload(v Version) bool {
	return safeFilename(v.Filename) && allowedDownload(v.URL) && v.Hash != ""
}

type maintenancePin struct {
	provider string
	version  Version
}

func (pin *maintenancePin) Error() string {
	return fmt.Sprintf("конфликт версий %s: требуется %s", pin.version.Project, pin.version.ID)
}

func (p *maintenancePlanner) resolve(ctx context.Context, keys []string) error {
	roots := p.chosen
	pins := map[string]string{}
	for {
		p.chosen = make(map[string]Version, len(roots))
		for k, v := range roots {
			p.chosen[k] = v
		}
		p.walked = nil
		clear(p.planned)
		var err error
		for _, k := range keys {
			if v, ok := p.chosen[k]; ok {
				if err = p.visit(ctx, p.records[k].Provider, v); err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, k := range keys {
				if _, pinned := pins[k]; pinned {
					*p.issues = append(*p.issues, modLabel(p.records[k])+": выбрана версия "+roots[k].ID+" по требованию другого мода")
				}
			}
			return nil
		}
		var pin *maintenancePin
		if !errors.As(err, &pin) {
			return err
		}
		k := key(pin.provider, pin.version.Project)
		if _, ok := roots[k]; !ok || pins[k] != "" || len(pins) >= 32 {
			return err
		}
		pins[k] = pin.version.ID
		roots[k] = pin.version
	}
}

func (p *maintenancePlanner) visit(ctx context.Context, provider string, v Version) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.ID == "" || v.Project == "" || !v.Compatible(p.target.Game, p.target.Loader) {
		return errors.New("каталог вернул несовместимую версию зависимости")
	}
	if p.walked == nil {
		p.walked = map[string]bool{}
	}
	k := key(provider, v.Project)
	if p.blocked[k] {
		return fmt.Errorf("зависимость %s изменена вручную", modLabel(p.records[k]))
	}
	if existing, ok := p.chosen[k]; ok && existing.ID != v.ID {
		return fmt.Errorf("конфликт версий %s: нужны %s и %s", v.Project, existing.ID, v.ID)
	}
	p.chosen[k] = v
	if p.walked[k] {
		return nil
	}
	if len(p.walked) >= 1024 {
		return errors.New("слишком много проектов в цепочке зависимостей")
	}
	p.walked[k] = true
	old := p.records[k]
	label := modLabel(old)
	if label == "" {
		label = v.Name
	}
	if old.Version != v.ID || !p.active[strings.ToLower(old.Filename)] {
		if !verifiableModDownload(v) {
			return fmt.Errorf("%s: каталог не разрешил проверяемую загрузку", label)
		}
		p.planned[k] = PlanEntry{Project: Project{Provider: provider, ID: v.Project, Name: label, Icon: old.Icon}, Version: v}
	}
	for _, dep := range v.Dependencies {
		depKey := key(provider, dep.Project)
		switch dep.Kind {
		case "incompatible":
			conflict, exists := p.chosen[depKey]
			record := p.records[depKey]
			if (exists && (dep.Version == "" || conflict.ID == dep.Version)) ||
				(!exists && p.active[strings.ToLower(record.Filename)] && (dep.Version == "" || record.Version == dep.Version)) {
				return fmt.Errorf("%s: несовместимая зависимость %s", label, dep.Project)
			}
		case "required":
			choice, err := p.dependency(ctx, provider, dep)
			if err != nil {
				return fmt.Errorf("%s: %w", label, err)
			}
			if err = p.visit(ctx, provider, choice); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *maintenancePlanner) dependency(ctx context.Context, provider string, dep Dependency) (Version, error) {
	if dep.Project != "" {
		k := key(provider, dep.Project)
		if v, ok := p.chosen[k]; ok {
			if dep.Version == "" || v.ID == dep.Version {
				return v, nil
			}
		}
	}
	if dep.Version != "" {
		v, err := p.manager.Client.Version(ctx, provider, dep.Project, dep.Version)
		if err != nil {
			return Version{}, err
		}
		if !safeID.MatchString(v.Project) || v.ID != dep.Version || (dep.Project != "" && dep.Project != v.Project) || !v.Compatible(p.target.Game, p.target.Loader) {
			return Version{}, errors.New("каталог вернул несовместимую версию зависимости")
		}
		if p.stableOnly && v.Release != "" && v.Release != "release" && p.records[key(provider, v.Project)].Version != v.ID {
			return Version{}, errors.New("точная зависимость доступна только в предварительном релизе")
		}
		if chosen, exists := p.chosen[key(provider, v.Project)]; exists && chosen.ID != v.ID {
			return Version{}, &maintenancePin{provider, v}
		}
		return v, nil
	}
	return p.latest(ctx, provider, dep.Project)
}
