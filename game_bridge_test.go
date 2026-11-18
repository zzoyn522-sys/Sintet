package main

import (
	"testing"
	"time"
)

func TestGameJoinRequiresLiveBridgeAndTunnel(t *testing.T) {
	m := &socialManager{}
	if m.connectGame() == nil {
		t.Fatal("accepted absent tunnel")
	}
	m.state.LocalPort = 25565
	if m.connectGame() == nil {
		t.Fatal("accepted absent game bridge")
	}
	m.bridgeSeen = time.Now().Add(-5 * time.Second)
	if m.connectGame() == nil {
		t.Fatal("accepted stale game bridge")
	}
	m.bridgeSeen = time.Now()
	if err := m.connectGame(); err != nil {
		t.Fatal(err)
	}
	if m.gameCommand != 1 || m.gameCommandPort != 25565 {
		t.Fatal("wrong local command")
	}
	m.state.LocalPort = 25566
	if err := m.connectGame(); err != nil {
		t.Fatal(err)
	}
	if m.gameCommand != 2 || m.gameCommandPort != 25566 {
		t.Fatal("command did not follow current tunnel")
	}
}

func TestLobbyAutoConnectWaitsForActualJoinAndBoundsRetries(t *testing.T) {
	m := &socialManager{epoch: 1, state: socialState{
		AutoConnect: true, WorldReady: true, LocalPort: 25565, Pack: &packStatus{Phase: "ready"},
	}}
	now := time.Now()
	if !m.autoLobbyReadyLocked(1, now) {
		t.Fatal("armed guest did not start when the world became available")
	}
	m.setGameCommandLocked(25565, true, time.Minute)
	if m.autoLobbyReadyLocked(1, now) || !m.state.AutoConnect {
		t.Fatal("queuing a command should retain intent and prevent duplicate joins")
	}
	m.gameCommandPort = 0
	m.gameJoinUntil = now.Add(time.Minute)
	if m.autoLobbyReadyLocked(1, now) || !m.state.AutoConnect {
		t.Fatal("launching with Quick Play should wait for the world connection")
	}
	m.gameJoinUntil = now.Add(-time.Second)
	m.autoJoinAttempts = 1
	m.autoJoinRetryAt = now.Add(-time.Second)
	if !m.autoLobbyReadyLocked(1, now) {
		t.Fatal("a failed join should allow one retry")
	}
	m.autoJoinAttempts = 2
	if m.autoLobbyReadyLocked(1, now) || !m.state.AutoConnect {
		t.Fatal("exhausting retries should leave the user's setting selected")
	}
	m.state.InWorld = true
	m.resetAutoLobbyRetryLocked()
	if m.autoLobbyReadyLocked(1, now) {
		t.Fatal("an already joined guest should never auto-connect again")
	}
}
