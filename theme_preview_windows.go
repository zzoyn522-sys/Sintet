package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"sintet/internal/sldcfg"
)

var themePreviewScreens = []string{"play", "settings", "profile"}

func previewBrowser() string {
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if !filepath.IsAbs(root) {
			continue
		}
		for _, relative := range []string{"Microsoft/Edge/Application/msedge.exe", "Google/Chrome/Application/chrome.exe"} {
			path := filepath.Join(root, relative)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
	}
	return ""
}

func themePreviewServer(pack sldcfg.Package) (string, func(), error) {
	frontend, err := fs.Sub(assets, "frontend")
	if err != nil {
		return "", nil, err
	}
	source, err := fs.ReadFile(frontend, "index.html")
	if err != nil {
		return "", nil, err
	}
	theme := pack.Settings
	if len(pack.Background) > 0 {
		theme.Background = "theme-preview.png"
	}
	if len(pack.Font) > 0 {
		theme.FontFile = "theme-preview.ttf"
	}
	payload, err := json.Marshal(map[string]any{"current": theme, "defaults": defaultAppearance(), "version": launcherVersion})
	if err != nil {
		return "", nil, err
	}
	listen, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	prefix := "/" + uuid.NewString() + "/"
	handler := http.NewServeMux()
	fallback := http.FileServer(http.FS(frontend))
	handler.Handle(prefix, http.StripPrefix(strings.TrimSuffix(prefix, "/"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<!doctype html><meta charset=utf-8><style>html,body{margin:0;background:#0d0d0d;overflow:hidden}iframe{display:block;border:0}</style>")
			for _, screen := range themePreviewScreens {
				fmt.Fprintf(w, "<iframe src=%q width=%d height=%d></iframe>", screen, windowWidth, windowHeight)
			}
		case "/play", "/settings", "/profile":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			html := strings.Replace(string(source), "<head>", "<head><script src=\"theme-preview-data.js\"></script><script src=\"theme-preview.js\"></script>", 1)
			fmt.Fprint(w, html)
		case "/theme-preview-data.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			fmt.Fprintf(w, "window.SintetThemePreview=%s;", payload)
		default:
			fallback.ServeHTTP(w, r)
		}
	})))
	for _, asset := range []struct {
		name, kind string
		bytes      []byte
	}{
		{"theme-preview.png", "image/png", pack.Background},
		{"theme-preview.ttf", "font/ttf", pack.Font},
	} {
		if len(asset.bytes) == 0 {
			continue
		}
		handler.HandleFunc("/custom-background/"+asset.name, func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Referer(), "http://"+listen.Addr().String()+prefix) {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", asset.kind)
			w.Header().Set("Cache-Control", "no-store")
			w.Write(asset.bytes)
		})
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != listen.Addr().String() || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'")
		handler.ServeHTTP(w, r)
	})}
	go server.Serve(listen)
	return "http://" + listen.Addr().String() + prefix, func() { _ = server.Close() }, nil
}

type previewCDP struct {
	socket *websocket.Conn
	id     int
}

func (cdp *previewCDP) call(method string, params any, result any) error {
	cdp.id++
	if err := cdp.socket.WriteJSON(map[string]any{"id": cdp.id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var reply struct {
			ID     int
			Result json.RawMessage
			Error  *struct{ Message string }
		}
		if err := cdp.socket.ReadJSON(&reply); err != nil {
			return err
		}
		if reply.ID != cdp.id {
			continue
		}
		if reply.Error != nil {
			return errors.New(reply.Error.Message)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(reply.Result, result)
	}
}

func previewBrowserSocket(ctx context.Context, profile string) (*websocket.Conn, error) {
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			port, parseErr := strconv.Atoi(strings.SplitN(string(data), "\n", 2)[0])
			if parseErr == nil && port > 0 && port <= 65535 {
				host := "127.0.0.1:" + strconv.Itoa(port)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/json/list", nil)
				if res, requestErr := client.Do(req); requestErr == nil {
					var pages []struct {
						Type                 string
						WebSocketDebuggerURL string
					}
					decodeErr := json.NewDecoder(res.Body).Decode(&pages)
					res.Body.Close()
					if decodeErr == nil {
						for _, page := range pages {
							if page.Type != "page" || !strings.HasPrefix(page.WebSocketDebuggerURL, "ws://"+host+"/devtools/page/") {
								continue
							}
							ws, _, dialErr := websocket.DefaultDialer.DialContext(ctx, page.WebSocketDebuggerURL, nil)
							if dialErr == nil {
								if deadline, ok := ctx.Deadline(); ok {
									ws.SetReadDeadline(deadline)
									ws.SetWriteDeadline(deadline)
								}
								ws.SetReadLimit(2 << 20)
								return ws, nil
							}
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func captureThemePreviews(ctx context.Context, pack sldcfg.Package) ([]byte, error) {
	browser := previewBrowser()
	if browser == "" {
		return nil, errors.New("Для предпросмотра нужен Microsoft Edge или Google Chrome.")
	}
	address, stop, err := themePreviewServer(pack)
	if err != nil {
		return nil, err
	}
	defer stop()
	profile, err := os.MkdirTemp("", "sintet-theme-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(profile)
	cmd := exec.CommandContext(ctx, browser, "--headless=new", "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1", "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--hide-scrollbars", "--mute-audio", "about:blank")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	finished := make(chan struct{})
	go func() { _ = cmd.Wait(); close(finished) }()
	defer func() {
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-finished
		}
	}()
	socket, err := previewBrowserSocket(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = socket.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		_ = socket.WriteJSON(map[string]any{"id": 1000, "method": "Browser.close"})
		_ = socket.Close()
	}()
	cdp := &previewCDP{socket: socket}
	totalHeight := windowHeight * len(themePreviewScreens)
	if err = cdp.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": windowWidth, "height": totalHeight, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		return nil, err
	}
	if err = cdp.call("Page.navigate", map[string]any{"url": address}, nil); err != nil {
		return nil, err
	}
	var ready struct{ Result struct{ Value bool } }
	waitForFrames := "new Promise(resolve=>{const timer=setInterval(()=>{const frames=[...document.querySelectorAll('iframe')];if(frames.length===3&&frames.every(frame=>frame.contentDocument?.documentElement?.dataset.themePreviewReady==='true')){clearInterval(timer);resolve(true)}},80);setTimeout(()=>{clearInterval(timer);resolve(false)},11000)})"
	if err = cdp.call("Runtime.evaluate", map[string]any{"expression": waitForFrames, "awaitPromise": true, "returnByValue": true}, &ready); err != nil {
		return nil, err
	}
	if !ready.Result.Value {
		return nil, errors.New("launcher preview frames did not finish")
	}
	var encoded bytes.Buffer
	for index := range themePreviewScreens {
		var result struct{ Data string }
		clip := map[string]any{"x": 0, "y": index * windowHeight, "width": windowWidth, "height": windowHeight, "scale": 1}
		if err = cdp.call("Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 83, "captureBeyondViewport": true, "clip": clip}, &result); err != nil {
			return nil, err
		}
		shot, decodeErr := base64.StdEncoding.DecodeString(result.Data)
		if decodeErr != nil || len(shot) == 0 || len(shot) > 512<<10 {
			return nil, errors.New("invalid preview frame")
		}
		if err = binary.Write(&encoded, binary.BigEndian, uint32(len(shot))); err != nil {
			return nil, err
		}
		encoded.Write(shot)
	}
	return encoded.Bytes(), nil
}
