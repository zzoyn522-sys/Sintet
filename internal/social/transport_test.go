package social

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"golang.org/x/net/ipv4"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebRTCTunnelCarriesConcurrentMinecraftStreams(t *testing.T) {
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	host, err := NewPeer(target.Addr().(*net.TCPAddr).Port, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	guest, err := NewPeer(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	offer, err := guest.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := host.Answer(ctx, *offer)
	if err != nil {
		t.Fatal(err)
	}
	if err = guest.SetAnswer(*answer); err != nil {
		t.Fatal(err)
	}
	port, err := guest.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	probe.SetDeadline(time.Now().Add(3 * time.Second))
	probe.Write([]byte("probe"))
	if _, err := io.ReadFull(probe, make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	if guest.GameConnections() != 1 || host.GameConnections() != 0 {
		t.Fatal("game connection count confused guest, host or pack traffic")
	}
	probe.Close()
	until := time.Now().Add(2 * time.Second)
	for guest.GameConnections() != 0 && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if guest.GameConnections() != 0 {
		t.Fatal("closed game connection remained active")
	}
	// Verify the address Minecraft derives from an actual multicast announcement
	// reaches the same tunnel, rather than accidentally advertising an unreachable IP.
	multicast, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(224, 0, 2, 60), Port: 4445})
	if err != nil {
		t.Fatal(err)
	}
	defer multicast.Close()
	ipv4.NewPacketConn(multicast).SetMulticastLoopback(true)
	announce, cancelAnnounce := context.WithCancel(ctx)
	defer cancelAnnounce()
	go Announce(announce, port, "SintetTunnelTest")
	multicast.SetReadDeadline(time.Now().Add(4 * time.Second))
	buffer := make([]byte, 2048)
	for {
		n, address, e := multicast.ReadFromUDP(buffer)
		if e != nil {
			t.Fatal("Minecraft LAN discovery failed:", e)
		}
		if !strings.Contains(string(buffer[:n]), "SintetTunnelTest") {
			continue
		}
		if !strings.Contains(string(buffer[:n]), fmt.Sprintf("[AD]%d[/AD]", port)) {
			t.Fatal("wrong LAN port")
		}
		conn, e := net.DialTimeout("tcp4", net.JoinHostPort(address.IP.String(), strconv.Itoa(port)), time.Second)
		if e != nil {
			t.Fatal("advertised LAN endpoint unreachable:", e)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, e = conn.Write([]byte("LAN ping")); e != nil {
			t.Fatal(e)
		}
		got := make([]byte, 8)
		_, e = io.ReadFull(conn, got)
		conn.Close()
		if e != nil || string(got) != "LAN ping" {
			t.Fatal("advertised endpoint is not the Minecraft tunnel", e)
		}
		break
	}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, e := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
			if e != nil {
				t.Error(e)
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(15 * time.Second))
			payload := make([]byte, 2<<20)
			rand.Read(payload)
			writeErr := make(chan error, 1)
			go func() { _, e := conn.Write(payload); writeErr <- e }()
			got := make([]byte, len(payload))
			if _, e = io.ReadFull(conn, got); e != nil {
				t.Error(e)
				return
			}
			if e = <-writeErr; e != nil {
				t.Error(e)
			}
			if !bytes.Equal(got, payload) {
				t.Error("Minecraft stream corrupted across SCTP messages")
			}
		}()
	}
	wg.Wait()
	for {
		hostQuality, guestQuality := host.Quality(), guest.Quality()
		if hostQuality.PingMS != nil && guestQuality.PingMS != nil && hostQuality.Loss != nil && guestQuality.Loss != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("both peers should report RTT and loss: host=%+v guest=%+v", hostQuality, guestQuality)
		case <-time.After(25 * time.Millisecond):
		}
	}
	guest.Close()
	if c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second); e == nil {
		c.Close()
		t.Fatal("listener survived disconnect")
	}
}

func TestLinkQualityWarnsBeforeTunnelCloses(t *testing.T) {
	peer, err := NewPeer(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	for i := 0; i < 12; i++ {
		peer.quality.record(40 * time.Millisecond)
	}
	peer.quality.record(0)
	if got := peer.Quality(); got.Level != "unstable" || got.Loss == nil || *got.Loss < 8 || *got.Loss > 9 {
		t.Fatalf("first missed probe should warn: %+v", got)
	}
	peer.quality.record(0)
	peer.quality.record(0)
	if got := peer.Quality(); got.Level != "poor" || got.PingMS != nil || got.Loss == nil || *got.Loss != 25 {
		t.Fatalf("three missed probes should show a poor connection and hide stale RTT: %+v", got)
	}
	for i := 0; i < 12; i++ {
		peer.quality.record(40 * time.Millisecond)
	}
	if got := peer.Quality(); got.Level != "good" || got.PingMS == nil || *got.PingMS != 40 || got.Loss == nil || *got.Loss != 0 {
		t.Fatalf("quality did not recover after healthy probes: %+v", got)
	}
}
