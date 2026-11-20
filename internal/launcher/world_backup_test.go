package launcher

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorldBackupIntervalAndRotation(t *testing.T) {
	t.Parallel()
	instance, destination := t.TempDir(), t.TempDir()
	world := filepath.Join(instance, "saves", "Мой мир")
	if err := os.MkdirAll(world, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"level.dat", "session.lock"} {
		if err := os.WriteFile(filepath.Join(world, name), []byte("world fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	interval := 30 * time.Minute
	for _, offset := range []time.Duration{0, time.Minute, interval, 2 * interval, 3 * interval, 4 * interval, 5 * interval} {
		if err := backupWorldSnapshot(t.Context(), instance, destination, interval, start.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(destination)
	if err != nil || len(files) != worldBackupKeep || files[0].Name() != start.Add(interval).Format("20060102T150405.000000000Z.zip") {
		t.Fatalf("unexpected retention or interval: %v, %v", files, err)
	}
	archive, err := zip.OpenReader(filepath.Join(destination, files[len(files)-1].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "saves/Мой мир/level.dat" {
		t.Fatalf("archive contains wrong world files: %+v", archive.File)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := backupWorldSnapshot(ctx, instance, destination, interval, start.Add(6*interval)); err == nil {
		t.Fatal("cancelled backup was accepted")
	}
	after, err := os.ReadDir(destination)
	if err != nil || len(after) != len(files) || after[0].Name() != files[0].Name() || after[len(after)-1].Name() != files[len(files)-1].Name() {
		t.Fatalf("failed backup changed existing copies: %v, %v", after, err)
	}
}
