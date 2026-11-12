// sldcfg-verify is the read-only validator used by the local portal.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"

	"sintet/internal/sldcfg"
)

func main() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, sldcfg.MaxFile+1))
	if err == nil {
		var p sldcfg.Package
		draft := len(os.Args) == 2 && os.Args[1] == "--draft"
		if draft {
			p, err = sldcfg.OpenDraft(data)
		} else {
			p, err = sldcfg.Open(data, sldcfg.TrustedKey())
		}
		if err == nil {
			metadata := map[string]any{"name": p.Name, "accent": p.Settings.Accent}
			if len(os.Args) == 2 && os.Args[1] == "--media" {
				images, scanErr := p.ModerationImages()
				if scanErr != nil {
					fmt.Fprintln(os.Stderr, scanErr)
					os.Exit(1)
				}
				metadata["images"] = images
				metadata["creatorVersion"] = p.CreatorVersion
				metadata["iconPNG"] = base64.StdEncoding.EncodeToString(p.Icon)
			}
			if draft {
				metadata["icon"] = preview(p.Icon, 64)
				metadata["background"] = preview(p.Background, 240)
				if len(p.Font) > 0 {
					metadata["fontBytes"] = len(p.Font)
					metadata["fontHash"] = fmt.Sprintf("%x", sha256.Sum256(p.Font))
				}
			}
			err = json.NewEncoder(os.Stdout).Encode(metadata)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func preview(data []byte, maxSide int) string {
	if len(data) == 0 {
		return ""
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	src := img.Bounds()
	extent := max(src.Dx(), src.Dy())
	w, h := max(1, src.Dx()*maxSide/extent), max(1, src.Dy()*maxSide/extent)
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(x, y, img.At(src.Min.X+x*src.Dx()/w, src.Min.Y+y*src.Dy()/h))
		}
	}
	var b bytes.Buffer
	if png.Encode(&b, out) != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}
