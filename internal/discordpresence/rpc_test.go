package discordpresence

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestActivityFrames(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := server.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn := connection{Conn: client}
		err := conn.open("123456789012345678")
		started <- err
		if err == nil {
			conn.run(ctx, func() (Activity, bool) {
				return Activity{Details: "Minecraft", State: "Join coming soon", Started: 100, LargeImage: "sintet",
					PartyID: "random-party", Players: 4, Capacity: 150}, true
			}, nil)
		}
	}()
	greeting, err := readPacket(server)
	if err != nil || greeting.opcode != handshake {
		t.Fatalf("handshake: %+v, %v", greeting, err)
	}
	var hello map[string]any
	if err := json.Unmarshal(greeting.body, &hello); err != nil || hello["client_id"] != "123456789012345678" || hello["v"] != float64(1) {
		t.Fatalf("bad handshake: %s", greeting.body)
	}
	peer := connection{Conn: server}
	if err := peer.writeJSON(frame, map[string]string{"cmd": "DISPATCH", "evt": "READY"}); err != nil {
		t.Fatal(err)
	}
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	activity, err := readPacket(server)
	if err != nil || activity.opcode != frame {
		t.Fatalf("activity: %+v, %v", activity, err)
	}
	var sent struct {
		Cmd   string
		Nonce string
		Args  struct {
			PID      int
			Activity map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(activity.body, &sent); err != nil || sent.Cmd != "SET_ACTIVITY" || sent.Args.PID <= 0 {
		t.Fatalf("bad activity command: %s", activity.body)
	}
	var party struct {
		ID   string
		Size [2]int
	}
	if err := json.Unmarshal(sent.Args.Activity["party"], &party); err != nil || party.ID != "random-party" || party.Size != [2]int{4, 150} {
		t.Fatalf("bad party: %s", sent.Args.Activity["party"])
	}
	if sent.Args.Activity["secrets"] != nil || sent.Args.Activity["buttons"] != nil {
		t.Fatal("a join action was published before it is available")
	}
	var assets map[string]string
	if err := json.Unmarshal(sent.Args.Activity["assets"], &assets); err != nil || assets["large_image"] != "sintet" {
		t.Fatalf("bad artwork: %s", sent.Args.Activity["assets"])
	}
	if err := peer.writeJSON(frame, map[string]string{"cmd": "SET_ACTIVITY", "evt": "ERROR", "nonce": sent.Nonce}); err != nil {
		t.Fatal(err)
	}
	fallback, err := readPacket(server)
	if err != nil || fallback.opcode != frame {
		t.Fatalf("fallback activity: %+v, %v", fallback, err)
	}
	sent.Args.Activity = nil
	if err := json.Unmarshal(fallback.body, &sent); err != nil || sent.Args.Activity["assets"] != nil || sent.Args.Activity["party"] == nil {
		t.Fatalf("fallback must keep the lobby without artwork: %s", fallback.body)
	}
	if err := peer.write(ping, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	p, err := readPacket(server)
	if err != nil || p.opcode != pong || string(p.body) != "ping" {
		t.Fatalf("pong: %+v, %v", p, err)
	}
	cancel()
	cleared, err := readPacket(server)
	if err != nil || cleared.opcode != frame {
		t.Fatalf("clear: %+v, %v", cleared, err)
	}
	if err := json.Unmarshal(cleared.body, &sent); err != nil || sent.Args.Activity != nil {
		t.Fatalf("clear activity: %s", cleared.body)
	}
	<-done
}

func TestOversizedFrameRejected(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		var header [8]byte
		binary.LittleEndian.PutUint32(header[4:], maxPayload+1)
		_, _ = server.Write(header[:])
	}()
	if _, err := readPacket(client); err == nil {
		t.Fatal("accepted oversized IPC payload")
	}
}

func TestDiscordInviteIPC(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := server.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const secret = "sd1.4dd09e13-a6a0-469e-b501-dcdd799700c5"
	var enabled atomic.Bool
	enabled.Store(true)
	joined := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn := connection{Conn: client}
		conn.run(ctx, func() (Activity, bool) {
			return Activity{Details: "Playing with friends", PartyID: "party", Players: 1, Capacity: 8, JoinSecret: secret}, enabled.Load()
		}, func(secret string) { joined <- secret })
	}()
	subscription, err := readPacket(server)
	var subscribed message
	if err != nil || subscription.opcode != frame || json.Unmarshal(subscription.body, &subscribed) != nil ||
		subscribed.Cmd != "SUBSCRIBE" || subscribed.Event != joinEvent || subscribed.Nonce == "" {
		t.Fatalf("invalid join subscription: %s, %v", subscription.body, err)
	}
	activity, err := readPacket(server)
	var wire struct {
		Args struct {
			Activity *struct {
				Secrets map[string]string
				Party   struct{ Privacy int }
			}
		}
	}
	if err != nil || json.Unmarshal(activity.body, &wire) != nil ||
		wire.Args.Activity == nil || wire.Args.Activity.Secrets["join"] != secret || wire.Args.Activity.Party.Privacy != 1 {
		t.Fatalf("invalid joinable activity: %s, %v", activity.body, err)
	}
	enabled.Store(false)
	cleared, err := readPacket(server)
	if err != nil || !bytes.Contains(cleared.body, []byte("\"activity\":null")) {
		t.Fatalf("activity stayed visible after opting out: %s, %v", cleared.body, err)
	}
	peer := connection{Conn: server}
	if err := peer.writeJSON(frame, map[string]any{"cmd": "DISPATCH", "evt": joinEvent, "data": map[string]string{"secret": secret}}); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-joined:
		if received != secret {
			t.Fatalf("received a mismatched invite")
		}
	case <-time.After(time.Second):
		t.Fatal("join event was not delivered with presence hidden")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RPC did not stop")
	}
}
