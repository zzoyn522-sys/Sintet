package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tphakala/go-opus/opus"
	"sintet/internal/accountfiles"
	p2p "sintet/internal/social"
)

type voiceMixer struct {
	Level int  `json:"level"`
	Muted bool `json:"muted"`
}

func (m *socialManager) voiceMixerPath() string {
	return filepath.Join(filepath.Dir(m.app.settingsPath), "voice-mixer.json")
}

func (m *socialManager) loadVoiceMixer() {
	m.voiceMix.Level = 100
	if m.app == nil || m.app.settingsPath == "" {
		return
	}
	if data, err := readLimitedFile(m.voiceMixerPath(), 4096); err == nil {
		settings := m.voiceMix
		if json.Unmarshal(data, &settings) == nil && settings.Level >= 0 && settings.Level <= 100 {
			m.voiceMix = settings
		}
	}
}

func (m *socialManager) setVoiceMixer(action, target string) error {
	m.mu.Lock()
	settings := m.voiceMix
	m.mu.Unlock()
	if action == "voice-volume" {
		level, err := strconv.Atoi(target)
		if err != nil || level < 0 || level > 100 {
			return errors.New("неверная громкость голосового чата")
		}
		settings.Level, settings.Muted = level, false
	} else {
		if target != "true" && target != "false" {
			return errors.New("неверная громкость голосового чата")
		}
		settings.Muted = target == "true"
	}
	if m.app != nil && m.app.settingsPath != "" {
		data, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err := accountfiles.Write(m.voiceMixerPath(), data); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.voiceMix = settings
	m.mu.Unlock()
	return nil
}

func (mix voiceMixer) gain() float64 {
	if mix.Muted {
		return 0
	}
	return float64(mix.Level) / 100
}

const (
	voiceListening  = 1
	voiceMicrophone = 2
	voicePositioned = 4
	voiceMaxOpus    = 160
)

type lobbyVoice struct {
	run      *voiceRun
	mic      bool
	failure  string
	users    map[string]*voiceUser
	pose     voiceGamePose
	sequence uint16
	flat     bool
	radius   int
}

type voiceRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

type voiceUser struct {
	flags       byte
	seen, spoke time.Time
	window      time.Time
	packets     int
	muted       bool
	frames      []voiceFrame
	sequence    uint16
	sequenced   bool
	decoder     *opus.Decoder
	left, right float64
}

type voiceFrame struct {
	position voicePosition
	data     [voiceMaxOpus]byte
	size     int
}

type voiceMixFrame struct {
	frame       voiceFrame
	decoder     *opus.Decoder
	left, right float64
}

func (v *lobbyVoice) user(id string) *voiceUser {
	if v.users == nil {
		v.users = make(map[string]*voiceUser)
	}
	if v.users[id] == nil {
		v.users[id] = &voiceUser{}
	}
	return v.users[id]
}

func (v *lobbyVoice) stop() {
	if v.run != nil {
		v.run.cancel()
		v.run = nil
	}
	v.mic, v.failure = false, ""
	v.pose = voiceGamePose{}
}

func (m *socialManager) voiceAction(action, target string) error {
	m.voiceActions.Lock()
	defer m.voiceActions.Unlock()
	if action == "voice-volume" || action == "voice-volume-mute" {
		return m.setVoiceMixer(action, target)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Room == nil || m.account == "" {
		return errors.New("войдите в лобби, чтобы использовать голосовой чат")
	}
	switch action {
	case "voice-range":
		if m.state.Room.Host != m.account {
			return errors.New("дальность голоса меняет лидер лобби")
		}
		radius, err := strconv.Atoi(target)
		if err != nil || radius < voiceRangeMin || radius > voiceRangeMax {
			return errors.New("дальность голоса должна быть от 8 до 128 блоков")
		}
		m.voice.radius = radius
	case "voice-spatial":
		if target != "true" && target != "false" {
			return errors.New("неверный режим голосового чата")
		}
		m.voice.flat = target == "false"
	case "voice-enable":
		if target == "true" && m.voice.run == nil {
			ctx, cancel := context.WithCancel(m.ctx)
			run := &voiceRun{cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
			m.voice.failure = ""
			m.voice.run = run
			go m.runVoice(ctx, run)
		} else if target == "false" && m.voice.run != nil {
			run := m.voice.run
			m.sendVoiceLocked(nil, "", 0, nil)
			m.voice.stop()
			for _, user := range m.voice.users {
				user.frames = nil
			}
			m.mu.Unlock()
			<-run.done
			m.mu.Lock()
		}
	case "voice-mic":
		run := m.voice.run
		if run != nil {
			m.voice.mic = target == "true"
			m.voice.failure = ""
			select {
			case run.wake <- struct{}{}:
			default:
			}
		}
	case "voice-mute":
		for _, member := range m.state.Members {
			if member.ID == target && target != m.account && m.voice.run != nil {
				user := m.voice.user(target)
				user.muted = !user.muted
				user.frames = nil
				break
			}
		}
	}
	return nil
}

func (m *socialManager) bindLobbyVoice(peer *p2p.Peer, peerID, guest string, epoch uint64) {
	peer.OnVoice(func(packet []byte) {
		m.mu.Lock()
		defer m.mu.Unlock()
		room := m.state.Room
		if room == nil || m.epoch != epoch || m.peers[peerID] != peer {
			return
		}
		fromHost := guest == ""
		sender := guest
		if fromHost {
			if m.guest != peerID || m.voice.run == nil {
				return
			}
			sender = string(packet[1:p2p.VoiceHeader])
			valid := sender == room.Host
			for _, member := range m.state.Members {
				valid = valid || member.ID == sender
			}
			if !valid || sender == m.account {
				return
			}
		}
		wireFlags := packet[0]
		flags := wireFlags &^ voicePositioned
		hasAudio := len(packet) > p2p.VoiceHeader
		if len(sender) != 36 || flags != 0 && flags != voiceListening && flags != voiceListening|voiceMicrophone || hasAudio && flags != voiceListening|voiceMicrophone {
			return
		}
		payload := packet[p2p.VoiceHeader:]
		samples := payload
		var position voicePosition
		var sequence uint16
		if hasAudio {
			if len(samples) < 3 {
				return
			}
			sequence = binary.BigEndian.Uint16(samples)
			samples = samples[2:]
			if wireFlags&voicePositioned != 0 {
				var valid bool
				position, valid = readWireVoicePosition(samples)
				if !valid {
					return
				}
				samples = samples[voicePositionSize:]
			}
			if len(samples) == 0 || len(samples) > voiceMaxOpus {
				return
			}
		} else if wireFlags != flags {
			return
		}
		now := time.Now()
		user := m.voice.user(sender)
		if !fromHost {
			if now.Sub(user.window) >= time.Second {
				user.window, user.packets = now, 0
			}
			user.packets++
			if user.packets > 78 {
				return
			}
		}
		if hasAudio {
			if user.sequenced && now.Sub(user.spoke) < 3*time.Second && int16(sequence-user.sequence) <= 0 {
				return
			}
			user.sequence, user.sequenced = sequence, true
		}
		user.flags, user.seen = flags, now
		if flags&voiceMicrophone == 0 {
			user.frames = nil
			user.spoke = time.Time{}
		}
		if hasAudio {
			user.spoke = now
			if m.voice.run != nil && !user.muted {
				if len(user.frames) == 4 {
					user.frames = user.frames[1:]
				}
				frame := voiceFrame{position: position, size: len(samples)}
				copy(frame.data[:], samples)
				user.frames = append(user.frames, frame)
			}
		}
		if !fromHost && room.Host == m.account {
			m.sendVoiceLocked(peer, sender, wireFlags, payload)
		}
	})
}

func (m *socialManager) sendVoiceLocked(source *p2p.Peer, sender string, flags byte, samples []byte) {
	room := m.state.Room
	if room == nil {
		return
	}
	if sender == "" {
		sender = m.account
	}
	if len(sender) != 36 {
		return
	}
	packet := make([]byte, p2p.VoiceHeader+len(samples))
	packet[0] = flags
	copy(packet[1:p2p.VoiceHeader], sender)
	copy(packet[p2p.VoiceHeader:], samples)
	if room.Host != m.account {
		if peer := m.peers[m.guest]; peer != nil {
			peer.SendVoice(packet)
		}
		return
	}
	now := time.Now()
	for _, signal := range m.state.Peers {
		user := m.voice.users[signal.Guest]
		peer := m.peers[signal.ID]
		if peer != nil && peer != source && user != nil && user.flags&voiceListening != 0 && now.Sub(user.seen) < 3*time.Second {
			peer.SendVoice(packet)
		}
	}
}

func (m *socialManager) runVoice(ctx context.Context, run *voiceRun) {
	defer close(run.done)
	encoder, encoderErr := opus.NewEncoder(opus.EncoderConfig{SampleRate: p2p.VoiceSampleRate, Channels: 1, Bitrate: 24000, Complexity: 4, CBR: true})
	playback, playErr := openVoiceAudio(false)
	if playback != nil {
		defer playback.close()
	}
	var mic *voiceAudio
	defer func() {
		if mic != nil {
			mic.close()
		}
	}()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	var nextStatus, nextMix, nextGameState time.Time
	var gameRunning bool
	var captureErr error
	retryMic := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-run.wake:
			nextStatus, retryMic = time.Time{}, time.Time{}
		}
		now := time.Now()
		if m.app != nil && !now.Before(nextGameState) {
			gameRunning = m.app.State().Running
			nextGameState = now.Add(100 * time.Millisecond)
		}
		m.mu.Lock()
		active := m.voice.run == run && m.state.Room != nil
		position, canSpeak := m.voice.pose.forPlayback(gameRunning, now)
		wantsMic := active && canSpeak && m.voice.mic && encoderErr == nil
		m.mu.Unlock()
		if !active {
			return
		}
		if wantsMic && mic == nil && now.After(retryMic) {
			mic, captureErr = openVoiceAudio(true)
			retryMic = now.Add(3 * time.Second)
		} else if !wantsMic {
			captureErr = nil
			if mic != nil {
				mic.close()
				mic = nil
			}
		}
		var captured [p2p.VoiceSamples]int16
		hasVoice := mic != nil && mic.record(&captured) && audibleVoice(&captured)
		var samples [voiceMaxOpus]byte
		var size int
		if hasVoice {
			size, _ = encoder.Encode(captured[:], samples[:])
		}
		m.mu.Lock()
		if m.voice.run != run {
			m.mu.Unlock()
			return
		}
		position, canSpeak = m.voice.pose.forPlayback(gameRunning, time.Now())
		m.voice.failure = ""
		if captureErr != nil {
			m.voice.failure = "Микрофон недоступен"
		} else if playErr != nil {
			m.voice.failure = "Устройство вывода недоступно"
		} else if encoderErr != nil {
			m.voice.failure = "Не удалось запустить голосовой чат"
		}
		flags := byte(voiceListening)
		if canSpeak && m.voice.mic && mic != nil {
			flags |= voiceMicrophone
		}
		if now.After(nextStatus) {
			m.sendVoiceLocked(nil, "", flags, nil)
			nextStatus = now.Add(time.Second)
		}
		if size > 0 && flags&voiceMicrophone != 0 {
			m.voice.user(m.account).spoke = now
			m.voice.sequence++
			var storage [p2p.VoiceMaxPayload]byte
			payload := binary.BigEndian.AppendUint16(storage[:0], m.voice.sequence)
			if position.dimension != 0 {
				flags |= voicePositioned
				payload = position.appendWire(payload)
			}
			payload = append(payload, samples[:size]...)
			m.sendVoiceLocked(nil, "", flags, payload)
		}
		mixing := !now.Before(nextMix)
		var frames []voiceMixFrame
		if mixing {
			if nextMix.IsZero() || now.Sub(nextMix) >= 20*time.Millisecond {
				nextMix = now
			}
			nextMix = nextMix.Add(20 * time.Millisecond)
			for _, user := range m.voice.users {
				if len(user.frames) == 0 {
					continue
				}
				frame := user.frames[0]
				user.frames = user.frames[1:]
				left, right := voiceGains(position, frame.position, m.voice.radius, m.voice.flat)
				if !canSpeak || left == 0 && right == 0 {
					user.left, user.right = 0, 0
					continue
				}
				if user.decoder == nil {
					user.decoder, _ = opus.NewDecoder(p2p.VoiceSampleRate, 1)
				}
				if user.decoder != nil {
					user.left += (left - user.left) * .55
					user.right += (right - user.right) * .55
					frames = append(frames, voiceMixFrame{frame: frame, decoder: user.decoder, left: user.left, right: user.right})
				}
			}
		}
		gain := m.voiceMix.gain()
		m.mu.Unlock()
		if len(frames) != 0 && playback != nil && gain > 0 {
			var mixed [2 * p2p.VoiceSamples]int32
			for _, voice := range frames {
				var mono [p2p.VoiceSamples]int16
				if n, err := voice.decoder.Decode(voice.frame.data[:voice.frame.size], mono[:]); err != nil || n != len(mono) {
					continue
				}
				for i, sample := range mono {
					mixed[2*i] += int32(float64(sample) * voice.left)
					mixed[2*i+1] += int32(float64(sample) * voice.right)
				}
			}
			var pcm [2 * p2p.VoiceSamples]int16
			for i, sample := range mixed {
				pcm[i] = int16(min(32767, max(-32768, int32(float64(sample)*gain))))
			}
			playback.play(&pcm)
		}
	}
}

func audibleVoice(frame *[p2p.VoiceSamples]int16) bool {
	var power int64
	for _, sample := range frame {
		value := int32(sample)
		power += int64(value * value)
	}
	return power > 200*200*p2p.VoiceSamples
}
