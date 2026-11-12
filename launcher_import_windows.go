package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func pathWithin(root, path string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(root), strings.ToLower(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

const importReparsePoint = windows.FILE_ATTRIBUTE_REPARSE_POINT

func importAttributes(path string) (uint32, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(name)
}

func safeImportDir(path string) bool {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, "\\\\") {
		return false
	}
	for path = filepath.Clean(path); ; path = filepath.Dir(path) {
		attrs, err := importAttributes(path)
		if err != nil || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return false
		}
		if parent := filepath.Dir(path); parent == path {
			return true
		}
	}
}

func regularImportFile(path string) bool {
	attrs, err := importAttributes(path)
	return err == nil && attrs&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) == 0 &&
		safeImportDir(filepath.Dir(path))
}

func readModrinthImports(root string) []launcherImportSource {
	path := filepath.Join(root, "app.db")
	if !regularImportFile(path) {
		return nil
	}
	db, ok := openImportDatabase(path)
	if !ok {
		return nil
	}
	defer db.dll.NewProc("sqlite3_close").Call(db.handle)
	profiles := filepath.Join(root, "profiles")
	if rows := db.rows("SELECT custom_dir FROM settings LIMIT 1", 1, 1); len(rows) != 0 && safeImportDir(rows[0][0]) {
		profiles = filepath.Join(rows[0][0], "profiles")
	}
	rows := db.rows("SELECT i.path, i.name, c.game_version, c.loader, c.loader_version FROM instances i JOIN instance_content_sets c ON c.id=i.applied_content_set_id AND c.instance_id=i.id LIMIT 250", 5, 250)
	if len(rows) == 0 {
		rows = db.rows("SELECT path, name, game_version, loader, loader_version FROM profiles LIMIT 250", 5, 250)
	}
	sources := make([]launcherImportSource, 0, len(rows))
	for _, row := range rows {
		if row[0] == "" || filepath.IsAbs(row[0]) {
			continue
		}
		dir := filepath.Join(profiles, row[0])
		if strings.EqualFold(dir, profiles) || !pathWithin(profiles, dir) || !safeImportDir(dir) {
			continue
		}
		kind, version := normalizeImportLoader(row[3], row[2], row[4])
		sources = append(sources, launcherImportSource{Launcher: "modrinth", Name: row[1], Game: row[2], Loader: kind, LoaderVersion: version, Folder: dir, icon: filepath.Join(dir, "icon.png")})
	}
	return sources
}

// Only fixed, read-only metadata queries use Windows' bundled SQLite.
type importDatabase struct {
	dll    *windows.LazyDLL
	handle uintptr
}

func openImportDatabase(path string) (importDatabase, bool) {
	dll := windows.NewLazySystemDLL("winsqlite3.dll")
	if err := dll.Load(); err != nil {
		return importDatabase{}, false
	}
	name, err := windows.BytePtrFromString(path)
	if err != nil {
		return importDatabase{}, false
	}
	open := dll.NewProc("sqlite3_open_v2")
	if err := open.Find(); err != nil {
		return importDatabase{}, false
	}
	var handle uintptr
	result, _, _ := open.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&handle)), 1, 0)
	if result != 0 {
		if handle != 0 {
			dll.NewProc("sqlite3_close").Call(handle)
		}
		return importDatabase{}, false
	}
	return importDatabase{dll, handle}, true
}

func (db importDatabase) rows(query string, columns, limit int) [][]string {
	source, _ := windows.BytePtrFromString(query)
	var statement uintptr
	result, _, _ := db.dll.NewProc("sqlite3_prepare_v2").Call(db.handle, uintptr(unsafe.Pointer(source)), uintptr(len(query)), uintptr(unsafe.Pointer(&statement)), 0)
	if result != 0 || statement == 0 {
		return nil
	}
	defer db.dll.NewProc("sqlite3_finalize").Call(statement)
	step := db.dll.NewProc("sqlite3_step")
	read := db.dll.NewProc("sqlite3_column_text")
	size := db.dll.NewProc("sqlite3_column_bytes")
	var rows [][]string
	for len(rows) < limit {
		result, _, _ := step.Call(statement)
		if result != 100 { // SQLITE_ROW
			break
		}
		row := make([]string, columns)
		for col := range row {
			value, _, _ := read.Call(statement, uintptr(col))
			length, _, _ := size.Call(statement, uintptr(col))
			if value != 0 && length > 0 && length <= 32768 {
				row[col] = string(unsafe.Slice((*byte)(unsafe.Pointer(value)), int(length)))
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func startImportRelaunch() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	relaunch := exec.Command(executable, "--sintet-import-relaunch", strconv.Itoa(os.Getpid()))
	relaunch.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return relaunch.Start()
}

func runImportRelaunch(value string) {
	pid, err := strconv.ParseUint(value, 10, 32)
	if err != nil || pid == 0 {
		return
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == nil {
		status, waitErr := windows.WaitForSingleObject(parent, 30000)
		_ = windows.CloseHandle(parent)
		if waitErr != nil || status != windows.WAIT_OBJECT_0 {
			return
		}
	} else if err != windows.ERROR_INVALID_PARAMETER {
		return
	}
	executable, err := os.Executable()
	if err == nil {
		_ = exec.Command(executable, "--sintet-skip-update").Start()
	}
}
