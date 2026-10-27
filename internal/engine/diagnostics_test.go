package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type brokenDiagnosticWriter struct{}

func (brokenDiagnosticWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture disk failure")
}

func diagnosticLogger(t *testing.T, w io.Writer) {
	t.Helper()
	previous, writer, flags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(w, nil)))
	t.Cleanup(func() { slog.SetDefault(previous); log.SetOutput(writer); log.SetFlags(flags) })
}

func TestEngineExitDiagnosticsAreJoined(t *testing.T) {
	var logs bytes.Buffer
	diagnosticLogger(t, &logs)
	c := faultyClient(t, "stderr")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Inspect(ctx, initialFEN, nil, false)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("process did not fail promptly: %v", err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	if !strings.Contains(output, "engine_exited") || !strings.Contains(output, "exit_code=19") || !strings.Contains(output, "engine_stderr_tail") {
		t.Fatalf("missing process diagnostics: %s", output)
	}
}

func TestBrokenLoggerStillDrainsEngineStderr(t *testing.T) {
	diagnosticLogger(t, brokenDiagnosticWriter{})
	c := faultyClient(t, "stderr")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Inspect(ctx, initialFEN, nil, false)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("log error blocked the stderr pipe: %v", err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
}
