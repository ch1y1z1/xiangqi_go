//go:build !windows

package diagnostics

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryLock(path string) (*os.File, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}

func releaseLock(f *os.File) error {
	return errors.Join(unix.Flock(int(f.Fd()), unix.LOCK_UN), f.Close())
}
