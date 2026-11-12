package identity

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallerKeepsKnownEncryptedDesktopAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.dpapi")
	local := New("http://127.0.0.1:3033", path)
	if err := local.storeCredential(credential{Base: local.base, Token: strings.Repeat("a", 43), Expires: time.Now().Add(time.Hour), Account: Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "Player"}}); err != nil {
		t.Fatal(err)
	}
	installed := New("https://sintet-identity.zzoyn522.workers.dev", path)
	installed.ServiceBase = installed.base
	if !installed.LoadSaved() || installed.SavedAccount().Username != "Player" {
		t.Fatal("installer lost an existing desktop account")
	}
	foreign := New("https://foreign.example", path)
	foreign.ServiceBase = installed.base
	if foreign.LoadSaved() {
		t.Fatal("accepted credentials for an unrelated origin")
	}
}
