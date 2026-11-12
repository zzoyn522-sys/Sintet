// Package sldcfg implements the Sintet theme exchange format. A package can
// contain only fixed fields; it has no filenames, external references or code.
package sldcfg

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Magic           = "SLD1"
	draftMagic      = "SLD0"
	draftMagicV2    = "SLD2"
	draftMagicV3    = "SLD3"
	draftMagicV4    = "SLD4"
	draftMagicV5    = "SLD5"
	draftMagicV6    = "SLD6"
	draftMagicV7    = "SLD7"
	draftMagicV8    = "SLD8"
	draftMagicV9    = "SLD9"
	Version         = 9
	Extension       = ".sldcfg"
	headerSize      = 74
	signatureOffset = 6
	lengthOffset    = 70
	MaxBackground   = 12 << 20
	MaxFont         = 4 << 20
	MaxIcon         = 512 << 10
	maxPlain        = MaxBackground + MaxFont + MaxIcon + 1024
	MaxFile         = headerSize + maxPlain + (64 << 10)
)

// This key is independent of the launcher update key.
const publicKeyHex = "ee1c8f4702d3b997fbf889ea4d5b544ae657a5638f43b1a1e51fa8ffe633b979"

var (
	ErrSignature = errors.New("Подпись повреждена или не доверена")
	errFormat    = errors.New("Некорректный контейнер оформления .sldcfg")
	errSettings  = errors.New("Некорректные параметры оформления")
)

type Package struct {
	Name           string
	CreatorVersion string
	FormatVersion  int
	Settings       Appearance
	Background     []byte // PNG
	Font           []byte // TrueType
	Icon           []byte // PNG
}

// A draft is private review input. It is uncompressed and cannot be opened
// by any signed theme importer.
func MarshalDraft(p Package) ([]byte, error) {
	version := packageVersion(p)
	plain, err := encode(p, version)
	if err != nil {
		return nil, err
	}
	magic := draftMagicV2
	switch version {
	case 3:
		magic = draftMagicV3
	case 4:
		magic = draftMagicV4
	case 5:
		magic = draftMagicV5
	case 6:
		magic = draftMagicV6
	case 7:
		magic = draftMagicV7
	case 8:
		magic = draftMagicV8
	case 9:
		magic = draftMagicV9
	}
	return append([]byte(magic), plain...), nil
}

func OpenDraft(data []byte) (Package, error) {
	if len(data) <= 4 || len(data) > maxPlain+4 {
		return Package{}, errors.New("Некорректный черновик оформления")
	}
	switch string(data[:4]) {
	case draftMagic:
		return decode(data[4:], 1)
	case draftMagicV2:
		return decode(data[4:], 2)
	case draftMagicV3:
		return decode(data[4:], 3)
	case draftMagicV4:
		return decode(data[4:], 4)
	case draftMagicV5:
		return decode(data[4:], 5)
	case draftMagicV6:
		return decode(data[4:], 6)
	case draftMagicV7:
		return decode(data[4:], 7)
	case draftMagicV8:
		return decode(data[4:], 8)
	case draftMagicV9:
		return decode(data[4:], 9)
	default:
		return Package{}, errors.New("Некорректный черновик оформления")
	}
}

func TrustedKey() ed25519.PublicKey {
	raw, _ := hex.DecodeString(publicKeyHex)
	return ed25519.PublicKey(raw)
}

func ValidName(name string) bool {
	return utf8.ValidString(name) && strings.TrimSpace(name) == name && name != "" &&
		utf8.RuneCountInString(name) <= 24 && strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) == -1
}

func Open(file []byte, public ed25519.PublicKey) (Package, error) {
	if len(file) <= headerSize || len(file) > MaxFile || string(file[:4]) != Magic ||
		int(binary.BigEndian.Uint32(file[lengthOffset:headerSize])) != len(file)-headerSize {
		return Package{}, errFormat
	}
	version := int(binary.BigEndian.Uint16(file[4:6]))
	if version < 1 || version > Version {
		return Package{}, errFormat
	}
	if len(public) != ed25519.PublicKeySize ||
		!ed25519.Verify(public, signedBytes(file), file[signatureOffset:lengthOffset]) {
		return Package{}, ErrSignature
	}

	src := bytes.NewReader(file[headerSize:])
	z, err := gzip.NewReader(src)
	if err != nil {
		return Package{}, errFormat
	}
	z.Multistream(false)
	plain, err := io.ReadAll(io.LimitReader(z, maxPlain+1))
	_ = z.Close()
	if err != nil || len(plain) > maxPlain || src.Len() != 0 {
		return Package{}, errFormat
	}
	return decode(plain, version)
}

func signedBytes(file []byte) []byte {
	message := make([]byte, 0, len(file)-ed25519.SignatureSize)
	message = append(message, file[:signatureOffset]...)
	return append(message, file[lengthOffset:]...)
}

func Seal(p Package, key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, ErrSignature
	}
	version := packageVersion(p)
	plain, err := encode(p, version)
	if err != nil {
		return nil, err
	}
	var file bytes.Buffer
	file.Write(make([]byte, headerSize))
	z := gzip.NewWriter(&file)
	if _, err = z.Write(plain); err != nil {
		return nil, err
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	data := file.Bytes()
	if len(data) > MaxFile {
		return nil, errFormat
	}
	copy(data, Magic)
	binary.BigEndian.PutUint16(data[4:6], uint16(version))
	binary.BigEndian.PutUint32(data[lengthOffset:headerSize], uint32(len(data)-headerSize))
	copy(data[signatureOffset:lengthOffset], ed25519.Sign(key, signedBytes(data)))
	return data, nil
}

// The order and types of these fields are frozen for version 1.
func tokens(p *Appearance) []*string {
	return []*string{&p.Theme, &p.Font, &p.Accent, &p.Shadow, &p.GradientStyle, &p.GradientShape,
		&p.GradientStart, &p.GradientMiddle, &p.GradientEnd, &p.BarColor, &p.MutedColor,
		&p.Fit, &p.CanvasColor, &p.PanelColor, &p.TextColor, &p.Position}
}

func numbers(p *Appearance) []*int {
	return []*int{&p.BorderWidth, &p.BorderOpacity, &p.ShadowStrength, &p.Vignette, &p.Grain,
		&p.GradientAngle, &p.GradientOpacity, &p.Opacity, &p.Blur, &p.Radius, &p.Dim}
}

func switches(p *Appearance) []*bool {
	return []*bool{&p.Gradient, &p.Animate, &p.Compact, &p.Borders}
}

func extendedNumbers(p *Appearance) []*int {
	return []*int{&p.TextScale, &p.TextWeight, &p.ButtonRadius, &p.BackgroundBlur, &p.BackgroundSaturation,
		&p.BackgroundZoom, &p.GradientX, &p.GradientY, &p.GradientSize, &p.MotionSpeed}
}

func sectionColors(p *Appearance) []*string {
	return []*string{&p.NavColor, &p.NavText, &p.NavActive, &p.ProfileColor, &p.ProfileText, &p.ProfileControl}
}

func navigationNumbers(p *Appearance) []*int {
	return []*int{&p.NavOpacity, &p.NavBlur, &p.NavHeight, &p.NavTabWidth, &p.NavTabHeight, &p.NavTabRadius, &p.NavTabGap, &p.NavTextSize}
}

func backgroundFilterNumbers(p *Appearance) []*int {
	return []*int{&p.BackgroundBrightness, &p.BackgroundContrast, &p.BackgroundHue, &p.BackgroundGrayscale, &p.BackgroundSepia, &p.BackgroundOpacity}
}

func waveNumbers(p *Appearance) []*int {
	return []*int{&p.WaveIntensity, &p.WaveAmplitude, &p.WaveTrail}
}

var creatorVersionPattern = regexp.MustCompile(`^\d{1,5}(?:\.\d{1,5}){2,3}$`)

func ValidCreatorVersion(value string) bool { return creatorVersionPattern.MatchString(value) }

func packageVersion(p Package) int {
	settings, defaults := p.Settings, Default()
	if settings.WaveOffset != defaults.WaveOffset {
		return 9
	}
	if settings.BackgroundEffect != defaults.BackgroundEffect || settings.WaveColor != defaults.WaveColor ||
		settings.WaveIntensity != defaults.WaveIntensity || settings.WaveAmplitude != defaults.WaveAmplitude || settings.WaveTrail != defaults.WaveTrail {
		return 8
	}
	originalFilters := backgroundFilterNumbers(&defaults)
	for i, value := range backgroundFilterNumbers(&settings) {
		if *value != *originalFilters[i] {
			return 7
		}
	}
	if settings.BackgroundSaturation > 180 {
		return 7
	}
	if p.CreatorVersion != "" {
		return 6
	}
	if settings.BackgroundEdgeBlur != 0 {
		return 5
	}
	original := navigationNumbers(&defaults)
	for i, value := range navigationNumbers(&settings) {
		if *value != *original[i] {
			return 4
		}
	}
	for _, color := range sectionColors(&settings) {
		if *color != "" {
			return 3
		}
	}
	return 2
}

func encode(p Package, version int) ([]byte, error) {
	if err := validate(p); err != nil {
		return nil, err
	}
	var out []byte
	write := func(value string) { out = append(append(out, byte(len(value))), value...) }
	write(p.Name)
	for _, token := range tokens(&p.Settings) {
		write(*token)
	}
	for _, value := range numbers(&p.Settings) {
		out = binary.BigEndian.AppendUint16(out, uint16(*value))
	}
	var flags byte
	for i, on := range switches(&p.Settings) {
		if *on {
			flags |= 1 << i
		}
	}
	out = append(out, flags)
	for _, asset := range [][]byte{p.Background, p.Font, p.Icon} {
		out = binary.BigEndian.AppendUint32(out, uint32(len(asset)))
		out = append(out, asset...)
	}
	for _, value := range extendedNumbers(&p.Settings) {
		out = binary.BigEndian.AppendUint16(out, uint16(*value))
	}
	if version >= 3 {
		for _, color := range sectionColors(&p.Settings) {
			write(*color)
		}
	}
	if version >= 4 {
		for _, value := range navigationNumbers(&p.Settings) {
			out = binary.BigEndian.AppendUint16(out, uint16(*value))
		}
	}
	if version >= 5 {
		out = binary.BigEndian.AppendUint16(out, uint16(p.Settings.BackgroundEdgeBlur))
	}
	if version >= 6 {
		write(p.CreatorVersion)
	}
	if version >= 7 {
		for _, value := range backgroundFilterNumbers(&p.Settings) {
			out = binary.BigEndian.AppendUint16(out, uint16(*value))
		}
	}
	if version >= 8 {
		write(p.Settings.BackgroundEffect)
		write(p.Settings.WaveColor)
		for _, value := range waveNumbers(&p.Settings) {
			out = binary.BigEndian.AppendUint16(out, uint16(*value))
		}
	}
	if version >= 9 {
		out = binary.BigEndian.AppendUint16(out, uint16(p.Settings.WaveOffset))
	}
	return out, nil
}

type decoder struct {
	data []byte
	bad  bool
}

func (d *decoder) take(n int) []byte {
	if n < 0 || n > len(d.data) {
		d.bad = true
		return nil
	}
	out := d.data[:n]
	d.data = d.data[n:]
	return out
}
func (d *decoder) text() string {
	n := d.take(1)
	if n == nil {
		return ""
	}
	return string(d.take(int(n[0])))
}
func (d *decoder) asset(maximum int) []byte {
	n := d.take(4)
	if n == nil {
		return nil
	}
	size := int(binary.BigEndian.Uint32(n))
	if size < 0 || size > maximum {
		d.bad = true
		return nil
	}
	return d.take(size)
}

func decode(data []byte, version int) (Package, error) {
	d := decoder{data: data}
	p := Package{Name: d.text(), Settings: Default(), FormatVersion: version}
	for _, token := range tokens(&p.Settings) {
		*token = d.text()
	}
	for _, value := range numbers(&p.Settings) {
		if raw := d.take(2); raw != nil {
			*value = int(binary.BigEndian.Uint16(raw))
		}
	}
	if flags := d.take(1); flags != nil && flags[0]&0xf0 == 0 {
		for i, on := range switches(&p.Settings) {
			*on = flags[0]&(1<<i) != 0
		}
	} else {
		d.bad = true
	}
	p.Background, p.Font, p.Icon = d.asset(MaxBackground), d.asset(MaxFont), d.asset(MaxIcon)
	if version >= 2 {
		for _, value := range extendedNumbers(&p.Settings) {
			if raw := d.take(2); raw != nil {
				*value = int(binary.BigEndian.Uint16(raw))
			}
		}
	}
	if version >= 3 {
		for _, color := range sectionColors(&p.Settings) {
			*color = d.text()
		}
	}
	if version >= 4 {
		for _, value := range navigationNumbers(&p.Settings) {
			if raw := d.take(2); raw != nil {
				*value = int(binary.BigEndian.Uint16(raw))
			}
		}
	}
	if version >= 5 {
		if raw := d.take(2); raw != nil {
			p.Settings.BackgroundEdgeBlur = int(binary.BigEndian.Uint16(raw))
		}
	}
	if version >= 6 {
		p.CreatorVersion = d.text()
		if version == 6 && p.CreatorVersion == "" {
			d.bad = true
		}
	}
	if version >= 7 {
		for _, value := range backgroundFilterNumbers(&p.Settings) {
			if raw := d.take(2); raw != nil {
				*value = int(binary.BigEndian.Uint16(raw))
			}
		}
	}
	if version >= 8 {
		p.Settings.BackgroundEffect = d.text()
		p.Settings.WaveColor = d.text()
		for _, value := range waveNumbers(&p.Settings) {
			if raw := d.take(2); raw != nil {
				*value = int(binary.BigEndian.Uint16(raw))
			}
		}
	}
	if version >= 9 {
		if raw := d.take(2); raw != nil {
			p.Settings.WaveOffset = int(binary.BigEndian.Uint16(raw))
		}
	}
	if d.bad || len(d.data) != 0 || version < 7 && p.Settings.BackgroundSaturation > 180 {
		return Package{}, errFormat
	}
	if err := validate(p); err != nil {
		return Package{}, err
	}
	return p, nil
}

func validate(p Package) error {
	if p.CreatorVersion != "" && !ValidCreatorVersion(p.CreatorVersion) {
		return errFormat
	}
	settings := p.Settings
	if !ValidName(p.Name) || settings.Background != "" || settings.FontFile != "" || (settings.Font == "custom") != (len(p.Font) != 0) {
		return errSettings
	}
	if len(p.Font) > 0 {
		settings.FontFile = "00000000000000000000000000000000.ttf"
	}
	if !settings.Valid() {
		return errSettings
	}
	for _, asset := range []struct {
		data          []byte
		side, maximum int
	}{
		{p.Background, 2560, MaxBackground},
		{p.Icon, 256, MaxIcon},
	} {
		if len(asset.data) == 0 {
			continue
		}
		if !validPNG(asset.data, asset.side, asset.maximum) {
			return errors.New("Изображение в оформлении повреждено или имеет недопустимый формат")
		}
	}
	if len(p.Font) > 0 && !validTrueType(p.Font) {
		return errors.New("В оформлении повреждён или не поддерживается шрифт TrueType")
	}
	return nil
}

// PreparePNG re-encodes source images, discarding original chunks and extra
// bytes. Only static PNG may be signed or imported.
func PreparePNG(data []byte, icon bool) ([]byte, error) {
	if icon {
		return normalizePNG(data, 256, MaxIcon)
	}
	return normalizePNG(data, 2560, MaxBackground)
}

func fitsImage(cfg image.Config, side int) bool {
	return cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= side && cfg.Height <= side && cfg.Width*cfg.Height <= 5_760_000
}

func validPNG(data []byte, side, maximum int) bool {
	if len(data) < 8 || len(data) > maximum || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return false
	}
	rest := data[8:]
	for chunks := 0; chunks < 4096 && len(rest) >= 12; chunks++ {
		n := uint64(binary.BigEndian.Uint32(rest[:4]))
		if n > uint64(len(rest)-12) {
			return false
		}
		kind := string(rest[4:8])
		if (chunks == 0) != (kind == "IHDR") {
			return false
		}
		rest = rest[n+12:]
		switch kind {
		case "IHDR", "PLTE", "tRNS", "IDAT":
		case "IEND":
			if n != 0 || len(rest) != 0 {
				return false
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil || !fitsImage(cfg, side) {
				return false
			}
			_, err = png.Decode(bytes.NewReader(data))
			return err == nil
		default:
			return false // No metadata, external references or animation chunks.
		}
	}
	return false
}

func normalizePNG(data []byte, side, maximum int) ([]byte, error) {
	bad := errors.New("Поддерживаются статичные изображения PNG и JPEG подходящего размера")
	if len(data) == 0 || len(data) > maximum {
		return nil, bad
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || !fitsImage(cfg, side) {
		return nil, bad
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, bad
	}
	var output bytes.Buffer
	if err = png.Encode(&output, img); err != nil || output.Len() > maximum {
		return nil, bad
	}
	return output.Bytes(), nil
}

// Only standalone TrueType outlines, with a bounded, non-overlapping table
// directory. Reject embedded SVG/bitmaps and all unrecognized tables.
func validTrueType(data []byte) bool {
	if len(data) < 12 || len(data) > MaxFont || binary.BigEndian.Uint32(data[:4]) != 0x00010000 {
		return false
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	end := 12 + 16*count
	if count < 9 || count > 32 || end > len(data) {
		return false
	}
	type span struct{ lo, hi uint64 }
	found := map[string]span{}
	for i := 12; i < end; i += 16 {
		tag := string(data[i : i+4])
		switch tag {
		case "cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "glyf", "loca", "FFTM",
			"cvt ", "fpgm", "prep", "gasp", "kern", "GDEF", "GPOS", "GSUB", "BASE", "DSIG":
		default:
			return false
		}
		lo := uint64(binary.BigEndian.Uint32(data[i+8 : i+12]))
		hi := lo + uint64(binary.BigEndian.Uint32(data[i+12:i+16]))
		if _, exists := found[tag]; exists || lo < uint64(end) || lo%4 != 0 || hi > uint64(len(data)) {
			return false
		}
		if tag == "FFTM" && (hi-lo != 28 || binary.BigEndian.Uint32(data[lo:lo+4]) != 1) {
			return false
		}
		for _, prior := range found {
			if lo < prior.hi && prior.lo < hi {
				return false
			}
		}
		found[tag] = span{lo, hi}
	}
	for tag, minimum := range map[string]uint64{"cmap": 4, "head": 54, "hhea": 36, "hmtx": 4, "maxp": 32, "name": 6, "post": 32, "glyf": 1, "loca": 4} {
		entry := found[tag]
		if entry.hi-entry.lo < minimum {
			return false
		}
	}
	head, maxp, loca, glyf := found["head"], found["maxp"], found["loca"], found["glyf"]
	if binary.BigEndian.Uint32(data[head.lo+12:head.lo+16]) != 0x5f0f3cf5 ||
		binary.BigEndian.Uint32(data[maxp.lo:maxp.lo+4]) != 0x00010000 {
		return false
	}
	count = int(binary.BigEndian.Uint16(data[maxp.lo+4:maxp.lo+6])) + 1
	var width uint64
	switch binary.BigEndian.Uint16(data[head.lo+50 : head.lo+52]) {
	case 0:
		width = 2
	case 1:
		width = 4
	default:
		return false
	}
	if uint64(count)*width != loca.hi-loca.lo {
		return false
	}
	var previous uint64
	for i := loca.lo; i < loca.hi; i += width {
		var offset uint64
		if width == 2 {
			offset = 2 * uint64(binary.BigEndian.Uint16(data[i:i+2]))
		} else {
			offset = uint64(binary.BigEndian.Uint32(data[i : i+4]))
		}
		if offset < previous || offset > glyf.hi-glyf.lo {
			return false
		}
		previous = offset
	}
	return true
}
