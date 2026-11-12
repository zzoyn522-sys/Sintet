package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfileSyncAndPrivateGameSession(t *testing.T) {
	token := strings.Repeat("q", 43)
	account := Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "SyncTest", AvatarURL: "https://example.test/avatar-a.png", Skin: SkinMetadata{Model: "slim", Digest: "first"}}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing private authorization")
		}
		json.NewEncoder(w).Encode(map[string]any{"account": account})
	}))
	defer site.Close()
	path := filepath.Join(t.TempDir(), "identity.dpapi")
	c := New(site.URL, path)
	c.credential = &credential{Account: account, Token: token, Base: site.URL, Expires: time.Now().Add(time.Hour)}
	c.state.Error = "old connection error"
	got, secret, err := c.GameIdentity(context.Background())
	if err != nil || got.Skin.Model != "slim" || secret != token {
		t.Fatal("identity not verified")
	}
	account.AvatarURL = "https://example.test/avatar-b.png"
	account.Email = "owner@example.test"
	account.Skin.Digest = "second"
	if _, err = c.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := c.State()
	if state.Account.AvatarURL != account.AvatarURL || state.Account.Email != account.Email || state.Account.Skin.Digest != "second" || state.Error != "" {
		t.Fatal("profile did not synchronize")
	}
	restored := New(site.URL, path)
	if !restored.LoadSaved() || restored.SavedAccount().AvatarURL != account.AvatarURL || restored.SavedAccount().Email != account.Email {
		t.Fatal("the refreshed profile was not restored")
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), token) {
		t.Fatal("private session leaked to Wails")
	}
}

func TestAvatarMetadataUpdateDoesNotContactWebsiteOrReplaceSession(t *testing.T) {
	base := "http://127.0.0.1:1"
	path := filepath.Join(t.TempDir(), "identity.dpapi")
	account := Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "SyncTest"}
	c := New(base, path)
	token := strings.Repeat("q", 43)
	c.credential = &credential{Account: account, Token: token, Base: base, Expires: time.Now().Add(time.Hour)}
	c.state.Account = &account
	if err := c.UpdateAvatar(account.ID, "https://assets.test/current.png"); err != nil {
		t.Fatal(err)
	}
	restored := New(base, path)
	if !restored.LoadSaved() || restored.SavedAccount().AvatarURL != "https://assets.test/current.png" || restored.credential.Token != token {
		t.Fatal("image update replaced or lost the saved session")
	}
	if c.State().Account.AvatarURL != "https://assets.test/current.png" {
		t.Fatal("visible profile was not updated")
	}
	if err := c.Forget(); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateAvatar(account.ID, "https://assets.test/stale.png"); err == nil || c.HasSession() {
		t.Fatal("late avatar response resurrected a removed account")
	}
}
