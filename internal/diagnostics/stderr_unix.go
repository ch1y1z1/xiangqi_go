//go:build !windows

package diagnostics

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// Keep fd 2 alive until process exit. Runtime fatal prefixes can be written
// before SetCrashOutput starts mirroring; native writes also bypass os.Stderr.
func captureStderr(path string) (io.Writer, io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	fd, err := unix.Dup(2)
	var mirror *os.File
	if err == nil {
		unix.CloseOnExec(fd)
		mirror = os.NewFile(uintptr(fd), "original stderr")
	}
	if err := unix.Dup2(int(f.Fd()), 2); err != nil {
		if mirror != nil {
			_ = mirror.Close()
		}
		return nil, nil, err
	}
	if mirror == nil {
		return io.Discard, nil, nil
	}
	return mirror, mirror, nil
}
