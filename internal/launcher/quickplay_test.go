package launcher

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestP2PQuickPlayUsesOnlyLoopbackAndRetainsLegacySupport(t *testing.T) {
	service := New(t.TempDir())
	r := Receipt{ID: "1.21.1", Base: "1.21.1", Loader: "vanilla", Java: "java", Classpath: []string{"client.jar"}}
	r.Profile.MainClass = "minecraft.Main"
	r.Profile.Arguments.Game = append(rawStrings("--username", "${auth_player_name}"), json.RawMessage(`{"rules":[{"action":"allow","features":{"is_quick_play_multiplayer":true}}],"value":["--quickPlayMultiplayer","${quickPlayMultiplayer}"]}`))
	options := LaunchOptions{MemoryMB: 4096, MinMemoryMB: 512, Width: 1280, Height: 720, InstanceMode: "profile", QuickPlayPort: 31234}
	spec, err := service.buildLaunch(r, "Player", options)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(spec.Args, " ")
	if strings.Count(args, "--quickPlayMultiplayer") != 1 || !strings.Contains(args, "--quickPlayMultiplayer 127.0.0.1:31234") || strings.Contains(args, "${") {
		t.Fatal(spec.Args)
	}
	r.Profile.Arguments.Game = rawStrings("--username", "${auth_player_name}")
	spec, err = service.buildLaunch(r, "Player", options)
	if err != nil || !strings.Contains(strings.Join(spec.Args, " "), "--server 127.0.0.1 --port 31234") {
		t.Fatal(spec.Args, err)
	}
	options.QueueBridgeJoin = func() {}
	spec, err = service.buildLaunch(r, "Player", options)
	if err != nil || strings.Contains(strings.Join(spec.Args, " "), "--server 127.0.0.1") {
		t.Fatal("bridge launch attempted a second connection via command-line flags", spec.Args, err)
	}
	fallback := appendQuickPlay(spec.Args, r.Profile, options.QuickPlayPort)
	if !strings.Contains(strings.Join(fallback, " "), "--server 127.0.0.1 --port 31234") {
		t.Fatal("legacy fallback lost its destination", fallback)
	}
	options.QuickPlayPort = 65536
	if _, err = service.buildLaunch(r, "Player", options); err == nil {
		t.Fatal("invalid auto-join port accepted")
	}
}
