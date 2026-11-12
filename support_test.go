package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSupportNoticeWelcomeAndReleaseLifecycle(t *testing.T) {
	root := t.TempDir()
	fresh := newSupportManager(root, true)
	if notice := fresh.notice("0.9.7"); notice.Kind != "welcome" || notice.Version != "0.9.7" {
		t.Fatalf("first launch: %+v", notice)
	}
	if err := fresh.acknowledge("0.9.7"); err != nil {
		t.Fatal(err)
	}
	restarted := newSupportManager(root, false)
	if notice := restarted.notice("0.9.7"); notice.Kind != "" {
		t.Fatal("ordinary restart repeated the notice")
	}
	if notice := restarted.notice("0.9.8"); notice.Kind != "update" {
		t.Fatal("new release did not offer an update notice")
	}
	if err := restarted.acknowledge("0.9.8"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.acknowledge("0.9.7"); err != nil {
		t.Fatal(err)
	}
	if notice := newSupportManager(root, false).notice("0.9.8"); notice.Kind != "" {
		t.Fatal("an older process overwrote the remembered release")
	}
	existing := newSupportManager(t.TempDir(), false)
	if notice := existing.notice("0.9.7"); notice.Kind != "update" {
		t.Fatal("existing installation received welcome copy")
	}
}

func TestCorruptSupportNoticeDoesNotSuppressFutureReleases(t *testing.T) {
	for _, data := range []string{"\x00\x00", `{"version":"broken"}`} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "support-notice.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if notice := newSupportManager(root, false).notice("0.9.7"); notice.Kind != "update" {
			t.Fatalf("corrupt reminder blocked release notice: %+v", notice)
		}
	}
}
