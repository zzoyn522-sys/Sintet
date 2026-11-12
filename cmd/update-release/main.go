package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type payload struct {
	Version  string   `json:"version"`
	Channel  string   `json:"channel,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	URL      string   `json:"url"`
	SHA256   string   `json:"sha256"`
	Size     int64    `json:"size"`
	Released string   `json:"releasedAt"`
}
type envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

func main() {
	keyPath := flag.String("key", "", "path to the local Ed25519 signing key")
	public := flag.Bool("public-key", false, "print the public key as lowercase hex")
	initKey := flag.Bool("init-key", false, "generate a signing key if missing")
	version := flag.String("version", "", "release version")
	channel := flag.String("channel", "", "release channel: empty for stable, test for a targeted preview")
	exe := flag.String("exe", "", "launcher executable to publish")
	manifest := flag.String("manifest", "", "output signed manifest path")
	verify := flag.String("verify", "", "verify an existing signed manifest")
	flag.Parse()
	if *channel != "" && *channel != "test" {
		fatal("unsupported release channel")
	}
	if *keyPath == "" {
		fatal("-key is required")
	}
	key, err := loadKey(*keyPath, *initKey)
	if err != nil {
		fatal(err.Error())
	}
	if *public {
		pub := key.Public().(ed25519.PublicKey)
		fmt.Print(hex.EncodeToString(pub))
		return
	}
	if *verify != "" {
		data, err := os.ReadFile(*verify)
		if err != nil {
			fatal(err.Error())
		}
		var signed envelope
		if err = json.Unmarshal(data, &signed); err != nil {
			fatal("invalid signed manifest")
		}
		raw, err := base64.StdEncoding.DecodeString(signed.Payload)
		if err != nil {
			fatal("invalid manifest payload encoding")
		}
		sig, err := base64.StdEncoding.DecodeString(signed.Signature)
		if err != nil || !ed25519.Verify(key.Public().(ed25519.PublicKey), raw, sig) {
			fatal("manifest signature verification failed")
		}
		var p payload
		if err = json.Unmarshal(raw, &p); err != nil || p.Version == "" || p.Size < 1 {
			fatal("invalid manifest payload")
		}
		fmt.Printf("Verified Sintet %s manifest signature\n", p.Version)
		return
	}
	if *version == "" || *exe == "" || *manifest == "" {
		fatal("use -version, -exe and -manifest to create a release manifest")
	}
	data, err := os.ReadFile(*exe)
	if err != nil {
		fatal(err.Error())
	}
	sum := sha256.Sum256(data)
	p := payload{Version: *version, Channel: *channel, URL: fmt.Sprintf("https://sintet-updates.zzoyn522.workers.dev/stable/Sintet-%s.exe", *version), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), Released: time.Now().UTC().Format(time.RFC3339)}
	raw, err := json.Marshal(p)
	if err != nil {
		fatal(err.Error())
	}
	signed := envelope{Payload: base64.StdEncoding.EncodeToString(raw), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw))}
	out, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err = os.MkdirAll(filepath.Dir(*manifest), 0755); err == nil {
		err = os.WriteFile(*manifest, append(out, '\n'), 0644)
	}
	if err != nil {
		fatal(err.Error())
	}
	fmt.Printf("Signed manifest for Sintet %s (%d bytes)\n", *version, len(data))
}

func loadKey(path string, generate bool) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("invalid PEM key at %s", path)
		}
		key := ed25519.PrivateKey(block.Bytes)
		if len(key) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid Ed25519 key at %s", path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) || !generate {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block := &pem.Block{Type: "SINTET ED25519 PRIVATE KEY", Bytes: key}
	if err = os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
