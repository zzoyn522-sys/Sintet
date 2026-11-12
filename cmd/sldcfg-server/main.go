// sldcfg-server validates and signs theme packages for the hosted portal.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"sintet/internal/sldcfg"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, sldcfg.MaxFile+1))
	if err != nil {
		return err
	}
	if len(os.Args) != 2 {
		return errors.New("Expected verify or sign")
	}
	switch os.Args[1] {
	case "verify", "media":
		pack, err := sldcfg.Open(data, sldcfg.TrustedKey())
		if err != nil {
			return err
		}
		metadata := map[string]any{"name": pack.Name, "accent": pack.Settings.Accent}
		if os.Args[1] == "media" {
			images, err := pack.ModerationImages()
			if err != nil {
				return err
			}
			metadata["images"] = images
			metadata["creatorVersion"] = pack.CreatorVersion
			metadata["iconPNG"] = base64.StdEncoding.EncodeToString(pack.Icon)
		}
		return json.NewEncoder(os.Stdout).Encode(metadata)
	case "sign":
		pack, err := sldcfg.OpenDraft(data)
		if err != nil {
			return err
		}
		key, err := base64.StdEncoding.DecodeString(os.Getenv("SINTET_THEME_SIGNING_KEY_B64"))
		if err != nil || len(key) != ed25519.PrivateKeySize {
			return errors.New("Theme signing service is not configured")
		}
		defer clear(key)
		private := ed25519.PrivateKey(key)
		if !private.Public().(ed25519.PublicKey).Equal(sldcfg.TrustedKey()) {
			return errors.New("Theme signing key does not match the trusted key")
		}
		signed, err := sldcfg.Seal(pack, private)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(signed)
		return err
	default:
		return errors.New("Expected verify or sign")
	}
}
