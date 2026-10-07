//go:build windows

package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

// MoveFileExW replaces an existing destination without a remove/rename gap.
// The temporary file is always on the same volume; COPY_ALLOWED is deliberately
// absent. WRITE_THROUGH requests completion before return. The file itself has
// already been flushed and closed by atomicWrite.
// https://learn.microsoft.com/windows/win32/api/winbase/nf-winbase-movefileexw
func replaceFile(from, to string) error {
	from16, err := windowsPath(from)
	if err != nil {
		return err
	}
	to16, err := windowsPath(to)
	if err != nil {
		return err
	}
	const replaceExisting = 0x1
	const writeThrough = 0x8
	result, _, callErr := moveFileExW.Call(uintptr(unsafe.Pointer(from16)), uintptr(unsafe.Pointer(to16)), replaceExisting|writeThrough)
	runtime.KeepAlive(from16)
	runtime.KeepAlive(to16)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return &os.LinkError{Op: "MoveFileExW", Old: from, New: to, Err: callErr}
	}
	return nil
}

func windowsPath(path string) (*uint16, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(abs, `\\?\`) {
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + abs[2:]
		} else {
			abs = `\\?\` + abs
		}
	}
	return syscall.UTF16PtrFromString(abs)
}

// Directory handles do not support os.File.Sync on Windows; replacement uses
// MoveFileExW's write-through flag. We do not hide errors from the replacement.
func syncDirectory(string) error { return nil }
