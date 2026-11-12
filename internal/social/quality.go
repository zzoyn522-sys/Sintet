package social

import (
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v4"
)

const qualityChannel = "sintet-quality-v1"
const probeInterval = time.Second

type LinkQuality struct {
	PingMS   *int64   `json:"pingMs,omitempty"`
	Loss     *float64 `json:"loss,omitempty"`
	KBPerSec float64  `json:"kbPerSec"`
	Level    string   `json:"level"`
}

type peerQuality struct {
	mu          sync.Mutex
	attached    bool
	samples     [12]bool
	count, next int
	missed      int
	rtt         time.Duration
	bytesPerSec float64
}

func (q *peerQuality) record(rtt time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	ok := rtt > 0
	q.samples[q.next] = ok
	q.next = (q.next + 1) % len(q.samples)
	q.count = min(q.count+1, len(q.samples))
	if ok {
		q.rtt = rtt
		q.missed = 0
	} else {
		q.missed++
	}
}

func (p *Peer) attachQuality(dc *webrtc.DataChannel) {
	retries := dc.MaxRetransmits()
	if dc.Ordered() || retries == nil || *retries != 0 {
		_ = dc.Close()
		return
	}
	dc.OnOpen(func() {
		p.quality.mu.Lock()
		if p.quality.attached {
			p.quality.mu.Unlock()
			_ = dc.Close()
			return
		}
		p.quality.attached = true
		p.quality.mu.Unlock()
		raw, err := dc.DetachWithDeadline()
		if err == nil {
			p.runProbes(raw)
		} else {
			_ = dc.Close()
		}
		p.quality.mu.Lock()
		p.quality.attached = false
		p.quality.count, p.quality.next, p.quality.missed, p.quality.rtt = 0, 0, 0, 0
		p.quality.mu.Unlock()
	})
}

// Both sides send one datagram and echo each other's probes. Unreliable, unordered
// delivery exposes missed round trips that Minecraft's reliable streams would hide.
func (p *Peer) runProbes(raw datachannel.ReadWriteCloserDeadliner) {
	defer raw.Close()
	const request, reply = 1, 2
	var outgoing, incoming [9]byte
	send := func(message []byte) {
		_ = raw.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = raw.Write(message)
	}
	for sequence := uint64(1); ; sequence++ {
		select {
		case <-p.closed:
			return
		default:
		}
		start := time.Now()
		deadline := start.Add(probeInterval)
		if err := raw.SetReadDeadline(deadline); err != nil {
			return
		}
		outgoing[0] = request
		binary.BigEndian.PutUint64(outgoing[1:], sequence)
		send(outgoing[:])
		var rtt time.Duration
		for time.Now().Before(deadline) {
			n, err := raw.Read(incoming[:])
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			if err != nil {
				return
			}
			if n != len(incoming) {
				continue
			}
			switch incoming[0] {
			case request:
				incoming[0] = reply
				send(incoming[:])
			case reply:
				if rtt == 0 && binary.BigEndian.Uint64(incoming[1:]) == sequence {
					rtt = time.Since(start)
				}
			}
		}
		p.quality.record(rtt)
	}
}

func (p *Peer) measureThroughput() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastTime, lastBytes := time.Now(), p.tunnelBytes.Load()
	for {
		select {
		case <-p.closed:
			return
		case <-ticker.C:
			now, bytes := time.Now(), p.tunnelBytes.Load()
			p.quality.mu.Lock()
			p.quality.bytesPerSec = float64(bytes-lastBytes) / now.Sub(lastTime).Seconds()
			p.quality.mu.Unlock()
			lastTime, lastBytes = now, bytes
		}
	}
}

func (p *Peer) Quality() LinkQuality {
	p.quality.mu.Lock()
	result := LinkQuality{KBPerSec: p.quality.bytesPerSec / 1000, Level: "unknown"}
	count, rtt, missed := p.quality.count, p.quality.rtt, p.quality.missed
	if count > 0 {
		lost := 0
		for _, received := range p.quality.samples[:count] {
			if !received {
				lost++
			}
		}
		loss := 100 * float64(lost) / float64(count)
		result.Loss = &loss
	}
	p.quality.mu.Unlock()
	if count == 0 {
		// Older launchers can still join. ICE exposes RTT, but no probe loss.
		if pair, ok := p.pc.SCTP().Transport().ICETransport().GetSelectedCandidatePairStats(); ok && pair.CurrentRoundTripTime > 0 {
			rtt = time.Duration(pair.CurrentRoundTripTime * float64(time.Second))
		}
	}
	if rtt > 0 && missed < 3 {
		ms := max(int64(1), rtt.Round(time.Millisecond).Milliseconds())
		result.PingMS = &ms
	}
	state := p.pc.ICEConnectionState()
	switch {
	case missed >= 3 || state == webrtc.ICEConnectionStateDisconnected || state == webrtc.ICEConnectionStateFailed:
		result.Level = "poor"
	case missed > 0 || rtt >= 180*time.Millisecond || (count >= 4 && *result.Loss >= 20):
		result.Level = "unstable"
	case result.PingMS != nil:
		result.Level = "good"
	}
	return result
}
