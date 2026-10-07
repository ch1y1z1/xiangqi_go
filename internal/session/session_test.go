package session

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
)

type answer struct {
	rules  engine.Snapshot
	result engine.Result
	err    error
}
type call struct {
	kind   string
	ctx    context.Context
	fen    string
	moves  []string
	hints  bool
	budget int
	reply  chan answer
}
type fakeBackend struct {
	calls      chan *call
	stops      chan chan error
	shutdown   chan struct{}
	holdStop   bool
	running    atomic.Int32
	overlapped atomic.Bool
}

func (f *fakeBackend) run(c *call) answer {
	if f.running.Add(1) != 1 {
		f.overlapped.Store(true)
	}
	defer f.running.Add(-1)
	f.calls <- c
	// Intentionally ignore cancellation so tests can deliver a successful late
	// result. The controller must reject it, even when already queued for UI.
	select {
	case a := <-c.reply:
		return a
	case <-f.shutdown:
		return answer{err: context.Canceled}
	}
}
func (f *fakeBackend) Inspect(ctx context.Context, fen string, moves []string, hints bool) (engine.Snapshot, error) {
	a := f.run(&call{kind: "inspect", ctx: ctx, fen: fen, moves: moves, hints: hints, reply: make(chan answer, 1)})
	return a.rules, a.err
}
func (f *fakeBackend) Search(ctx context.Context, fen string, moves []string, budget int) (engine.Result, error) {
	a := f.run(&call{kind: "search", ctx: ctx, fen: fen, moves: moves, budget: budget, reply: make(chan answer, 1)})
	return a.result, a.err
}
func (f *fakeBackend) Stop() error {
	gate := make(chan error, 1)
	f.stops <- gate
	if !f.holdStop {
		return nil
	}
	select {
	case err := <-gate:
		return err
	case <-f.shutdown:
		return nil
	}
}

type harness struct {
	t  *testing.T
	s  *Session
	f  *fakeBackend
	ui chan func()
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("channel handshake timed out")
		var zero T
		return zero
	}
}

func newHarness(t *testing.T, study domain.Study, holdStop bool) *harness {
	t.Helper()
	f := &fakeBackend{calls: make(chan *call, 100), stops: make(chan chan error, 100), shutdown: make(chan struct{}), holdStop: holdStop}
	s := New(study, f, nil)
	h := &harness{t: t, s: s, f: f, ui: make(chan func(), 1000)}
	s.Dispatch = func(fn func()) { h.ui <- fn }
	t.Cleanup(func() {
		s.Leave()
		close(f.shutdown)
		receive(t, s.tail)
		if f.overlapped.Load() {
			t.Error("backend Inspect/Search overlapped")
		}
	})
	return h
}
func initialStudy() domain.Study {
	return domain.NewStudy("测试", domain.InitialPieces(), domain.Red)
}
func move(uci string) domain.Move {
	m, err := domain.ParseMove(uci)
	if err != nil {
		panic(err)
	}
	return m
}
func rules() engine.Snapshot {
	return engine.Snapshot{LegalMoves: []string{"a3a4", "c3c4", "a6a5", "c6c5"}}
}
func score(value int) engine.Result {
	return engine.Result{BestMove: "a3a4", PV: []string{"a3a4"}, Score: value, HasScore: true, Depth: 8}
}
func (h *harness) next(kind string) *call {
	h.t.Helper()
	c := receive(h.t, h.f.calls)
	if c.kind != kind {
		h.t.Fatalf("want %s, got %s", kind, c.kind)
	}
	return c
}
func (h *harness) dispatch()               { h.t.Helper(); receive(h.t, h.ui)() }
func (h *harness) reply(c *call, a answer) { h.t.Helper(); c.reply <- a; h.dispatch() }
func (h *harness) activate(evaluation bool) {
	h.t.Helper()
	h.s.SetShowEvaluation(evaluation)
	h.s.Activate()
	if !h.s.RulesBusy {
		h.t.Fatal("Activate must expose asynchronous rule inspection")
	}
	h.reply(h.next("inspect"), answer{rules: rules()})
}
func (h *harness) noJob() {
	h.t.Helper()
	if h.s.job != nil {
		h.t.Fatalf("unexpected scheduled task: %+v", h.s.job)
	}
}

func TestContractSurface(t *testing.T) {
	var _ interface {
		Activate()
		Leave()
		Tap(domain.Square)
		Drag(domain.Square, domain.Square)
		Jump(string)
		Back()
		Forward() bool
		Root()
		Flip()
		SetAI(domain.Side)
		ResumeAI()
		Recommend()
		Adopt()
		Stop()
		SetBudget(int)
		SetShowLights(bool)
		SetShowEvaluation(bool)
		SetShowBranchScores(bool)
		OpenBranches(string)
		CloseBranches()
		Reanalyze()
		EvalText(string) string
		EvalDetail(string) string
		Comparison(string, string) string
		ApplyEdit(domain.Study) error
	} = (*Session)(nil)
	s := New(initialStudy(), nil, nil)
	if s.AISide != "" || !s.ShowEvaluation || !s.ShowBranchScores || s.Budget != 1000 || s.Active {
		t.Fatal("incorrect defaults")
	}
	s.Activate()
	if s.Active || s.Error == "" {
		t.Fatal("missing dispatcher/backend must fail explicitly")
	}
}

func TestInspectDispatchThenValidateNode(t *testing.T) {
	study := initialStudy()
	child := study.Play(move("a3a4"))
	study.CurrentID = study.RootID
	h := newHarness(t, study, false)
	s := h.s
	s.SetShowEvaluation(false)
	s.Activate()
	old := h.next("inspect")
	old.reply <- answer{rules: engine.Snapshot{Error: "late rule error"}}
	queued := receive(t, h.ui)
	if s.Rules.Error != "" || !s.RulesBusy {
		t.Fatal("worker mutated UI before Dispatch")
	}
	s.Jump(child)
	queued()
	if s.Rules.Error != "" || !s.RulesBusy {
		t.Fatal("old rules replaced current node state")
	}
	current := h.next("inspect")
	if !reflect.DeepEqual(current.moves, []string{"a3a4"}) {
		t.Fatal(current.moves)
	}
	h.reply(current, answer{rules: rules()})
	if s.RulesBusy || s.Study.CurrentID != child {
		t.Fatal("current rules not installed")
	}
}

func TestLateResultsAfterLeave(t *testing.T) {
	for _, kind := range []string{"inspect", "search"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t, initialStudy(), false)
			s := h.s
			if kind == "inspect" {
				s.Activate()
			} else {
				h.activate(false)
				s.Recommend()
			}
			c := h.next(kind)
			c.reply <- answer{rules: engine.Snapshot{Error: "late"}, result: score(100)}
			queued := receive(t, h.ui)
			s.Leave()
			changes := 0
			s.OnChange = func() { changes++ }
			queued()
			if changes != 0 || s.Active || s.Suggestion != nil || s.Error != "" || s.Busy || s.RulesBusy {
				t.Fatal("late result touched left session")
			}
		})
	}
}

func TestPreemptBackgroundWaitsForSearchAndStop(t *testing.T) {
	h := newHarness(t, initialStudy(), true)
	s := h.s
	h.activate(true)
	old := h.next("search")
	s.Recommend()
	stopGate := receive(t, h.f.stops) // Stop is reached while old search is blocked.
	if old.ctx.Err() == nil {
		t.Fatal("preempted search context was not cancelled")
	}
	if !s.Busy || s.job.purpose != recommendSearch {
		t.Fatal("active request did not preempt background")
	}
	old.reply <- answer{result: score(900)}
	h.dispatch() // Cannot cache a late background result.
	if len(s.evaluations) != 0 {
		t.Fatal("stale background result cached")
	}
	select {
	case <-s.tail:
		t.Fatal("replacement completed before Stop barrier")
	default:
	}
	stopGate <- nil
	active := h.next("search")
	h.reply(active, answer{result: score(120)})
	if s.Suggestion == nil || s.Suggestion.Score != 120 || len(s.Study.Nodes) != 1 {
		t.Fatal("recommendation should not play")
	}
	s.Adopt()
	if len(s.Study.Nodes) != 2 || s.Suggestion != nil {
		t.Fatal("Adopt must play once and clear suggestion")
	}
	h.reply(h.next("inspect"), answer{rules: rules()})
}

func TestStopCompletesBeforeOldSearchStillWaitsForOldSearch(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	h.activate(true)
	old := h.next("search")
	h.s.Recommend()
	receive(t, h.f.stops)
	if old.ctx.Err() == nil {
		t.Fatal("expected cancelled context")
	}
	old.reply <- answer{result: score(999)}
	h.dispatch()
	active := h.next("search")
	h.reply(active, answer{result: score(50)})
	if h.s.Suggestion == nil || h.s.Suggestion.Score != 50 {
		t.Fatal("active result lost")
	}
}

func TestStopRejectsQueuedSuggestionAndDoesNotRestart(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	h.activate(false)
	s := h.s
	s.Recommend()
	c := h.next("search")
	c.reply <- answer{result: score(200)}
	queued := receive(t, h.ui)
	s.Stop()
	queued()
	if s.Suggestion != nil || s.Busy || len(s.evaluations) != 0 {
		t.Fatal("Stop accepted late result")
	}
	h.noJob()
	s.Recommend()
	h.reply(h.next("search"), answer{result: score(30)})
	if s.Suggestion == nil {
		t.Fatal("explicit retry failed")
	}
}

func TestQueuedResultRejectedAfterBudgetModeOrSetupChange(t *testing.T) {
	for _, change := range []string{"budget", "mode", "setup-same-IDs", "node"} {
		t.Run(change, func(t *testing.T) {
			study := initialStudy()
			child := study.Play(move("a3a4"))
			study.CurrentID = study.RootID
			h := newHarness(t, study, false)
			h.activate(false)
			s := h.s
			s.Recommend()
			c := h.next("search")
			c.reply <- answer{result: score(777)}
			queued := receive(t, h.ui)
			switch change {
			case "budget":
				s.SetBudget(3000)
			case "mode":
				s.SetAI(domain.Black)
			case "setup-same-IDs":
				edited := cloneStudy(s.Study)
				edited.InitialPieces[0].Square = domain.Square{File: 1, Rank: 5}
				if err := s.ApplyEdit(edited); err != nil {
					t.Fatal(err)
				}
			case "node":
				s.Jump(child)
			}
			queued()
			if s.Suggestion != nil || len(s.evaluations) != 0 {
				t.Fatal("invalidated result was applied")
			}
		})
	}
}

func TestThreeModesPauseNavigationAndExplicitRecommendation(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	h.activate(false)
	s := h.s
	s.SetAI(domain.Black)
	h.noJob() // Human red moves first.
	s.Drag(move("a3a4").From, move("a3a4").To)
	h.reply(h.next("inspect"), answer{rules: rules()})
	ai := h.next("search")
	if !reflect.DeepEqual(ai.moves, []string{"a3a4"}) {
		t.Fatal(ai.moves)
	}
	result := score(20)
	result.BestMove = "a6a5"
	h.reply(ai, answer{result: result})
	h.reply(h.next("inspect"), answer{rules: rules()})
	if len(s.Study.Nodes) != 3 {
		t.Fatal("automatic black move missing")
	}
	s.Back()
	if !s.Paused {
		t.Fatal("Back must pause AI")
	}
	h.reply(h.next("inspect"), answer{rules: rules()})
	h.noJob()
	s.ResumeAI()
	retry := h.next("search")
	s.Recommend()
	receive(t, h.f.stops)
	retry.reply <- answer{result: result}
	h.dispatch()
	h.reply(h.next("search"), answer{result: result})
	if !s.Paused || s.Suggestion == nil || len(s.Study.History()) != 1 {
		t.Fatal("recommendation triggered automatic adoption")
	}
	s.SetAI("")
	if s.AISide != "" || s.Paused || s.Suggestion != nil {
		t.Fatal("manual mode not restored")
	}
	s.Root()
	h.reply(h.next("inspect"), answer{rules: rules()})
	s.SetAI(domain.Red)
	h.reply(h.next("search"), answer{result: score(10)})
	h.reply(h.next("inspect"), answer{rules: rules()})
	if s.Study.SideToMove() != domain.Black {
		t.Fatal("AI red did not move")
	}
}

func branchStudy() (domain.Study, string, string, string) {
	s := initialStudy()
	root := s.RootID
	a := s.Play(move("a3a4"))
	deep := s.Play(move("a6a5"))
	s.CurrentID = root
	b := s.Play(move("c3c4"))
	s.CurrentID = root
	return s, a, b, deep
}

func TestForwardUsesPreferenceAndNeverGuessesBranch(t *testing.T) {
	study, a, b, _ := branchStudy()
	h := newHarness(t, study, false)
	s := h.s
	if s.Forward() || s.Study.CurrentID != study.RootID {
		t.Fatal("Forward guessed a branch")
	}
	s.Jump(b)
	s.Back()
	if !s.Forward() || s.Study.CurrentID != b {
		t.Fatal("selected branch not remembered")
	}
	s.Jump(a)
	s.Back()
	if !s.Forward() || s.Study.CurrentID != a {
		t.Fatal("changed preference ignored")
	}
	if !s.Forward() {
		t.Fatal("single child should advance")
	}
}

func TestBranchesInspectImmediateChildrenAndCompareExactScores(t *testing.T) {
	study, a, b, deep := branchStudy()
	h := newHarness(t, study, false)
	s := h.s
	h.activate(true)
	current := h.next("search")
	s.OpenBranches(study.RootID)
	if s.job.targetID != study.RootID {
		t.Fatal("branches preempted current evaluation")
	}
	h.reply(current, answer{result: score(0)})
	first := h.next("inspect")
	if !reflect.DeepEqual(first.moves, []string{"a3a4"}) || first.hints {
		t.Fatal("branch must inspect immediate child without lights")
	}
	h.reply(first, answer{rules: rules()})
	h.reply(h.next("search"), answer{result: score(-120)})
	if s.Comparison(a, study.RootID) != "" {
		t.Fatal("partial group comparison")
	}
	second := h.next("inspect")
	if !reflect.DeepEqual(second.moves, []string{"c3c4"}) {
		t.Fatal("wrong branch history")
	}
	h.reply(second, answer{rules: rules()})
	h.reply(h.next("search"), answer{result: score(-75)})
	h.noJob()
	if s.EvalText(a) != "红 +1.20" || s.EvalText(deep) != "待评估" {
		t.Fatal("wrong perspective or evaluated branch leaf")
	}
	if s.Comparison(a, study.RootID) != "本组较优" || s.Comparison(b, study.RootID) != "较本组较优着差 0.45" {
		t.Fatal("wrong branch comparison")
	}
	if s.Comparison(deep, study.RootID) != "" {
		t.Fatal("compared non-child")
	}
}

func TestCloseBranchesCancelsInspectionAndDropsLateResult(t *testing.T) {
	study, a, _, _ := branchStudy()
	h := newHarness(t, study, false)
	h.activate(false)
	s := h.s
	s.OpenBranches(study.RootID)
	c := h.next("inspect")
	s.CloseBranches()
	receive(t, h.f.stops)
	c.reply <- answer{rules: engine.Snapshot{Finished: true, Winner: "red"}}
	h.dispatch()
	if s.EvalText(a) != "待评估" || s.BranchParentID != "" {
		t.Fatal("closed branch result applied")
	}
	h.noJob()
}

func TestEvaluationSwitchesDoNotCancelActiveSearch(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	h.activate(false)
	s := h.s
	s.Recommend()
	c := h.next("search")
	s.SetShowEvaluation(true)
	s.SetShowEvaluation(false)
	s.SetShowBranchScores(false)
	if c.ctx.Err() != nil {
		t.Fatal("evaluation toggles cancelled recommendation")
	}
	h.reply(c, answer{result: score(20)})
	if s.Suggestion == nil || s.EvalText(s.Study.CurrentID) != "红 +0.20" {
		t.Fatal("both switches off suppressed explicit score")
	}
	h.noJob()
}

func TestFailuresRequireExplicitRetry(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	h.activate(true)
	s := h.s
	root := s.Study.RootID
	h.reply(h.next("search"), answer{err: errors.New("worker unavailable")})
	if s.EvalText(root) != "未获评分" || !strings.Contains(s.EvalDetail(root), "worker unavailable") {
		t.Fatal("failure not exposed")
	}
	s.SetShowEvaluation(false)
	s.SetShowEvaluation(true)
	h.noJob()
	s.Reanalyze()
	h.reply(h.next("search"), answer{result: score(25)})
	if s.EvalText(root) != "红 +0.25" {
		t.Fatal("retry did not replace error")
	}
	s.Recommend()
	h.reply(h.next("search"), answer{result: engine.Result{BestMove: "z0z1"}})
	if s.Error == "" || s.Suggestion != nil {
		t.Fatal("invalid move not explicit failure")
	}
	h.noJob()
	s.Recommend()
	h.reply(h.next("search"), answer{result: engine.Result{BestMove: "a3a4"}})
	if s.Suggestion == nil {
		t.Fatal("legal suggestion without score should remain usable")
	}
}

func TestRuleFailureTerminalAndRetry(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	s := h.s
	s.Activate()
	h.reply(h.next("inspect"), answer{err: errors.New("bad setup")})
	if s.RulesBusy || s.Rules.Error != "bad setup" || s.Error == "" {
		t.Fatal("inspect failure hidden")
	}
	s.SetShowEvaluation(true)
	h.noJob()
	s.Reanalyze()
	h.reply(h.next("inspect"), answer{rules: engine.Snapshot{Finished: true, Winner: "black", Outcome: "困毙"}})
	if s.EvalText(s.Study.RootID) != "黑胜" || s.EvalDetail(s.Study.RootID) != "已结束 · 规则判定" {
		t.Fatal("terminal score incorrect")
	}
	s.Drag(move("a3a4").From, move("a3a4").To)
	s.Recommend()
	h.noJob()
	if len(s.Study.Nodes) != 1 {
		t.Fatal("terminal position was played")
	}
}

func TestPersistenceFailureAndCopyIsolation(t *testing.T) {
	study := initialStudy()
	h := newHarness(t, study, false)
	s := h.s
	study.InitialPieces[0].Square = domain.Square{File: 8, Rank: 4}
	if reflect.DeepEqual(study.InitialPieces, s.Study.InitialPieces) {
		t.Fatal("New shared caller's pieces")
	}
	original := cloneStudy(s.Study)
	s.persist = func(copy domain.Study) error {
		copy.InitialPieces[0].Square = domain.Square{File: 3, Rank: 3}
		return errors.New("disk full")
	}
	edited := cloneStudy(s.Study)
	edited.Name = "edited"
	if s.ApplyEdit(edited) == nil || s.Saved || s.Error == "" || !reflect.DeepEqual(s.Study, original) {
		t.Fatal("failed edit replaced study or hid save error")
	}
	s.Flip()
	if s.Saved || s.Error == "" {
		t.Fatal("Flip save error hidden")
	}
	if !reflect.DeepEqual(s.Study.InitialPieces, original.InitialPieces) {
		t.Fatal("persist callback mutated study")
	}
}

func TestCacheBudgetSetupHistoryAndFlip(t *testing.T) {
	study, a, b, _ := branchStudy()
	h := newHarness(t, study, false)
	h.activate(true)
	s := h.s
	h.reply(h.next("search"), answer{result: score(100)})
	key := s.cacheKey(study.RootID)
	s.Flip()
	if s.cacheKey(study.RootID) != key || s.EvalText(study.RootID) != "红 +1.00" {
		t.Fatal("Flip changed evaluation identity")
	}
	if s.cacheKey(a) == s.cacheKey(b) {
		t.Fatal("different histories share cache")
	}
	s.SetBudget(300)
	if s.EvalText(study.RootID) != "计算中…" {
		t.Fatal("budget retained old cache")
	}
	h.reply(h.next("search"), answer{result: score(200)})
	edited := cloneStudy(s.Study)
	edited.InitialSide = domain.Black
	if err := s.ApplyEdit(edited); err != nil {
		t.Fatal(err)
	}
	if len(s.evaluations) != 0 {
		t.Fatal("setup with same root ID retained cache")
	}
	h.reply(h.next("inspect"), answer{rules: rules()})
	h.reply(h.next("search"), answer{result: score(50)})
	if s.EvalText(study.RootID) != "红 -0.50" {
		t.Fatal("new setup perspective not used")
	}
}

func TestLightsRefreshIsAsyncAndFlipKeepsBothSides(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	s := h.s
	s.SetShowEvaluation(false)
	s.Activate()
	snapshot := rules()
	snapshot.Captures = []engine.Capture{{Move: "a3a4", Side: "red"}, {Move: "a6a5", Side: "black"}}
	h.reply(h.next("inspect"), answer{rules: snapshot})
	s.Flip()
	if len(s.Rules.Captures) != 2 {
		t.Fatal("Flip must keep both sides for UI observer filtering")
	}
	s.SetShowLights(false)
	if !s.RulesBusy || len(s.Rules.Captures) != 0 {
		t.Fatal("lights off must clear stale hints immediately")
	}
	c := h.next("inspect")
	if c.hints {
		t.Fatal("disabled hints sent")
	}
	h.reply(c, answer{rules: rules()})
	h.noJob()
}

func TestBlackBranchComparisonAndIncompleteKinds(t *testing.T) {
	study, a, b, _ := branchStudy()
	study.InitialSide = domain.Black
	s := New(study, nil, nil)
	put := func(id string, value engine.Result) {
		v, ok := scored(value, domain.Red, s.Budget, s.cacheKey(id))
		if !ok {
			t.Fatal("invalid fixture")
		}
		s.evaluations[id] = v
	}
	put(a, score(125))
	put(b, score(-50))
	if s.Comparison(b, study.RootID) != "本组较优" || s.Comparison(a, study.RootID) != "较本组较优着差 1.75" {
		t.Fatal("black must minimize red score")
	}
	for _, bound := range []string{"lowerbound", "upperbound"} {
		r := score(10)
		r.Bound = bound
		put(a, r)
		if s.Comparison(b, study.RootID) != "" {
			t.Fatal("bound participated in difference")
		}
	}
	r := score(5)
	r.Mate = true
	put(a, r)
	if s.Comparison(b, study.RootID) != "" {
		t.Fatal("mate participated in difference")
	}
	s.terminal(a, engine.Snapshot{Finished: true})
	if s.Comparison(b, study.RootID) != "" {
		t.Fatal("terminal participated in difference")
	}
	delete(s.evaluations, a)
	if s.Comparison(b, study.RootID) != "" {
		t.Fatal("missing score participated in difference")
	}
}
