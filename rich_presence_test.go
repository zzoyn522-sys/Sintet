package main

import (
	"encoding/base64"
	"net/http"
	"testing"
)

func TestRichPresenceDoesNotShareLocalAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:25565", "192.168.1.8", "[fd00::4]:25565", "minecraft.local:25565"} {
		if label := publicServerLabel("Private Server", address); label != "" {
			t.Fatalf("published a local server label for %q", address)
		}
	}
	if label := publicServerLabel("Minecraft Server", "Play.Minecraft.net:25565"); label != "play.minecraft.net" {
		t.Fatalf("unexpected public hostname %q", label)
	}
	if label := publicServerLabel("§aCastle\x00", "play.minecraft.net"); label != "Castle" {
		t.Fatalf("unexpected named server %q", label)
	}
}

func TestRichPresenceClearsServerOnReturnToMenu(t *testing.T) {
	m := &socialManager{}
	headers := http.Header{}
	headers.Set("X-Sintet-World", "1")
	headers.Set("X-Sintet-Activity", "multiplayer")
	headers.Set("X-Sintet-Server-Address", base64.RawURLEncoding.EncodeToString([]byte("play.minecraft.net:25565")))
	m.observeBridgeActivity(headers)
	if m.bridgeMode != "multiplayer" || m.bridgeServer != "play.minecraft.net" {
		t.Fatalf("invalid bridge activity: %q, %q", m.bridgeMode, m.bridgeServer)
	}
	headers.Set("X-Sintet-World", "0")
	m.observeBridgeActivity(headers)
	if m.bridgeMode != "menu" || m.bridgeServer != "" || m.bridgeWorld {
		t.Fatal("server activity was retained in the main menu")
	}
}

func TestRichPresenceLogFallbackRequiresConnection(t *testing.T) {
	m := &socialManager{}
	m.observeGameLog([]byte("[Render thread/INFO]: [CHAT] Connecting to fake.minecraft.net, 25565\n"))
	if !m.connectingAt.IsZero() {
		t.Fatal("chat was treated as a connection event")
	}
	m.observeGameLog([]byte("[Render thread/INFO] [minecraft/ConnectScreen]: Connecting to play.minecraft.net, 25565\n"))
	if m.loggedRemote {
		t.Fatal("pending connection was treated as a game session")
	}
	m.observeGameLog([]byte("[Render thread/INFO]: Loaded 2 advancements\n"))
	if !m.loggedRemote || m.loggedServer != "play.minecraft.net" {
		t.Fatal("successful connection was not reflected in the log fallback")
	}
	m.observeGameLog([]byte("[Render thread/WARN]: Client disconnected with reason: connection closed\n"))
	if m.loggedRemote || m.loggedServer != "" {
		t.Fatal("server activity remained after disconnect")
	}
}
