package sldcfg

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

// ModerationImages supplies bounded copies of the actual embedded image assets.
func (p Package) ModerationImages() ([]string, error) {
	images := make([]string, 0, 2)
	for _, data := range [][]byte{p.Background, p.Icon} {
		if len(data) == 0 {
			continue
		}
		src, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		bounds := src.Bounds()
		extent := max(bounds.Dx(), bounds.Dy(), 768)
		width, height := max(1, bounds.Dx()*768/extent), max(1, bounds.Dy()*768/extent)
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				resized.Set(x, y, src.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
			}
		}
		out := image.NewRGBA(resized.Bounds())
		draw.Draw(out, out.Bounds(), image.NewUniform(color.Gray{Y: 190}), image.Point{}, draw.Src)
		draw.Draw(out, out.Bounds(), resized, image.Point{}, draw.Over)
		var encoded bytes.Buffer
		if err = jpeg.Encode(&encoded, out, &jpeg.Options{Quality: 86}); err != nil {
			return nil, err
		}
		images = append(images, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(encoded.Bytes()))
	}
	return images, nil
}
