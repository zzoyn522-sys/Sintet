package social

import (
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const voiceChannel = "sintet-voice-v2"
const VoiceSamples = 320
const VoiceSampleRate = 16000
const VoiceHeader = 37      // one state byte and a 36-byte lobby profile ID
const VoiceMaxPayload = 194 // 2-byte sequence, 32-byte position, and up to 160 Opus bytes

type voiceLink struct {
	mu      sync.Mutex
	receive func([]byte)
	queue   chan []byte
}

func (p *Peer) OnVoice(receive func([]byte)) {
	p.voice.mu.Lock()
	defer p.voice.mu.Unlock()
	p.voice.receive = receive
}

// SendVoice drops stale frames before they can block the game tunnel.
// Callers retain ownership of the immutable packet shared across peers.
func (p *Peer) SendVoice(packet []byte) {
	p.voice.mu.Lock()
	defer p.voice.mu.Unlock()
	if p.voice.queue == nil {
		return
	}
	select {
	case p.voice.queue <- packet:
	default:
	}
}

func (p *Peer) attachVoice(dc *webrtc.DataChannel) {
	retries := dc.MaxRetransmits()
	if dc.Ordered() || retries == nil || *retries != 0 {
		_ = dc.Close()
		return
	}
	dc.OnOpen(func() {
		raw, err := dc.DetachWithDeadline()
		if err != nil {
			_ = dc.Close()
			return
		}
		defer raw.Close()
		p.voice.mu.Lock()
		if p.voice.queue != nil {
			p.voice.mu.Unlock()
			return
		}
		out := make(chan []byte, 8)
		p.voice.queue = out
		p.voice.mu.Unlock()
		done := make(chan struct{})
		defer func() {
			close(done)
			p.voice.mu.Lock()
			p.voice.queue = nil
			p.voice.mu.Unlock()
		}()
		go func() {
			for {
				select {
				case <-done:
					return
				case <-p.closed:
					return
				case packet := <-out:
					_ = raw.SetWriteDeadline(time.Now().Add(35 * time.Millisecond))
					_, _ = raw.Write(packet)
				}
			}
		}()
		var buf [VoiceHeader + VoiceMaxPayload + 1]byte
		window := time.Now()
		count := 0
		for {
			n, err := raw.Read(buf[:])
			if err != nil {
				return
			}
			if time.Since(window) >= time.Second {
				window, count = time.Now(), 0
			}
			count++
			if count > 400 {
				continue
			}
			if n < VoiceHeader || n > VoiceHeader+VoiceMaxPayload {
				continue
			}
			p.voice.mu.Lock()
			receive := p.voice.receive
			p.voice.mu.Unlock()
			if receive != nil {
				receive(buf[:n])
			}
		}
	})
}
