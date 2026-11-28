package main

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	voiceRangeDefault = 48
	voiceRangeMin     = 8
	voiceRangeMax     = 128
)

func lobbyVoiceRange(value int) int {
	if value < voiceRangeMin || value > voiceRangeMax {
		return voiceRangeDefault
	}
	return value
}

const voicePositionSize = 32

type voicePosition struct {
	dimension    uint64
	x, y, z, yaw float64
}

type voiceGamePose struct {
	position voicePosition
	at       time.Time
	menu     bool
}

var voiceDimensionPattern = regexp.MustCompile(`^[a-z0-9_.-]+:[a-z0-9_/.-]{1,120}$`)

func parseVoicePose(value string) voicePosition {
	if len(value) > 256 {
		return voicePosition{}
	}
	fields := strings.Fields(value)
	if len(fields) != 5 || !voiceDimensionPattern.MatchString(fields[0]) {
		return voicePosition{}
	}
	var result voicePosition
	for i, dest := range []*float64{&result.x, &result.y, &result.z, &result.yaw} {
		number, err := strconv.ParseFloat(fields[i+1], 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 60_000_000 {
			return voicePosition{}
		}
		*dest = number
	}
	sum := sha256.Sum256([]byte(fields[0]))
	result.dimension = binary.BigEndian.Uint64(sum[:8])
	return result
}

func lobbyVoiceDestination(address string, port int) bool {
	if port == 0 {
		return false
	}
	host, service, err := net.SplitHostPort(address)
	if err != nil || service != strconv.Itoa(port) {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return net.ParseIP(host).IsLoopback()
}

func (m *socialManager) observeLobbyVoicePose(w http.ResponseWriter, headers http.Header) {
	run := headers.Get("X-Sintet-Bridge-Run")
	if run == "" || m.app == nil || run != m.app.State().GameBridgeRun {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	room := m.state.Room
	if room == nil || m.voice.run == nil {
		return
	}
	w.Header().Set("X-Sintet-Voice-Track", "1")
	world := headers.Get("X-Sintet-World")
	if world != "0" && world != "1" {
		return
	}
	pose := voiceGamePose{at: time.Now(), menu: world == "0"}
	mode := headers.Get("X-Sintet-Activity")
	ours := room.Host == m.account && m.state.WorldReady && mode == "singleplayer"
	guest := room.Host != m.account && mode == "multiplayer" &&
		lobbyVoiceDestination(activityHeader(headers, "X-Sintet-Server-Address"), m.guestPort)
	if !pose.menu && (ours || guest) {
		pose.position = parseVoicePose(headers.Get("X-Sintet-Voice-Pose"))
	}
	m.voice.pose = pose
}

func (pose voiceGamePose) forPlayback(gameRunning bool, now time.Time) (voicePosition, bool) {
	if !gameRunning {
		return voicePosition{}, true
	}
	if now.Sub(pose.at) > time.Second {
		return voicePosition{}, false
	}
	return pose.position, pose.menu || pose.position.dimension != 0
}

func (p voicePosition) appendWire(dst []byte) []byte {
	dst = binary.BigEndian.AppendUint64(dst, p.dimension)
	for _, value := range []float64{p.x, p.y, p.z} {
		dst = binary.BigEndian.AppendUint64(dst, math.Float64bits(value))
	}
	return dst
}

func readWireVoicePosition(payload []byte) (voicePosition, bool) {
	if len(payload) < voicePositionSize {
		return voicePosition{}, false
	}
	p := voicePosition{dimension: binary.BigEndian.Uint64(payload)}
	for i, dst := range []*float64{&p.x, &p.y, &p.z} {
		position := 8 + i*8
		*dst = math.Float64frombits(binary.BigEndian.Uint64(payload[position:]))
		if math.IsNaN(*dst) || math.IsInf(*dst, 0) || math.Abs(*dst) > 60_000_000 {
			return voicePosition{}, false
		}
	}
	return p, p.dimension != 0
}

func voiceGains(listener, speaker voicePosition, radius int, flat bool) (left, right float64) {
	if listener.dimension != speaker.dimension {
		return 0, 0
	}
	if speaker.dimension == 0 {
		return math.Sqrt(0.5), math.Sqrt(0.5)
	}
	dx, dy, dz := speaker.x-listener.x, speaker.y-listener.y, speaker.z-listener.z
	horizontal := math.Hypot(dx, dz)
	distance := math.Hypot(horizontal, dy)
	rangeBlocks := float64(lobbyVoiceRange(radius))
	if distance >= rangeBlocks {
		return 0, 0
	}
	if flat {
		return math.Sqrt(0.5), math.Sqrt(0.5)
	}
	falloff := 1 - max(0, distance-2)/(rangeBlocks-2)
	falloff *= falloff
	pan := 0.0
	if horizontal > .25 {
		angle := listener.yaw * math.Pi / 180
		// At yaw zero the camera faces +Z and its right side is -X.
		pan = -(dx*math.Cos(angle) + dz*math.Sin(angle)) / horizontal
	}
	pan = max(-1, min(1, pan)) * .85
	return falloff * math.Sqrt((1-pan)/2), falloff * math.Sqrt((1+pan)/2)
}
