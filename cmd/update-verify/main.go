package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

var publicKeyHex = ""

type envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type payload struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

func main() {
	if len(os.Args) != 3 {
		fail(errors.New("usage: update-verify manifest executable"))
	}
	if err := verify(os.Args[1], os.Args[2]); err != nil {
		fail(err)
	}
}

func verify(manifestPath, executablePath string) error {
	publicKey, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid embedded public key")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil || len(data) > 64*1024 {
		return errors.New("cannot read signed update manifest")
	}
	var signed envelope
	if err = json.Unmarshal(data, &signed); err != nil {
		return errors.New("invalid update manifest")
	}
	raw, err := base64.StdEncoding.DecodeString(signed.Payload)
	if err != nil {
		return errors.New("invalid manifest payload")
	}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), raw, signature) {
		return errors.New("manifest signature verification failed")
	}
	var release payload
	if err = json.Unmarshal(raw, &release); err != nil {
		return errors.New("invalid signed release metadata")
	}
	parsed, err := url.Parse(release.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "sintet-updates.zzoyn522.workers.dev" || !strings.HasPrefix(parsed.Path, "/stable/") || release.Size < 1 || release.Size > 350*1024*1024 || len(release.SHA256) != 64 {
		return errors.New("release metadata is not allowed")
	}
	file, err := os.Open(executablePath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != release.Size {
		return errors.New("downloaded executable size does not match the manifest")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(release.SHA256) {
		return errors.New("downloaded executable hash does not match the signed manifest")
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
