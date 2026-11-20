package main

import "testing"

func TestLauncherTourPersistsUntilDismissed(t *testing.T) {
	root := t.TempDir()
	existing := &App{launcherTourMarker: prepareLauncherTour(root, false)}
	if existing.LauncherTourPending() {
		t.Fatal("existing installations should not be offered the first-launch tour")
	}
	first := &App{launcherTourMarker: prepareLauncherTour(root, true)}
	if !first.LauncherTourPending() || !existing.LauncherTourPending() {
		t.Fatal("first-launch tour should remain pending across a restart")
	}
	if err := first.FinishLauncherTour(); err != nil {
		t.Fatal(err)
	}
	if existing.LauncherTourPending() {
		t.Fatal("tour should not recur after it is dismissed")
	}
	if err := existing.FinishLauncherTour(); err != nil {
		t.Fatalf("repeated dismissal should be safe: %v", err)
	}
}
