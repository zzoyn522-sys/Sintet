package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sintet/internal/launcher"
	"strings"
	"testing"
)

// Opt-in local integration harness. Never part of the production executable.
func TestDevelopmentBridge(t *testing.T) {
	if os.Getenv("SINTET_TEST_SERVER") != "1" {
		t.Skip("opt-in local UI integration")
	}
	app := NewApp()
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("frontend")))
	stop := make(chan struct{})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		var args []json.RawMessage
		json.NewDecoder(r.Body).Decode(&args)
		str := func(i int) string {
			var value string
			if i < len(args) {
				json.Unmarshal(args[i], &value)
			}
			return value
		}
		var value any
		var err error
		switch strings.TrimPrefix(r.URL.Path, "/api/") {
		case "GetSettings":
			value = app.GetSettings()
		case "SaveSettings":
			var settings launcher.Settings
			var move bool
			json.Unmarshal(args[0], &settings)
			json.Unmarshal(args[1], &move)
			value, err = app.SaveSettings(settings, move)
		case "CheckJava":
			value, err = app.CheckJava(str(0))
		case "VersionVariants":
			value, err = app.VersionVariants()
		case "ListVersions":
			value, err = app.ListVersions()
		case "Installations":
			value = app.Installations()
		case "LoaderVersions":
			value, err = app.LoaderVersions(str(0), str(1))
		case "State":
			value = app.State()
		case "Install":
			err = app.Install(str(0), str(1), str(2))
		case "Launch":
			err = app.Launch(str(0), str(1))
		case "Logs":
			value = app.Logs()
		case "CancelInstall":
			app.CancelInstall()
		case "TestShutdown":
			close(stop)
		default:
			err = fmt.Errorf("unknown test method")
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"result": value})
		}
	})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	url := fmt.Sprintf("http://%s", listener.Addr())
	os.WriteFile("_checks/web-url.txt", []byte(url), 0644)
	t.Log(url, app.GetSettings())
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	<-stop
	server.Close()
	app.service.Shutdown()
}
