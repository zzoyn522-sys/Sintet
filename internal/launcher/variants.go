package launcher

import (
	"context"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VersionVariant struct {
	Base          string `json:"base"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
	Installed     bool   `json:"installed"`
}
type VariantCatalog struct {
	Variants []VersionVariant `json:"variants"`
	Warnings []string         `json:"warnings"`
}

func (s *Service) VersionVariants() (VariantCatalog, error) {
	s.manifestMu.Lock()
	manifest := s.manifest
	s.manifestMu.Unlock()
	if len(manifest.Versions) == 0 {
		if _, e := s.ListVersions(); e != nil {
			return VariantCatalog{}, e
		}
		s.manifestMu.Lock()
		manifest = s.manifest
		s.manifestMu.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	byKind := map[string]map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	warnings := []string{}
	for _, kind := range []string{"fabric", "forge", "neoforge"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			result := map[string]string{}
			var err error
			cache := filepath.Join(s.Root, ".sintet", "cache", kind+"-variants.json")
			if kind == "fabric" {
				var games []struct {
					Version string `json:"version"`
				}
				err = jsonURL(ctx, s.HTTP, "https://meta.fabricmc.net/v2/versions/game", &games)
				if err == nil {
					for _, game := range games {
						result[game.Version] = ""
					}
				}
			} else {
				url := "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
				if kind == "neoforge" {
					url = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
				}
				var data []byte
				data, err = fetch(ctx, s.HTTP, url)
				if err == nil {
					var metadata struct {
						Versions []string `xml:"versioning>versions>version"`
					}
					err = xml.Unmarshal(data, &metadata)
					for _, version := range metadata.Versions {
						base := ""
						if kind == "forge" {
							base = strings.SplitN(version, "-", 2)[0]
						} else {
							base = neoMinecraftVersion(version)
						}
						if base == "" {
							continue
						}
						if old, ok := result[base]; !ok || versionLess(old, version) {
							result[base] = version
						}
					}
				}
			}
			if err != nil {
				if readJSON(cache, &result) != nil {
					mu.Lock()
					warnings = append(warnings, fmt.Sprintf("%s: не удалось загрузить каталог", kind))
					mu.Unlock()
				}
			} else {
				_ = saveJSON(cache, result)
			}
			mu.Lock()
			byKind[kind] = result
			mu.Unlock()
		}(kind)
	}
	wg.Wait()
	installed := s.Installations()
	known := map[string]InstalledProfile{}
	for _, p := range installed {
		known[p.Base+"/"+p.Loader] = p
	}
	variants := []VersionVariant{}
	for _, version := range manifest.Versions {
		for _, kind := range []string{"forge", "fabric", "neoforge", "vanilla"} {
			key := version.ID + "/" + kind
			p, exists := known[key]
			build, supported := byKind[kind][version.ID]
			if kind == "vanilla" || supported || exists {
				if exists {
					build = p.LoaderVersion
				}
				variants = append(variants, VersionVariant{version.ID, kind, build, exists})
			}
		}
	}
	return VariantCatalog{variants, warnings}, nil
}
func neoMinecraftVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return ""
	}
	major, e := strconv.Atoi(parts[0])
	if e != nil {
		return ""
	}
	minor, e := strconv.Atoi(parts[1])
	if e != nil {
		return ""
	}
	if major >= 26 {
		return fmt.Sprintf("%d.%d", major, minor)
	}
	if major < 20 {
		return ""
	}
	if minor == 0 {
		return fmt.Sprintf("1.%d", major)
	}
	return fmt.Sprintf("1.%d.%d", major, minor)
}
