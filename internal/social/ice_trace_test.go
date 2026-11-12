package social

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestICETraceCorrelatesSTUNAcrossPeers(t *testing.T) {
	hostTrace, guestTrace := NewICETrace(), NewICETrace()
	host, err := NewPeerWithICETrace(-1, false, hostTrace)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	guest, err := NewPeerWithICETrace(0, false, guestTrace)
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	waitOffer, err := guest.StartOffer()
	if err != nil {
		t.Fatal(err)
	}
	offer, err := waitOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := host.Answer(ctx, *offer)
	if err != nil {
		t.Fatal(err)
	}
	// Give the answer delivery enough delay to cross Pion's default
	// per-pair check budget, while the host is already checking.
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(2800 * time.Millisecond):
	}
	if err := guest.SetAnswer(*answer); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.Listen(ctx); err != nil {
		t.Fatal(err)
	}
	if err := host.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	hostReport, guestReport := hostTrace.Snapshot(), guestTrace.Snapshot()
	pairStarted := make(map[[2]int]int64)
	extendedChecks := false
	for _, event := range hostReport.Events {
		switch event.Event {
		case "check":
			pair := [2]int{event.A, event.B}
			if started, ok := pairStarted[pair]; !ok {
				pairStarted[pair] = event.MS
			} else if event.MS-started >= 2200 {
				extendedChecks = true
			}
		case "pair_valid":
			delete(pairStarted, [2]int{event.A, event.B})
		}
	}
	if !extendedChecks {
		t.Fatal("host exhausted ICE checks before the delayed guest answer could be applied")
	}
	hostTX := make(map[string]bool)
	hostRX := make(map[string]bool)
	guestTX := make(map[string]bool)
	guestRX := make(map[string]bool)
	for _, side := range []struct {
		report ICEReport
		rx, tx map[string]bool
	}{{hostReport, hostRX, hostTX}, {guestReport, guestRX, guestTX}} {
		selected := false
		for _, event := range side.report.Events {
			switch event.Event {
			case "udp_rx":
				side.rx[event.TX] = true
			case "udp_tx":
				side.tx[event.TX] = true
			case "pair_selected":
				selected = event.A != 0 && event.B != 0
			}
		}
		if !selected {
			t.Fatalf("selected pair missing: %+v", side.report.Events)
		}
		wire, err := json.Marshal(side.report)
		if err != nil || strings.Contains(string(wire), "127.0.0.1") {
			t.Fatalf("trace serialization failed or contains an IP: %s %v", wire, err)
		}
	}
	for hash := range hostTX {
		if guestRX[hash] && hostRX[hash] && guestTX[hash] {
			return
		}
	}
	t.Fatal("no STUN transaction contained a request and response matched across both peers")
}
