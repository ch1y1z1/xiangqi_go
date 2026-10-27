// Package diagnostics retains local logs and Go crash reports, without changing
// panic behavior or relying on the UI thread during a crash.
package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type Info struct {
	Version       string `json:"version"`
	EngineCommit  string `json:"engineCommit"`
	NetworkSHA256 string `json:"networkSHA256"`
}

type record struct {
	Info
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitzero"`
	Notified  time.Time `json:"notified,omitzero"`
	State     string    `json:"state"`
	GoVersion string    `json:"goVersion"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Revision  string    `json:"revision,omitempty"`
	Modified  string    `json:"modified,omitempty"`
}

type logFailure struct{ err error }

type Session struct {
	Root, Directory string
	mu              sync.Mutex
	record          record
	lock            *os.File
	output          *rollingFile
	mirror          io.Writer
	mirrorCloser    io.Closer
	failure         atomic.Pointer[logFailure]
	previous        []Report
}

// Start must run once, before plugins, windows and background work. The runtime
// owns a duplicate crash descriptor for the rest of the process, including while
// main's deferred Close runs during panic unwinding.
func Start(root string, info Info) (*Session, error) {
	if info.Version == "" {
		info.Version = "development"
	}
	s, err := newSession(root, info)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Directory, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err == nil {
		err = debug.SetCrashOutput(f, debug.CrashOptions{})
		err = errors.Join(err, f.Close())
	}
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	mirror, closer, captureErr := captureStderr(filepath.Join(s.Directory, "stderr.log"))
	if captureErr != nil {
		s.noteFailure(captureErr)
	} else {
		s.mirror, s.mirrorCloser = mirror, closer
	}
	debug.SetTraceback("all")
	h := &syncHandler{Handler: slog.NewTextHandler(sessionWriter{s}, nil), session: s}
	slog.SetDefault(slog.New(h).With("session", s.record.ID))
	slog.Info("application_started", "version", info.Version, "pid", s.record.PID)
	return s, nil
}

func newSession(root string, info Info) (*Session, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(abs, "sessions")
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(abs, 0700); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	dir, err := os.MkdirTemp(base, fmt.Sprintf("%s-%d-", now.Format("20060102T150405.000000000Z"), os.Getpid()))
	if err != nil {
		return nil, err
	}
	s := &Session{Root: abs, Directory: dir, mirror: os.Stderr}
	s.record = record{Info: info, ID: filepath.Base(dir), PID: os.Getpid(), Started: now, State: "running", GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, v := range build.Settings {
			switch v.Key {
			case "vcs.revision":
				s.record.Revision = v.Value
			case "vcs.modified":
				s.record.Modified = v.Value
			}
		}
	}
	var acquired bool
	s.lock, acquired, err = tryLock(filepath.Join(dir, "session.lock"))
	if err == nil && !acquired {
		err = errors.New("new diagnostic session is already locked")
	}
	if err == nil {
		s.output, err = newRollingFile(filepath.Join(dir, "app.log"), logLimit)
	}
	if err == nil {
		err = writeRecord(dir, s.record)
	}
	if err != nil {
		_ = s.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	reports, err := s.reports()
	if err != nil {
		s.noteFailure(err)
	}
	for i, report := range reports {
		if i >= 10 {
			// Probe again immediately before deletion. Never prune a live process.
			lock, acquired, lockErr := tryLock(filepath.Join(report.directory, "session.lock"))
			if lockErr == nil && acquired {
				// Windows cannot remove the locked file until its handle is closed.
				_ = releaseLock(lock)
				_ = os.RemoveAll(report.directory)
			}
			continue
		}
		if report.Unclean && report.record.Notified.IsZero() {
			s.previous = append(s.previous, report)
		}
	}
	return s, nil
}

func writeRecord(dir string, r record) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "session-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(append(data, '\n'))
	err = errors.Join(err, f.Sync(), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, "session.json"))
	}
	return err
}

func (s *Session) noteFailure(err error) {
	if err != nil {
		s.failure.CompareAndSwap(nil, &logFailure{err})
	}
}

func (s *Session) Err() error {
	if failure := s.failure.Load(); failure != nil {
		return failure.err
	}
	return nil
}

func (s *Session) Previous() []Report { return append([]Report(nil), s.previous...) }

func (s *Session) LogDirectory() string { return s.Root }

// Finish is only called on the explicit successful exit path, after Shutdown.
// Close deliberately does not mark a session clean or disable crash output.
func (s *Session) Finish() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record.State, s.record.Ended = "closed", time.Now().UTC()
	err := errors.Join(s.output.Sync(), writeRecord(s.Directory, s.record))
	s.noteFailure(err)
	return err
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.output != nil {
		err = s.output.Close()
	}
	if s.lock != nil {
		err = errors.Join(err, releaseLock(s.lock))
		s.lock = nil
	}
	if s.mirrorCloser != nil {
		err = errors.Join(err, s.mirrorCloser.Close())
		s.mirrorCloser = nil
	}
	return err
}

func (s *Session) Acknowledge() error {
	var all []error
	for _, report := range s.previous {
		lock, acquired, err := tryLock(filepath.Join(report.directory, "session.lock"))
		if err != nil {
			all = append(all, err)
			continue
		}
		if !acquired {
			continue
		}
		r := report.record
		r.Notified = time.Now().UTC()
		all = append(all, writeRecord(report.directory, r), releaseLock(lock))
	}
	return errors.Join(all...)
}

type sessionWriter struct{ session *Session }

func (w sessionWriter) Write(p []byte) (int, error) {
	_, err := w.session.output.Write(p)
	w.session.noteFailure(err)
	_, _ = w.session.mirror.Write(p)
	// Logging is a side channel: a full disk must not stop a stderr consumer
	// or turn a recoverable application operation into a fatal error.
	return len(p), nil
}

type syncHandler struct {
	slog.Handler
	session *Session
}

func (h *syncHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.Handler.Handle(ctx, r)
	if r.Level >= slog.LevelError {
		h.session.noteFailure(h.session.output.Sync())
	}
	return err
}
func (h *syncHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &syncHandler{h.Handler.WithAttrs(a), h.session}
}
func (h *syncHandler) WithGroup(g string) slog.Handler {
	return &syncHandler{h.Handler.WithGroup(g), h.session}
}

// Error intentionally records classifications, not arbitrary err.Error() text:
// wrapped errors can contain user study names, image paths or credential URLs.
func Error(operation string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	kind := "operation_failed"
	switch {
	case errors.Is(err, os.ErrPermission):
		kind = "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		kind = "not_found"
	case errors.Is(err, os.ErrClosed):
		kind = "closed"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "timeout"
	case errors.Is(err, io.EOF):
		kind = "end_of_stream"
	default:
		var syntax *json.SyntaxError
		var path *os.PathError
		if errors.As(err, &syntax) {
			kind = "invalid_json"
		} else if errors.As(err, &path) {
			kind = "file_io"
		}
	}
	slog.Error("operation_failed", "operation", operation, "kind", kind, "error_type", fmt.Sprintf("%T", err))
}

func Operation(operation string, started time.Time, err error) {
	slog.Info("operation_completed", "operation", operation, "elapsed_ms", time.Since(started).Milliseconds(), "success", err == nil)
	Error(operation, err)
}

var _ io.Writer = sessionWriter{}
