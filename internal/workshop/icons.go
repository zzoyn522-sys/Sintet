package workshop

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"regexp"
	"strconv"
)

var logoFile = regexp.MustCompile(`(?m)^\s*logoFile\s*=\s*["']([^"']+)["']`)

// Icon reads bounded PNG data without extracting arbitrary archive paths.
func Icon(dir, kind, filename string) ([]byte, error) {
	path, err := localFilePath(dir, kind, filename)
	if err != nil {
		return nil, err
	}
	jar, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer jar.Close()
	name := "pack.png"
	if kind == "mods" {
		name = "icon.png"
		if data, err := zipBytes(&jar.Reader, "fabric.mod.json", 256<<10); err == nil {
			var meta struct{ Icon json.RawMessage }
			if json.Unmarshal(data, &meta) == nil {
				if json.Unmarshal(meta.Icon, &name) != nil {
					var variants map[string]string
					if json.Unmarshal(meta.Icon, &variants) == nil {
						best := 0
						for raw, path := range variants {
							size, _ := strconv.Atoi(raw)
							if size > best && size <= 512 {
								best = size
								name = path
							}
						}
					}
				}
			}
		}
		for _, file := range []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"} {
			if data, err := zipBytes(&jar.Reader, file, 256<<10); err == nil {
				if match := logoFile.FindSubmatch(data); len(match) > 1 {
					name = string(match[1])
				}
			}
		}
	}
	data, err := zipBytes(&jar.Reader, name, 2<<20)
	if err != nil {
		return nil, err
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > 1024 || config.Height > 1024 {
		return nil, errors.New("invalid icon")
	}
	return data, nil
}
