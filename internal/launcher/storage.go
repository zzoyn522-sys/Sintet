package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const diskHeadroom = uint64(16 << 20)

type diskSpaceError struct {
	Path                string
	Required, Available uint64
	Cause               error
}

func diskBytes(n uint64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f ГБ", float64(n)/(1<<30))
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f МБ", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d КБ", n/1024)
}

func (e *diskSpaceError) Error() string {
	volume := filepath.VolumeName(e.Path)
	if volume == "" {
		volume = e.Path
	}
	message := "Недостаточно места на диске " + volume + "."
	if e.Required > 0 {
		message += " Для загрузки нужно " + diskBytes(e.Required) + ", свободно " + diskBytes(e.Available) + "."
	}
	return message + " Освободите место или выберите другой диск в настройках папки Minecraft, затем повторите установку."
}
func (e *diskSpaceError) Unwrap() error { return e.Cause }

func IsDiskFull(err error) bool {
	var space *diskSpaceError
	return errors.As(err, &space) || errors.Is(err, syscall.ENOSPC) || platformDiskFull(err)
}

func friendlyStorageError(err error, path string) error {
	var space *diskSpaceError
	if err == nil || errors.As(err, &space) || !IsDiskFull(err) {
		return err
	}
	return &diskSpaceError{Path: path, Cause: err}
}

func storageDetails(err error) string {
	var space *diskSpaceError
	if errors.As(err, &space) && space.Cause != nil {
		return space.Cause.Error()
	}
	return err.Error()
}

func existingDiskPath(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func (s *Service) CheckDiskSpace(bytes int64) error {
	return s.checkDiskSpace(s.Root, bytes)
}

func (s *Service) checkDiskSpace(path string, bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	probe := s.diskAvailable
	if probe == nil {
		probe = availableDiskSpace
	}
	free, err := probe(existingDiskPath(path))
	if err != nil {
		// Network filesystems may not expose quota information. A failed probe is
		// not itself evidence of a full disk; actual write errors are still handled.
		return nil
	}
	required := uint64(bytes) + diskHeadroom
	if free < required {
		return &diskSpaceError{Path: path, Required: required, Available: free}
	}
	return nil
}
