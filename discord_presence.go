package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"sintet/internal/discordpresence"
	"sintet/internal/launcher"
)

const discordApplicationID = "1573056089828630652"
const discordLargeImage = "https://cdn.discordapp.com/app-icons/1573056089828630652/bbbbe4d04706cc2af1160eed90540c6d.png?size=512"
const discordInvitePrefix = "sd1."

type discordCopy struct {
	launcher, menu, singleplayer, multiplayer, playing, lobby, preparing, ready string
}

var discordPresenceCopy = map[string]discordCopy{
	"en": {"In the launcher", "Main menu", "Singleplayer", "Multiplayer", "In game", "Playing with friends", "Getting the world ready", "World open"},
	"ru": {"В лаунчере", "Главное меню", "Одиночный мир", "Мультиплеер", "В игре", "Игра с друзьями", "Готовим мир", "Мир открыт"},
	"uk": {"У лаунчері", "Головне меню", "Одиночний світ", "Мультиплеєр", "У грі", "Гра з друзями", "Готуємо світ", "Світ відкритий"},
	"pt": {"No launcher", "Menu principal", "Modo individual", "Multijogador", "A jogar", "A jogar com amigos", "A preparar o mundo", "Mundo aberto"},
	"es": {"En el launcher", "Menú principal", "Un jugador", "Multijugador", "En partida", "Jugando con amigos", "Preparando el mundo", "Mundo abierto"},
}

var discordLoaderNames = map[string]string{"forge": "Forge", "neoforge": "NeoForge", "fabric": "Fabric", "quilt": "Quilt"}

type discordLobby struct {
	id, secret        string
	players, capacity int
	worldReady        bool
}

func discordActivity(activity socialActivity, lang, largeImage string, launcherStarted int64, lobby discordLobby) discordpresence.Activity {
	labels, ok := discordPresenceCopy[lang]
	if !ok {
		labels = discordPresenceCopy["en"]
	}
	status := discordpresence.Activity{Details: labels.launcher, State: "Minecraft: Java Edition", Started: launcherStarted, LargeImage: largeImage}
	if activity.Mode != "launcher" {
		status.State = labels.playing
		switch activity.Mode {
		case "menu":
			status.State = labels.menu
		case "singleplayer":
			status.State = labels.singleplayer
		case "multiplayer":
			status.State = labels.multiplayer
		}
		status.Details = strings.TrimSpace("Minecraft " + cleanActivityLabel(activity.Version))
		if loader := discordLoaderNames[strings.ToLower(activity.Loader)]; loader != "" {
			status.Details += " · " + loader
		}
		if activity.StartedAt > 0 {
			status.Started = activity.StartedAt
		}
	}
	if lobby.id != "" {
		if activity.Mode == "launcher" {
			status.Details, status.State = labels.lobby, labels.preparing
			if lobby.worldReady {
				status.State = labels.ready
			}
		} else {
			status.State = labels.lobby + " · " + labels.preparing
			if lobby.worldReady {
				status.State = labels.lobby + " · " + labels.ready
			}
		}
		status.PartyID, status.Players = lobby.id, max(1, lobby.players)
		status.Capacity = max(status.Players, lobby.capacity)
		if status.Players < status.Capacity {
			status.JoinSecret = lobby.secret
		}
	}
	return status
}

func validDiscordInvite(secret string) bool {
	raw, found := strings.CutPrefix(secret, discordInvitePrefix)
	if !found || len(raw) != 36 {
		return false
	}
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}

type discordInviteCache struct {
	mu            sync.Mutex
	party, secret string
	retry         time.Time
	pending       bool
}

func (c *discordInviteCache) get(ctx context.Context, m *socialManager, room socialRoom) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.party != room.DiscordPartyID {
		c.party, c.secret, c.retry, c.pending = room.DiscordPartyID, "", time.Time{}, false
	}
	if c.secret == "" && !c.pending && time.Now().After(c.retry) {
		c.pending = true
		go func() {
			request, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			var result struct{ Secret string }
			err := m.post(request, map[string]string{"action": "discord-invite", "code": room.Code}, &result)
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.party != room.DiscordPartyID {
				return
			}
			c.pending, c.retry = false, time.Now().Add(10*time.Second)
			if err == nil && validDiscordInvite(result.Secret) {
				c.secret = result.Secret
			}
		}()
	}
	return c.secret
}

func (a *App) startDiscordPresence(ctx context.Context) {
	appID := strings.TrimSpace(os.Getenv("SINTET_DISCORD_APPLICATION_ID"))
	if appID == "" {
		appID = discordApplicationID
	}
	if !discordpresence.ValidApplicationID(appID) {
		return
	}
	image := strings.TrimSpace(os.Getenv("SINTET_DISCORD_LARGE_IMAGE"))
	if image == "" || len(image) > 256 {
		image = discordLargeImage
	}
	m := a.social
	launcherStarted := time.Now().Unix()
	var invites discordInviteCache
	var joining atomic.Bool
	a.discordDone = make(chan struct{})
	go func() {
		defer close(a.discordDone)
		var onJoin func(string)
		if previewDataDirectory == "" {
			if err := discordpresence.RegisterLaunch(appID); err != nil {
				m.logError(err)
			}
			onJoin = func(secret string) {
				if !validDiscordInvite(secret) || !joining.CompareAndSwap(false, true) {
					return
				}
				go func() {
					defer joining.Store(false)
					a.ShowLauncher()
					m.identity()
					m.mu.Lock()
					m.state.DiscordJoinNotice, m.state.DiscordJoinError = time.Now().UnixMilli(), ""
					m.mu.Unlock()
					request, cancel := context.WithTimeout(ctx, 3*time.Minute)
					defer cancel()
					if err := m.action(request, "discord-join", secret); err != nil && ctx.Err() == nil {
						m.mu.Lock()
						m.state.DiscordJoinNotice, m.state.DiscordJoinError = m.state.DiscordJoinNotice+1, playerFacingSocialError(err)
						m.mu.Unlock()
					}
				}()
			}
		}
		discordpresence.Run(ctx, appID, func() (discordpresence.Activity, bool) {
			a.mu.RLock()
			enabled, lang := a.settings.DiscordPresence, a.settings.InterfaceLanguage
			a.mu.RUnlock()
			if !enabled {
				return discordpresence.Activity{}, false
			}
			activity, _, _ := m.gameActivity()
			m.mu.Lock()
			invisible := m.state.Privacy.Invisible
			var room socialRoom
			lobby := discordLobby{worldReady: m.state.WorldReady}
			if m.state.Room != nil {
				room = *m.state.Room
				lobby.id, lobby.players, lobby.capacity = room.DiscordPartyID, len(m.state.Members), max(4, room.Capacity)
			}
			isHost := room.Host != "" && room.Host == m.account
			m.mu.Unlock()
			if invisible {
				return discordpresence.Activity{}, false
			}
			if onJoin != nil && isHost && lobby.id != "" && max(1, lobby.players) < lobby.capacity {
				lobby.secret = invites.get(ctx, m, room)
			}
			lang = launcher.InterfaceLanguage(lang, launcher.SystemLanguage())
			return discordActivity(activity, lang, image, launcherStarted, lobby), true
		}, onJoin)
	}()
}
