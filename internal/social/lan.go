package social

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/ipv4"
)

// LAN discovery advertises an interface address; the TCP listener accepts only this PC.
func Announce(ctx context.Context, port int, owner string) error {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return err
	}
	defer conn.Close()
	packet := ipv4.NewPacketConn(conn)
	if err = packet.SetMulticastLoopback(true); err != nil {
		return err
	}
	packet.SetMulticastTTL(1)
	owner = strings.NewReplacer("[", "", "]", "", "\n", "", "\r", "").Replace(owner)
	message := []byte(fmt.Sprintf("[MOTD][Sintet P2P] Мир %s[/MOTD][AD]%d[/AD]", owner, port))
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	destination := &net.UDPAddr{IP: net.IPv4(224, 0, 2, 60), Port: 4445}
	for {
		if _, err = conn.WriteTo(message, destination); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
