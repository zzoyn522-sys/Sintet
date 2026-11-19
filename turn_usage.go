package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"sintet/internal/identity"
	p2p "sintet/internal/social"
)

const turnLimitMessage = "Лимит TURN подключения закончился"

var errTURNLimit = errors.New(turnLimitMessage)

type turnUsageView struct {
	Used        int64  `json:"used"`
	Limit       int64  `json:"limit"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
}

func (v *turnUsageView) exhausted() bool {
	return v != nil && v.Limit > 0 && v.Used >= v.Limit
}

func readTURNUsage(ctx context.Context, client *identity.Client, room ...string) (turnUsageView, error) {
	var usage turnUsageView
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	endpoint := "/api/social/turn-usage"
	if len(room) > 0 && room[0] != "" {
		endpoint += "?room=" + url.QueryEscape(room[0])
	}
	err := client.SocialRequest(ctx, "GET", endpoint, nil, &usage)
	return usage, err
}

func (a *App) TurnUsage() (turnUsageView, error) {
	client := a.activeUpdateClient()
	if client == nil {
		return turnUsageView{}, errors.New("Войди через Sintet ID, чтобы увидеть лимит TURN")
	}
	return readTURNUsage(context.Background(), client)
}

func normalizeTURNError(err error) error {
	if err != nil && strings.Contains(err.Error(), turnLimitMessage) {
		return errTURNLimit
	}
	return err
}

func (m *socialManager) notifyTURNLimitLocked() {
	if time.Since(time.UnixMilli(m.state.TURNNotice)) > 8*time.Second {
		m.state.TURNNotice = time.Now().UnixMilli()
	}
}

func (m *socialManager) notifyTURNLimit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifyTURNLimitLocked()
}

type roomTURNCheck struct {
	account, room string
	at            time.Time
	usage         turnUsageView
	err           error
}

func (m *socialManager) readRoomTURNUsage(client *identity.Client, account, room string) (turnUsageView, error) {
	m.roomTURNMu.Lock()
	defer m.roomTURNMu.Unlock()
	current := &m.roomTURNCache
	if current.account == account && current.room == room && time.Since(current.at) < 10*time.Second {
		return current.usage, current.err
	}
	usage, err := readTURNUsage(m.ctx, client, room)
	*current = roomTURNCheck{account: account, room: room, at: time.Now(), usage: usage, err: err}
	return usage, err
}

func (m *socialManager) watchTURNPeer(client *identity.Client, account string, peer *p2p.Peer, room ...string) {
	roomCode := ""
	if len(room) > 0 {
		roomCode = room[0]
	}
	check := func() bool {
		var usage turnUsageView
		var err error
		if roomCode != "" {
			usage, err = m.readRoomTURNUsage(client, account, roomCode)
		} else {
			usage, err = readTURNUsage(m.ctx, client)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.account != account || roomCode != "" && (m.state.Room == nil || m.state.Room.Code != roomCode) {
			return true
		}
		if err != nil {
			return false
		}
		slot := &m.state.TURNUsage
		if roomCode != "" {
			slot = &m.state.LobbyTURNUsage
		}
		current := *slot
		if current == nil || usage.PeriodStart > current.PeriodStart ||
			usage.PeriodStart == current.PeriodStart && usage.Used >= current.Used {
			*slot = &usage
		}
		if (*slot).exhausted() {
			m.notifyTURNLimitLocked()
			return true
		}
		return false
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if check() {
			peer.Close()
			return
		}
		select {
		case <-m.ctx.Done():
			return
		case <-peer.Closed():
			// Cloudflare may revoke the credential before this client polls.
			check()
			return
		case <-ticker.C:
		}
	}
}
