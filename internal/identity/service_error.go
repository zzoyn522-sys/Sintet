package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type serviceError struct {
	message, method, route string
	cause                  error
	temporary              bool
}

func (e *serviceError) Error() string { return e.message }
func (e *serviceError) Unwrap() error { return e.cause }

func TemporaryServiceError(err error) bool {
	var service *serviceError
	return errors.As(err, &service) && service.temporary
}

// Keep request tokens and query parameters out of diagnostic files.
func ServiceDiagnostic(err error) string {
	var service *serviceError
	if !errors.As(err, &service) {
		return fmt.Sprint(err)
	}
	cause := service.cause
	var request *url.Error
	if errors.As(cause, &request) {
		cause = request.Err
	}
	route, _, _ := strings.Cut(service.route, "?")
	return fmt.Sprintf("%s [%s %s]: %v", service.message, service.method, route, cause)
}

func safeServiceRetry(method, path string, body any) bool {
	if method == http.MethodGet {
		return true
	}
	if method == http.MethodPost && path == "/api/social" {
		if value, ok := body.(map[string]any); ok {
			return value["action"] == "presence" || value["action"] == "peer-state"
		}
	}
	return false
}

func (c *Client) request(ctx context.Context, method, path, token string, body any, out any) error {
	retry := safeServiceRetry(method, path, body)
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		call, cancel := context.WithCancel(ctx)
		if retry {
			cancel()
			call, cancel = context.WithTimeout(ctx, 8*time.Second)
		}
		err := c.requestOnce(call, method, path, token, body, out)
		cancel()
		if err == nil || !retry || attempt == 1 || !TemporaryServiceError(err) {
			return err
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
