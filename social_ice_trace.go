package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"sintet/internal/identity"
	p2p "sintet/internal/social"
)

// Enabled only by the diagnostic TEST release build.
var iceTraceBuild string

func (m *socialManager) startICETrace(side string) (*p2p.ICETrace, func(string, *p2p.Peer, string)) {
	skip := func(string, *p2p.Peer, string) {}
	if iceTraceBuild != "1" || launcherChannel != "test" {
		return nil, skip
	}
	m.mu.Lock()
	client, account := m.client, m.account
	m.mu.Unlock()
	switch account {
	case "21d0ed78-f394-4adf-bbb4-77d586677c31", "38511d5b-1efd-46b2-9f63-70164aa6e549":
	default:
		return nil, skip
	}
	if client == nil {
		return nil, skip
	}
	trace := p2p.NewICETrace()
	var once sync.Once
	return trace, func(id string, peer *p2p.Peer, result string) {
		if id == "" {
			return
		}
		once.Do(func() {
			if peer != nil {
				peer.RecordICETraceStatus(trace)
			}
			report := struct {
				p2p.ICEReport
				Attempt string `json:"attempt"`
				Side    string `json:"side"`
				Version string `json:"version"`
				Result  string `json:"result"`
			}{trace.Snapshot(), id, side, launcherVersion, result}
			go func() {
				var err error
				for _, pause := range []time.Duration{0, time.Second, 4 * time.Second, 12 * time.Second} {
					time.Sleep(pause)
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					err = client.SocialRequest(ctx, http.MethodPost, "/api/social/ice-trace", report, nil)
					cancel()
					if err == nil {
						return
					}
					if !identity.TemporaryServiceError(err) {
						break
					}
				}
				m.logError(fmt.Errorf("ICE trace upload (%s): %w", side, err))
			}()
		})
	}
}
