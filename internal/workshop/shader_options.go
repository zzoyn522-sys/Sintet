package workshop

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/magiconair/properties"
	"sintet/internal/accountfiles"
)

type ShaderOptions struct {
	Revision string                `json:"revision"`
	Groups   []ShaderOptionGroup   `json:"groups"`
	Profiles []ShaderOptionProfile `json:"profiles"`
}

type ShaderOptionGroup struct {
	Name    string         `json:"name"`
	Options []ShaderOption `json:"options"`
}

type ShaderOption struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Default     string            `json:"default"`
	Value       string            `json:"value"`
	Choices     []string          `json:"choices"`
	Labels      map[string]string `json:"labels"`
	Toggle      bool              `json:"toggle"`
	Slider      bool              `json:"slider"`
}

type ShaderOptionProfile struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

var shaderDefine = regexp.MustCompile(`^[ \t]*(//[ \t]*)?#[ \t]*define[ \t]+([A-Za-z_][A-Za-z0-9_]*)(?:[ \t]+([^/ \t]+))?[ \t]*(?://(.*))?$`)
var shaderCheck = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*ifn?def[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
var shaderConst = regexp.MustCompile(`^[ \t]*const[ \t]+(?:int|float)[ \t]+(shadowMapResolution|shadowDistance|shadowDistanceRenderMul|shadowIntervalSize|sunPathRotation|ambientOcclusionLevel|wetnessHalflife|drynessHalflife|eyeBrightnessHalflife|centerDepthHalflife|superSamplingLevel|noiseTextureResolution)[ \t]*=[ \t]*([^; \t]+)[ \t]*;[ \t]*//(.*)$`)
var shaderChoices = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*(.*)$`)
var shaderValue = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,80}$`)

const shaderConfigLimit = 256 << 10

func shaderProperties(data []byte, encoding properties.Encoding) (*properties.Properties, error) {
	loader := properties.Loader{Encoding: encoding, DisableExpansion: true}
	return loader.LoadBytes(data)
}

// Join logical property lines before parsing. Several packs start screen lists
// on the next line with a bare continuation immediately after the separator.
func joinShaderPropertyLines(data []byte) []byte {
	var joined strings.Builder
	continuing := false
	for raw := range strings.SplitSeq(string(data), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if continuing {
			line = strings.TrimLeft(line, " \t\f")
		} else if comment := strings.TrimLeft(line, " \t\f"); strings.HasPrefix(comment, "#") || strings.HasPrefix(comment, "!") {
			continue
		}
		slashes := len(line) - len(strings.TrimRight(line, "\\"))
		continuing = slashes%2 != 0
		if continuing {
			joined.WriteString(line[:len(line)-1])
		} else {
			joined.WriteString(line)
			joined.WriteByte('\n')
		}
	}
	return []byte(joined.String())
}

func shaderArchiveText(entry *zip.File, budget *int64) ([]byte, error) {
	const maxEntry = 4 << 20
	limit := min(int64(maxEntry), *budget)
	if entry.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("параметры шейдера превышают допустимый размер")
	}
	file, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("параметры шейдера превышают допустимый размер")
	}
	*budget -= int64(len(data))
	return data, err
}

func shaderRoot(entries []*zip.File) string {
	root := ""
	for _, entry := range entries {
		name := entry.Name
		if name != path.Clean(name) || !entry.Mode().IsRegular() {
			continue
		}
		if !strings.HasPrefix(name, "shaders/") {
			continue
		}
		return "shaders/"
	}
	for _, entry := range entries {
		name := entry.Name
		if name != path.Clean(name) || !entry.Mode().IsRegular() {
			continue
		}
		first, rest, ok := strings.Cut(name, "/")
		if ok && strings.HasPrefix(rest, "shaders/") && first != ".." && first != "" {
			candidate := first + "/shaders/"
			if root == "" || candidate < root {
				root = candidate
			}
		}
	}
	return root
}

func shaderLocale(language string) string {
	switch language {
	case "ru", "uk", "pt", "es":
		return map[string]string{"ru": "ru_ru", "uk": "uk_ua", "pt": "pt_br", "es": "es_es"}[language]
	default:
		return "en_us"
	}
}

func readShaderArchive(archive *zip.Reader, language string) (map[string]ShaderOption, *properties.Properties, map[string]string, error) {
	root := shaderRoot(archive.File)
	if root == "" {
		return nil, nil, nil, errors.New("в архиве нет папки shaders")
	}
	config := properties.NewProperties()
	labels := map[string]string{}
	options := map[string]ShaderOption{}
	conflicts := map[string]bool{}
	checked := map[string]bool{}
	localized := map[string][]byte{}
	budget := int64(48 << 20)

	for _, entry := range archive.File {
		name, inside := strings.CutPrefix(entry.Name, root)
		if !inside || !entry.Mode().IsRegular() || entry.Name != path.Clean(entry.Name) {
			continue
		}
		ext := path.Ext(name)
		switch {
		case name == "shaders.properties":
			data, err := shaderArchiveText(entry, &budget)
			if err != nil {
				return nil, nil, nil, err
			}
			config, err = shaderProperties(joinShaderPropertyLines(data), properties.ISO_8859_1)
			if err != nil {
				return nil, nil, nil, err
			}
		case strings.EqualFold(name, "lang/en_us.lang") || strings.EqualFold(name, "lang/"+shaderLocale(language)+".lang"):
			data, err := shaderArchiveText(entry, &budget)
			if err == nil {
				localized[strings.ToLower(name)] = data
			}
		case slices.Contains([]string{".glsl", ".vsh", ".fsh", ".gsh", ".csh", ".tcs", ".tes", ".inc"}, ext):
			data, err := shaderArchiveText(entry, &budget)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, match := range shaderCheck.FindAllSubmatch(data, -1) {
				checked[string(match[1])] = true
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				id, option, ok := parseShaderOption(strings.TrimSuffix(line, "\r"))
				if !ok || conflicts[id] {
					continue
				}
				if previous, exists := options[id]; exists {
					if previous.Default != option.Default || !slices.Equal(previous.Choices, option.Choices) {
						conflicts[id] = true
						delete(options, id)
					}
					continue
				}
				options[id] = option
			}
		}
	}
	for id, option := range options {
		if option.Toggle && !checked[id] {
			delete(options, id)
		}
	}
	if language == "original" {
		return options, config, labels, nil
	}
	for _, locale := range []string{"en_us", shaderLocale(language)} {
		data := localized["lang/"+locale+".lang"]
		encoding := properties.UTF8
		if !utf8.Valid(data) {
			encoding = properties.ISO_8859_1
		}
		if lang, err := shaderProperties(data, encoding); err == nil {
			for _, key := range lang.Keys() {
				labels[key], _ = lang.Get(key)
			}
		}
	}
	return options, config, labels, nil
}

func parseShaderOption(line string) (string, ShaderOption, bool) {
	match := shaderDefine.FindStringSubmatch(line)
	if match == nil {
		if c := shaderConst.FindStringSubmatch(line); c != nil {
			match = []string{c[0], "", c[1], c[2], c[3]}
		} else {
			return "", ShaderOption{}, false
		}
	}
	id, value, comment := match[2], match[3], strings.TrimSpace(match[4])
	option := ShaderOption{ID: id, Name: id, Description: comment}
	if value == "" {
		option.Toggle = true
		option.Default = "true"
		if match[1] != "" {
			option.Default = "false"
		}
		option.Choices = []string{"false", "true"}
		return id, option, true
	}
	choices := shaderChoices.FindStringSubmatch(comment)
	if match[1] != "" || choices == nil {
		return "", ShaderOption{}, false
	}
	option.Default = value
	option.Description = strings.TrimSpace(choices[2])
	for _, choice := range strings.Fields(choices[1]) {
		if !shaderValue.MatchString(choice) {
			return "", ShaderOption{}, false
		}
		if !slices.Contains(option.Choices, choice) {
			option.Choices = append(option.Choices, choice)
		}
	}
	if !shaderValue.MatchString(value) {
		return "", ShaderOption{}, false
	}
	if !slices.Contains(option.Choices, value) {
		option.Choices = append(option.Choices, value)
	}
	return id, option, len(option.Choices) > 1
}

func shaderLabel(labels map[string]string, key, fallback string) string {
	if label := strings.TrimSpace(labels[key]); label != "" {
		return label
	}
	return fallback
}

func shaderOptionGroups(options map[string]ShaderOption, config *properties.Properties, labels map[string]string) []ShaderOptionGroup {
	groups := []ShaderOptionGroup{{}}
	explicit := map[string]bool{}
	for _, key := range config.Keys() {
		if key != "screen" && !strings.HasPrefix(key, "screen.") {
			continue
		}
		value, _ := config.Get(key)
		for _, id := range strings.Fields(value) {
			if _, ok := options[id]; ok {
				explicit[id] = true
			}
		}
	}
	var remaining []string
	for id := range options {
		if !explicit[id] {
			remaining = append(remaining, id)
		}
	}
	slices.Sort(remaining)
	seen := map[string]bool{}
	var addScreen func(string, string, int)
	addScreen = func(id, title string, group int) {
		if seen[id] || len(seen) >= 128 {
			return
		}
		seen[id] = true
		value, ok := config.Get(id)
		if !ok && id == "screen" {
			value = "*"
		}
		for _, item := range strings.Fields(value) {
			switch {
			case item == "*":
				for _, name := range remaining {
					groups[group].Options = append(groups[group].Options, options[name])
				}
			case len(item) > 2 && item[0] == '[' && item[len(item)-1] == ']':
				name := item[1 : len(item)-1]
				key := "screen." + name
				if _, exists := config.Get(key); !exists || seen[key] || len(seen) >= 128 {
					continue
				}
				label := shaderLabel(labels, key, name)

				if title != "" {
					label = title + " / " + label
				}
				groups = append(groups, ShaderOptionGroup{Name: label})
				addScreen(key, label, len(groups)-1)
			default:
				if option, exists := options[item]; exists {
					groups[group].Options = append(groups[group].Options, option)
				}
			}
		}
	}
	if _, ok := config.Get("screen"); !ok {
		explicit = nil
		remaining = remaining[:0]
		for id := range options {
			remaining = append(remaining, id)
		}
		slices.Sort(remaining)
	}
	addScreen("screen", "", 0)
	return slices.DeleteFunc(groups, func(group ShaderOptionGroup) bool { return len(group.Options) == 0 })
}

func shaderProfiles(config *properties.Properties, options map[string]ShaderOption, labels map[string]string) []ShaderOptionProfile {
	result := []ShaderOptionProfile{}
	cache := map[string]map[string]string{}
	visiting := map[string]bool{}
	var read func(string, int) map[string]string
	read = func(key string, depth int) map[string]string {
		if values, ok := cache[key]; ok {
			return values
		}
		value, ok := config.Get(key)
		if !ok || visiting[key] || depth > 32 {
			return nil
		}
		visiting[key] = true
		defer delete(visiting, key)
		values := map[string]string{}
		for _, item := range strings.Fields(value) {
			if strings.HasPrefix(item, "profile.") {
				for id, value := range read(item, depth+1) {
					values[id] = value
				}
				continue
			}
			id, value, hasValue := strings.Cut(item, "=")
			if !hasValue {
				id, value, hasValue = strings.Cut(item, ":")
			}
			if !hasValue {
				var negated bool
				id, negated = strings.CutPrefix(item, "!")
				value = "true"
				if negated {
					value = "false"
				}
			}
			if option, ok := options[id]; ok && (hasValue || option.Toggle) && slices.Contains(option.Choices, value) {
				values[id] = value
			}
		}
		cache[key] = values
		return values
	}
	for _, key := range config.Keys() {
		name, ok := strings.CutPrefix(key, "profile.")
		if !ok || len(result) >= 64 {
			continue
		}
		if values := read(key, 0); len(values) > 0 {
			name = shaderLabel(labels, key, name)
			result = append(result, ShaderOptionProfile{Name: name, Values: values})
		}
	}
	return result
}

type shaderOptionFile struct {
	path   string
	config *properties.Properties
	view   ShaderOptions
}

func openShaderOptions(dir, filename, language string) (shaderOptionFile, error) {
	var result shaderOptionFile
	archivePath, err := localFilePath(dir, "shaders", filename)
	if err != nil {
		return result, err
	}
	result.path = strings.TrimSuffix(archivePath, disabledSuffix) + ".txt"
	source, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return result, err
	}
	archive, err := zip.NewReader(source, info.Size())
	if err != nil {
		return result, err
	}
	if len(archive.File) > 16000 {
		return result, errors.New("слишком много файлов в архиве шейдера")
	}
	options, layout, labels, err := readShaderArchive(archive, language)
	if err != nil {
		return result, err
	}
	var data []byte
	if stat, err := os.Lstat(result.path); err == nil {
		if !stat.Mode().IsRegular() || stat.Size() > shaderConfigLimit {
			return result, errors.New("файл настроек шейдера должен быть обычным текстовым файлом")
		}
		file, err := os.Open(result.path)
		if err != nil {
			return result, err
		}
		data, err = io.ReadAll(io.LimitReader(file, shaderConfigLimit+1))
		file.Close()
		if err != nil {
			return result, err
		}
		if len(data) > shaderConfigLimit {
			return result, errors.New("файл настроек шейдера слишком большой")
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}
	result.config, err = shaderProperties(joinShaderPropertyLines(data), properties.ISO_8859_1)
	if err != nil {
		return result, err
	}
	sliders := strings.Fields(layout.GetString("sliders", ""))
	for id, option := range options {
		option.Value = option.Default
		if saved, ok := result.config.Get(id); ok && slices.Contains(option.Choices, saved) {
			option.Value = saved
		}
		if label := labels["option."+id]; label != "" {
			option.Name = label
		}
		if description := labels["option."+id+".comment"]; description != "" {
			option.Description = description
		}
		option.Labels = map[string]string{}
		for _, value := range option.Choices {
			if label := labels["value."+id+"."+value]; label != "" {
				option.Labels[value] = label
			}
		}
		option.Slider = !option.Toggle && slices.Contains(sliders, id)
		options[id] = option
	}
	result.view = ShaderOptions{
		Revision: fmt.Sprintf("%x:%d:%d", sha256.Sum256(data), info.Size(), info.ModTime().UnixNano()),
		Groups:   shaderOptionGroups(options, layout, labels),
	}
	visible := map[string]ShaderOption{}
	for _, group := range result.view.Groups {
		for _, option := range group.Options {
			visible[option.ID] = option
		}
	}
	result.view.Profiles = shaderProfiles(layout, visible, labels)
	return result, nil
}

func ReadShaderOptions(dir, filename, language string) (ShaderOptions, error) {
	file, err := openShaderOptions(dir, filename, language)
	return file.view, err
}

func SaveShaderOptions(dir, filename, revision string, changes map[string]string) error {
	file, err := openShaderOptions(dir, filename, "en")
	if err != nil {
		return err
	}
	if file.view.Revision != revision {
		return errors.New("настройки шейдера изменились, открой редактор заново")
	}
	if len(changes) == 0 {
		return nil
	}
	options := map[string]ShaderOption{}
	for _, group := range file.view.Groups {
		for _, option := range group.Options {
			options[option.ID] = option
		}
	}
	for id, value := range changes {
		option, ok := options[id]
		if !ok || !slices.Contains(option.Choices, value) {
			return fmt.Errorf("недопустимое значение параметра %q", id)
		}
		if value == option.Default {
			file.config.Delete(id)
		} else if _, _, err := file.config.Set(id, value); err != nil {
			return err
		}
	}
	var data bytes.Buffer
	if _, err := file.config.WriteComment(&data, "# ", properties.ISO_8859_1); err != nil {
		return err
	}
	if data.Len() > shaderConfigLimit {
		return errors.New("файл настроек шейдера слишком большой")
	}
	return accountfiles.Write(file.path, data.Bytes())
}
