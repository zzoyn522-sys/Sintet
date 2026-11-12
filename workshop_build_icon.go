package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
)

func customBuildIconName(value string) string {
	digest, custom := strings.CutPrefix(value, "custom:")
	if !custom || !cachedAvatarName.MatchString(digest+".png") {
		return ""
	}
	return digest + ".png"
}

func (a *App) hasCustomBuildIcon(value string) bool {
	name := customBuildIconName(value)
	if name == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(a.profiles.root, "BuildIcons", name))
	return err == nil && info.Mode().IsRegular()
}

func (a *App) ChooseWorkshopBuildIcon() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Иконка сборки",
		Filters: []runtime.FileFilter{{DisplayName: "Изображения PNG и JPEG", Pattern: "*.png;*.jpg;*.jpeg"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	data, err := readLimitedFile(path, iconFileLimit)
	if err != nil {
		return "", err
	}
	name, err := saveCustomIcon(filepath.Join(a.profiles.root, "BuildIcons"), data)
	if err != nil {
		return "", err
	}
	return "custom:" + strings.TrimSuffix(name, ".png"), nil
}

const iconFileLimit = 8 << 20

func saveCustomIcon(directory string, data []byte) (string, error) {
	if len(data) > iconFileLimit {
		return "", errors.New("иконка должна занимать не больше 8 МБ")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return "", errors.New("выбери изображение PNG или JPEG")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return "", errors.New("размер изображения должен быть не больше 4096×4096")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("не удалось прочитать изображение")
	}

	const size = 96
	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	left := bounds.Min.X + (bounds.Dx()-side)/2
	top := bounds.Min.Y + (bounds.Dy()-side)/2
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			icon.Set(x, y, source.At(left+(2*x+1)*side/(2*size), top+(2*y+1)*side/(2*size)))
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%x.png", sha256.Sum256(encoded.Bytes()))
	if err := accountfiles.Write(filepath.Join(directory, name), encoded.Bytes()); err != nil {
		return "", err
	}
	return name, nil
}
