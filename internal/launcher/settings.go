package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sintet/internal/accountfiles"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	InterfaceLanguage    string `json:"interfaceLanguage,omitempty"`
	OverlayEnabled       bool   `json:"overlayEnabled"`
	GameDirectory        string `json:"gameDirectory"`
	InstanceMode         string `json:"instanceMode"`
	Width                int    `json:"width"`
	Height               int    `json:"height"`
	Fullscreen           bool   `json:"fullscreen"`
	JavaPath             string `json:"javaPath"`
	MemoryMB             int    `json:"memoryMB"`
	AutoMemory           bool   `json:"autoMemory"`
	AutoClose            bool   `json:"autoClose"`
	GameStartAction      string `json:"gameStartAction"`
	CloseAction          string `json:"closeAction"`
	MinimizeToTray       bool   `json:"minimizeToTray"`
	StartWithWindows     bool   `json:"startWithWindows"`
	StartInTray          bool   `json:"startInTray"`
	OverlayHotkey        string `json:"overlayHotkey"`
	ApplyOverlayFont     bool   `json:"applyOverlayFont"`
	WorkshopLanguage     string `json:"workshopLanguage,omitempty"`
	Snapshots            bool   `json:"snapshots"`
	PreReleases          bool   `json:"preReleases"`
	ReleaseCandidates    bool   `json:"releaseCandidates"`
	ExperimentalVersions bool   `json:"experimentalVersions"`
	OldVersions          bool   `json:"oldVersions"`
	OnlyInstalled        bool   `json:"onlyInstalled"`
}
type Configuration struct {
	InterfaceLanguage string   `json:"interfaceLanguage"`
	Settings          Settings `json:"settings"`
	Defaults          Settings `json:"defaults"`
	SystemMemoryMB    int      `json:"systemMemoryMB"`
	EffectiveMemoryMB int      `json:"effectiveMemoryMB"`
	Warning           string   `json:"warning"`
}

func DefaultSettings(root string) Settings {
	return Settings{OverlayEnabled: true, ApplyOverlayFont: true, GameDirectory: root, InstanceMode: "profile", Width: 1280, Height: 720, MemoryMB: 4096, AutoMemory: true, GameStartAction: "tray", CloseAction: "quit", OverlayHotkey: "Shift + Tab"}
}
func ReadSettings(path string, defaults Settings) (Settings, error) {
	settings := defaults
	if e := readJSON(path, &settings); e != nil {
		return settings, e
	}
	// A removed custom Java must not discard the directory and other preferences.
	// Its existence and version are checked when saving or launching instead.
	validation := settings
	validation.JavaPath = ""
	return settings, ValidateSettings(validation)
}
func WriteSettings(path string, settings Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return accountfiles.Write(path, data)
}
func ValidateSettings(s Settings) error {
	if s.InterfaceLanguage != "" && InterfaceLanguage(s.InterfaceLanguage, "") != s.InterfaceLanguage {
		return fmt.Errorf("invalid interface language")
	}
	if s.WorkshopLanguage != "" && !regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`).MatchString(s.WorkshopLanguage) {
		return fmt.Errorf("неверный язык перевода описаний")
	}
	if s.GameStartAction != "" && s.GameStartAction != "tray" && s.GameStartAction != "keep" && s.GameStartAction != "quit" {
		return fmt.Errorf("неизвестное действие при запуске игры")
	}
	if s.CloseAction != "" && s.CloseAction != "tray" && s.CloseAction != "quit" {
		return fmt.Errorf("неизвестное действие при закрытии окна")
	}
	if _, _, _, err := ParseOverlayHotkey(s.OverlayHotkey); err != nil {
		return err
	}
	if !filepath.IsAbs(s.GameDirectory) || filepath.VolumeName(filepath.Clean(s.GameDirectory))+string(os.PathSeparator) == filepath.Clean(s.GameDirectory) {
		return fmt.Errorf("укажите полный путь к папке игры, не корень диска")
	}
	if s.InstanceMode != "profile" && s.InstanceMode != "family" && s.InstanceMode != "shared" {
		return fmt.Errorf("неизвестный режим папок")
	}
	if s.Width < 640 || s.Width > 16384 || s.Height < 480 || s.Height > 16384 {
		return fmt.Errorf("разрешение: ширина 640–16384, высота 480–16384")
	}
	if s.MemoryMB < 512 || s.MemoryMB > 131072 {
		return fmt.Errorf("память должна быть от 512 до 131072 МБ")
	}
	if s.JavaPath != "" {
		if !filepath.IsAbs(s.JavaPath) {
			return fmt.Errorf("нужен полный путь к java.exe")
		}
		info, e := os.Stat(s.JavaPath)
		if e != nil || info.IsDir() {
			return fmt.Errorf("файл Java не найден")
		}
	}
	return nil
}
func EffectiveMemory(settings Settings, total int) int {
	if !settings.AutoMemory {
		return settings.MemoryMB
	}
	memory := total / 3 / 256 * 256
	if memory > 4096 {
		memory = 4096
	}
	if memory < 1024 {
		memory = 1024
	}
	return memory
}

type LaunchOptions struct {
	QuickPlayPort         int
	OpenOverlayLobby      bool
	LauncherVersion       string
	ObserveLog            func([]byte)
	ObserveRun            func(bool)
	PrepareGameBridge     func() (string, error)
	QueueBridgeJoin       func()
	OverlayIPC            string
	OverlayIPCSecret      string
	OverlayAvatar         string
	OverlayFont           string
	OverlayNetworkPinFile string
	InterfaceLanguage     string
	OverlayHotkey         string
	OverlayBuildName      string
	OverlayBuildPreset    string
	OverlayBuildIcon      string
	PrepareOverlay        func(javaPath string) (string, io.Closer, error)
	IdentityProvider      string
	AccessToken           string
	AuthlibAPI            string
	AuthlibAgent          string
	AuthlibMetadata       string
	IdentityUUID          string
	IdentityClientID      string
	IdentityXUID          string
	MinMemoryMB           int
	MemoryMB              int
	Width                 int
	Height                int
	Fullscreen            bool
	JavaPath              string
	JVMArgs               []string
	InstanceMode          string
}

func (s Settings) LaunchOptions(total int) LaunchOptions {
	return LaunchOptions{MinMemoryMB: 512, MemoryMB: EffectiveMemory(s, total), Width: s.Width, Height: s.Height, Fullscreen: s.Fullscreen, JavaPath: s.JavaPath, InstanceMode: s.InstanceMode}
}

type JavaInfo struct {
	Path    string `json:"path"`
	Major   int    `json:"major"`
	Version string `json:"version"`
}

func CheckJava(ctx context.Context, path string) (JavaInfo, error) {
	info := JavaInfo{Path: path}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	probe := path
	if strings.EqualFold(filepath.Base(path), "javaw.exe") {
		probe = filepath.Join(filepath.Dir(path), "java.exe")
	}
	cmd := exec.CommandContext(ctx, probe, "-version")
	hideProcess(cmd)
	data, e := cmd.CombinedOutput()
	if e != nil {
		return info, fmt.Errorf("Java не запускается: %w", e)
	}
	match := regexp.MustCompile(`(?:openjdk|java) version "([^"]+)"`).FindStringSubmatch(string(data))
	if len(match) < 2 {
		return info, fmt.Errorf("не удалось определить версию Java")
	}
	info.Version = match[1]
	parts := strings.Split(info.Version, ".")
	first := parts[0]
	if first == "1" && len(parts) > 1 {
		first = parts[1]
	}
	info.Major, _ = strconv.Atoi(first)
	if info.Major == 0 {
		return info, fmt.Errorf("неизвестная версия Java %s", info.Version)
	}
	return info, nil
}
func checkJavaForProfile(ctx context.Context, path string, major int) error {
	if major == 0 {
		major = 8
	}
	info, e := CheckJava(ctx, path)
	if e != nil {
		return e
	}
	if info.Major != major {
		return fmt.Errorf("для выбранной версии нужна Java %d, а выбрана Java %d; включите автоматический выбор", major, info.Major)
	}
	return nil
}

// ProfileDirectory resolves the same directory as launch, including the current
// profile/family/shared preference, rather than the receipt's install-time path.
func (s *Service) ProfileDirectory(id, mode string) (string, error) {
	if mode != "profile" && mode != "family" && mode != "shared" {
		return "", fmt.Errorf("неверный режим папок")
	}
	r, err := s.readReceipt(id)
	if err != nil || !s.Installed(id) {
		return "", fmt.Errorf("сначала установите выбранную версию")
	}
	if err = validateID(r.Base); err != nil {
		return "", err
	}
	if err = validateID(r.Loader); err != nil {
		return "", err
	}
	if strings.HasPrefix(r.ID, "build-") {
		mode = "profile"
	}
	path := s.gameDirectory(r, mode)
	rel, err := filepath.Rel(s.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("неверный путь профиля")
	}
	if err = os.MkdirAll(filepath.Join(path, "mods"), 0755); err != nil {
		return "", err
	}
	return path, nil
}
func (s *Service) gameDirectory(r Receipt, mode string) string {
	if strings.HasPrefix(r.ID, "build-") {
		return filepath.Join(s.Root, "instances", r.ID)
	}
	switch mode {
	case "shared":
		return s.Root
	case "family":
		parts := strings.Split(r.Base, ".")
		family := r.Base
		if len(parts) > 2 && parts[0] == "1" {
			family = strings.Join(parts[:2], ".")
		}
		return filepath.Join(s.Root, "instances", "family", r.Loader+"-"+family)
	default:
		return filepath.Join(s.Root, "instances", r.ID)
	}
}
