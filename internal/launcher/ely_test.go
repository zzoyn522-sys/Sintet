package launcher

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
)

func TestElySkinAgentAndSessionArguments(t *testing.T) {
	s := New(t.TempDir())
	r := Receipt{ID: "test", Base: "1.21.1", Loader: "forge", Java: "java", Classpath: []string{"client.jar"}}
	r.Profile.MainClass = "main"
	r.Profile.Arguments.Game = rawStrings("--username", "${auth_player_name}", "--uuid", "${auth_uuid}", "--accessToken", "${auth_access_token}")
	options := LaunchOptions{MemoryMB: 2048, Width: 1280, Height: 720, InstanceMode: "profile", IdentityUUID: "ba815a63-4176-45db-8e70-12e3801c1207", IdentityProvider: "ely", AccessToken: "test-access-token", AuthlibAgent: filepath.Join(s.Root, "agent with spaces.jar"), AuthlibMetadata: base64.StdEncoding.EncodeToString([]byte(`{"meta":{}}`))}
	spec, e := s.buildLaunch(r, "ElyPlayer", options)
	if e != nil {
		t.Fatal(e)
	}
	agentIndex, mainIndex := -1, -1
	for i, arg := range spec.Args {
		if arg == "-javaagent:"+options.AuthlibAgent+"="+elyInjectorAPI {
			agentIndex = i
		}
		if arg == "main" {
			mainIndex = i
		}
	}
	if agentIndex < 0 || agentIndex >= mainIndex {
		t.Fatal("agent is not a JVM argument")
	}
	if !strings.Contains(strings.Join(spec.Args, " "), "--username ElyPlayer --uuid ba815a63417645db8e7012e3801c1207 --accessToken test-access-token") {
		t.Fatal("Ely profile not passed to game")
	}
	options.IdentityProvider = "sintet"
	if _, e = s.buildLaunch(r, "ElyPlayer", options); e == nil {
		t.Fatal("Ely token accepted for Sintet")
	}
	options.AccessToken = ""
	spec, e = s.buildLaunch(r, "ElyPlayer", options)
	if e != nil {
		t.Fatal(e)
	}
	for _, arg := range spec.Args {
		if strings.Contains(arg, "javaagent") || strings.Contains(arg, "authlibinjector") {
			t.Fatal("Ely skins leaked into Sintet launch")
		}
	}
}
func TestGameLogsRedactTokenAcrossEveryWriteBoundary(t *testing.T) {
	secret := "test-ely-session-token-123456789"
	for split := 1; split < len(secret); split++ {
		var output bytes.Buffer
		redactor := &tokenRedactor{target: &output, secret: secret}
		redactor.Write([]byte("before " + secret[:split]))
		redactor.Write([]byte(secret[split:] + " after " + secret))
		redactor.Flush()
		if got := output.String(); got != "before [REDACTED] after [REDACTED]" {
			t.Fatal("token redaction failed at split", split)
		}
	}
}
