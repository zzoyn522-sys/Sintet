package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

type socialActivity struct {
	Mode      string         `json:"mode"`
	Version   string         `json:"version,omitempty"`
	Loader    string         `json:"loader,omitempty"`
	Build     string         `json:"build,omitempty"`
	Server    string         `json:"server,omitempty"`
	StartedAt int64          `json:"startedAt,omitempty"`
	Music     *presenceMusic `json:"music,omitempty"`
	accountID string
}

type presenceMusic struct {
	Title  string `json:"title"`
	Artist string `json:"artist,omitempty"`
	Digest string `json:"digest,omitempty"`
	Cover  string `json:"cover,omitempty"`
}

type presencePrivacy struct {
	Invisible  bool `json:"invisible"`
	ShareMusic bool `json:"shareMusic"`
}

type friendActivityTotals struct {
	LauncherMs  int64 `json:"launcherMs"`
	MinecraftMs int64 `json:"minecraftMs"`
}

func (m *socialManager) publishPresence(activity socialActivity, status, version string) error {
	m.mu.Lock()
	client, epoch, account := m.client, m.epoch, m.account
	privacy, uploaded := m.state.Privacy, m.sharedMusicCover
	m.mu.Unlock()
	if client == nil {
		m.app.presenceMedia(false)
		return nil
	}
	if privacy.Invisible {
		activity, status, version = socialActivity{Mode: "launcher"}, "launcher", ""
	}
	music, cover := m.app.presenceMedia(privacy.ShareMusic && !privacy.Invisible)
	var uploadKey string
	if music != nil && music.Digest != "" {
		uploadKey = account + ":" + music.Digest
		if uploadKey != uploaded {
			music.Cover = base64.StdEncoding.EncodeToString(cover)
		}
	}
	activity.Music = music
	ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
	defer cancel()
	err := client.SocialRequest(ctx, "POST", "/api/social", map[string]any{
		"action": "presence", "status": status, "version": version, "activity": activity,
	}, nil)
	if err == nil && uploadKey != "" {
		m.mu.Lock()
		if m.epoch == epoch && m.state.Privacy == privacy {
			m.sharedMusicCover = uploadKey
		}
		m.mu.Unlock()
	}
	return err
}

func (a *App) SavePresencePrivacy(invisible, shareMusic bool) (presencePrivacy, error) {
	m := a.social
	if m == nil {
		return presencePrivacy{}, errors.New("сервис друзей запускается")
	}
	m.mu.Lock()
	client, epoch := m.client, m.epoch
	m.mu.Unlock()
	if client == nil {
		return presencePrivacy{}, errors.New("войдите через Sintet ID")
	}
	ctx, cancel := context.WithTimeout(m.ctx, 12*time.Second)
	defer cancel()
	var result presencePrivacy
	if err := client.SocialRequest(ctx, "POST", "/api/social", map[string]any{
		"action": "privacy", "invisible": invisible, "shareMusic": shareMusic,
	}, &result); err != nil {
		return result, err
	}
	m.mu.Lock()
	if m.epoch == epoch {
		m.state.Privacy = result
		m.sharedMusicCover = ""
		m.privacyRevision++
	}
	m.mu.Unlock()
	return result, nil
}

func (a *App) FriendsMusicCover(account, digest string) (string, error) {
	if a.social == nil || len(digest) != sha256.Size*2 {
		return "", nil
	}
	if _, err := uuid.Parse(account); err != nil {
		return "", nil
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", nil
	}
	query := url.Values{"account": {account}, "digest": {digest}}
	var result struct {
		Cover string `json:"cover"`
	}
	if err := a.social.request(a.social.ctx, "GET", "/api/social/music-cover?"+query.Encode(), nil, &result); err != nil {
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Cover)
	if err != nil || len(decoded) > 40*1024 {
		return "", nil
	}
	actual := sha256.Sum256(decoded)
	if hex.EncodeToString(actual[:]) != digest {
		return "", nil
	}
	return "data:image/png;base64," + result.Cover, nil
}

func (a socialActivity) legacyStatus() string {
	switch a.Mode {
	case "launcher", "menu":
		return a.Mode
	default:
		return "world"
	}
}

var minecraftFormatting = regexp.MustCompile(`(?i)§[0-9a-fk-orx]`)

func cleanActivityLabel(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, minecraftFormatting.ReplaceAllString(text, ""))
	runes := []rune(strings.Join(strings.Fields(text), " "))
	return strings.TrimSpace(string(runes[:min(len(runes), 72)]))
}

var serverHostname = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func serverHost(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(address), "[]."))
}

// A server's name or public hostname can be shared with friends. Addresses of
// LAN servers, loopback tunnels and numeric IPs stay local.
func publicServerLabel(name, address string) string {
	host := serverHost(address)
	if !serverHostname.MatchString(host) {
		return ""
	}
	for _, suffix := range []string{".localhost", ".local", ".lan", ".internal", ".test", ".invalid", ".example", ".home", ".corp", ".arpa"} {
		if strings.HasSuffix(host, suffix) {
			return ""
		}
	}
	label := cleanActivityLabel(name)
	if _, err := netip.ParseAddr(serverHost(label)); err == nil {
		label = ""
	}
	switch strings.ToLower(label) {
	case "", "minecraft server", "minecraft-сервер", "сервер minecraft", "sintet p2p":
		return cleanActivityLabel(host)
	default:
		return label
	}
}

func activityHeader(headers http.Header, key string) string {
	value := headers.Get(key)
	if len(value) > 1400 {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func (m *socialManager) observeBridgeActivity(headers http.Header) {
	world := headers.Get("X-Sintet-World")
	if world != "0" && world != "1" {
		return
	}
	mode := headers.Get("X-Sintet-Activity")
	server := ""
	switch {
	case world == "0":
		mode = "menu"
	case mode == "multiplayer":
		server = publicServerLabel(activityHeader(headers, "X-Sintet-Server-Name"), activityHeader(headers, "X-Sintet-Server-Address"))
	case mode != "singleplayer":
		mode = ""
	}
	m.gameMu.Lock()
	defer m.gameMu.Unlock()
	if m.bridgeWorld && world == "0" {
		m.loggedRemote, m.loggedServer, m.connectingAt = false, "", time.Time{}
		m.observedWorld, m.observedPort = false, 0
	}
	m.bridgeWorld, m.bridgeMode, m.bridgeServer = world == "1", mode, server
	m.bridgeWorldSeen = time.Now()
}

var presenceLogPrefix = regexp.MustCompile(`^(?:\[\d\d:\d\d:\d\d\] )?\[[^\]\r\n]+/(?:INFO|WARN|ERROR)\](?: \[[^\]\r\n]+\])?:\s*`)
var connectingMessage = regexp.MustCompile(`^Connecting to ([^,\r\n]+), \d{1,5}$`)
var loadedAdvancements = regexp.MustCompile(`^Loaded \d+ advancements?$`)

// Kept under gameMu. The normal game journal supplies a fallback on runtimes
// where the agent cannot inspect the client. Chat lines are ignored.
func (m *socialManager) observeRemoteGameLines(lines string) {
	for _, line := range strings.Split(lines, "\n") {
		line = strings.TrimSpace(line)
		prefix := presenceLogPrefix.FindStringIndex(line)
		if prefix == nil {
			continue
		}
		message := line[prefix[1]:]
		switch {
		case strings.HasPrefix(message, "Connecting to "):
			if match := connectingMessage.FindStringSubmatch(message); match != nil {
				m.loggedServer = publicServerLabel("", match[1])
				m.loggedRemote = false
				m.connectingAt = time.Now()
			}
		case loadedAdvancements.MatchString(message):
			if !m.connectingAt.IsZero() && time.Since(m.connectingAt) < 45*time.Second && !m.observedWorld {
				m.loggedRemote = true
				m.connectingAt = time.Time{}
			}
		case strings.HasPrefix(message, "Disconnecting from "), strings.HasPrefix(message, "Disconnected"),
			strings.HasPrefix(message, "Lost connection"), strings.HasPrefix(message, "Client disconnected"),
			strings.HasPrefix(message, "Failed to connect"), strings.HasPrefix(message, "Connection timed out"),
			strings.HasPrefix(message, "Starting integrated minecraft server"):
			m.loggedRemote, m.loggedServer, m.connectingAt = false, "", time.Time{}
		}
	}
}

func (m *socialManager) gameActivity() (socialActivity, string, int) {
	m.app.mu.RLock()
	service := m.app.service
	m.app.mu.RUnlock()
	game := service.State()
	if !game.Running {
		return socialActivity{Mode: "launcher"}, "", 0
	}
	activity := socialActivity{Mode: "menu", Version: game.GameVersion, Loader: game.GameLoader,
		Build: cleanActivityLabel(game.GameBuild), StartedAt: game.GameStarted, accountID: game.GameAccount}
	m.gameMu.Lock()
	defer m.gameMu.Unlock()
	port := m.observedPort
	if m.observedWorld || port != 0 {
		activity.Mode = "singleplayer"
	} else if m.loggedRemote {
		activity.Mode, activity.Server = "multiplayer", m.loggedServer
	}
	if time.Since(m.bridgeWorldSeen) <= 3*time.Second {
		switch m.bridgeMode {
		case "menu":
			activity.Mode, activity.Server, port = "menu", "", 0
		case "singleplayer":
			activity.Mode, activity.Server = "singleplayer", ""
		case "multiplayer":
			activity.Mode, activity.Server, port = "multiplayer", m.bridgeServer, 0
			if activity.Server == "" && m.loggedRemote {
				activity.Server = m.loggedServer
			}
		case "":
			if m.bridgeWorld && activity.Mode == "menu" {
				activity.Mode = "world"
			}
		}
	}
	return activity, game.Profile, port
}
