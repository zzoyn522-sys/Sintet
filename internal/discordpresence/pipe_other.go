//go:build !windows

package discordpresence

import (
	"context"
	"errors"
	"net"
)

func dial(context.Context) (net.Conn, error) {
	return nil, errors.New("Discord IPC transport is unavailable on this platform")
}

func RegisterLaunch(string) error { return nil }
