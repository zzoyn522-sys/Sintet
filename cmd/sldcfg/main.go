// Command sldcfg signs reviewed theme packages and checks signed packages.
// It is an operator tool; it is not bundled with the launcher or exposed by the site.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sintet/internal/accountfiles"
	"sintet/internal/sldcfg"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: sldcfg keygen | pack | verify")
	}
	switch os.Args[1] {
	case "approve-stdin":
		if len(os.Args) != 2 {
			return errors.New("approve-stdin does not accept arguments")
		}
		draft, err := io.ReadAll(io.LimitReader(os.Stdin, sldcfg.MaxFile+1))
		if err != nil {
			return err
		}
		p, err := sldcfg.OpenDraft(draft)
		if err != nil {
			return err
		}
		data, err := sign(p, defaultKeyPath())
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	case "verify":
		flags := flag.NewFlagSet("verify", flag.ContinueOnError)
		input := flags.String("in", "-", "signed .sldcfg file or '-' for standard input")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		var data []byte
		var err error
		if *input == "-" {
			data, err = io.ReadAll(io.LimitReader(os.Stdin, sldcfg.MaxFile+1))
		} else {
			data, err = read(*input, sldcfg.MaxFile)
		}
		if err != nil {
			return err
		}
		p, err := sldcfg.Open(data, sldcfg.TrustedKey())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Name   string `json:"name"`
			Accent string `json:"accent"`
		}{p.Name, p.Settings.Accent})
	case "keygen":
		flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
		key := flags.String("key", defaultKeyPath(), "private DPAPI key file")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		return generate(*key)
	case "pack":
		flags := flag.NewFlagSet("pack", flag.ContinueOnError)
		key := flags.String("key", defaultKeyPath(), "private DPAPI key file")
		name := flags.String("name", "", "theme name")
		in := flags.String("appearance", "", "optional path to Sintet's own saved appearance.json")
		out := flags.String("out", "", "destination .sldcfg")
		background := flags.String("background", "", "reviewed PNG or JPEG background")
		icon := flags.String("icon", "", "reviewed PNG or JPEG icon")
		font := flags.String("font", "", "reviewed TrueType .ttf font")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if !sldcfg.ValidName(*name) || !strings.EqualFold(filepath.Ext(*out), sldcfg.Extension) {
			return errors.New("Укажи название до 24 символов и путь к файлу .sldcfg")
		}
		return pack(*key, *name, *in, *out, *background, *icon, *font)
	}
	return errors.New("usage: sldcfg keygen | pack | verify")
}

func defaultKeyPath() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "SintetSigning", "sldcfg-ed25519.dpapi")
}

func generate(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("Нужен абсолютный путь к ключу")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	defer clear(private)
	secret, err := protect(private)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(secret)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	fmt.Println(hex.EncodeToString(public))
	return nil
}

func pack(key, name, source, out, background, icon, font string) error {
	p := sldcfg.Package{Name: name, Settings: sldcfg.Default()}
	if source != "" {
		data, err := read(source, 256<<10)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, &p.Settings); err != nil || !p.Settings.Valid() {
			return errors.New("Настройки лаунчера повреждены")
		}
		if (p.Settings.Background != "" && background == "") || (p.Settings.Font == "custom" && font == "") {
			return errors.New("Укажи проверенные фон и шрифт явно через -background и -font")
		}
	}
	p.Settings.Background, p.Settings.FontFile = "", ""
	var err error
	for _, image := range []struct {
		path   string
		target *[]byte
		icon   bool
		limit  int
	}{
		{background, &p.Background, false, sldcfg.MaxBackground}, {icon, &p.Icon, true, sldcfg.MaxIcon},
	} {
		if image.path == "" {
			continue
		}
		data, e := read(image.path, image.limit)
		if e != nil {
			return e
		}
		*image.target, err = sldcfg.PreparePNG(data, image.icon)
		if err != nil {
			return err
		}
	}
	if font != "" {
		p.Font, err = read(font, sldcfg.MaxFont)
		if err != nil {
			return err
		}
		p.Settings.Font = "custom"
	}
	data, err := sign(p, key)
	if err != nil {
		return err
	}
	return accountfiles.Write(out, data)
}

func sign(p sldcfg.Package, key string) ([]byte, error) {
	ciphertext, err := read(key, 4096)
	if err != nil {
		return nil, err
	}
	keyData, err := unprotect(ciphertext)
	if err != nil {
		return nil, err
	}
	defer clear(keyData)
	if len(keyData) != ed25519.PrivateKeySize {
		return nil, sldcfg.ErrSignature
	}
	private := ed25519.PrivateKey(keyData)
	if !private.Public().(ed25519.PublicKey).Equal(sldcfg.TrustedKey()) {
		return nil, sldcfg.ErrSignature
	}
	data, err := sldcfg.Seal(p, private)
	runtime.KeepAlive(private)
	return data, err
}

func read(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(limit) {
		return nil, errors.New("Недопустимый размер файла")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err == nil && int64(len(data)) != info.Size() {
		return nil, errors.New("Файл изменился во время чтения")
	}
	return data, err
}
