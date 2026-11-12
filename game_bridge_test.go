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
