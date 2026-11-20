package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiscordPresenceLobby(t *testing.T) {
	game := socialActivity{Mode: "multiplayer", Version: "1.21.1", Loader: "fabric", StartedAt: 200, Server: "private.example.net", Build: "Private Pack"}
	presence := discordActivity(game, "ru", "", 100, discordLobby{id: "opaque-party-id", secret: "sd1.example", players: 9, capacity: 150, worldReady: true})
	if presence.Details != "Minecraft 1.21.1 · Fabric" || presence.State != "Игра с друзьями · Мир открыт" ||
		presence.Players != 9 || presence.Capacity != 150 || presence.Started != 200 || presence.JoinSecret != "sd1.example" {
		t.Fatalf("unexpected in-game Rich Presence: %+v", presence)
	}
	data, _ := json.Marshal(presence)
	for _, private := range []string{game.Server, game.Build} {
		if strings.Contains(string(data), private) {
			t.Fatalf("published private data %q", private)
		}
	}
	if menu := discordActivity(socialActivity{Mode: "launcher"}, "en", "", 100, discordLobby{}); menu.Details != "In the launcher" || menu.Started != 100 || menu.PartyID != "" {
		t.Fatalf("unexpected launcher activity: %+v", menu)
	}
}

func TestDiscordPresenceFullLobby(t *testing.T) {
	presence := discordActivity(socialActivity{Mode: "launcher"}, "en", "", 100, discordLobby{id: "party", secret: "sd1.example", players: 4, capacity: 4})
	if presence.JoinSecret != "" || presence.State != "Getting the world ready" || presence.Capacity != 4 {
		t.Fatalf("unexpected full lobby activity: %+v", presence)
	}
}
