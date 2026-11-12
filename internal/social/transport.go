// Package social carries Minecraft TCP streams over authenticated, encrypted WebRTC.
package social

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/pion/webrtc/v4"
)

type Peer struct {
	pc          *webrtc.PeerConnection
	mu          sync.Mutex
	session     *yamux.Session
	listener    net.Listener
	ready       chan struct{}
	closed      chan struct{}
	once        sync.Once
	readyOnce   sync.Once
	trace       []string
	started     time.Time
	hostPort    int
	hostWorld   context.Context
	cancelWorld context.CancelFunc
	packSession *yamux.Session
	packReady   chan struct{}
	packOnce    sync.Once
	packHandler http.Handler
	relay       bool
	gameStreams atomic.Int32
	tunnelBytes atomic.Uint64
	quality     peerQuality
}

func NewPeer(hostPort int, stun bool) (*Peer, error) {
	return NewPeerWithICETrace(hostPort, stun, nil)
}

func NewPeerWithICETrace(hostPort int, stun bool, trace *ICETrace) (*Peer, error) {
	return newPeer(hostPort, stun, nil, trace)
}

func NewRelayPeer(hostPort int, relay []webrtc.ICEServer, trace *ICETrace) (*Peer, error) {
	if len(relay) == 0 {
		return nil, errors.New("no TURN credentials")
	}
	return newPeer(hostPort, true, relay, trace)
}

func newPeer(hostPort int, stun bool, relay []webrtc.ICEServer, trace *ICETrace) (*Peer, error) {
	var settings webrtc.SettingEngine
	settings.DetachDataChannels()
	settings.EnableDataChannelBlockWrite(true)
	settings.SetIncludeLoopbackCandidate(!stun)
	settings.SetICETimeouts(20*time.Second, 10*time.Second, 2*time.Second)
	// At Pion's 200 ms check cadence, keep each direct candidate pair
	// active for about four seconds while both networks open the UDP path.
	settings.SetICEMaxBindingRequests(20)
	if trace != nil {
		trace.configure(&settings)
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	config := webrtc.Configuration{}
	if len(relay) > 0 {
		config.ICEServers = relay
		config.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	} else if stun {
		config.ICEServers = []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302", "stun:stun.cloudflare.com:3478"}}}
	}
	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}
	p := &Peer{started: time.Now(), pc: pc, relay: len(relay) > 0, hostPort: max(0, hostPort), ready: make(chan struct{}), packReady: make(chan struct{}), closed: make(chan struct{})}
	pc.SCTP().Transport().ICETransport().OnSelectedCandidatePairChange(func(pair *webrtc.ICECandidatePair) {
		if pair != nil && pair.Local != nil && pair.Remote != nil {
			p.record("pair=" + pair.Local.Typ.String() + "/" + pair.Remote.Typ.String() + "/" + pair.Local.Protocol.String())
		}
	})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		p.record("gathering=" + state.String())
		trace.Stage("gather_" + state.String())
	})
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		p.record("ice=" + state.String())
		trace.Stage("ice_" + state.String())
	})
	if trace != nil {
		pc.SCTP().Transport().OnStateChange(func(state webrtc.DTLSTransportState) {
			trace.Stage("dtls_" + state.String())
		})
	}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			p.record("candidates-complete")
			return
		}
		p.record("candidate=" + c.Typ.String() + "/" + c.Protocol.String())
		if trace != nil {
			trace.localCandidate(c)
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.record("connection=" + state.String())
		trace.Stage("pc_" + state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			go p.Close()
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if hostPort != 0 && dc.Label() == qualityChannel {
			p.attachQuality(dc)
			return
		}
		if hostPort == 0 || (dc.Label() != "sintet-minecraft" && dc.Label() != "sintet-pack-v1") {
			_ = dc.Close()
			return
		}
		p.attachChannel(dc, true, dc.Label() == "sintet-pack-v1")
	})
	return p, nil
}

func (p *Peer) attach(dc *webrtc.DataChannel, host bool) {
	p.attachChannel(dc, host, false)
}
func (p *Peer) attachChannel(dc *webrtc.DataChannel, host, pack bool) {
	dc.OnError(func(err error) { p.record("datachannel-error") })
	dc.OnClose(func() { p.record("datachannel-closed") })
	dc.OnOpen(func() {
		p.record("datachannel-open")
		raw, err := dc.Detach()
		if err != nil {
			p.Close()
			return
		}
		config := yamux.DefaultConfig()
		config.LogOutput = io.Discard
		config.StreamOpenTimeout = 10 * time.Second
		config.MaxStreamWindowSize = 1 << 20
		stream := &messageStream{raw: raw, pending: make([]byte, 65536), bytes: &p.tunnelBytes}
		var session *yamux.Session
		if host {
			session, err = yamux.Server(stream, config)
		} else {
			session, err = yamux.Client(stream, config)
		}
		if err != nil {
			raw.Close()
			p.Close()
			return
		}
		p.mu.Lock()
		select {
		case <-p.closed:
			p.mu.Unlock()
			session.Close()
			return
		default:
		}
		if (pack && p.packSession != nil) || (!pack && p.session != nil) {
			p.mu.Unlock()
			session.Close()
			return
		}
		if pack {
			p.packSession = session
		} else {
			p.session = session
		}
		p.mu.Unlock()
		if pack {
			p.packOnce.Do(func() { close(p.packReady) })
			if host {
				go p.servePack(session)
			}
			return
		}
		p.readyOnce.Do(func() { close(p.ready) })
		go p.measureThroughput()
		if host {
			go p.serveHost(session)
		}
		go func() { <-session.CloseChan(); p.Close() }()
	})
}

// SCTP data channels preserve messages; yamux expects a byte stream. Cache each
// complete message so small header reads cannot truncate Minecraft payloads.
type messageStream struct {
	raw        io.ReadWriteCloser
	pending    []byte
	start, end int
	bytes      *atomic.Uint64
}

func (s *messageStream) Read(out []byte) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	if s.start == s.end {
		n, err := s.raw.Read(s.pending)
		s.bytes.Add(uint64(n))
		if err != nil {
			return 0, err
		}
		s.start, s.end = 0, n
	}
	n := copy(out, s.pending[s.start:s.end])
	s.start += n
	return n, nil
}
func (s *messageStream) Write(data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		size := min(len(data), 16<<10)
		n, err := s.raw.Write(data[:size])
		s.bytes.Add(uint64(n))
		total += n
		if err != nil {
			return total, err
		}
		if n != size {
			return total, io.ErrShortWrite
		}
		data = data[size:]
	}
	return total, nil
}
func (s *messageStream) Close() error { return s.raw.Close() }

func (p *Peer) startDescription(offer bool) (func(context.Context) (*webrtc.SessionDescription, error), error) {
	complete := webrtc.GatheringCompletePromise(p.pc)
	var desc webrtc.SessionDescription
	var err error
	if offer {
		desc, err = p.pc.CreateOffer(nil)
	} else {
		desc, err = p.pc.CreateAnswer(nil)
	}
	if err != nil {
		return nil, err
	}
	if err = p.pc.SetLocalDescription(desc); err != nil {
		return nil, err
	}
	return func(ctx context.Context) (*webrtc.SessionDescription, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.closed:
			return nil, p.connectionError(net.ErrClosed)
		case <-complete:
			return p.pc.LocalDescription(), nil
		}
	}, nil
}

func (p *Peer) Offer(ctx context.Context) (*webrtc.SessionDescription, error) {
	wait, err := p.StartOffer()
	if err != nil {
		return nil, err
	}
	return wait(ctx)
}

// StartOffer begins gathering candidates in Pion's background workers. The
// caller can send independent signaling requests before waiting for the SDP.
func (p *Peer) StartOffer() (func(context.Context) (*webrtc.SessionDescription, error), error) {
	dc, err := p.pc.CreateDataChannel("sintet-minecraft", nil)
	if err != nil {
		return nil, err
	}
	p.attach(dc, false)
	pack, err := p.pc.CreateDataChannel("sintet-pack-v1", nil)
	if err != nil {
		return nil, err
	}
	p.attachChannel(pack, false, true)
	ordered, retransmits := false, uint16(0)
	if probes, err := p.pc.CreateDataChannel(qualityChannel, &webrtc.DataChannelInit{Ordered: &ordered, MaxRetransmits: &retransmits}); err == nil {
		p.attachQuality(probes)
	}
	return p.startDescription(true)
}
func (p *Peer) Answer(ctx context.Context, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	if offer.Type != webrtc.SDPTypeOffer {
		return nil, errors.New("invalid offer")
	}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}
	wait, err := p.startDescription(false)
	if err != nil {
		return nil, err
	}
	return wait(ctx)
}
func (p *Peer) SetAnswer(answer webrtc.SessionDescription) error {
	if answer.Type != webrtc.SDPTypeAnswer {
		return errors.New("invalid answer")
	}
	return p.pc.SetRemoteDescription(answer)
}
func (p *Peer) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return p.connectionError(ctx.Err())
	case <-p.closed:
		return p.connectionError(net.ErrClosed)
	case <-p.ready:
		return nil
	}
}
func (p *Peer) record(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.trace) < 60 {
		p.trace = append(p.trace, fmt.Sprintf("%dms:%s", time.Since(p.started).Milliseconds(), event))
	}
}
func (p *Peer) connectionError(cause error) error {
	p.mu.Lock()
	trace := strings.Join(p.trace, ";")
	p.mu.Unlock()
	return fmt.Errorf("WebRTC: ICE=%s, connection=%s, DTLS=%s: %w | trace=%s",
		p.pc.ICEConnectionState(), p.pc.ConnectionState(), p.pc.SCTP().Transport().State(), cause, trace)
}
func (p *Peer) RecordICETraceStatus(trace *ICETrace) {
	trace.Stage("ice_" + p.pc.ICEConnectionState().String())
	trace.Stage("pc_" + p.pc.ConnectionState().String())
	trace.Stage("dtls_" + p.pc.SCTP().Transport().State().String())
}

func (p *Peer) Closed() <-chan struct{} { return p.closed }

// Only guest Minecraft TCP streams are counted; pack transfers use a separate
// DataChannel/session and must never be mistaken for joining the world.
func (p *Peer) GameConnections() int { return int(p.gameStreams.Load()) }
func (p *Peer) Connected() bool {
	select {
	case <-p.closed:
		return false
	default:
	}
	select {
	case <-p.ready:
		return true
	default:
		return false
	}
}

// A negative initial port creates a lobby host before a Minecraft world exists.
// Switching the LAN endpoint closes old game streams and retains the pack channel.
func (p *Peer) SetHostPort(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if port < 0 || port > 65535 {
		port = 0
	}
	if p.hostPort == port {
		return
	}
	if p.cancelWorld != nil {
		p.cancelWorld()
		p.hostWorld, p.cancelWorld = nil, nil
	}
	p.hostPort = port
}
func (p *Peer) serveHost(session *yamux.Session) {
	limit := make(chan struct{}, 32)
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			return
		}
		select {
		case limit <- struct{}{}:
			go func() {
				defer func() { <-limit }()
				defer stream.Close()
				p.mu.Lock()
				port := p.hostPort
				if port != 0 && p.hostWorld == nil {
					p.hostWorld, p.cancelWorld = context.WithCancel(context.Background())
				}
				world := p.hostWorld
				p.mu.Unlock()
				if port == 0 {
					return
				}
				dialer := net.Dialer{Timeout: 5 * time.Second}
				target, err := dialer.DialContext(world, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
				if err != nil {
					return
				}
				defer target.Close()
				stop := context.AfterFunc(world, func() { _ = target.Close() })
				defer stop()
				bridge(stream, target)
			}()
		default:
			stream.Close()
		}
	}
}
func bridge(a, b net.Conn) {
	done := make(chan struct{}, 1)
	go func() { io.Copy(a, b); a.Close(); done <- struct{}{} }()
	io.Copy(b, a)
	b.Close()
	<-done
}
func (p *Peer) Listen(ctx context.Context) (int, error) {
	if err := p.Wait(ctx); err != nil {
		return 0, err
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		listener.Close()
		return 0, net.ErrClosed
	default:
	}
	p.listener = listener
	session := p.session
	p.mu.Unlock()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				// LAN discovery uses the sender's interface IP. Accept only this PC,
				// including its own interface address, never other machines on the LAN.
				remote := conn.RemoteAddr().(*net.TCPAddr).IP
				local := conn.LocalAddr().(*net.TCPAddr).IP
				if !remote.IsLoopback() && !remote.Equal(local) {
					return
				}
				stream, err := session.OpenStream()
				if err != nil {
					return
				}
				defer stream.Close()
				p.gameStreams.Add(1)
				defer p.gameStreams.Add(-1)
				bridge(conn, stream)
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
func (p *Peer) Close() error {
	p.once.Do(func() {
		close(p.closed)
		p.mu.Lock()
		listener, session, pack := p.listener, p.session, p.packSession
		if p.cancelWorld != nil {
			p.cancelWorld()
		}
		p.mu.Unlock()
		if listener != nil {
			listener.Close()
		}
		if session != nil {
			session.Close()
		}
		if pack != nil {
			pack.Close()
		}
		p.pc.Close()
	})
	return nil
}
