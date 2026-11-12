package workshop

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type transferError struct {
	err   error
	delay time.Duration
}

func (e *transferError) Error() string { return e.err.Error() }
func (e *transferError) Unwrap() error { return e.err }

func transientStatus(status int) bool {
	switch status {
	case 408, 425, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func retryDelay(header string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 60)) * time.Second
	}
	if date, err := http.ParseTime(header); err == nil {
		return max(0, min(time.Until(date), time.Minute))
	}
	return 0
}

func waitTransferRetry(ctx context.Context, attempt int, err error) error {
	delay := 200 * time.Millisecond * time.Duration(1<<min(attempt, 4))
	var temporary *transferError
	if errors.As(err, &temporary) {
		delay = max(delay, temporary.delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
