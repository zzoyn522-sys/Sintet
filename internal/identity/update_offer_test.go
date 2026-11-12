package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateOffersUseTheIndependentServiceAndNeverSendCallerSuppliedIdentity(t *testing.T) {
	token := strings.Repeat("t", 43)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/launcher/update" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("wrong update authentication")
		}
		json.NewEncoder(w).Encode(map[string]any{"release": map[string]string{"payload": "fixture", "signature": "signature"}})
	}))
	defer service.Close()
	client := New("http://unreachable-portal.invalid", "")
	client.ServiceBase = service.URL
	if _, err := client.UpdateOffer(context.Background()); err == nil {
		t.Fatal("anonymous scoped request accepted")
	}
	client.credential = &credential{Token: token}
	data, err := client.UpdateOffer(context.Background())
	if err != nil || !strings.Contains(string(data), "fixture") {
		t.Fatal("private offer unavailable without portal", err)
	}
}
