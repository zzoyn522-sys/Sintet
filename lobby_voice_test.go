package main

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/tphakala/go-opus/opus"
	p2p "sintet/internal/social"
)

func TestVoiceMixerKeepsVolumeAndMuteAcrossSessions(t *testing.T) {
	app := &App{settingsPath: filepath.Join(t.TempDir(), "settings.json")}
	m := newSocialManager(app, context.Background())
	if m.voiceMix.gain() != 1 {
		t.Fatal("voice mixer must start at full volume")
	}
	if err := m.voiceAction("voice-volume", "35"); err != nil {
		t.Fatal(err)
	}
	if err := m.voiceAction("voice-volume-mute", "true"); err != nil {
		t.Fatal(err)
	}
	restored := newSocialManager(app, context.Background())
	if restored.voiceMix.Level != 35 || restored.voiceMix.gain() != 0 {
		t.Fatalf("volume or mute was lost: %+v", restored.voiceMix)
	}
	if err := restored.voiceAction("voice-volume", "101"); err == nil {
		t.Fatal("invalid gain was accepted")
	}
	if err := restored.voiceAction("voice-volume", "70"); err != nil || restored.voiceMix.gain() != .7 {
		t.Fatalf("dragging the slider should unmute and apply the new level: %+v, %v", restored.voiceMix, err)
	}
}

func TestVoiceCodec(t *testing.T) {
	encoder, err := opus.NewEncoder(opus.EncoderConfig{SampleRate: p2p.VoiceSampleRate, Channels: 1, Bitrate: 24000, Complexity: 4, CBR: true})
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := opus.NewDecoder(p2p.VoiceSampleRate, 1)
	if err != nil {
		t.Fatal(err)
	}
	var original, decoded [p2p.VoiceSamples]int16
	for i := range original {
		original[i] = int16(9000 * math.Sin(float64(i)*.2))
	}
	packet := make([]byte, voiceMaxOpus)
	n, err := encoder.Encode(original[:], packet)
	if err != nil || n < 1 || n > 80 {
		t.Fatalf("encoded %d, %v", n, err)
	}
	n, err = decoder.Decode(packet[:n], decoded[:])
	if err != nil || n != len(decoded) || !audibleVoice(&decoded) {
		t.Fatalf("decoded %d, %v", n, err)
	}
}

func TestSpatialVoiceFalloffPanAndWorldIsolation(t *testing.T) {
	listener := parseVoicePose("minecraft:overworld 120 65 -24 0")
	speaker := parseVoicePose("minecraft:overworld 125 65 -24 0")
	decoded, ok := readWireVoicePosition(speaker.appendWire(nil))
	if !ok || decoded != speaker || listener.dimension == 0 {
		t.Fatal("position wire round trip failed")
	}
	left, right := voiceGains(listener, speaker, 0, false)
	if left <= right || right <= 0 {
		t.Fatalf("speaker to the left gave %f/%f", left, right)
	}
	listener.yaw = 180
	reversedLeft, reversedRight := voiceGains(listener, speaker, 48, false)
	if math.Abs(reversedLeft-right) > .001 || math.Abs(reversedRight-left) > .001 {
		t.Fatal("turning did not flip stereo")
	}
	speaker.y += 12
	if a, b := voiceGains(listener, speaker, 48, false); a+b >= left+right {
		t.Fatal("vertical distance did not reduce sound")
	}
	speaker.y += 25
	if a, b := voiceGains(listener, speaker, 32, false); a != 0 || b != 0 {
		t.Fatal("speaker beyond range audible")
	}
	if a, b := voiceGains(listener, speaker, 32, true); a != 0 || b != 0 {
		t.Fatal("non-spatial voice exceeded the host's range")
	}
	if a, b := voiceGains(listener, speaker, 48, true); a <= 0 || a != b {
		t.Fatalf("non-spatial voice should be centered within the host's range: %f/%f", a, b)
	}
	speaker = parseVoicePose("minecraft:the_nether 120 65 -24 0")
	if a, b := voiceGains(listener, speaker, 48, false); a != 0 || b != 0 {
		t.Fatal("other dimension audible")
	}
	if a, b := voiceGains(listener, speaker, 48, true); a != 0 || b != 0 {
		t.Fatal("non-spatial voice crossed dimensions")
	}
	if _, ok := (voiceGamePose{position: listener, at: time.Now().Add(-2 * time.Second)}).forPlayback(true, time.Now()); ok {
		t.Fatal("stale player position used")
	}
	if !lobbyVoiceDestination("127.0.0.1:12345", 12345) || lobbyVoiceDestination("example.org:12345", 12345) || lobbyVoiceDestination("127.0.0.1:12346", 12345) {
		t.Fatal("game server was not isolated to the lobby tunnel")
	}
}

func TestLobbyVoiceRelaysAuthenticatedSpeaker(t *testing.T) {
	const host = "10000000-0000-4000-8000-000000000001"
	const speaker = "10000000-0000-4000-8000-000000000002"
	const listener = "10000000-0000-4000-8000-000000000003"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	m := newSocialManager(nil, ctx)
	m.account, m.state.Room = host, &socialRoom{Host: host}
	connect := func(id string) *p2p.Peer {
		hostPeer, err := p2p.NewPeer(-1, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { hostPeer.Close() })
		guestPeer, err := p2p.NewPeer(0, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { guestPeer.Close() })
		m.mu.Lock()
		m.peers[id] = hostPeer
		m.state.Peers = append(m.state.Peers, socialPeer{ID: id, Guest: id})
		m.mu.Unlock()
		m.bindLobbyVoice(hostPeer, id, id, 0)
		offer, err := guestPeer.Offer(ctx)
		if err != nil {
			t.Fatal(err)
		}
		answer, err := hostPeer.Answer(ctx, *offer)
		if err != nil {
			t.Fatal(err)
		}
		if err := guestPeer.SetAnswer(*answer); err != nil {
			t.Fatal(err)
		}
		if err := guestPeer.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		return guestPeer
	}
	from, to := connect(speaker), connect(listener)
	received := make(chan []byte, 1)
	to.OnVoice(func(packet []byte) {
		select {
		case received <- bytes.Clone(packet):
		default:
		}
	})
	ready := make([]byte, p2p.VoiceHeader)
	ready[0] = voiceListening
	frame := make([]byte, p2p.VoiceHeader+2)
	frame[0] = voiceListening | voiceMicrophone | voicePositioned
	frame = parseVoicePose("minecraft:overworld 8 0 2 0").appendWire(frame)
	frame = append(frame, 123)
	copy(frame[1:], host)
	retry := time.NewTicker(35 * time.Millisecond)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("voice never reached an opted-in listener")
		case <-retry.C:
			to.SendVoice(ready)
			from.SendVoice(frame)
		case packet := <-received:
			if string(packet[1:p2p.VoiceHeader]) != speaker || !bytes.Equal(packet[p2p.VoiceHeader:], frame[p2p.VoiceHeader:]) {
				t.Fatalf("incorrect relay packet: %q", packet[1:p2p.VoiceHeader])
			}
			return
		}
	}
}
