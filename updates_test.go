package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sintet/internal/releaseversion"
	"strings"
	"testing"
)

func TestIndependentReleaseBranches(t *testing.T) {
	build := func(version, channel string) updatePayload {
		t.Helper()
		id, err := releaseversion.Build(version, channel)
		if err != nil {
			t.Fatal(err)
		}
		p := updatePayload{
			Version: id, DisplayVersion: version, Channel: channel, Size: 1, SHA256: strings.Repeat("a", 64),
			URL: "https://sintet-updates.zzoyn522.workers.dev/stable/Sintet-" + id + ".exe",
		}
		if channel == "beta" {
			p.Channel, p.Beta = "", true
		}
		if !validUpdatePayload(p) {
			t.Fatalf("rejected %s build %s", version, id)
		}
		return p
	}
	preview := build("0.1.0", "test")
	previewNext := build("0.1.1", "test")
	official := build("1.1.0", "")
	beta := build("1.1.0", "beta")
	betaNext := build("1.1.1", "beta")
	if preview.displayLabel() != "0.1.0 TEST" || official.displayLabel() != "1.1.0" || beta.displayLabel() != "1.1.0 BETA" {
		t.Fatal("release labels lost their branch versions")
	}
	for _, transition := range [][2]string{
		{preview.Version, "1.10.2.85"}, {official.Version, "1.10.2.70"}, {previewNext.Version, preview.Version},
		{official.Version, beta.Version}, {betaNext.Version, official.Version},
	} {
		if !versionIsNewer(transition[0], transition[1]) {
			t.Errorf("update blocked: %v", transition)
		}
	}
	for _, scenario := range []struct {
		current, channel string
		officialOnly     bool
		want             updatePayload
	}{
		{official.Version, "", false, preview},
		{preview.Version, "test", true, official},
	} {
		got := selectUpdate(official, preview, scenario.current, scenario.channel, scenario.officialOnly)
		if got.Version != scenario.want.Version || !updateAvailable(got, scenario.current, scenario.channel, scenario.officialOnly) {
			t.Fatalf("branch switch failed: %+v -> %+v", scenario, got)
		}
	}
	if got := selectUpdate(beta, preview, preview.Version, "test", true); got.Version != beta.Version {
		t.Fatal("public channel selected a TEST build")
	}
	if !updateAvailable(beta, preview.Version, "test", true) ||
		updateAvailable(beta, preview.Version, "test", false) {
		t.Fatal("switching from TEST to official updates should allow a public BETA")
	}
	if got := selectUpdate(official, updatePayload{}, preview.Version, "test", false); got.Version != preview.Version {
		t.Fatal("TEST unexpectedly downloaded a public upgrade")
	}
	for _, invalid := range [][2]string{{"1.1.0", "test"}, {"0.1.0", ""}, {"0.1.0", "beta"}, {"1.2.0", ""}, {"0.01.0", "test"}} {
		if _, err := releaseversion.Build(invalid[0], invalid[1]); err == nil {
			t.Errorf("accepted invalid branch version: %v", invalid)
		}
	}
	preview.DisplayVersion = "0.1.1"
	if validUpdatePayload(preview) {
		t.Fatal("accepted a label that identifies a different TEST build")
	}
}

func TestTesterChannelSelection(t *testing.T) {
	official := updatePayload{Version: "1.10.1.0"}
	test := updatePayload{Version: "1.10.1.1", Channel: "test"}
	if result := selectUpdate(official, test, official.Version, "", false); result.Version != test.Version {
		t.Fatal("eligible stable installation must receive TEST")
	}
	if result := selectUpdate(official, test, official.Version, "", true); result.Version != official.Version || result.Channel != "" {
		t.Fatal("official-only preference must exclude TEST")
	}
	if result := selectUpdate(official, test, "1.10.1.2", "test", false); result.Version != "1.10.1.2" {
		t.Fatal("TEST must not automatically downgrade")
	}
	if result := selectUpdate(official, test, test.Version, "test", true); result.Version != official.Version {
		t.Fatal("official-only preference must expose the signed official rollback target")
	}
}

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
	p.Beta = true
	beta, err := parseSignedUpdate(encode(p))
	if err != nil || !beta.Beta || beta.Channel != "" || releaseVersionLabel(beta.Version, "beta") != "0.9.9.1 BETA" {
		t.Fatal("public beta must keep the existing manifest format and show its label", beta, err)
	}
	p.Beta, p.PublicBeta = false, true
	if compat, err := parseSignedUpdate(encode(p)); err != nil || !compat.Beta || compat.Channel != "" {
		t.Fatalf("legacy official feed lost the BETA designation: %+v, %v", compat, err)
	}
	p.PublicBeta, p.Beta = false, true
	p.Channel = "test"
	if _, err = parseSignedUpdate(encode(p)); err == nil {
		t.Fatal("accepted a manifest for both beta and test")
	}
	p.Beta = false
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
	if !versionIsNewer("1.10.0.1", "0.9.25.79") || !versionIsNewer("1.10.0.1", "1.10.0.0") ||
		!versionIsNewer("1.10.1.0", "1.10.0.1") || releaseVersionLabel("1.10.0.1", "") != "1.10.0.1" {
		t.Fatal("four-part stable release version is ordered or labeled incorrectly")
	}
}

func TestStableReleaseDisplayVersionDoesNotAffectUpdateOrder(t *testing.T) {
	payload := updatePayload{Version: "1.10.2.32", DisplayVersion: "1.10.2.20"}
	if payload.displayLabel() != "1.10.2.20" || !versionIsNewer(payload.Version, "1.10.2.31") {
		t.Fatalf("stable label or upgrade order failed: %+v", payload)
	}
	payload.Channel = "test"
	if payload.displayLabel() != "1.10.2 TEST" {
		t.Fatal("stable display override applied to TEST")
	}
}
