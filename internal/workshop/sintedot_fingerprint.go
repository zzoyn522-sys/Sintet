package workshop

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"sintet/internal/packsync"
)

func (m *Manager) identifyCurseForge(ctx context.Context, path, name, digest string) (Installed, bool) {
	c := m.Client
	if c.Key == "" {
		return Installed{}, false
	}
	fingerprint, err := packsync.CurseFingerprint(ctx, path)
	if err != nil {
		return Installed{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return Installed{}, false
	}
	hash := sha1.New()
	size, err := io.Copy(hash, contextArchiveReader{ctx, file})
	_ = file.Close()
	if err != nil {
		return Installed{}, false
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	body, err := json.Marshal(struct {
		Fingerprints []uint32 `json:"fingerprints"`
	}{[]uint32{fingerprint}})
	if err != nil {
		return Installed{}, false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.CurseForge, "/")+"/fingerprints/432", bytes.NewReader(body))
	if err != nil {
		return Installed{}, false
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-api-key", c.Key)
	request.Header.Set("User-Agent", "Sintet/0.9 (sintet.xyz)")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return Installed{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Installed{}, false
	}
	var matches struct {
		Data struct {
			ExactMatches []struct {
				File struct {
					cfFile
					Fingerprint uint32 `json:"fileFingerprint"`
				}
			}
		}
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&matches) != nil {
		return Installed{}, false
	}
	var result Installed
	for _, match := range matches.Data.ExactMatches {
		f := match.File
		if f.Fingerprint != fingerprint || f.FileLength != size || f.ModID <= 0 || f.ID <= 0 {
			continue
		}
		for _, h := range f.Hashes {
			if h.Algo != 1 || !strings.EqualFold(h.Value, sha) {
				continue
			}
			record := Installed{Provider: "curseforge", Project: strconv.Itoa(f.ModID), Version: strconv.Itoa(f.ID), Filename: name, Name: f.DisplayName, Hash: digest}
			if result.Project != "" && (result.Project != record.Project || result.Version != record.Version) {
				return Installed{}, false
			}
			result = record
		}
	}
	return result, result.Project != ""
}
