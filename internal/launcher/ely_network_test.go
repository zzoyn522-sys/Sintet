package launcher

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestElyInjectorNetworkSmoke(t *testing.T) {
	java := os.Getenv("SINTET_ELY_JAVA")
	if java == "" {
		t.Skip("opt-in official agent download and Java startup")
	}
	root := os.Getenv("SINTET_GAME_ROOT")
	if root == "" {
		root = t.TempDir()
	}
	s := New(root)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	jar, metadata, e := s.prepareEly(ctx)
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, java, "-javaagent:"+jar+"="+elyInjectorAPI, "-Dauthlibinjector.yggdrasil.prefetched="+metadata, "-version")
	hideProcess(cmd)
	output, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("verified authlib-injector failed to start", e)
	}
	if !strings.Contains(string(output), "authlib-injector") || !strings.Contains(string(output), "version") {
		t.Fatal("agent startup was not confirmed")
	}
	t.Log("Official authlib-injector SHA256 verified, Ely metadata prefetched, Java agent started successfully")
}

func TestSintetInjectorNetworkSmoke(t *testing.T) {
	java, api := os.Getenv("SINTET_SKIN_JAVA"), os.Getenv("SINTET_SKIN_API")
	if java == "" || api == "" {
		t.Skip("opt-in local signed skin server and Java agent")
	}
	s := New(os.Getenv("SINTET_GAME_ROOT"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	jar, metadata, err := s.prepareSkinServer(ctx, api)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, java, "-javaagent:"+jar+"="+api, "-Dauthlibinjector.yggdrasil.prefetched="+metadata, "-version")
	hideProcess(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("Sintet agent failed", err)
	}
	if !strings.Contains(string(output), "authlib-injector") || !strings.Contains(string(output), "version") {
		t.Fatal("agent startup not confirmed")
	}
	t.Log("Pinned agent hash checked, Sintet metadata loaded, Java agent started")
}
