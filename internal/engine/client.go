// Package engine drives the bundled offline Pikafish JSONL helper. It has no
// dependency on UI or domain types and never opens a network connection.
package engine

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const EngineCommit = "4c17cee11f888ae1d48a9494f2e2239f019f0a1f"
const NetworkSHA256 = "7d13d73569a9b571ba0eb20cf1596247bc2a42738967e61afef6482b231e900e"
const maxLine = 1024 * 1024
const controlTimeout = 3 * time.Second

var ErrClosed = errors.New("engine client closed")

type Capture struct {
	Move string `json:"move"`
	Side string `json:"side"`
}
type Snapshot struct {
	Error      string    `json:"error"`
	LegalMoves []string  `json:"legalMoves"`
	Check      bool      `json:"check"`
	Finished   bool      `json:"finished"`
	Winner     string    `json:"winner"`
	Outcome    string    `json:"outcome"`
	Captures   []Capture `json:"captures"`
}

// Result scores are relative to the side to move, before any UI conversion.
// Mate scores use signed plies, exactly as in the original direct engine bridge.
// HasScore distinguishes an actual score from the numeric zero default.
type Result struct {
	BestMove  string   `json:"bestMove"`
	PV        []string `json:"pv"`
	Depth     int      `json:"depth"`
	Score     int      `json:"score"`
	Mate      bool     `json:"mate"`
	Bound     string   `json:"bound"`
	HasScore  bool     `json:"hasScore"`
	Cancelled bool     `json:"cancelled"`
}
type response struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
}
type ticket struct {
	id    string
	reply chan response
}

type Client struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     tailBuffer
	writes     chan []byte
	done       chan struct{}
	exited     chan struct{}
	readDone   chan struct{}
	writeDone  chan struct{}
	searchGate chan struct{}
	mu         sync.Mutex
	next       uint64
	pending    map[string]chan response
	failure    error
	activeID   string
	stopEpoch  uint64
	closeOnce  sync.Once
}

// New verifies the exact bundled NNUE and protocol/engine identity before use.
// Paths may be absolute or relative; they are resolved before starting the helper.
// Model loading is deferred until the first Search; call New off the UI thread.
func New(workerPath, networkPath string) (*Client, error) {
	networkPath, err := filepath.Abs(networkPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(networkPath)
	if err != nil {
		return nil, fmt.Errorf("offline NNUE: %w", err)
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	closeErr := f.Close()
	if err != nil {
		return nil, fmt.Errorf("read offline NNUE: %w", err)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if hex.EncodeToString(h.Sum(nil)) != NetworkSHA256 {
		return nil, errors.New("offline NNUE checksum mismatch")
	}
	workerPath, err = filepath.Abs(workerPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(workerPath)
	configureProcess(cmd)
	c, err := startClient(cmd)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var hello struct {
		Protocol      int    `json:"protocol"`
		EngineCommit  string `json:"engineCommit"`
		NetworkSHA256 string `json:"networkSHA256"`
	}
	err = c.call(ctx, map[string]any{"op": "hello", "networkPath": networkPath}, &hello)
	if err == nil && (hello.Protocol != 1 || hello.EngineCommit != EngineCommit || hello.NetworkSHA256 != NetworkSHA256) {
		err = errors.New("incompatible engine helper identity")
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func startClient(cmd *exec.Cmd) (*Client, error) {
	c := &Client{cmd: cmd, writes: make(chan []byte, 128), done: make(chan struct{}), exited: make(chan struct{}), readDone: make(chan struct{}), writeDone: make(chan struct{}), searchGate: make(chan struct{}, 1), pending: make(map[string]chan response)}
	var err error
	c.stdin, err = cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Own the read side: Cmd.Wait must not close it before the scanner has drained
	// the child's final response (the StdoutPipe/Wait race).
	read, write, err := os.Pipe()
	if err != nil {
		_ = c.stdin.Close()
		return nil, err
	}
	c.stdout = read
	cmd.Stdout = write
	cmd.Stderr = &c.stderr
	cmd.WaitDelay = controlTimeout
	if err = cmd.Start(); err != nil {
		_ = c.stdin.Close()
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("start offline engine: %w", err)
	}
	_ = write.Close()
	go c.readLoop()
	go c.writeLoop()
	go func() {
		err := cmd.Wait()
		close(c.exited)
		// Preserve the scanner's more useful protocol error when available.
		<-c.readDone
		if err == nil {
			err = io.EOF
		}
		c.fail(fmt.Errorf("engine process exited: %w", err), false)
	}()
	return c, nil
}

func (c *Client) fail(err error, kill bool) {
	c.mu.Lock()
	if c.failure != nil {
		c.mu.Unlock()
		return
	}
	c.failure = err
	c.pending = make(map[string]chan response)
	close(c.done)
	c.mu.Unlock()
	_ = c.stdin.Close() // Also interrupts a writer blocked on a non-reading child.
	if kill {
		_ = c.cmd.Process.Kill()
		_ = c.stdout.Close()
	}
}
func (c *Client) failureError() error { c.mu.Lock(); defer c.mu.Unlock(); return c.failure }

func (c *Client) readLoop() {
	defer close(c.readDone)
	defer c.stdout.Close()
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 4096), maxLine+1)
	for scanner.Scan() {
		var r response
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			c.fail(fmt.Errorf("engine invalid JSONL: %w", err), true)
			return
		}
		if r.Version != 1 || r.ID == "" || (r.Type != "result" && r.Type != "error") || (r.Type == "result" && (len(r.Result) == 0 || string(r.Result) == "null")) {
			c.fail(errors.New("engine invalid response envelope"), true)
			return
		}
		c.mu.Lock()
		ch := c.pending[r.ID]
		delete(c.pending, r.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.fail(fmt.Errorf("engine stdout: %w", err), true)
}
func (c *Client) writeLoop() {
	defer close(c.writeDone)
	for {
		select {
		case <-c.done:
			return
		case data := <-c.writes:
			if _, err := c.stdin.Write(data); err != nil {
				c.fail(fmt.Errorf("engine stdin: %w", err), true)
				return
			}
		}
	}
}
func (c *Client) forget(id string) { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }
func (c *Client) begin(ctx context.Context, request map[string]any) (ticket, error) {
	if err := ctx.Err(); err != nil {
		return ticket{}, err
	}
	c.mu.Lock()
	if c.failure != nil {
		err := c.failure
		c.mu.Unlock()
		return ticket{}, err
	}
	if len(c.pending) >= 128 {
		c.mu.Unlock()
		return ticket{}, errors.New("too many pending engine requests")
	}
	c.next++
	t := ticket{strconv.FormatUint(c.next, 10), make(chan response, 1)}
	request["id"] = t.id
	request["version"] = 1
	data, err := json.Marshal(request)
	if err != nil || len(data) > maxLine {
		c.mu.Unlock()
		if err != nil {
			return ticket{}, err
		}
		return ticket{}, errors.New("engine request exceeds 1 MiB")
	}
	c.pending[t.id] = t.reply
	c.mu.Unlock()
	select {
	case c.writes <- append(data, '\n'):
		return t, nil
	case <-ctx.Done():
		c.forget(t.id)
		return ticket{}, ctx.Err()
	case <-c.done:
		c.forget(t.id)
		return ticket{}, c.failureError()
	}
}
func decode(r response, out any) error {
	if r.Type == "error" {
		return fmt.Errorf("engine: %s", r.Error)
	}
	if err := json.Unmarshal(r.Result, out); err != nil {
		return fmt.Errorf("engine result: %w", err)
	}
	return nil
}
func (c *Client) call(ctx context.Context, request map[string]any, out any) error {
	t, err := c.begin(ctx, request)
	if err != nil {
		return err
	}
	defer c.forget(t.id)
	select {
	case r := <-t.reply:
		if err := ctx.Err(); err != nil {
			return err
		}
		return decode(r, out)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.failureError()
	}
}
func positionRequest(op, fen string, moves []string) map[string]any {
	// Always encode [] instead of null, and own the history slice for this request.
	return map[string]any{"op": op, "fen": fen, "moves": append([]string{}, moves...)}
}

// Inspect replays the full path in a disposable rules Position. Invalid chess
// positions return Snapshot.Error; transport/protocol failures return an error.
func (c *Client) Inspect(ctx context.Context, fen string, moves []string, hints bool) (Snapshot, error) {
	request := positionRequest("inspect", fen, moves)
	request["hints"] = hints
	var out Snapshot
	err := c.call(ctx, request, &out)
	return out, err
}

// Search serializes engine access. Cancellation returns immediately with ctx.Err
// and Cancelled=true; a bounded background drain holds the search slot until the
// stopped search actually completes. This prevents stale stop/new-search races.
func (c *Client) Search(ctx context.Context, fen string, moves []string, milliseconds int) (Result, error) {
	if milliseconds < 1 || milliseconds > 600000 {
		return Result{}, errors.New("milliseconds must be 1..600000")
	}
	select {
	case c.searchGate <- struct{}{}:
	case <-ctx.Done():
		return Result{Cancelled: true}, ctx.Err()
	case <-c.done:
		return Result{}, c.failureError()
	}
	request := positionRequest("search", fen, moves)
	request["milliseconds"] = milliseconds
	// Stop epochs cover the interval between request publication and assigning
	// activeID; targeted stop messages cannot affect subsequent searches.
	c.mu.Lock()
	epoch := c.stopEpoch
	c.mu.Unlock()
	t, err := c.begin(ctx, request)
	if err != nil {
		<-c.searchGate
		return Result{Cancelled: ctx.Err() != nil}, err
	}
	c.mu.Lock()
	c.activeID = t.id
	stopped := epoch != c.stopEpoch
	c.mu.Unlock()
	if stopped {
		go c.stopTarget(t.id)
	}
	release := func() {
		c.forget(t.id)
		c.mu.Lock()
		if c.activeID == t.id {
			c.activeID = ""
		}
		c.mu.Unlock()
		<-c.searchGate
	}
	select {
	case r := <-t.reply:
		defer release()
		if err := ctx.Err(); err != nil {
			return Result{Cancelled: true}, err
		}
		var out Result
		err := decode(r, &out)
		return out, err
	case <-ctx.Done():
		go func() {
			defer release()
			if err := c.stopTarget(t.id); err != nil {
				return
			}
			timer := time.NewTimer(controlTimeout)
			defer timer.Stop()
			select {
			case <-t.reply:
			case <-c.done:
			case <-timer.C:
				c.fail(errors.New("engine did not finish cancelled search"), true)
			}
		}()
		return Result{Cancelled: true}, ctx.Err()
	case <-c.done:
		release()
		return Result{}, c.failureError()
	}
}
func (c *Client) stopTarget(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	var ack struct {
		Stopped bool `json:"stopped"`
	}
	err := c.call(ctx, map[string]any{"op": "stop", "target": id}, &ack)
	if err != nil {
		c.fail(fmt.Errorf("engine stop failed: %w", err), true)
	}
	return err
}

// Stop targets only the search active at the time of this call. It waits for the
// reader's acknowledgment, not for search completion. Search returns Cancelled.
func (c *Client) Stop() error {
	c.mu.Lock()
	c.stopEpoch++
	id := c.activeID
	err := c.failure
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	return c.stopTarget(id)
}

// Close is idempotent and safe during any request. EOF asks the worker to stop
// and join its threads; a nonresponsive process is killed and reaped after 3s.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.fail(ErrClosed, false)
		timer := time.NewTimer(controlTimeout)
		defer timer.Stop()
		select {
		case <-c.exited:
		case <-timer.C:
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
		_ = c.stdout.Close()
		<-c.readDone
		<-c.writeDone
	})
	return nil
}

// Diagnostics returns only the last 8 KiB of helper stderr for troubleshooting.
func (c *Client) Diagnostics() string {
	c.stderr.mu.Lock()
	defer c.stderr.mu.Unlock()
	return string(c.stderr.data)
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) >= 8192 {
		b.data = append(b.data[:0], p[len(p)-8192:]...)
	} else {
		b.data = append(b.data, p...)
		if len(b.data) > 8192 {
			b.data = append([]byte{}, b.data[len(b.data)-8192:]...)
		}
	}
	return n, nil
}
