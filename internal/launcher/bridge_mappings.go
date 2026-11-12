package launcher

import (
	"context"
	"net/url"
	"path/filepath"
	"time"
)

// Mojang's older vanilla JARs retain obfuscated class names. The bridge
// reads just its own class names from the publisher's verified mapping file.
func (s *Service) bridgeMappings(ctx context.Context, r Receipt) (string, error) {
	if r.Loader != "" && r.Loader != "vanilla" {
		return "", nil
	}
	if err := validateID(r.Base); err != nil {
		return "", err
	}
	f := r.Profile.Downloads.ClientMappings
	if f.URL == "" {
		var base Profile
		if err := readJSON(filepath.Join(s.Root, "versions", r.Base, r.Base+".json"), &base); err != nil {
			return "", nil
		}
		f = base.Downloads.ClientMappings
	}
	if f.URL == "" {
		return "", nil
	}
	address, err := url.Parse(f.URL)
	if err != nil || address.Scheme != "https" || f.SHA1 == "" || f.Size <= 0 || f.Size > 24<<20 {
		return "", nil
	}
	switch address.Hostname() {
	case "piston-data.mojang.com", "launcher.mojang.com":
	default:
		return "", nil
	}
	f.Path = ".sintet/cache/bridge-mappings/" + r.Base + "-" + f.SHA1 + ".txt"
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := s.download(ctx, f, nil); err != nil {
		return "", err
	}
	return join(s.Root, f.Path)
}
