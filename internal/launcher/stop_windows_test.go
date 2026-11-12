package launcher

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCloseGameWaitsForPendingOperation(t *testing.T) {
	service := &Service{state: State{Busy: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.CloseGame(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("game close returned while launcher was busy: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	service.mu.Lock()
	service.state.Busy = false
	service.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCloseGameReturnsDeadlineWhenStillBusy(t *testing.T) {
	service := &Service{state: State{Busy: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := service.CloseGame(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}
