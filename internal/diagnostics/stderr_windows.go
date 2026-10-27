package diagnostics

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func captureStderr(path string) (io.Writer, io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var handle windows.Handle
	process := windows.CurrentProcess()
	if err := windows.DuplicateHandle(process, windows.Handle(f.Fd()), process, &handle, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, nil, err
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, handle); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, nil, err
	}
	// The runtime uses GetStdHandle for fd 2. Its handle must survive deferred
	// cleanup, while the original os.Stderr remains the developer console sink.
	return os.Stderr, nil, nil
}
