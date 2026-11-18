package launcher

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

type skinTestTransport func(*http.Request) (*http.Response, error)

func (f skinTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const skinTestMetadata = "{\"meta\":{\"serverName\":\"Sintet ID\"},\"skinDomains\":[\"sintet.xyz\"],\"signaturePublickey\":\"fixture-key\"}"

func TestSintetSkinMetadataFallbackAndPersistence(t *testing.T) {
	s := New(t.TempDir())
	var calls []string
	s.HTTP.Transport = skinTestTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		if r.URL.String() == sintetSkinWorker {
			return nil, errors.New("connection lost")
		}
		if r.URL.String() != sintetSkinSite {
			t.Fatalf("unexpected host: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(skinTestMetadata)), Header: http.Header{}}, nil
	})
	raw, note, err := s.sintetSkinMetadata(t.Context(), sintetSkinWorker)
	if err != nil || !strings.Contains(note, "резервного") || !slices.Equal(calls, []string{sintetSkinWorker, sintetSkinSite}) {
		t.Fatalf("fallback failed: %v, %q, %v", err, note, calls)
	}
	var data struct{ SkinDomains []string }
	if err = json.Unmarshal(raw, &data); err != nil || !slices.Contains(data.SkinDomains, "sintet-identity.zzoyn522.workers.dev") {
		t.Fatalf("worker's texture domain missing: %s, %v", raw, err)
	}
	reopened := New(s.Root)
	reopened.HTTP.Transport = skinTestTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection lost") })
	cached, note, err := reopened.sintetSkinMetadata(t.Context(), sintetSkinWorker)
	if err != nil || string(cached) != string(raw) || !strings.Contains(note, "сохранённые") {
		t.Fatalf("cache failed: %s, %q, %v", cached, note, err)
	}
	if _, _, err = reopened.sintetSkinMetadata(t.Context(), "https://other.example/api/minecraft"); !errors.Is(err, errSintetSkinMetadata) {
		t.Fatalf("cache leaked across origins: %v", err)
	}
}

func TestSintetSkinMetadataRetriesAndRejectsHTML(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, "<html>Service offline</html>")
			return
		}
		_, _ = io.WriteString(w, skinTestMetadata)
	}))
	defer server.Close()
	s := New(t.TempDir())
	_, _, err := s.sintetSkinMetadata(t.Context(), server.URL)
	if err != nil || calls != 2 {
		t.Fatalf("invalid response was not retried: %d, %v", calls, err)
	}
}
