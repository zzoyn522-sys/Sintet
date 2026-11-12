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
	"strconv"
	"strings"
	"time"
)

func resolveArguments(raw []json.RawMessage, vars map[string]string) ([]string, error) {
	var result []string
	for _, item := range raw {
		var plain string
		if json.Unmarshal(item, &plain) == nil {
			result = append(result, plain)
			continue
		}
		var conditional struct {
			Rules []Rule          `json:"rules"`
			Value json.RawMessage `json:"value"`
		}
		if e := json.Unmarshal(item, &conditional); e != nil {
			return nil, e
		}
		if !allowed(conditional.Rules, map[string]bool{"has_custom_resolution": true}) {
			continue
		}
		if json.Unmarshal(conditional.Value, &plain) == nil {
			result = append(result, plain)
		} else {
			var values []string
			if e := json.Unmarshal(conditional.Value, &values); e != nil {
				return nil, e
			}
			result = append(result, values...)
		}
	}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	for i, arg := range result {
		var missing string
		result[i] = re.ReplaceAllStringFunc(arg, func(v string) string {
			key := v[2 : len(v)-1]
			value, ok := vars[key]
			if !ok {
				missing = key
			}
			return value
		})
		if missing != "" {
			return nil, fmt.Errorf("неизвестный аргумент версии: %s", missing)
		}
	}
	return result, nil
}
func (s *Service) Launch(id, name string, memory int, autoclose bool) error {
	return s.LaunchWithOptions(id, name, LaunchOptions{MemoryMB: memory * 1024, Width: 1100, Height: 700, InstanceMode: "profile"})
}
func (s *Service) LaunchWithOptions(id, name string, options LaunchOptions) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`).MatchString(name) {
		return fmt.Errorf("ник: 3–16 символов A–Z, 0–9 и _")
	}
	if options.MinMemoryMB == 0 {
		options.MinMemoryMB = 512
	}
	if options.MinMemoryMB < 512 || options.MinMemoryMB > options.MemoryMB {
		return fmt.Errorf("минимальная память должна быть не меньше 512 МБ и не превышать максимум")
	}
	validation := DefaultSettings(s.Root)
	validation.MemoryMB = options.MemoryMB
	validation.Width = options.Width
	validation.Height = options.Height
	validation.InstanceMode = options.InstanceMode
	validation.JavaPath = options.JavaPath
	if e := ValidateSettings(validation); e != nil {
		return e
	}
	r, e := s.readReceipt(id)
	if e != nil || !s.Installed(id) {
		return fmt.Errorf("сначала установите выбранную версию")
	}
	s.mu.Lock()
	if s.state.Busy || s.state.Running {
		s.mu.Unlock()
		return fmt.Errorf("игра или установка уже запущена")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.state = State{Busy: true, Profile: id, Stage: "Проверка файлов перед запуском"}
	s.mu.Unlock()
	go s.launch(ctx, cancel, r, name, options)
	return nil
}

type launchSpec struct {
	Java      string
	Args      []string
	Directory string
}

func (s *Service) buildLaunch(r Receipt, name string, options LaunchOptions) (launchSpec, error) {
	gameDir := s.gameDirectory(r, options.InstanceMode)
	java := options.JavaPath
	if java == "" {
		java = r.Java
	}
	spec := launchSpec{Java: java, Directory: gameDir}
	identityUUID := offlineUUID(name)
	if options.IdentityUUID != "" {
		if !regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`).MatchString(options.IdentityUUID) {
			return spec, fmt.Errorf("неверный UUID аккаунта")
		}
		identityUUID = strings.ReplaceAll(options.IdentityUUID, "-", "")
	}
	launcherVersion := options.LauncherVersion
	if launcherVersion == "" {
		launcherVersion = "0.8.2"
	}
	vars := map[string]string{"auth_player_name": name, "version_name": r.ID, "game_directory": gameDir, "assets_root": filepath.Join(s.Root, "assets"), "assets_index_name": r.AssetIndexID, "auth_uuid": identityUUID, "auth_access_token": "0", "clientid": "", "auth_xuid": "", "user_type": "mojang", "version_type": r.Profile.Type, "natives_directory": filepath.Join(s.Root, "natives", r.ID), "launcher_name": "Sintet", "launcher_version": launcherVersion, "classpath": strings.Join(r.Classpath, string(os.PathListSeparator)), "classpath_separator": string(os.PathListSeparator), "library_directory": filepath.Join(s.Root, "libraries"), "resolution_width": strconv.Itoa(options.Width), "resolution_height": strconv.Itoa(options.Height), "user_properties": "{}", "auth_session": "token:0:" + identityUUID, "game_assets": filepath.Join(s.Root, "assets", "virtual", r.AssetIndexID)}
	if options.IdentityProvider == "microsoft" {
		if options.AccessToken == "" || options.IdentityUUID == "" || options.IdentityClientID == "" {
			return spec, fmt.Errorf("сессия Microsoft не подготовлена")
		}
		vars["auth_access_token"] = options.AccessToken
		vars["auth_session"] = "token:" + options.AccessToken + ":" + identityUUID
		vars["user_type"] = "msa"
		vars["clientid"] = options.IdentityClientID
		vars["auth_xuid"] = options.IdentityXUID
	} else if options.IdentityProvider == "ely" || (options.IdentityProvider == "sintet" && options.AccessToken != "") {
		if options.AccessToken == "" || options.IdentityUUID == "" || options.AuthlibAgent == "" || options.AuthlibMetadata == "" {
			return spec, fmt.Errorf("вход с подключением скинов не подготовлен")
		}
		vars["auth_access_token"] = options.AccessToken
		vars["auth_session"] = "token:" + options.AccessToken + ":" + identityUUID
	} else if options.AccessToken != "" {
		return spec, fmt.Errorf("неверный источник сессии")
	}
	args, e := resolveArguments(r.Profile.Arguments.JVM, vars)
	if e != nil {
		return spec, e
	}
	if len(args) == 0 {
		args = []string{"-Djava.library.path=" + vars["natives_directory"], "-cp", vars["classpath"]}
	}
	if r.Loader == "forge" || r.Loader == "neoforge" {
		for i, arg := range args {
			if strings.HasPrefix(arg, "-DignoreList=") {
				args[i] = arg + "," + r.Base + ".jar"
			}
		}
	}
	args = append([]string{"-Xms" + strconv.Itoa(options.MinMemoryMB) + "M", "-Xmx" + strconv.Itoa(options.MemoryMB) + "M", "-Dlog4j2.formatMsgNoLookups=true"}, args...)
	if len(options.JVMArgs) > 0 {
		combined := make([]string, 0, len(args)+len(options.JVMArgs))
		combined = append(combined, args[:3]...)
		combined = append(combined, options.JVMArgs...)
		args = append(combined, args[3:]...)
	}
	if log := r.Profile.Logging.Client; log.Argument != "" {
		args = append(args, strings.ReplaceAll(log.Argument, "${path}", filepath.Join(s.Root, "assets", "log_configs", log.File.ID)))
	}
	if options.IdentityProvider == "ely" || (options.IdentityProvider == "sintet" && options.AccessToken != "") {
		api := elyInjectorAPI
		if options.IdentityProvider == "sintet" {
			api = options.AuthlibAPI
			if err := validateSkinServer(api); err != nil {
				return spec, err
			}
		}
		args = append(args, "-javaagent:"+options.AuthlibAgent+"="+api, "-Dauthlibinjector.yggdrasil.prefetched="+options.AuthlibMetadata)
	}
	args = append(args, r.Profile.MainClass)
	game, e := resolveArguments(r.Profile.Arguments.Game, vars)
	if e != nil {
		return spec, e
	}
	if r.Profile.MinecraftArguments != "" {
		game, e = resolveArguments(rawStrings(strings.Fields(r.Profile.MinecraftArguments)...), vars)
		if e != nil {
			return spec, e
		}
	}
	hasWidth, hasHeight := false, false
	for _, arg := range game {
		hasWidth = hasWidth || arg == "--width"
		hasHeight = hasHeight || arg == "--height"
	}
	if !hasWidth {
		game = append(game, "--width", vars["resolution_width"])
	}
	if !hasHeight {
		game = append(game, "--height", vars["resolution_height"])
	}
	if options.Fullscreen {
		game = append(game, "--fullscreen")
	}
	if options.QuickPlayPort != 0 {
		if options.QuickPlayPort < 1 || options.QuickPlayPort > 65535 {
			return spec, fmt.Errorf("неверный порт P2P")
		}
		if options.QueueBridgeJoin == nil {
			game = appendQuickPlay(game, r.Profile, options.QuickPlayPort)
		}
	}
	spec.Args = append(args, game...)
	return spec, nil
}
func appendQuickPlay(args []string, profile Profile, port int) []string {
	address := strconv.Itoa(port)
	for _, value := range profile.Arguments.Game {
		if strings.Contains(string(value), "--quickPlayMultiplayer") {
			return append(args, "--quickPlayMultiplayer", "127.0.0.1:"+address)
		}
	}
	return append(args, "--server", "127.0.0.1", "--port", address)
}

func (s *Service) launch(ctx context.Context, cancel context.CancelFunc, r Receipt, name string, options LaunchOptions) {
	defer cancel()
	var log *os.File
	fail := func(e error) {
		if log != nil {
			fmt.Fprintf(log, "\nОшибка запуска: %v\n", e)
		}
		s.mu.Lock()
		s.state.Busy = false
		s.state.Running = false
		s.state.Stage = "Ошибка запуска"
		s.state.Error = e.Error()
		s.cancel = nil
		s.mu.Unlock()
	}
	logDir := filepath.Join(s.Root, ".sintet", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fail(err)
		return
	}
	logName := r.ID + "-" + time.Now().Format("20060102-150405.000000000") + ".log"
	logPath := filepath.Join(logDir, logName)
	var e error
	log, e = os.Create(logPath)
	if e != nil {
		fail(e)
		return
	}
	defer log.Close()
	fmt.Fprintf(log, "Sintet | %s | %s\nMinecraft: %s | %s %s\n", r.ID, time.Now().Format(time.RFC3339), r.Base, r.Loader, r.LoaderVersion)
	s.mu.Lock()
	s.state.Log = logPath
	s.mu.Unlock()
	for _, f := range r.Files {
		if e := ctx.Err(); e != nil {
			fail(e)
			return
		}
		path, e := join(s.Root, f.Path)
		if e != nil || !validFile(path, f, true) {
			fail(fmt.Errorf("повреждён или отсутствует файл %s; нажмите «Переустановить»", f.Path))
			return
		}
	}
	if options.JavaPath != "" {
		e = checkJavaForProfile(ctx, options.JavaPath, r.Profile.JavaVersion.MajorVersion)
		r.Java = options.JavaPath
	} else {
		r.Java, e = s.ensureJava(ctx, r.Profile.JavaVersion.MajorVersion)
	}
	if e != nil {
		fail(e)
		return
	}
	if options.IdentityProvider == "ely" {
		options.AuthlibAgent, options.AuthlibMetadata, e = s.prepareEly(ctx)
		if e != nil {
			fail(e)
			return
		}
	}
	if options.IdentityProvider == "sintet" && options.AccessToken != "" {
		options.AuthlibAgent, options.AuthlibMetadata, e = s.prepareSkinServer(ctx, options.AuthlibAPI)
		if e != nil {
			fail(e)
			return
		}
	}
	spec, e := s.buildLaunch(r, name, options)
	if e != nil {
		fail(e)
		return
	}
	r.GameDir = spec.Directory
	if e = os.MkdirAll(filepath.Join(r.GameDir, "mods"), 0755); e != nil {
		fail(e)
		return
	}
	if e = s.prepare(ctx, &r); e != nil {
		fail(e)
		return
	}
	fmt.Fprintf(log, "Java: %s\nMemory: %d MB | Resolution: %dx%d | Fullscreen: %t\nGameDir: %s\n", spec.Java, options.MemoryMB, options.Width, options.Height, options.Fullscreen, spec.Directory)
	if e = ctx.Err(); e != nil {
		fail(e)
		return
	}
	var overlayPath, bridgeMappings string
	bridgeEnabled := false
	if options.PrepareOverlay != nil {
		path, lock, overlayErr := options.PrepareOverlay(spec.Java)
		if overlayErr != nil {
			fmt.Fprintf(log, "Sintet Overlay: %v\n", overlayErr)
		} else if path != "" {
			overlayPath = path
			if lock != nil {
				defer lock.Close()
			}
			spec.Args = append([]string{"-agentpath:" + path}, spec.Args...)
		}
	}
	if options.PrepareGameBridge != nil {
		bridge, err := options.PrepareGameBridge()
		switch {
		case err != nil:
			fmt.Fprintf(log, "Sintet Bridge: мост отключён для этого запуска: %v\n", err)
		case strings.Contains(bridge, "="):
			fmt.Fprintln(log, "Sintet Bridge: мост отключён для этого запуска: путь содержит знак =")
		case bridge != "":
			bridgeEnabled = true
			spec.Args = append([]string{"-javaagent:" + bridge}, spec.Args...)
			var mappingErr error
			bridgeMappings, mappingErr = s.bridgeMappings(ctx, r)
			if mappingErr != nil {
				fmt.Fprintf(log, "Sintet Bridge: не удалось получить таблицу имён Minecraft: %v\n", mappingErr)
			}
		}
	}
	if options.QueueBridgeJoin != nil && !bridgeEnabled && options.QuickPlayPort != 0 {
		spec.Args = appendQuickPlay(spec.Args, r.Profile, options.QuickPlayPort)
	}
	cmd := exec.Command(spec.Java, spec.Args...)
	cmd.Dir = spec.Directory
	if overlayPath != "" || bridgeEnabled {
		cmd.Env = append(cmd.Environ(), overlayEnvironment(r, name, options)...)
		cmd.Env = append(cmd.Env, "SINTET_BRIDGE_MAPPINGS="+bridgeMappings)
	}
	var logOutput io.Writer = log
	if options.ObserveLog != nil {
		logOutput = &observedGameLog{target: log, observe: options.ObserveLog}
	}
	// Apply the same streaming redaction to both the Minecraft token and the local IPC credential.
	ipcRedactor := &tokenRedactor{target: logOutput, secret: options.OverlayIPCSecret}
	redacted := &tokenRedactor{target: ipcRedactor, secret: options.AccessToken}
	cmd.Stdout = redacted
	cmd.Stderr = redacted
	hideProcess(cmd)
	if e = cmd.Start(); e != nil {
		fail(e)
		return
	}
	if options.ObserveRun != nil {
		options.ObserveRun(true)
	}
	s.mu.Lock()
	s.state.PID = cmd.Process.Pid
	s.state.Directory = spec.Directory
	s.state.GameVersion = r.Base
	s.state.GameLoader = r.Loader
	s.state.GameBuild = options.OverlayBuildName
	s.state.GameStarted = time.Now().Unix()
	if options.IdentityProvider == "sintet" {
		s.state.GameAccount = options.IdentityUUID
	}
	s.state.Busy = false
	s.state.Running = true
	s.state.Stage = "Minecraft запущен"
	s.cancel = nil
	s.mu.Unlock()
	if bridgeEnabled && options.QueueBridgeJoin != nil {
		options.QueueBridgeJoin()
	}
	e = cmd.Wait()
	redacted.Flush()
	ipcRedactor.Flush()
	if e != nil {
		fmt.Fprintf(log, "\nMinecraft завершился с ошибкой: %v\n", e)
	}
	if options.ObserveRun != nil {
		options.ObserveRun(false)
	}
	s.mu.Lock()
	s.state.PID = 0
	s.state.Running = false
	if e != nil {
		s.state.Stage = "Minecraft завершился с ошибкой"
		s.state.Error = e.Error()
	} else {
		s.state.Stage = "Игра закрыта"
	}
	s.mu.Unlock()
}

func overlayEnvironment(r Receipt, name string, options LaunchOptions) []string {
	label, key, modifiers, _ := ParseOverlayHotkey(options.OverlayHotkey)
	base := r.Base
	if base == "" {
		base = r.ID
	}
	loader := map[string]string{"vanilla": "Vanilla", "forge": "Forge", "fabric": "Fabric", "neoforge": "NeoForge"}[r.Loader]
	if loader != "" && r.LoaderVersion != "" {
		version := strings.TrimSuffix(strings.TrimPrefix(r.LoaderVersion, base+"-"), "-"+base)
		loader += " " + version
	}
	return []string{
		"SINTET_OVERLAY_OPEN_LOBBY=" + strconv.FormatBool(options.OpenOverlayLobby),
		"SINTET_OVERLAY_LANGUAGE=" + options.InterfaceLanguage,
		"SINTET_OVERLAY_HOTKEY=" + label,
		"SINTET_OVERLAY_HOTKEY_KEY=" + strconv.Itoa(key),
		"SINTET_OVERLAY_HOTKEY_MODS=" + strconv.Itoa(modifiers),
		"SINTET_OVERLAY_PLAYER=" + name,
		"SINTET_OVERLAY_VERSION=" + base,
		"SINTET_OVERLAY_LOADER=" + loader,
		"SINTET_OVERLAY_BUILD_NAME=" + options.OverlayBuildName,
		"SINTET_OVERLAY_BUILD_PRESET=" + options.OverlayBuildPreset,
		"SINTET_OVERLAY_BUILD_ICON=" + options.OverlayBuildIcon,
		"SINTET_OVERLAY_AVATAR=" + options.OverlayAvatar,
		"SINTET_OVERLAY_FONT=" + options.OverlayFont,
		"SINTET_OVERLAY_NETWORK_PIN_FILE=" + options.OverlayNetworkPinFile,
		"SINTET_OVERLAY_ID=" + overlayIdentity(name, options),
		"SINTET_OVERLAY_IPC=" + options.OverlayIPC,
		"SINTET_OVERLAY_IPC_SECRET=" + options.OverlayIPCSecret,
	}
}

func overlayIdentity(name string, options LaunchOptions) string {
	if options.IdentityUUID != "" {
		return options.IdentityUUID
	}
	return offlineUUID(name)
}

// Observe only redacted output, with the same ordering as the on-disk log.
type observedGameLog struct {
	target  io.Writer
	observe func([]byte)
}

func (w *observedGameLog) Write(data []byte) (int, error) {
	n, err := w.target.Write(data)
	if n > 0 {
		w.observe(data[:n])
	}
	return n, err
}
