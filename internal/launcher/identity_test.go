package launcher

import (
	"strings"
	"testing"
)

func TestVerifiedIdentityOverridesOfflineUUIDWithoutLeakingSession(t *testing.T) {
	s := New(t.TempDir())
	r := Receipt{ID: "test", Base: "1.21.1", Loader: "vanilla", Java: "java", Classpath: []string{"client.jar"}}
	r.Profile.MainClass = "main"
	r.Profile.Arguments.Game = rawStrings("--username", "${auth_player_name}", "--uuid", "${auth_uuid}", "--accessToken", "${auth_access_token}")
	o := LaunchOptions{MemoryMB: 2048, Width: 1280, Height: 720, InstanceMode: "profile", IdentityUUID: "ba815a63-4176-45db-8e70-12e3801c1207"}
	spec, e := s.buildLaunch(r, "SintetTest", o)
	if e != nil {
		t.Fatal(e)
	}
	args := strings.Join(spec.Args, " ")
	if !strings.Contains(args, "--username SintetTest --uuid ba815a63417645db8e7012e3801c1207 --accessToken 0") {
		t.Fatal(args)
	}
	o.IdentityUUID = "invalid"
	if _, e = s.buildLaunch(r, "SintetTest", o); e == nil {
		t.Fatal("invalid UUID accepted")
	}
}
