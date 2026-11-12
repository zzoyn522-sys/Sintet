package launcher

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func storageFixture(data []byte, url string) File {
	sum := sha1.Sum(data)
	return File{Path: "assets/objects/ee/fixture", URL: url, Size: int64(len(data)), SHA1: hex.EncodeToString(sum[:])}
}

func TestDiskPreflightStopsBeforeNetworkOrPartialFiles(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte("fixture")) }))
	defer server.Close()
	s := New(t.TempDir())
	s.diskAvailable = func(string) (uint64, error) { return 1, nil }
	err := s.downloadAll(context.Background(), []File{storageFixture([]byte("fixture"), server.URL)})
	if !IsDiskFull(err) || calls.Load() != 0 || !strings.Contains(err.Error(), "Недостаточно места") {
		t.Fatal("low disk did not stop before download", calls.Load(), err)
	}
	files, _ := filepath.Glob(filepath.Join(s.Root, "assets", "objects", "ee", ".download-*"))
	if len(files) != 0 {
		t.Fatal("created partial downloads on full disk", files)
	}
}

func TestValidCachedAssetsAreReusedEvenWithNoSpace(t *testing.T) {
	s := New(t.TempDir())
	s.diskAvailable = func(string) (uint64, error) { return 0, nil }
	data := []byte("already installed asset")
	file := storageFixture(data, "")
	dest := filepath.Join(s.Root, filepath.FromSlash(file.Path))
	os.MkdirAll(filepath.Dir(dest), 0755)
	os.WriteFile(dest, data, 0600)
	if err := s.downloadAll(context.Background(), []File{file}); err != nil {
		t.Fatal("requested space or network for an existing verified asset", err)
	}
	if state := s.State(); state.Files != 1 || state.Done != int64(len(data)) {
		t.Fatal("cached assets lost progress accounting", state)
	}
}

func TestPerFileDiskCheckDoesNotRetryOrLeaveGarbage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte("fixture")) }))
	defer server.Close()
	s := New(t.TempDir())
	s.diskAvailable = func(string) (uint64, error) { return 0, nil }
	err := s.download(context.Background(), storageFixture([]byte("fixture"), server.URL), nil)
	if !IsDiskFull(err) || calls.Load() != 1 {
		t.Fatal("retried a full disk as a network error", calls.Load(), err)
	}
	files, _ := filepath.Glob(filepath.Join(s.Root, "assets", "objects", "ee", ".download-*"))
	if len(files) != 0 {
		t.Fatal("partial download left behind", files)
	}
}

func TestDiskErrorPreservesTechnicalDetailsAndFullJournal(t *testing.T) {
	raw := &os.PathError{Op: "write", Path: "C:/fixture/.download-123", Err: syscall.ENOSPC}
	err := friendlyStorageError(raw, "C:/Minecraft")
	if !errors.Is(err, syscall.ENOSPC) || !strings.Contains(err.Error(), "Недостаточно места") || !strings.Contains(storageDetails(err), ".download-123") {
		t.Fatal("lost the disk error cause", err)
	}
	s := New(t.TempDir())
	details := storageDetails(err) + strings.Repeat(" detail", 1000) + " END_OF_FULL_ERROR"
	s.state = State{Profile: "1.21.1", Stage: "Не удалось установить", Error: err.Error(), ErrorDetails: details}
	journal, readErr := s.Journal("")
	if readErr != nil || !strings.Contains(journal.Text, details) || !strings.HasSuffix(journal.Text, "END_OF_FULL_ERROR") || len(journal.Findings) != 1 || journal.Findings[0].ID != "disk-space" {
		t.Fatal("installation error omitted or truncated in journal", journal.Findings, readErr)
	}
	unchanged, readErr := s.Journal(journal.Revision)
	if readErr != nil || unchanged.Revision != journal.Revision || unchanged.Text != "" {
		t.Fatal("unchanged journal did not respect revision", readErr)
	}
}
