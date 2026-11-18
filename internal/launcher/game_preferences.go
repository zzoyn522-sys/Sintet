package launcher

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sintet/internal/accountfiles"
)

// Empty values leave the corresponding game options alone.
type GamePreferences struct {
	Language        string `json:"language,omitempty"`
	GUIScale        string `json:"guiScale,omitempty"`
	MasterVolume    string `json:"masterVolume,omitempty"`
	FOV             string `json:"fov,omitempty"`
	DisableBobbing  bool   `json:"disableBobbing"`
	DisableMusic    bool   `json:"disableMusic"`
	DisableNarrator bool   `json:"disableNarrator"`
}

var (
	gameLocale   = regexp.MustCompile(`^[a-z]{2,8}(?:_[a-z0-9]{2,8}){0,2}$`)
	releaseID    = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?(?:$|[-_ ])`)
	snapshotID   = regexp.MustCompile(`^(\d{2})w(\d{2})[a-z]`)
	preReleaseID = regexp.MustCompile(`^(?:[ac]\d|rd-|inf-|in-)`)
	betaID       = regexp.MustCompile(`^b1\.(\d+)`)
)

func (p GamePreferences) validate() error {
	if p.Language != "" && !gameLocale.MatchString(p.Language) {
		return fmt.Errorf("неверный код языка Minecraft")
	}
	if p.GUIScale != "" && (len(p.GUIScale) != 1 || p.GUIScale[0] < '0' || p.GUIScale[0] > '8') {
		return fmt.Errorf("масштаб интерфейса Minecraft должен быть от 0 до 8")
	}
	if p.MasterVolume != "" && !gamePreferenceInRange(p.MasterVolume, 0, 100) {
		return fmt.Errorf("общая громкость Minecraft должна быть от 0 до 100%%")
	}
	if p.FOV != "" && !gamePreferenceInRange(p.FOV, 70, 110) {
		return fmt.Errorf("поле зрения Minecraft должно быть от 70 до 110 градусов")
	}
	return nil
}

func gamePreferenceInRange(value string, min, max int) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n >= min && n <= max && strconv.Itoa(n) == value
}

type gameOptionFormat struct {
	language, scale, modernMusic, narrator, narratorHotkey, largeScale bool
	fov, bobView, legacySound, masterVolume, booleanSound              bool
}

func optionFormat(base string) gameOptionFormat {
	if preReleaseID.MatchString(base) {
		return gameOptionFormat{}
	}
	if beta := betaID.FindStringSubmatch(base); beta != nil {
		minor, _ := strconv.Atoi(beta[1])
		return gameOptionFormat{scale: minor >= 5, fov: minor >= 8, bobView: true, legacySound: true}
	}
	if snapshot := snapshotID.FindStringSubmatch(base); snapshot != nil {
		year, _ := strconv.Atoi(snapshot[1])
		week, _ := strconv.Atoi(snapshot[2])
		date := year*100 + week
		return gameOptionFormat{
			language: true, scale: true, modernMusic: date >= 1336, narrator: date >= 1713,
			narratorHotkey: date >= 2333, largeScale: date >= 1743,
			fov: true, bobView: true, legacySound: date < 1336, masterVolume: date >= 1336,
		}
	}
	if release := releaseID.FindStringSubmatch(base); release != nil && release[1] == "1" {
		minor, _ := strconv.Atoi(release[2])
		patch, _ := strconv.Atoi(release[3])
		return gameOptionFormat{
			language: minor >= 1, scale: true, modernMusic: minor >= 7, narrator: minor >= 12,
			narratorHotkey: minor > 20 || minor == 20 && patch >= 2, largeScale: minor >= 13,
			fov: true, bobView: true, legacySound: minor < 7, masterVolume: minor >= 7,
		}
	}
	// Year-based and unrecognized future releases use today's native keys.
	return gameOptionFormat{
		language: true, scale: true, modernMusic: true, narrator: true, narratorHotkey: true,
		largeScale: true, fov: true, bobView: true, masterVolume: true,
	}
}

type gameOption struct{ key, value string }

func (s *Service) applyGamePreferences(r Receipt, p GamePreferences) error {
	if p == (GamePreferences{}) {
		return nil
	}
	format := optionFormat(r.Base)
	if p.MasterVolume != "" || p.FOV != "" || p.DisableBobbing || p.DisableMusic && preReleaseID.MatchString(r.Base) {
		s.inspectGameOptionFormat(r.Base, &format)
	}
	var changes []gameOption
	if p.Language != "" && format.language {
		if code := s.installedGameLocale(r, p.Language); code != "" {
			changes = append(changes, gameOption{"lang", code})
		}
	}
	if p.GUIScale != "" && format.scale {
		scale := p.GUIScale
		if !format.largeScale && scale > "3" {
			scale = "3"
		}
		changes = append(changes, gameOption{"guiScale", scale})
	}
	if p.DisableMusic {
		key := "music"
		value := "0.0"
		if format.modernMusic {
			key = "soundCategory_music"
		} else if format.booleanSound {
			value = "false"
		}
		changes = append(changes, gameOption{key, value})
	}
	if p.DisableNarrator && format.narrator {
		changes = append(changes, gameOption{"narrator", "0"})
		if format.narratorHotkey {
			changes = append(changes, gameOption{"narratorHotkey", "false"})
		}
	}
	if p.MasterVolume != "" {
		percent, _ := strconv.Atoi(p.MasterVolume)
		volume := strconv.FormatFloat(float64(percent)/100, 'f', 2, 64)
		switch {
		case format.masterVolume:
			changes = append(changes, gameOption{"soundCategory_master", volume})
		case format.legacySound && (!format.booleanSound || percent == 0):
			if format.booleanSound {
				volume = "false"
			}
			changes = append(changes, gameOption{"sound", volume})
			if !p.DisableMusic {
				changes = append(changes, gameOption{"music", volume})
			}
		}
	}
	if p.FOV != "" && format.fov {
		degrees, _ := strconv.Atoi(p.FOV)
		// Minecraft stores a slider position, even in current clients.
		changes = append(changes, gameOption{"fov", strconv.FormatFloat(float64(degrees-70)/40, 'f', 3, 64)})
	}
	if p.DisableBobbing && format.bobView {
		changes = append(changes, gameOption{"bobView", "false"})
	}
	if len(changes) == 0 {
		return nil
	}
	file := filepath.Join(r.GameDir, "options.txt")
	data, err := readGameOptions(file)
	if err != nil {
		return err
	}
	if updated := mergeGameOptions(data, changes); updated != data {
		return accountfiles.Write(file, []byte(updated))
	}
	return nil
}

func (s *Service) inspectGameOptionFormat(base string, format *gameOptionFormat) {
	if validateID(base) != nil {
		return
	}
	client, err := zip.OpenReader(filepath.Join(s.Root, "versions", base, base+".jar"))
	if err != nil {
		return
	}
	defer client.Close()

	early := preReleaseID.MatchString(base)
	for _, entry := range client.File {
		if entry.Name != "net/minecraft/client/Options.class" && (!early || !strings.HasSuffix(entry.Name, ".class")) {
			continue
		}
		const limit = 512 << 10
		if entry.UncompressedSize64 > limit {
			continue
		}
		f, err := entry.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, limit))
		f.Close()
		if err != nil || early && !hasClassOptionString(data, "options.txt") {
			continue
		}
		format.fov = hasClassOptionString(data, "fov")
		format.bobView = hasClassOptionString(data, "bobView")
		if early {
			format.legacySound = hasClassOptionString(data, "sound") && hasClassOptionString(data, "music")
			format.booleanSound = !hasClassOptionString(data, "parseFloat")
		} else {
			// New clients use a string-concat recipe to append the sound category.
			format.masterVolume = hasClassOptionString(data, "soundCategory_master") ||
				hasClassOptionString(data, "soundCategory_") || hasClassOptionString(data, "soundCategory_\x01")
		}
		return
	}
}

func hasClassOptionString(data []byte, value string) bool {
	// Match complete CONSTANT_Utf8 entries, not substrings of other option names.
	return bytes.Contains(data, append([]byte{1, byte(len(value) >> 8), byte(len(value))}, value...))
}

func (s *Service) installedGameLocale(r Receipt, wanted string) string {
	// Look up the exact spelling shipped by Minecraft. Older clients use
	// ru_RU, for example, while newer clients use ru_ru.
	if r.AssetIndexID != "" && validateID(r.AssetIndexID) == nil {
		var index AssetIndex
		if readJSON(filepath.Join(s.Root, "assets", "indexes", r.AssetIndexID+".json"), &index) == nil {
			for key := range index.Objects {
				if code := localeAsset(key, wanted); code != "" {
					return code
				}
			}
		}
	}
	if validateID(r.Base) != nil {
		return ""
	}
	client, err := zip.OpenReader(filepath.Join(s.Root, "versions", r.Base, r.Base+".jar"))
	if err != nil {
		return ""
	}
	defer client.Close()
	for _, entry := range client.File {
		if code := localeAsset(entry.Name, wanted); code != "" {
			return code
		}
	}
	return ""
}

func localeAsset(name, wanted string) string {
	dir := path.Dir(name)
	if dir != "minecraft/lang" && dir != "assets/minecraft/lang" && dir != "lang" {
		return ""
	}
	ext := path.Ext(name)
	if ext != ".lang" && ext != ".json" {
		return ""
	}
	code := strings.TrimSuffix(path.Base(name), ext)
	if strings.EqualFold(code, wanted) {
		return code
	}
	return ""
}

func readGameOptions(file string) (string, error) {
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	const limit = 4 << 20
	if !info.Mode().IsRegular() || info.Size() > limit {
		return "", fmt.Errorf("не удалось безопасно открыть options.txt")
	}
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && (len(data) > limit || strings.ContainsRune(string(data), 0)) {
		return "", fmt.Errorf("неподдерживаемый формат options.txt")
	}
	return string(data), err
}

func mergeGameOptions(data string, changes []gameOption) string {
	const bom = "\ufeff"
	newline := "\n"
	if strings.Contains(data, "\r\n") {
		newline = "\r\n"
	}
	pending := make(map[string]string, len(changes))
	for _, change := range changes {
		pending[change.key] = change.value
	}
	seen := make(map[string]bool, len(changes))
	var output strings.Builder
	if strings.HasPrefix(data, bom) {
		output.WriteString(bom)
		data = strings.TrimPrefix(data, bom)
	}
	for _, line := range strings.SplitAfter(data, "\n") {
		key, previous, ok := strings.Cut(line, ":")
		value, managed := pending[key]
		if !ok || !managed {
			output.WriteString(line)
			continue
		}
		seen[key] = true
		if key == "music" || key == "sound" {
			updated, apply := legacyVolume(previous, value)
			if !apply {
				output.WriteString(line)
				continue
			}
			value = updated
		}
		output.WriteString(key + ":" + value)
		// Preserve the terminator of each line, even in a mixed file.
		if strings.HasSuffix(line, "\r\n") {
			output.WriteString("\r\n")
		} else if strings.HasSuffix(line, "\n") {
			output.WriteByte('\n')
		}
	}
	for _, change := range changes {
		if !seen[change.key] {
			current := output.String()
			if current != "" && current != bom && !strings.HasSuffix(current, "\n") {
				output.WriteString(newline)
			}
			output.WriteString(change.key + ":" + change.value + newline)
		}
	}
	return output.String()
}

func legacyVolume(previous, wanted string) (string, bool) {
	previous = strings.TrimSpace(previous)
	if previous == "true" || previous == "false" {
		if wanted == "false" {
			return wanted, true
		}
		volume, _ := strconv.ParseFloat(wanted, 64)
		return "false", volume == 0
	}
	if wanted != "false" {
		current, err := strconv.ParseFloat(previous, 64)
		limit, _ := strconv.ParseFloat(wanted, 64)
		// Old clients have separate music and sound sliders. A cap keeps
		// already quieter channels, including muted ones, at their current level.
		if err == nil && current >= 0 && current <= limit {
			return previous, false
		}
	}
	return wanted, true
}
