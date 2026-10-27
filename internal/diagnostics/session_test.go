package diagnostics

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Run the fixture on the actual main goroutine, before the test runner starts.
// The parent process never changes its logger, stderr or runtime crash sink.
func init() {
	if os.Getenv("XIANGQI_DIAGNOSTIC_PROCESS") != "" {
		runDiagnosticFixture()
		os.Exit(0)
	}
}

func runDiagnosticFixture() {
	mode := os.Getenv("XIANGQI_DIAGNOSTIC_PROCESS")
	if mode == "" {
		return
	}
	s, err := Start(os.Getenv("XIANGQI_DIAGNOSTIC_ROOT"), Info{Version: "fixture", EngineCommit: "engine-fixture"})
	if err != nil {
		panic(err)
	}
	defer s.Close()
	log.Print("legacy_logger_fixture")
	switch mode {
	case "main":
		panic("main panic fixture")
	case "worker":
		go func() { panic("worker panic fixture") }()
		select {}
	case "fatal":
		var mu sync.Mutex
		mu.Unlock()
	case "exit":
		slog.Error("startup_failed_fixture")
		os.Exit(17)
	case "privacy":
		Error("save_fixture", fmt.Errorf("credential URL https://user:secret@example.invalid/?key=private-marker"))
	case "sink_failure":
		_ = s.output.file.Close()
		slog.Error("fixture_write_to_closed_sink")
		if s.Err() == nil {
			panic("logging failure was hidden")
		}
	case "wait":
		fmt.Println(s.Directory)
		_, _ = os.Stdin.Read(make([]byte, 1))
	}
	if err := s.Finish(); err != nil && mode != "sink_failure" {
		panic(err)
	}
}

func diagnosticCommand(t *testing.T, root, mode string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDiagnosticProcess$")
	cmd.Env = append(os.Environ(), "XIANGQI_DIAGNOSTIC_PROCESS="+mode, "XIANGQI_DIAGNOSTIC_ROOT="+root)
	return cmd
}

func TestRealCrashReports(t *testing.T) {
	for _, tc := range []struct{ mode, text string }{{"main", "main panic fixture"}, {"worker", "worker panic fixture"}, {"fatal", "unlock of unlocked mutex"}, {"exit", ""}} {
		t.Run(tc.mode, func(t *testing.T) {
			root := t.TempDir()
			output, err := diagnosticCommand(t, root, tc.mode).CombinedOutput()
			if err == nil {
				t.Fatal("fatal process exited successfully")
			}
			entries, err := os.ReadDir(filepath.Join(root, "sessions"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("sessions: %v %s", err, output)
			}
			dir := filepath.Join(root, "sessions", entries[0].Name())
			crash, err := os.ReadFile(filepath.Join(dir, "crash.log"))
			if err != nil {
				t.Fatal(err)
			}
			stderr, err := os.ReadFile(filepath.Join(dir, "stderr.log"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.text != "" && (!strings.Contains(string(crash)+string(stderr), tc.text) || !strings.Contains(string(crash), "goroutine")) {
				t.Fatalf("incomplete crash evidence: crash=%s stderr=%s", crash, stderr)
			}
			if tc.text == "" && len(crash) != 0 {
				t.Fatal("os.Exit unexpectedly produced a Go traceback")
			}
			app, err := os.ReadFile(filepath.Join(dir, "app.log"))
			if err != nil || !strings.Contains(string(app), "legacy_logger_fixture") {
				t.Fatalf("legacy log missing: %v %s", err, app)
			}
			next := testSession(t, root)
			previous := next.Previous()
			if len(previous) != 1 || !previous[0].Unclean || previous[0].HasCrash != (tc.text != "") {
				t.Fatalf("incorrect prior crash classification: %+v", previous)
			}
		})
	}
}

func testSession(t *testing.T, root string) *Session {
	t.Helper()
	s, err := newSession(root, Info{Version: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestLiveSessionAndForcedExit(t *testing.T) {
	root := t.TempDir()
	cmd := diagnosticCommand(t, root, "wait")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() {
		t.Fatal("child did not become ready")
	}
	activeDir := scanner.Text()
	observer := testSession(t, root)
	if len(observer.Previous()) != 0 {
		t.Fatal("live process was classified as crashed")
	}
	if err = observer.Finish(); err != nil {
		t.Fatal(err)
	}
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	next := testSession(t, root)
	previous := next.Previous()
	if len(previous) != 1 || previous[0].ID != filepath.Base(activeDir) || previous[0].HasCrash {
		t.Fatalf("forced exit classification: %+v", previous)
	}
	if err = next.Acknowledge(); err != nil {
		t.Fatal(err)
	}
	if err = next.Finish(); err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
	if len(testSession(t, root).Previous()) != 0 {
		t.Fatal("acknowledged notification repeated")
	}
}

func TestExportWhitelistAndNormalExit(t *testing.T) {
	root := t.TempDir()
	first := testSession(t, root)
	if _, err := first.output.Write([]byte("fixture operation\n")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"image.jpg", "preferences.json", "mygo-update.key", "study.json"} {
		if err := os.WriteFile(filepath.Join(first.Directory, name), []byte("private-marker"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Finish(); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	current := testSession(t, root)
	if len(current.Previous()) != 0 {
		t.Fatal("normal exit produced a warning")
	}
	preview, err := current.Preview()
	if err != nil || !strings.Contains(preview, "fixture operation") || strings.Contains(preview, "private-marker") {
		t.Fatalf("unsafe preview: %v %s", err, preview)
	}
	destination := filepath.Join(t.TempDir(), "diagnostics.zip")
	if err = current.Export(destination); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) < 4 {
		t.Fatal("export omitted diagnostic files")
	}
	allowed := map[string]bool{"session.json": true, "crash.log": true, "app.log": true, "app.log.1": true, "app.log.2": true}
	allowed["stderr.log"] = true
	for _, f := range z.File {
		if !allowed[filepath.Base(f.Name)] {
			t.Fatalf("private file exported: %s", f.Name)
		}
	}
	if err = current.Export(filepath.Join(current.Directory, "app.log")); err == nil {
		t.Fatal("export overwrote its own log")
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{current.Directory: 0700, filepath.Join(current.Directory, "session.json"): 0600, filepath.Join(current.Directory, "app.log"): 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("unsafe permissions for %s: %v", path, err)
			}
		}
	}
}

func TestExportRejectsLinkedLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}
	s := testSession(t, t.TempDir())
	private := filepath.Join(t.TempDir(), "private.key")
	if err := os.WriteFile(private, []byte("private-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, filepath.Join(s.Directory, "crash.log")); err != nil {
		t.Fatal(err)
	}
	if err := s.Export(filepath.Join(t.TempDir(), "unsafe.zip")); err == nil {
		t.Fatal("followed symlink into a private file")
	}
}

func TestRetentionPreservesLiveSession(t *testing.T) {
	root := t.TempDir()
	live := testSession(t, root)
	for i := 0; i < 12; i++ {
		s := testSession(t, root)
		if err := s.Finish(); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
	}
	current := testSession(t, root)
	reports, err := current.reports()
	if err != nil || len(reports) != 10 {
		t.Fatalf("retention: %d %v", len(reports), err)
	}
	if _, err = os.Stat(live.Directory); err != nil {
		t.Fatal("live session was deleted")
	}
}

func TestRollingFileRetainsThreeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := newRollingFile(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, record := range []string{"0000000000", "1111111111", "2222222222", "3333333333"} {
		if _, err = w.Write([]byte(record)); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"app.log": "3333333333", "app.log.1": "2222222222", "app.log.2": "1111111111"} {
		got, err := os.ReadFile(filepath.Join(filepath.Dir(path), name))
		if err != nil || string(got) != want {
			t.Fatalf("rotation %s: %q %v", name, got, err)
		}
	}
	if _, err = os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatal("retained excess backups")
	}
	if n, err := w.Write([]byte(strings.Repeat("x", 32))); n != 32 || err != nil {
		t.Fatalf("oversized write: %d %v", n, err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > 16 {
		t.Fatal("oversized record bypassed rotation limit")
	}
}

func TestPrivateErrorsAndBrokenSink(t *testing.T) {
	for _, mode := range []string{"privacy", "sink_failure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			output, err := diagnosticCommand(t, root, mode).CombinedOutput()
			if err != nil {
				t.Fatalf("logger failure affected process: %v %s", err, output)
			}
			if strings.Contains(string(output), "private-marker") {
				t.Fatal("raw credential URL reached a log")
			}
			if len(testSession(t, root).Previous()) != 0 {
				t.Fatal("logging error changed a normal process exit into a crash")
			}
		})
	}
}

func TestUnwritableLogRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(root, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if session, err := Start(root, Info{}); err == nil || session != nil {
		t.Fatal("invalid log directory silently enabled logging")
	}
}

func TestSnapshotDoesNotPreventRotation(t *testing.T) {
	s := testSession(t, t.TempDir())
	s.output.limit = 32
	if _, err := s.output.Write([]byte("retained old record")); err != nil {
		t.Fatal(err)
	}
	files, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer closeSnapshot(files)
	if _, err := s.output.Write([]byte(strings.Repeat("n", 40))); err != nil {
		t.Fatalf("export snapshot prevented rotation: %v", err)
	}
	for _, file := range files {
		if filepath.Base(file.name) == "app.log" {
			data := make([]byte, file.size)
			if _, err := file.file.Read(data); err != nil || string(data) != "retained old record" {
				t.Fatalf("snapshot changed during rotation: %s %v", data, err)
			}
		}
	}
}

func TestPreviewShowsRecentEvents(t *testing.T) {
	s := testSession(t, t.TempDir())
	if _, err := s.output.Write([]byte(strings.Repeat("old event\n", 2000) + "latest failure fixture\n")); err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview()
	if err != nil || !strings.Contains(preview, "latest failure fixture") {
		t.Fatalf("recent failure missing from preview: %v", err)
	}
}
