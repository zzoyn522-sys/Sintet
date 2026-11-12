package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAvatarDatabaseReadsWithoutWebsiteOrSession(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 90, G: 40, B: 190, A: 255})
	png.Encode(&buffer, img)
	sum := sha256.Sum256(buffer.Bytes())
	digest := hex.EncodeToString(sum[:])
	mode := "valid"
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buffer.Bytes()) }))
	defer images.Close()
	database := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/rest/v1/sintet_id_avatars" || r.URL.Query().Get("account_id") != "eq."+id || r.URL.Query().Get("select") != "account_id,digest" {
			t.Error("unexpected database request")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("apikey") != "sb_publishable_test" {
			t.Error("avatar read used private credentials")
		}
		switch mode {
		case "outage":
			w.WriteHeader(503)
		case "invalid":
			w.Write([]byte(`[{"account_id":"` + id + `","digest":"../../private"}]`))
		default:
			json.NewEncoder(w).Encode([]map[string]string{{"account_id": id, "digest": digest}})
		}
	}))
	defer database.Close()
	db := &avatarDatabase{URL: database.URL, Key: "sb_publishable_test", ImageOrigin: images.URL, HTTP: database.Client()}
	app := &App{avatarDB: db, profiles: &profileRoster{root: t.TempDir(), base: "http://127.0.0.1:1"}}
	address, err := db.avatar(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	local, err := app.ResolveAvatar(address)
	if err != nil {
		t.Fatal(err)
	}
	mode = "outage"
	if _, err = db.avatar(context.Background(), id); err == nil {
		t.Fatal("database outage was accepted as empty avatar")
	}
	images.Close()
	if cached, err := app.ResolveAvatar(address); err != nil || cached != local {
		t.Fatal("lost the last valid avatar during an outage", err)
	}
	mode = "invalid"
	if _, err = db.avatar(context.Background(), id); err == nil {
		t.Fatal("accepted invalid digest")
	}
	if db.allowedTexture(images.URL+"/.system/signing-key.json") || db.allowedTexture(strings.Replace(address, "127.0.0.1", "unrelated.test", 1)) {
		t.Fatal("allowed unrelated image resource")
	}
}

func TestLiveAvatarDatabaseAndR2(t *testing.T) {
	if os.Getenv("SINTET_AVATAR_INTEGRATION") != "1" {
		t.Skip("opt-in read-only verification of public database and R2 images")
	}
	db := newAvatarDatabase()
	if db == nil || db.ImageOrigin == "" {
		t.Fatal("missing public avatar configuration")
	}
	req, _ := http.NewRequest("GET", db.URL+"/rest/v1/sintet_id_avatars?select=account_id&limit=5", nil)
	req.Header.Set("apikey", db.Key)
	response, err := db.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("public read failed", response.StatusCode)
	}
	var rows []struct {
		ID string `json:"account_id"`
	}
	if err = json.NewDecoder(response.Body).Decode(&rows); err != nil || len(rows) == 0 {
		t.Fatal("no avatars available for verification", err)
	}
	app := &App{avatarDB: db, profiles: &profileRoster{root: t.TempDir(), base: "http://127.0.0.1:1"}}
	for _, row := range rows {
		address, err := db.avatar(context.Background(), row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = app.ResolveAvatar(address); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Verified %d avatar records and matching R2 PNGs with website unavailable", len(rows))
}
