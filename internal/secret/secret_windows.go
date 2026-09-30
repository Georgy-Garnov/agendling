//go:build windows

package secret

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const prefix = "dpapi:"

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func takeBlob(out *windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	res := make([]byte, out.Size)
	copy(res, unsafe.Slice(out.Data, out.Size))
	return res
}

func protect(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(data), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}

func unprotect(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(data), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}

// UseKeyFileIn is a no-op on Windows (DPAPI needs no key storage).
func UseKeyFileIn(string) {}
