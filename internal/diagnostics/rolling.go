package diagnostics

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

const logLimit = 2 << 20

// Writes go straight to the OS, without an application buffer. The mutex also
// protects rotation while diagnostic exports open their snapshot descriptors.
type rollingFile struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	size, limit int64
}

func newRollingFile(path string, limit int64) (*rollingFile, error) {
	w := &rollingFile{path: path, limit: limit}
	return w, w.open()
}

func (w *rollingFile) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file, w.size = f, st.Size()
	return nil
}

func (w *rollingFile) rotate() error {
	err := w.file.Close()
	w.file = nil
	if err == nil {
		err = os.Remove(w.path + ".2")
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	for i := 1; err == nil && i >= 0; i-- {
		from := w.path
		if i > 0 {
			from = fmt.Sprintf("%s.%d", w.path, i)
		}
		err = os.Rename(from, fmt.Sprintf("%s.%d", w.path, i+1))
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	return errors.Join(err, w.open())
}

func (w *rollingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	n := len(p)
	if int64(n) > w.limit {
		p = p[int64(n)-w.limit:]
	}
	if w.size > 0 && w.size+int64(len(p)) > w.limit {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := w.file.Write(p)
	w.size += int64(written)
	if err != nil {
		return written, err
	}
	return n, nil
}

func (w *rollingFile) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return os.ErrClosed
	}
	return w.file.Sync()
}

func (w *rollingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := errors.Join(w.file.Sync(), w.file.Close())
	w.file = nil
	return err
}
