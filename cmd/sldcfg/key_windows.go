package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(data []byte) ([]byte, error)   { return dpapi(data, true) }
func unprotect(data []byte) ([]byte, error) { return dpapi(data, false) }

func dpapi(data []byte, encrypt bool) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if encrypt {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	raw := unsafe.Slice(out.Data, int(out.Size))
	result := append([]byte(nil), raw...)
	clear(raw)
	_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data))))
	return result, nil
}
