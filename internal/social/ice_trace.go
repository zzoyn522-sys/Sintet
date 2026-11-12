package social

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/webrtc/v4"
)

const (
	maxICEPackets   = 512
	maxICEEvents    = 256
	maxICEEndpoints = 256
)

// ICETrace records only allowlisted events and ephemeral endpoint numbers.
// Socket addresses stay in memory and the STUN payload is never retained.
type ICETrace struct {
	start                      time.Time
	mu                         sync.Mutex
	closed                     atomic.Bool
	events                     []ICEEvent
	endpoints                  map[string]int
	candidates                 map[candidateKey]bool
	packets, controls, dropped int
}

type candidateKey struct {
	event                   string
	endpoint, kind, network int
}

type ICEEvent struct {
	MS    int64  `json:"ms"`
	Event string `json:"event"`
	A     int    `json:"a,omitempty"`
	B     int    `json:"b,omitempty"`
	N     int    `json:"n,omitempty"`
	TX    string `json:"tx,omitempty"`
}

type ICEReport struct {
	StartedAt int64      `json:"startedAt"`
	Dropped   int        `json:"dropped"`
	Events    []ICEEvent `json:"events"`
}

func NewICETrace() *ICETrace {
	return &ICETrace{start: time.Now(), endpoints: make(map[string]int), candidates: make(map[candidateKey]bool)}
}

func (t *ICETrace) Stage(event string) {
	t.add(event, "", "", 0, "")
}

func (t *ICETrace) Snapshot() ICEReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed.Store(true)
	t.endpoints = nil
	t.candidates = nil
	return ICEReport{StartedAt: t.start.UnixMilli(), Dropped: t.dropped, Events: t.events}
}

func (t *ICETrace) endpoint(address string) int {
	if address == "" {
		return 0
	}
	if value, ok := t.endpoints[address]; ok {
		return value
	}
	if len(t.endpoints) >= maxICEEndpoints {
		return 0
	}
	id := len(t.endpoints) + 1
	t.endpoints[address] = id
	return id
}

func (t *ICETrace) add(event, local, remote string, value int, tx string) {
	if t == nil || t.closed.Load() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed.Load() {
		return
	}
	if tx != "" {
		if t.packets >= maxICEPackets {
			t.dropped++
			return
		}
		t.packets++
	} else if t.controls >= maxICEEvents && (local != "" || remote != "") {
		t.dropped++
		return
	} else if len(t.events) >= maxICEPackets+maxICEEvents+32 {
		t.dropped++
		return
	} else {
		t.controls++
	}
	t.events = append(t.events, ICEEvent{time.Since(t.start).Milliseconds(), event, t.endpoint(local), t.endpoint(remote), value, tx})
}

func (t *ICETrace) candidate(event string, address string, kind, network int) {
	if t == nil || t.closed.Load() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed.Load() {
		return
	}
	id := t.endpoint(address)
	key := candidateKey{event, id, kind, network}
	if t.candidates[key] {
		return
	}
	if t.controls >= maxICEEvents || id == 0 {
		t.dropped++
		return
	}
	t.candidates[key] = true
	t.controls++
	t.events = append(t.events, ICEEvent{MS: time.Since(t.start).Milliseconds(), Event: event, A: id, B: kind, N: network})
}

func (t *ICETrace) localCandidate(c *webrtc.ICECandidate) {
	network := ice.NetworkTypeUDP4
	if c.Protocol == webrtc.ICEProtocolTCP {
		network = ice.NetworkTypeTCP4
	}
	if ip, err := netip.ParseAddr(c.Address); err == nil && ip.Is6() && !ip.Is4In6() {
		network++
	}
	t.candidate("local_candidate", net.JoinHostPort(c.Address, strconv.Itoa(int(c.Port))), int(c.Typ), int(network))
}

func (t *ICETrace) configure(settings *webrtc.SettingEngine) {
	settings.LoggerFactory = iceTraceFactory{fallback: logging.NewDefaultLoggerFactory(), trace: t}
	network, err := stdnet.NewNet()
	if err == nil {
		settings.SetNet(iceTraceNet{Net: network, trace: t})
	} else {
		t.Stage("socket_trace_unavailable")
	}
}

type iceTraceFactory struct {
	fallback logging.LoggerFactory
	trace    *ICETrace
}

func (f iceTraceFactory) NewLogger(scope string) logging.LeveledLogger {
	logger := f.fallback.NewLogger(scope)
	if scope == "ice" {
		return iceTraceLogger{LeveledLogger: logger, trace: f.trace}
	}
	return logger
}

type iceTraceLogger struct {
	logging.LeveledLogger
	trace *ICETrace
}

func (l iceTraceLogger) Tracef(format string, args ...any) {
	if l.trace.closed.Load() {
		return
	}
	switch format {
	case "Ping STUN from %s to %s", "Ping STUN (nominate candidate pair) from %s to %s":
		local, remote := l.candidate("local_candidate", args[0]), l.candidate("remote_candidate", args[1])
		event := "check"
		if strings.Contains(format, "nominate") {
			event = "nominate"
		}
		l.trace.add(event, local, remote, 0, "")
	case "Maximum requests reached for pair %s, marking it as failed":
		l.pair("pair_exhausted", args[0])
	case "Found valid candidate pair: %s":
		l.pair("pair_valid", args[0])
	case "Set selected candidate pair: %s":
		l.pair("pair_selected", args[0])
	}
}

func (l iceTraceLogger) Debugf(format string, args ...any) {
	switch format {
	case "Adding a new peer-reflexive candidate: %s ":
		l.candidate("remote_candidate", args[0])
	case "Failed to resolve STUN host: %s %s: %v":
		l.trace.Stage("stun_dns_failed")
	case "failed to get server reflexive address %s %s: %v":
		l.trace.Stage("stun_gather_failed")
	}
	l.LeveledLogger.Debugf(format, args...)
}

func (l iceTraceLogger) Warnf(format string, args ...any) {
	switch format {
	case "Discard request with wrong username from (%s), %v":
		l.trace.Stage("reject_username")
	case "Discard request with broken integrity from (%s), %v":
		l.trace.Stage("reject_integrity")
	case "Discard success response from (%s), unknown TransactionID 0x%x",
		"Discard message from (%s), unknown TransactionID 0x%x":
		l.trace.Stage("reject_unknown_transaction")
	case "Discard message: transaction source and destination does not match expected(%s), actual(%s)":
		l.trace.Stage("reject_address_mismatch")
	case "Received role conflict error (487) from %s, switching role":
		l.trace.Stage("role_conflict")
	}
	l.LeveledLogger.Warnf(format, args...)
}

func (l iceTraceLogger) candidate(event string, value any) string {
	c, ok := value.(ice.Candidate)
	if !ok || c == nil {
		return ""
	}
	address := net.JoinHostPort(c.Address(), strconv.Itoa(c.Port()))
	l.trace.candidate(event, address, int(c.Type()), int(c.NetworkType()))
	return address
}

func (l iceTraceLogger) pair(event string, value any) {
	if pair, ok := value.(*ice.CandidatePair); ok && pair != nil {
		local, remote := l.candidate("local_candidate", pair.Local), l.candidate("remote_candidate", pair.Remote)
		l.trace.add(event, local, remote, 0, "")
	}
}

type iceTraceNet struct {
	transport.Net
	trace *ICETrace
}

func (n iceTraceNet) ListenUDP(network string, address *net.UDPAddr) (transport.UDPConn, error) {
	conn, err := n.Net.ListenUDP(network, address)
	if err != nil {
		n.trace.Stage("udp_bind_failed")
		return nil, err
	}
	return iceTraceUDP{UDPConn: conn, trace: n.trace}, nil
}

type iceTraceUDP struct {
	transport.UDPConn
	trace *ICETrace
}

func (c iceTraceUDP) ReadFrom(packet []byte) (int, net.Addr, error) {
	n, remote, err := c.UDPConn.ReadFrom(packet)
	if err == nil {
		c.trace.packet("udp_rx", c.LocalAddr(), remote, packet[:n])
	}
	return n, remote, err
}

func (c iceTraceUDP) WriteTo(packet []byte, remote net.Addr) (int, error) {
	n, err := c.UDPConn.WriteTo(packet, remote)
	event := "udp_tx"
	if err != nil {
		event = "udp_tx_failed"
	}
	c.trace.packet(event, c.LocalAddr(), remote, packet)
	return n, err
}

func (t *ICETrace) packet(event string, local, remote net.Addr, packet []byte) {
	if t.closed.Load() || len(packet) < 20 || binary.BigEndian.Uint32(packet[4:8]) != 0x2112a442 {
		return
	}
	size := int(binary.BigEndian.Uint16(packet[2:4]))
	if size%4 != 0 || size+20 != len(packet) {
		return
	}
	kind := int(binary.BigEndian.Uint16(packet[:2]))
	switch kind {
	case 0x0001, 0x0101, 0x0111: // Binding request, success, error.
	default:
		return
	}
	if kind == 0x0001 {
		for data := packet[20:]; len(data) >= 4; {
			attr := binary.BigEndian.Uint16(data[:2])
			size := 4 + (int(binary.BigEndian.Uint16(data[2:4]))+3)&^3
			if size > len(data) {
				break
			}
			switch attr {
			case 0x0006:
				kind |= 0x1000 // ICE username present.
			case 0x0025:
				kind |= 0x2000 // USE-CANDIDATE.
			case 0x8029:
				kind |= 0x4000 // ICE-CONTROLLED.
			case 0x802a:
				kind |= 0x8000 // ICE-CONTROLLING.
			}
			data = data[size:]
		}
	}
	hash := sha256.Sum256(packet[8:20])
	t.add(event, endpointAddress(local), endpointAddress(remote), kind, hex.EncodeToString(hash[:8]))
}

func endpointAddress(address net.Addr) string {
	if address == nil {
		return ""
	}
	if udp, ok := address.(*net.UDPAddr); ok {
		ip, ok := netip.AddrFromSlice(udp.IP)
		if ok {
			return netip.AddrPortFrom(ip.Unmap(), uint16(udp.Port)).String()
		}
	}
	return address.String()
}
