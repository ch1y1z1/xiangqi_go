package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const initialFEN = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

func TestRealHelper(t *testing.T) {
	if os.Getenv("XIANGQI_ENGINE_INTEGRATION") != "1" {
		t.Skip("set XIANGQI_ENGINE_INTEGRATION=1 after preparing/building bundled helper")
	}
	root := filepath.Join("..", "..")
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	c, err := New(filepath.Join(root, "resources", runtime.GOOS+"-"+runtime.GOARCH, "bin", "xiangqi-engine"+suffix), filepath.Join(root, "resources", "pikafish.nnue"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	inspect := func(fen string, moves []string, hints bool) Snapshot {
		t.Helper()
		s, err := c.Inspect(ctx, fen, moves, hints)
		if err != nil || s.Error != "" {
			t.Fatalf("inspect: %+v %v; %s", s, err, c.Diagnostics())
		}
		return s
	}
	rootState := inspect(initialFEN, nil, true)
	if len(rootState.LegalMoves) != 44 || rootState.Check || rootState.Finished {
		t.Fatalf("initial: %+v", rootState)
	}
	t.Log("initial: 44 legal moves")
	// Both full branches lead to Red's turn; only the cannon in that branch can
	// move from e2 back to its original file. Revisiting restores the same rules.
	a := inspect(initialFEN, []string{"b2e2", "b9c7"}, true)
	b := inspect(initialFEN, []string{"h2e2", "b9c7"}, true)
	if !slices.Contains(a.LegalMoves, "e2b2") || slices.Contains(b.LegalMoves, "e2b2") || !slices.Contains(b.LegalMoves, "e2h2") || slices.Contains(a.LegalMoves, "e2h2") {
		t.Fatalf("branches not independently replayed: %+v %+v", a, b)
	}
	if !reflect.DeepEqual(a, inspect(initialFEN, []string{"b2e2", "b9c7"}, true)) {
		t.Fatal("branch revisit changed position")
	}
	t.Log("full paths: two independent branches and revisit passed")
	for _, tc := range []struct {
		fen  string
		want bool
	}{
		{"4k4/9/9/9/4p4/9/9/9/R1p6/4K4 w - - 0 1", true},
		{"4k4/2r6/9/9/4p4/9/9/9/R1p6/4K4 w - - 0 1", false},
		{"R1r1k4/9/9/9/4p4/9/9/9/R1p6/4K4 w - - 0 1", true},
	} {
		s := inspect(tc.fen, nil, true)
		if slices.Contains(s.Captures, Capture{"a1c1", "red"}) != tc.want {
			t.Fatalf("capture %s: %+v", tc.fen, s.Captures)
		}
		if len(inspect(tc.fen, nil, false).Captures) != 0 {
			t.Fatal("hints=false still returned captures")
		}
	}
	// Color-swapped, rank-reflected version of the unprotected fixture.
	black := inspect("4k4/r1P6/9/9/9/4P4/9/9/9/4K4 b - - 0 1", nil, true)
	if !slices.Contains(black.Captures, Capture{"a8c8", "black"}) {
		t.Fatalf("black capture: %+v", black)
	}
	t.Log("lights: unprotected / protected / pinned defender / black-side passed")
	invalid, err := c.Inspect(ctx, "9/9/9/9/9/9/9/9/9/9 w - - 0 1", nil, false)
	if err != nil || invalid.Error == "" {
		t.Fatalf("draft validation: %+v %v", invalid, err)
	}
	invalid, err = c.Inspect(ctx, initialFEN, []string{"a0a9"}, false)
	if err != nil || invalid.Error == "" {
		t.Fatalf("illegal history: %+v %v", invalid, err)
	}
	for _, tc := range []struct {
		fen, outcome, winner string
		check                bool
	}{
		{"4k4/3R1R3/9/9/9/9/9/9/9/3K5 b - - 0 1", "困毙", "red", false},
		{"4k4/3RPR3/9/9/9/9/9/9/9/3K5 b - - 0 1", "将死", "red", true},
		{"3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1", "和棋", "", false},
	} {
		s := inspect(tc.fen, nil, false)
		if !s.Finished || s.Outcome != tc.outcome || s.Winner != tc.winner || s.Check != tc.check {
			t.Fatalf("terminal: %+v", s)
		}
	}
	started := time.Now()
	result, err := c.Search(ctx, initialFEN, nil, 250)
	if err != nil || result.Cancelled || !result.HasScore || result.Depth < 1 || !slices.Contains(rootState.LegalMoves, result.BestMove) || len(result.PV) == 0 {
		t.Fatalf("search: %+v %v %s", result, err, c.Diagnostics())
	}
	t.Logf("250ms search (first model load included): %v, best=%s depth=%d score=%d bound=%s", time.Since(started), result.BestMove, result.Depth, result.Score, result.Bound)
	// Validate every PV move against the same full history.
	path := []string{}
	for _, move := range result.PV {
		s := inspect(initialFEN, path, false)
		if !slices.Contains(s.LegalMoves, move) {
			t.Fatalf("illegal PV %v + %s", path, move)
		}
		path = append(path, move)
	}
	// Inspect remains responsive while Search holds the search slot.
	searchCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		r, e := c.Search(searchCtx, initialFEN, nil, 10000)
		if !r.Cancelled {
			done <- fmt.Errorf("not cancelled: %+v %v", r, e)
			return
		}
		done <- e
	}()
	waitActive(t, c)
	time.Sleep(50 * time.Millisecond)
	started = time.Now()
	if !reflect.DeepEqual(rootState, inspect(initialFEN, nil, true)) {
		t.Fatal("search contaminated rule Position")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("inspect blocked behind search: %v", elapsed)
	}
	started = time.Now()
	stop()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel: %v", e)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("slow client cancellation: %v", elapsed)
	}
	t.Logf("cancel returned in %v; concurrent inspect passed", time.Since(started))
	// A new search must await the stopped search's completion, then be usable.
	next, err := c.Search(ctx, initialFEN, []string{"b2e2"}, 250)
	if err != nil || next.Cancelled || !next.HasScore {
		t.Fatalf("search after cancel: %+v %v", next, err)
	}
	t.Logf("black-to-move search: best=%s score=%d mate=%v", next.BestMove, next.Score, next.Mate)
	explicit := make(chan Result, 1)
	go func() { r, _ := c.Search(ctx, initialFEN, nil, 10000); explicit <- r }()
	waitActive(t, c)
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-explicit:
		if !r.Cancelled {
			t.Fatalf("explicit stop: %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not reach running helper")
	}
	// Rapid cancellation at the request/start boundary cannot poison future work.
	for i := 0; i < 8; i++ {
		short, end := context.WithTimeout(ctx, time.Millisecond)
		r, e := c.Search(short, initialFEN, nil, 10000)
		end()
		if !r.Cancelled || !errors.Is(e, context.DeadlineExceeded) {
			t.Fatalf("rapid cancel: %+v %v", r, e)
		}
	}
	next, err = c.Search(ctx, initialFEN, nil, 250)
	if err != nil || next.Cancelled {
		t.Fatalf("after rapid cancel: %+v %v", next, err)
	}
	// Closing an active search reaps the process and unblocks its caller.
	closeDone := make(chan error, 1)
	go func() { _, e := c.Search(ctx, initialFEN, nil, 10000); closeDone <- e }()
	waitActive(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; !errors.Is(err, ErrClosed) {
		t.Fatalf("close active: %v", err)
	}
	select {
	case <-c.exited:
	default:
		t.Fatal("child not reaped")
	}
}

func waitActive(t *testing.T, c *Client) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		id := c.activeID
		c.mu.Unlock()
		if id != "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("search did not become active")
}

// The test executable doubles as a deterministic faulty subprocess, avoiding
// shell/platform assumptions and exercising actual pipe/process lifecycle paths.
func TestProtocolProcess(t *testing.T) {
	mode := os.Getenv("XIANGQI_TEST_PROCESS")
	if mode == "" {
		return
	}
	if mode == "ignore" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	switch mode {
	case "malformed":
		fmt.Println("{broken")
	case "oversized":
		fmt.Println(strings.Repeat("x", maxLine+20))
	case "exit":
		os.Exit(17)
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", 64000))
		os.Exit(19)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
func faultyClient(t *testing.T, mode string) *Client {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProtocolProcess$")
	cmd.Env = append(os.Environ(), "XIANGQI_TEST_PROCESS="+mode)
	configureProcess(cmd)
	c, err := startClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestProtocolFailuresUnblockRequests(t *testing.T) {
	for _, mode := range []string{"malformed", "oversized", "exit", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			c := faultyClient(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.Inspect(ctx, initialFEN, nil, false)
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("not promptly failed: %v", err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if len(c.Diagnostics()) > 8192 {
				t.Fatal("unbounded stderr")
			}
			if mode == "malformed" && !strings.Contains(err.Error(), "invalid JSONL") {
				t.Fatalf("lost scan failure: %v", err)
			}
			if mode == "oversized" && !strings.Contains(err.Error(), "token too long") {
				t.Fatalf("lost scanner error: %v", err)
			}
		})
	}
}
func TestCancelBlockedWriteAndClose(t *testing.T) {
	c := faultyClient(t, "ignore")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Inspect(ctx, strings.Repeat("x", maxLine/2), nil, false)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("blocked write swallowed cancellation: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.exited:
	default:
		t.Fatal("unresponsive child not reaped")
	}
	if _, err := c.Inspect(context.Background(), initialFEN, nil, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("use after close: %v", err)
	}
}
func TestCancelledBeforePublication(t *testing.T) {
	c := faultyClient(t, "ignore")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := c.Search(ctx, initialFEN, nil, 250); !errors.Is(err, context.Canceled) || !r.Cancelled {
		t.Fatalf("pre-cancel: %+v %v", r, err)
	}
	c.mu.Lock()
	n := c.next
	c.mu.Unlock()
	if n != 0 {
		t.Fatal("pre-cancelled request published")
	}
}
func TestResourceValidation(t *testing.T) {
	dir := t.TempDir()
	network := filepath.Join(dir, "pikafish.nnue")
	if _, err := New("missing", network); err == nil {
		t.Fatal("missing model accepted")
	}
	if err := os.WriteFile(network, []byte("wrong model"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New("missing", network); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("model checksum not checked: %v", err)
	}
}
