package launcher

import (
	"strings"
	"testing"
)

func TestSintetSkinServerStaysSeparateFromEly(t *testing.T) {
	s := New(t.TempDir())
	r := Receipt{ID: "test", Base: "1.21.1", Java: "java", Classpath: []string{"client.jar"}}
	r.Profile.MainClass = "main"
	r.Profile.Arguments.Game = rawStrings("--username", "${auth_player_name}", "--uuid", "${auth_uuid}", "--accessToken", "${auth_access_token}")
	o := LaunchOptions{MemoryMB: 2048, Width: 1280, Height: 720, IdentityProvider: "sintet", IdentityUUID: "ba815a63-4176-45db-8e70-12e3801c1207", AccessToken: "private-sintet-token", AuthlibAPI: "http://127.0.0.1:3033/api/minecraft", AuthlibAgent: "agent.jar", AuthlibMetadata: "e30="}
	spec, err := s.buildLaunch(r, "SintetTest", o)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(spec.Args, " ")
	if !strings.Contains(args, "-javaagent:agent.jar=http://127.0.0.1:3033/api/minecraft") || !strings.Contains(args, "--accessToken private-sintet-token") || strings.Contains(args, "ely.by") {
		t.Fatal("wrong identity or skin server")
	}
	for _, api := range []string{"http://remote.example/api", "file:///tmp/x", "https://user:pass@example.com/api", "https://example.com/api?token=x"} {
		o.AuthlibAPI = api
		if _, err = s.buildLaunch(r, "SintetTest", o); err == nil {
			t.Fatal("unsafe skin server accepted")
		}
	}
}
