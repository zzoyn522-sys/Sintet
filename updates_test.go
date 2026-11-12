package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestScopedReleasesStillRequireSignatureAndDoNotTreatCurrentStableAsAnError(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previous := updatePublicKeyHex
	updatePublicKeyHex = hex.EncodeToString(public)
	defer func() { updatePublicKeyHex = previous }()
	encode := func(p updatePayload) []byte {
		raw, _ := json.Marshal(p)
		data, _ := json.Marshal(signedUpdateEnvelope{Payload: base64.StdEncoding.EncodeToString(raw), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, raw))})
		return data
	}
	p := updatePayload{Version: "0.9.9", URL: "https://sintet-updates.zzoyn522.workers.dev/stable/Sintet-0.9.9.exe", SHA256: strings.Repeat("a", 64), Size: 1024}
	if _, err = parseSignedUpdate(encode(p)); err != nil {
		t.Fatal("current stable manifest rejected", err)
	}
	p.Version = "0.9.9.1"
	p.URL = "https://sintet-updates.zzoyn522.workers.dev/stable/Sintet-0.9.9.1.exe"
	if _, err = parseSignedUpdate(encode(p)); err != nil {
		t.Fatal(err)
	}
	var envelope signedUpdateEnvelope
	json.Unmarshal(encode(p), &envelope)
	envelope.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	tampered, _ := json.Marshal(envelope)
	if _, err = parseSignedUpdate(tampered); err == nil {
		t.Fatal("unsigned private release accepted")
	}
	p.URL = "https://other.test/stable/Sintet-0.9.9.1.exe"
	if _, err = parseSignedUpdate(encode(p)); err == nil {
		t.Fatal("foreign executable accepted")
	}
	if !versionIsNewer("0.9.9.1", "0.9.9") || versionIsNewer("0.9.9", "0.9.9.1") || !versionIsNewer("0.9.10", "0.9.9.1") {
		t.Fatal("preview upgrade order failed")
	}
	if releaseVersionLabel("0.9.9.1", "test") != "0.9.9 TEST" || releaseVersionLabel("0.9.9", "") != "0.9.9" {
		t.Fatal("test release not clearly labeled")
	}
}
