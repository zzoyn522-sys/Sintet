package main

import (
	"context"
	"errors"
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

func readTURNUsage(ctx context.Context, client *identity.Client) (turnUsageView, error) {
	var usage turnUsageView
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	err := client.SocialRequest(ctx, "GET", "/api/social/turn-usage", nil, &usage)
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

func (m *socialManager) watchTURNPeer(client *identity.Client, account string, peer *p2p.Peer) {
	check := func() bool {
		usage, err := readTURNUsage(m.ctx, client)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.account != account {
			return true
		}
		if err != nil {
			return false
		}
		current := m.state.TURNUsage
		if current == nil || usage.PeriodStart > current.PeriodStart ||
			usage.PeriodStart == current.PeriodStart && usage.Used >= current.Used {
			m.state.TURNUsage = &usage
		}
		if m.state.TURNUsage.exhausted() {
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
