package discordpresence

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows/registry"
)

func dial(ctx context.Context) (net.Conn, error) {
	var err error
	for i := 0; i < 10 && ctx.Err() == nil; i++ {
		attempt, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		var conn net.Conn
		conn, err = winio.DialPipeContext(attempt, fmt.Sprintf("\\\\.\\pipe\\discord-ipc-%d", i))
		cancel()
		if err == nil {
			return conn, nil
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	return nil, err
}

func RegisterLaunch(id string) error {
	if !ValidApplicationID(id) {
		return fmt.Errorf("invalid Discord application ID")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("Discord launcher path must be absolute")
	}
	path := "Software\\Classes\\discord-" + id
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("", "URL:Sintet Discord Lobby"); err != nil {
		return err
	}
	if err := key.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	command, _, err := registry.CreateKey(key, "shell\\open\\command", registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer command.Close()
	return command.SetStringValue("", syscall.EscapeArg(exe)+" --discord-launch")
}
