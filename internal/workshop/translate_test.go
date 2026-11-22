package workshop

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type translationTransport func(*http.Request) (*http.Response, error)

func (f translationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestTranslationUsesLingvaWhenMyMemoryIsRateLimited(t *testing.T) {
	const input = "Lighting controls the amount of visible light."
	client := New("")
	calls := 0
	client.HTTP = &http.Client{Transport: translationTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		code, body := 200, ""
		switch calls {
		case 1:
			if r.URL.Host != "api.mymemory.translated.net" {
				t.Fatalf("unexpected primary: %s", r.URL.Host)
			}
			code, body = 429, `{"responseStatus":429}`
		case 2:
			if r.URL.Host != "translate.plausibility.cloud" || r.URL.Path != "/api/v1/auto/ru/"+input {
				t.Fatalf("unexpected fallback request: %s", r.URL)
			}
			body = `{"translation":"Настройки освещения"}`
		default:
			t.Fatalf("unexpected translation call to %s", r.URL.Host)
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	got, err := client.TranslateDescription(context.Background(), input, "ru")
	if err != nil || got != "Настройки освещения" || calls != 2 {
		t.Fatalf("got %q, %v, calls=%d", got, err, calls)
	}
}
